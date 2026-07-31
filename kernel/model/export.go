// SiYuan - Refactor your thinking
// Copyright (c) 2020-present, b3log.org
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package model

import (
	"bytes"
	"crypto/sha1"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/88250/go-humanize"
	"github.com/88250/gulu"
	"github.com/88250/lute/ast"
	"github.com/88250/lute/editor"
	"github.com/88250/lute/html"
	"github.com/88250/lute/lex"
	"github.com/88250/lute/parse"
	"github.com/88250/lute/render"
	"github.com/emirpasic/gods/sets/hashset"
	"github.com/emirpasic/gods/stacks/linkedliststack"
	"github.com/imroc/req/v3"
	shellquote "github.com/kballard/go-shellquote"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/font"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/httpclient"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/riff"
	"github.com/siyuan-note/siyuan/kernel/av"
	"github.com/siyuan-note/siyuan/kernel/filesys"
	"github.com/siyuan-note/siyuan/kernel/sql"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func ExportCodeBlock(blockID string) (filePath string, err error) {
	// Supports exporting a code block as a file https://github.com/siyuan-note/siyuan/pull/16774

	err = withExportReadLockByBlockID(blockID, func() error {
		tree, _ := LoadTreeByBlockID(blockID)
		if nil == tree {
			return ErrBlockNotFound
		}

		node := treenode.GetNodeInTree(tree, blockID)
		if nil == node {
			return ErrBlockNotFound
		}

		if ast.NodeCodeBlock != node.Type {
			return errors.New("not a code block")
		}

		code := node.ChildByType(ast.NodeCodeBlockCode)
		if nil == code {
			return errors.New("code block has no code node")
		}

		name := tree.Root.IALAttr("title") + "-" + util.CurrentTimeSecondsStr() + ".txt"
		name = util.FilterFileName(name)
		exportFolder := filepath.Join(util.TempDir, "export")
		// Exports from encrypted notebooks go under a boxID subdirectory, so LockBox cleanup and server-side lock
		// verification work correctly
		if IsEncryptedBox(tree.Box) {
			exportFolder = filepath.Join(exportFolder, tree.Box)
		}
		exportFolder = filepath.Join(exportFolder, "code")
		if mkdirErr := os.MkdirAll(exportFolder, 0755); mkdirErr != nil {
			return mkdirErr
		}

		code.Tokens = bytes.ReplaceAll(code.Tokens, []byte(editor.Zwj+"```"), []byte("```"))

		writePath := filepath.Join(exportFolder, name)
		if writeErr := filelock.WriteFile(writePath, code.Tokens); writeErr != nil {
			return writeErr
		}

		// Exports from encrypted notebooks must register a managed token, otherwise the server-side guard rejects
		// the download
		if IsEncryptedBox(tree.Box) {
			filePath = "/export/" + registerManagedEncryptedExport(tree.Box, "code", writePath)
		} else {
			filePath = "/export/code/" + url.PathEscape(name)
		}
		return nil
	})
	return
}

func ExportAv2CSV(avID, blockID string) (zipPath string, err error) {
	// Database block supports export as CSV https://github.com/siyuan-note/siyuan/issues/10072

	err = withExportReadLockByBlockID(blockID, func() error {
		avBoxID := ""
		if bt := treenode.GetBlockTree(blockID); nil != bt && IsEncryptedBox(bt.BoxID) {
			avBoxID = bt.BoxID
			av.SetAVBoxID(avID, avBoxID)
		}

		var attrView *av.AttributeView
		if avBoxID != "" {
			attrView, err = av.ParseAttributeViewInBox(avID, avBoxID)
		} else {
			attrView, err = av.ParseAttributeView(avID)
		}
		if err != nil {
			return err
		}

		node, _, nodeErr := getNodeByBlockID(nil, blockID)
		if nil == node {
			return nodeErr
		}
		viewID := node.IALAttr(av.NodeAttrView)
		view, viewErr := attrView.GetCurrentView(viewID)
		if viewErr != nil {
			return viewErr
		}

		name := util.FilterFileName(getAttrViewName(attrView))
		table := getAttrViewTable(attrView, view, "")

		// Follow the view's filter and sort rules. Use filtering and sorting of current view settings when exporting database blocks https://github.com/siyuan-note/siyuan/issues/10474
		cachedAttrViews := map[string]*av.AttributeView{}
		rollupFurtherCollections := sql.GetFurtherCollections(attrView, cachedAttrViews)
		av.Filter(table, attrView, rollupFurtherCollections, cachedAttrViews)
		av.Sort(table, attrView)

		exportFolder := filepath.Join(util.TempDir, "export")
		// Exports from encrypted notebooks go under a boxID subdirectory, so LockBox cleanup and server-side lock
		// verification work correctly
		if avBoxID != "" {
			exportFolder = filepath.Join(exportFolder, avBoxID)
		}
		exportFolder = filepath.Join(exportFolder, "csv", name)
		if mkdirErr := os.MkdirAll(exportFolder, 0755); mkdirErr != nil {
			return mkdirErr
		}
		csvPath := filepath.Join(exportFolder, name+".csv")

		f, openErr := os.OpenFile(csvPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
		if openErr != nil {
			return openErr
		}

		if _, err = f.WriteString("\xEF\xBB\xBF"); err != nil { // Write a UTF-8 BOM to avoid garbled text when opening with Microsoft Excel
			f.Close()
			return err
		}

		writer := csv.NewWriter(f)
		var header []string
		for _, col := range table.Columns {
			header = append(header, col.Name)
		}
		if err = writer.Write(header); err != nil {
			f.Close()
			return err
		}

		var assets []string
		rowNum := 1
		for _, row := range table.Rows {
			var rowVal []string
			for _, cell := range row.Cells {
				var val string
				if nil != cell.Value {
					if av.KeyTypeDate == cell.Value.Type {
						if nil != cell.Value.Date {
							cell.Value.Date = av.NewFormattedValueDate(cell.Value.Date.Content, cell.Value.Date.Content2, av.DateFormatNone, cell.Value.Date.IsNotTime, cell.Value.Date.HasEndDate)
						}
					} else if av.KeyTypeCreated == cell.Value.Type {
						if nil != cell.Value.Created {
							key, _ := attrView.GetKey(cell.Value.KeyID)
							isNotTime := false
							if nil != key && nil != key.Created {
								isNotTime = !key.Created.IncludeTime
							}

							cell.Value.Created = av.NewFormattedValueCreated(cell.Value.Created.Content, 0, av.CreatedFormatNone, isNotTime)
						}
					} else if av.KeyTypeUpdated == cell.Value.Type {
						if nil != cell.Value.Updated {
							key, _ := attrView.GetKey(cell.Value.KeyID)
							isNotTime := false
							if nil != key && nil != key.Updated {
								isNotTime = !key.Updated.IncludeTime
							}

							cell.Value.Updated = av.NewFormattedValueUpdated(cell.Value.Updated.Content, 0, av.UpdatedFormatNone, isNotTime)
						}
					} else if av.KeyTypeMAsset == cell.Value.Type {
						if nil != cell.Value.MAsset {
							buf := &bytes.Buffer{}
							for _, a := range cell.Value.MAsset {
								if av.AssetTypeImage == a.Type {
									buf.WriteString("![")
									buf.WriteString(a.Name)
									buf.WriteString("](")
									buf.WriteString(a.Content)
									buf.WriteString(") ")
									if util.IsAssetLinkDest([]byte(a.Content), true) {
										assets = append(assets, a.Content)
									}
								} else if av.AssetTypeFile == a.Type {
									buf.WriteString("[")
									buf.WriteString(a.Name)
									buf.WriteString("](")
									buf.WriteString(a.Content)
									buf.WriteString(") ")
									if util.IsAssetLinkDest([]byte(a.Content), true) {
										assets = append(assets, a.Content)
									}
								} else {
									buf.WriteString(a.Content)
									buf.WriteString(" ")
								}
							}
							val = strings.TrimSpace(buf.String())
						}
					} else if av.KeyTypeLineNumber == cell.Value.Type {
						val = strconv.Itoa(rowNum)
					} else if av.KeyTypeRollup == cell.Value.Type {
						for _, content := range cell.Value.Rollup.Contents {
							if av.KeyTypeMAsset == content.Type {
								buf := &bytes.Buffer{}
								for _, a := range content.MAsset {
									if av.AssetTypeImage == a.Type {
										buf.WriteString("![")
										buf.WriteString(a.Name)
										buf.WriteString("](")
										buf.WriteString(a.Content)
										buf.WriteString(") ")
										if util.IsAssetLinkDest([]byte(a.Content), true) {
											assets = append(assets, a.Content)
										}
									} else if av.AssetTypeFile == a.Type {
										buf.WriteString("[")
										buf.WriteString(a.Name)
										buf.WriteString("](")
										buf.WriteString(a.Content)
										buf.WriteString(") ")
										if util.IsAssetLinkDest([]byte(a.Content), true) {
											assets = append(assets, a.Content)
										}
									} else {
										buf.WriteString(a.Content)
										buf.WriteString(" ")
									}
								}
								val = strings.TrimSpace(buf.String())
							}
						}
					}

					if "" == val {
						val = cell.Value.String(true)
					}
				}

				rowVal = append(rowVal, val)
			}
			if err = writer.Write(rowVal); err != nil {
				logging.LogErrorf("write csv row [%s] failed: %s", rowVal, err)
				f.Close()
				return err
			}
			rowNum++
		}
		writer.Flush()

		for _, asset := range assets {
			srcAbsPath, getErr := GetAssetAbsPathInBox(asset, avBoxID)
			if getErr != nil {
				logging.LogWarnf("resolve path of asset [%s] failed: %s", asset, getErr)
				continue
			}
			targetAbsPath := filepath.Join(exportFolder, AssetPathWithoutQuery(asset))
			if copyErr := copyAssetDecryptIfEncrypted(srcAbsPath, targetAbsPath); copyErr != nil {
				logging.LogWarnf("copy asset from [%s] to [%s] failed: %s", srcAbsPath, targetAbsPath, copyErr)
			}
		}

		absZipPath := exportFolder + ".db.zip"
		zip, createErr := gulu.Zip.Create(absZipPath)
		if createErr != nil {
			f.Close()
			return createErr
		}

		if err = zip.AddDirectory("", exportFolder); err != nil {
			f.Close()
			return err
		}

		if err = zip.Close(); err != nil {
			f.Close()
			return err
		}

		f.Close()
		os.RemoveAll(exportFolder)

		// Exports from encrypted notebooks must register a managed token, otherwise the server-side guard rejects
		// the download
		if avBoxID != "" {
			zipPath = "/export/" + registerManagedEncryptedExport(avBoxID, "csv", absZipPath)
		} else {
			zipPath = "/export/csv/" + url.PathEscape(filepath.Base(absZipPath))
		}
		return nil
	})
	return
}

func Export2Liandi(id string) (err error) {
	err = withExportReadLockByBlockID(id, func() error {
		tree, loadErr := LoadTreeByBlockID(id)
		if loadErr != nil {
			logging.LogErrorf("load tree by block id [%s] failed: %s", id, loadErr)
			return loadErr
		}

		if IsUserGuide(tree.Box) {
			// Doc in the user guide no longer supports one-click sending to the community https://github.com/siyuan-note/siyuan/issues/8388
			return errors.New(Conf.Language(204))
		}

		assets := getAssetsLinkDests(tree.Root, false)
		embedAssets := getQueryEmbedNodesAssetsLinkDests(tree.Root)
		assets = append(assets, embedAssets...)
		assets = gulu.Str.RemoveDuplicatedElem(assets)
		_, err = uploadAssets2Cloud(assets, bizTypeExport2Liandi, false)
		if err != nil {
			return err
		}

		msgId := util.PushMsg(Conf.Language(182), 15000)
		defer util.PushClearMsg(msgId)

		// Check whether the post already exists; if so, use the update API
		const liandiArticleIdAttrName = "custom-liandi-articleid"
		const liandiArticleIdAttrNameOld = "custom-liandi-articleId" // for compatibility with the old attribute name
		foundArticle := false
		// Prefer the new attribute name, and fall back to the old one if it doesn't exist
		articleId := tree.Root.IALAttr(liandiArticleIdAttrName)
		if "" == articleId {
			articleId = tree.Root.IALAttr(liandiArticleIdAttrNameOld)
		}
		if "" != articleId {
			result := gulu.Ret.NewResult()
			request := httpclient.NewCloudRequest30s()
			resp, getErr := request.
				SetSuccessResult(result).
				SetCookies(&http.Cookie{Name: "symphony", Value: Conf.GetUser().UserToken}).
				Get(util.GetCloudAccountServer() + "/api/v2/article/update/" + articleId)
			if nil != getErr {
				logging.LogErrorf("get liandi article info failed: %s", getErr)
				return getErr
			}

			switch resp.StatusCode {
			case 200:
				if 0 == result.Code {
					foundArticle = true
				} else if 1 == result.Code {
					foundArticle = false
				}
			case 404:
				foundArticle = false
			default:
				return fmt.Errorf("get liandi article info failed [sc=%d]", resp.StatusCode)
			}
		}

		apiURL := util.GetCloudAccountServer() + "/api/v2/article"
		if foundArticle {
			apiURL += "/" + articleId
		}

		title := path.Base(tree.HPath)
		tags := tree.Root.IALAttr("tags")
		content := exportMarkdownContent0(id, tree, util.GetCloudForumAssetsServer()+time.Now().Format("2006/01")+"/siyuan/"+Conf.GetUser().UserId+"/",
			true, false, false,
			".md", 3, 1, 1,
			"#", "#",
			"", "",
			false, false, nil, true, false)
		result := gulu.Ret.NewResult()
		request := httpclient.NewCloudRequest30s()
		request = request.
			SetSuccessResult(result).
			SetCookies(&http.Cookie{Name: "symphony", Value: Conf.GetUser().UserToken}).
			SetBody(map[string]any{
				"articleTitle":   title,
				"articleTags":    tags,
				"articleContent": content})
		var resp *req.Response
		var sendErr error
		if foundArticle {
			resp, sendErr = request.Put(apiURL)
		} else {
			resp, sendErr = request.Post(apiURL)
		}
		if nil != sendErr {
			logging.LogErrorf("send article to liandi failed: %s", sendErr)
			return sendErr
		}
		if 200 != resp.StatusCode {
			msg := fmt.Sprintf("send article to liandi failed [sc=%d]", resp.StatusCode)
			logging.LogError(msg)
			return errors.New(msg)
		}

		if 0 != result.Code {
			msg := fmt.Sprintf("send article to liandi failed [code=%d, msg=%s]", result.Code, result.Msg)
			logging.LogError(msg)
			util.PushClearMsg(msgId)
			return errors.New(result.Msg)
		}

		if !foundArticle {
			articleId = result.Data.(string)
			tree, _ = LoadTreeByBlockID(id) // must reload here since the earlier export step already modified the tree structure
			tree.Root.SetIALAttr(liandiArticleIdAttrName, articleId)
			if err = writeTreeUpsertQueue(tree); err != nil {
				return err
			}
		}

		util.PushMsg(fmt.Sprintf(Conf.Language(181), util.GetCloudAccountServer()+"/article/"+articleId), 7000)
		return nil
	})
	return
}

func ExportSystemLog() (zipPath string) {
	exportFolder := filepath.Join(util.TempDir, "export", "system-log")
	os.RemoveAll(exportFolder)
	if err := os.MkdirAll(exportFolder, 0755); err != nil {
		logging.LogErrorf("create export temp folder failed: %s", err)
		return
	}

	appLog := filepath.Join(util.HomeDir, ".config", "siyuan", "app.log")
	if gulu.File.IsExist(appLog) {
		to := filepath.Join(exportFolder, "app.log")
		if err := filelock.Copy(appLog, to); err != nil {
			logging.LogErrorf("copy app log from [%s] to [%s] failed: %s", err, appLog, to)
		}
	}

	kernelLog := filepath.Join(util.HomeDir, ".config", "siyuan", "kernel.log")
	if gulu.File.IsExist(kernelLog) {
		to := filepath.Join(exportFolder, "kernel.log")
		if err := filelock.Copy(kernelLog, to); err != nil {
			logging.LogErrorf("copy kernel log from [%s] to [%s] failed: %s", err, kernelLog, to)
		}
	}

	siyuanLog := filepath.Join(util.TempDir, "siyuan.log")
	if gulu.File.IsExist(siyuanLog) {
		to := filepath.Join(exportFolder, "siyuan.log")
		if err := filelock.Copy(siyuanLog, to); err != nil {
			logging.LogErrorf("copy kernel log from [%s] to [%s] failed: %s", err, siyuanLog, to)
		}
	}

	mobileLog := filepath.Join(util.TempDir, "mobile.log")
	if gulu.File.IsExist(mobileLog) {
		to := filepath.Join(exportFolder, "mobile.log")
		if err := filelock.Copy(mobileLog, to); err != nil {
			logging.LogErrorf("copy mobile log from [%s] to [%s] failed: %s", err, mobileLog, to)
		}
	}

	zipPath = exportFolder + ".zip"
	zip, err := gulu.Zip.Create(zipPath)
	if err != nil {
		logging.LogErrorf("create export log zip [%s] failed: %s", exportFolder, err)
		return ""
	}

	if err = zip.AddDirectory("log", exportFolder); err != nil {
		logging.LogErrorf("create export log zip [%s] failed: %s", exportFolder, err)
		return ""
	}

	if err = zip.Close(); err != nil {
		logging.LogErrorf("close export log zip failed: %s", err)
	}

	os.RemoveAll(exportFolder)
	zipPath = "/export/" + url.PathEscape(filepath.Base(zipPath))
	return
}

// exportLockedByBlockID looks up the boxID from a docID/blockID and checks whether the owning encrypted notebook
// is still locked.
// Returns true if locked (the caller should abort the export and return an empty result); returns false for a
// regular notebook or an already-unlocked one.
// Used as a unified guard at document-level export entry points, to avoid reading out ciphertext or an empty
// result while locked.
func exportLockedByBlockID(id string) bool {
	bt := treenode.GetBlockTree(id)
	if nil == bt {
		return false // block tree not found, let the subsequent flow handle it
	}
	return IsEncryptedBox(bt.BoxID) && !IsBoxUnlocked(bt.BoxID)
}

// withExportReadLockByBlockID looks up the boxID from a blockID, and if it belongs to an encrypted notebook, holds
// the read lock for the entire duration of fn.
// While the lock is held, LockBox (auto-lock) blocks and waits, preventing the DEK from being cleared or the
// export directory from being deleted mid-operation, which could otherwise leak partial plaintext.
// For a regular notebook or a missing block tree, fn is executed directly. Safe for nested calls:
// sync.RWMutex.RLock is reentrant.
func withExportReadLockByBlockID(id string, fn func() error) error {
	bt := treenode.GetBlockTree(id)
	if nil == bt || !IsEncryptedBox(bt.BoxID) {
		return fn()
	}
	if !IsBoxUnlocked(bt.BoxID) {
		return errors.New(Conf.Language(314))
	}
	HoldBoxReadLock(bt.BoxID)
	defer ReleaseBoxReadLock(bt.BoxID)
	if _, dekErr := GetDEKIfUnlocked(bt.BoxID); dekErr != nil {
		return errors.New(Conf.Language(314))
	}
	return fn()
}

func ExportNotebookSY(id string) (zipPath string) {
	// An encrypted notebook must already be unlocked to be exported (the DEK needs to be in memory to read
	// the plaintext .sy/assets/AV data)
	if IsEncryptedBox(id) && !IsBoxUnlocked(id) {
		logging.LogErrorf("export encrypted notebook [%s] failed: locked", id)
		return
	}
	zipPath = exportBoxSYZip(id)
	return
}

func ExportSYs(ids []string) (zipPath string) {
	block := treenode.GetBlockTree(ids[0])
	if nil != block && IsEncryptedBox(block.BoxID) && !IsBoxUnlocked(block.BoxID) {
		logging.LogErrorf("export encrypted doc [%s] failed: box [%s] locked", ids[0], block.BoxID)
		return
	}
	box := Conf.Box(block.BoxID)
	baseFolderName := path.Base(block.HPath)
	if "." == baseFolderName {
		baseFolderName = path.Base(block.Path)
	}

	var docPaths []string
	bts := treenode.GetBlockTrees(ids)
	for _, bt := range bts {
		docPaths = append(docPaths, bt.Path)

		if Conf.Export.IncludeSubDocs {
			listPath := strings.TrimSuffix(bt.Path, ".sy")
			if IsBoxDoc(bt.BoxID, bt.RootID) {
				listPath = "/"
			}
			docFiles := box.ListFiles(listPath)
			for _, docFile := range docFiles {
				if docFile.path == bt.Path {
					continue
				}
				docPaths = append(docPaths, docFile.path)
			}
		}
	}
	zipPath = exportSYZip(block.BoxID, path.Dir(block.Path), baseFolderName, docPaths, false)
	return
}

func ExportDataInFolder(exportFolder string) (name string, err error) {
	util.PushEndlessProgress(Conf.Language(65))
	defer util.ClearPushProgress(100)

	data := filepath.Join(util.WorkspaceDir, "data")
	if util.ContainerStd == util.Container {
		// Check available disk space on the desktop client

		dataSize, sizeErr := util.SizeOfDirectory(data)
		if sizeErr != nil {
			logging.LogErrorf("get size of data dir [%s] failed: %s", data, sizeErr)
			err = sizeErr
			return
		}

		_, _, tempExportFree := util.GetDiskUsage(util.TempDir)
		if int64(tempExportFree) < dataSize*2 { // compressing the zip file needs twice the space of the data dir
			err = fmt.Errorf(Conf.Language(242), humanize.BytesCustomCeil(tempExportFree, 2), humanize.BytesCustomCeil(uint64(dataSize)*2, 2))
			return
		}

		_, _, targetExportFree := util.GetDiskUsage(exportFolder)
		if int64(targetExportFree) < dataSize { // copying the zip needs at most the same space as the data dir
			err = fmt.Errorf(Conf.Language(242), humanize.BytesCustomCeil(targetExportFree, 2), humanize.BytesCustomCeil(uint64(dataSize), 2))
			return
		}
	}

	zipPath, err := ExportData()
	if err != nil {
		return
	}
	name = filepath.Base(zipPath)
	name, err = url.PathUnescape(name)
	if err != nil {
		logging.LogErrorf("url unescape [%s] failed: %s", name, err)
		return
	}

	util.PushEndlessProgress(Conf.Language(65))
	defer util.ClearPushProgress(100)

	targetZipPath := filepath.Join(exportFolder, name)
	zipAbsPath := filepath.Join(util.TempDir, "export", name)
	err = filelock.Copy(zipAbsPath, targetZipPath)
	if err != nil {
		logging.LogErrorf("copy export zip from [%s] to [%s] failed: %s", zipAbsPath, targetZipPath, err)
		return
	}
	if removeErr := os.Remove(zipAbsPath); nil != removeErr {
		logging.LogErrorf("remove export zip failed: %s", removeErr)
	}
	return
}

func ExportData() (zipPath string, err error) {
	util.PushEndlessProgress(Conf.Language(65))
	defer util.ClearPushProgress(100)

	name := util.FilterFileName(util.WorkspaceName) + "-" + util.CurrentTimeSecondsStr()
	exportFolder := filepath.Join(util.TempDir, "export", name)
	zipPath, err = exportData(exportFolder)
	if err != nil {
		return
	}
	zipPath = "/export/" + url.PathEscape(filepath.Base(zipPath))
	return
}

func exportData(exportFolder string) (zipPath string, err error) {
	FlushTxQueue()

	logging.LogInfof("exporting data...")

	baseFolderName := "data-" + util.CurrentTimeSecondsStr()
	if err = os.MkdirAll(exportFolder, 0755); err != nil {
		logging.LogErrorf("create export temp folder failed: %s", err)
		return
	}

	data := filepath.Join(util.WorkspaceDir, "data")
	if err = filelock.Copy(data, exportFolder); err != nil {
		logging.LogErrorf("copy data dir from [%s] to [%s] failed: %s", data, baseFolderName, err)
		err = fmt.Errorf(Conf.Language(14), err.Error())
		return
	}

	zipPath = exportFolder + ".zip"
	zip, err := gulu.Zip.Create(zipPath)
	if err != nil {
		logging.LogErrorf("create export data zip [%s] failed: %s", exportFolder, err)
		return
	}

	zipCallback := func(filename string) {
		util.PushEndlessProgress(Conf.language(65) + " " + fmt.Sprintf(Conf.language(253), filename))
	}

	if err = zip.AddDirectory(baseFolderName, exportFolder, zipCallback); err != nil {
		logging.LogErrorf("create export data zip [%s] failed: %s", exportFolder, err)
		return
	}

	if err = zip.Close(); err != nil {
		logging.LogErrorf("close export data zip failed: %s", err)
	}

	os.RemoveAll(exportFolder)
	logging.LogInfof("export data done [%s]", zipPath)
	return
}

func ExportResources(resourcePaths []string, mainName string) (exportFilePath string, err error) {
	FlushTxQueue()

	encryptedBoxID, detectErr := exportResourcesEncryptedBox(resourcePaths)
	if detectErr != nil {
		return "", detectErr
	}

	exportBasePath := filepath.Join(util.TempDir, "export")
	if encryptedBoxID != "" {
		// Hold the read lock for the entire duration of an encrypted resource export. LockBox will wait for the
		// export to finish before deleting that box's export directory, preventing plaintext from being written
		// back after locking.
		HoldBoxReadLock(encryptedBoxID)
		defer ReleaseBoxReadLock(encryptedBoxID)
		if _, dekErr := GetDEKIfUnlocked(encryptedBoxID); dekErr != nil {
			return "", errors.New(Conf.Language(314))
		}
		exportBasePath = filepath.Join(exportBasePath, encryptedBoxID, "resources")
	}

	// The physical export directory for an encrypted notebook uses a random identifier; mainName is only used
	// for the user-visible archive name and the top-level directory inside the archive.
	exportID, err := newManagedEncryptedExportID()
	if err != nil {
		return "", err
	}
	exportFolderPath := filepath.Join(exportBasePath, exportID)
	zipBaseName := util.FilterFileName(filepath.Base(mainName))
	if zipBaseName == "" || zipBaseName == "." || zipBaseName == ".." {
		zipBaseName = "resources"
	}
	zipFileName := zipBaseName + ".zip"
	// Both regular and encrypted exports use a random exportID as the physical directory; zipBaseName is only
	// used for the top-level directory name inside the ZIP and the downloaded file name
	zipFilePath := filepath.Join(exportBasePath, exportID+"-"+zipFileName)
	if err = os.MkdirAll(exportFolderPath, 0755); err != nil {
		logging.LogErrorf("create export temp folder failed: %s", err)
		return
	}
	defer func() {
		os.RemoveAll(exportFolderPath)
		if err != nil {
			os.Remove(zipFilePath)
			os.Remove(zipFilePath + ".partial")
		}
	}()

	// Copy the files/folders to be exported into the temp folder
	for _, resourcePath := range resourcePaths {
		resourceFullPath := filepath.Join(util.WorkspaceDir, resourcePath) // full path of the resource
		if !util.IsAbsPathInWorkspace(resourceFullPath) {
			logging.LogErrorf("resource path [%s] is not in workspace", resourceFullPath)
			err = errors.New("resource path [" + resourcePath + "] is not in workspace")
			return
		}

		resourceBaseName := filepath.Base(resourceFullPath)                   // resource name
		resourceCopyPath := filepath.Join(exportFolderPath, resourceBaseName) // full path of the resource copy
		if err = copyExportResource(resourceFullPath, resourceCopyPath); err != nil {
			logging.LogErrorf("copy resource will be exported from [%s] to [%s] failed: %s", resourcePath, resourceCopyPath, err)
			err = fmt.Errorf(Conf.Language(14), err.Error())
			return
		}
	}

	zipPartialPath := zipFilePath + ".partial"
	zip, err := gulu.Zip.Create(zipPartialPath)
	if err != nil {
		logging.LogErrorf("create export zip [%s] failed: %s", zipFilePath, err)
		return
	}
	zipClosed := false
	defer func() {
		if !zipClosed {
			_ = zip.Close()
		}
	}()

	if err = zip.AddDirectory(zipBaseName, exportFolderPath); err != nil {
		logging.LogErrorf("create export zip [%s] failed: %s", exportFolderPath, err)
		return
	}

	err = zip.Close()
	zipClosed = true
	if err != nil {
		logging.LogErrorf("close export zip failed: %s", err)
		return
	}
	if err = os.Rename(zipPartialPath, zipFilePath); err != nil {
		logging.LogErrorf("publish export zip [%s] failed: %s", zipFilePath, err)
		return
	}

	exportFilePath = path.Join("temp", "export")
	if encryptedBoxID != "" {
		exportFilePath = path.Join("temp", "export", registerManagedEncryptedExport(encryptedBoxID, "resources", zipFilePath))
	} else {
		exportFilePath = path.Join(exportFilePath, filepath.Base(zipFilePath))
	}
	return
}

// copyExportResource copies an export resource, processing directories file by file to avoid reading encrypted
// resources as plain files.
func copyExportResource(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("exporting symbolic links is not supported")
	}
	if !info.IsDir() {
		return copyExportFile(source, destination)
	}

	return filepath.WalkDir(source, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("exporting symbolic links is not supported")
		}
		relativePath, relErr := filepath.Rel(source, current)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(destination, relativePath)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if !entry.Type().IsRegular() {
			return errors.New("exporting special files is not supported")
		}
		return copyExportFile(current, target)
	})
}

// copyExportFile copies a single export file, restoring the user-visible name when a name mapping exists for
// an encrypted resource.
func copyExportFile(source, destination string) error {
	boxID := ExtractBoxIDFromAssetsPath(source)
	if boxID != "" && IsEncryptedBox(boxID) {
		diskName := filepath.Base(source)
		if diskName == ".names.json" {
			return nil
		}
		if originalName := LookupAssetOriginalName(boxID, diskName); originalName != "" {
			fileName := util.FilterFileName(filepath.Base(originalName))
			if fileName != "" && fileName != "." {
				destination = uniqueExportFilePath(filepath.Join(filepath.Dir(destination), fileName))
			}
		}
	}
	return copyAssetDecryptIfEncrypted(source, destination)
}

// uniqueExportFilePath generates a stable numeric suffix for files with the same name within the same export
// directory.
func uniqueExportFilePath(destination string) string {
	if _, err := os.Lstat(destination); err != nil {
		return destination
	}
	extension := filepath.Ext(destination)
	base := strings.TrimSuffix(destination, extension)
	for index := 2; ; index++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, index, extension)
		if _, err := os.Lstat(candidate); err != nil {
			return candidate
		}
	}
}

// exportResourcesEncryptedBox validates that a resource export does not cross an encryption boundary, and
// returns the single allowed encrypted source boxID.
func exportResourcesEncryptedBox(resourcePaths []string) (encryptedBoxID string, err error) {
	hasNormalResource := false
	for _, resourcePath := range resourcePaths {
		resourceFullPath := filepath.Join(util.WorkspaceDir, resourcePath)
		if !util.IsAbsPathInWorkspace(resourceFullPath) {
			return "", errors.New("resource path [" + resourcePath + "] is not in workspace")
		}
		boxID := ExtractBoxIDFromAssetsPath(resourceFullPath)
		if boxID == "" || !IsEncryptedBox(boxID) {
			hasNormalResource = true
			continue
		}

		assetsPath := filepath.Join(util.DataDir, boxID, "assets")
		if !gulu.File.IsSubPath(assetsPath, resourceFullPath) {
			return "", errors.New("exporting non-asset files from encrypted notebooks is not supported")
		}
		if encryptedBoxID == "" {
			encryptedBoxID = boxID
		} else if encryptedBoxID != boxID {
			return "", errors.New("exporting resources across encrypted notebook boundaries is not supported")
		}
	}
	if encryptedBoxID != "" && hasNormalResource {
		return "", errors.New("exporting encrypted and normal notebook resources together is not supported")
	}
	return
}

func ExportPreview(id string, fillCSSVar bool) (retStdHTML string) {
	if exportErr := withExportReadLockByBlockID(id, func() error {
		blockRefMode := Conf.Export.BlockRefMode
		bt := treenode.GetBlockTree(id)
		if nil == bt {
			return nil
		}

		tree := prepareExportTree(bt)
		tree = exportTree(tree, false, false, true,
			blockRefMode, Conf.Export.BlockEmbedMode, Conf.Export.FileAnnotationRefMode,
			"#", "#", // always use # to wrap tags here, otherwise tags cannot be parsed correctly https://github.com/siyuan-note/siyuan/issues/13857
			Conf.Export.BlockRefTextLeft, Conf.Export.BlockRefTextRight,
			Conf.Export.AddTitle, Conf.Export.InlineMemo, true, true)
		luteEngine := NewLute()
		enableLuteInlineSyntax(luteEngine)
		luteEngine.SetFootnotes(true)
		addBlockIALNodes(tree, false)

		adjustHeadingLevel(bt, tree)

		// Remove the attribute list of super blocks https://github.com/siyuan-note/siyuan/issues/13451
		var unlinks []*ast.Node
		ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
			if entering && ast.NodeKramdownBlockIAL == n.Type && nil != n.Previous && ast.NodeSuperBlock == n.Previous.Type {
				unlinks = append(unlinks, n)
			}
			return ast.WalkContinue
		})
		for _, unlink := range unlinks {
			unlink.Unlink()
		}

		ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
			if !entering {
				return ast.WalkContinue
			}

			if ast.NodeFootnotesRef == n.Type && nil != n.Next {
				// https://github.com/siyuan-note/siyuan/issues/15654
				nextText := n.NextNodeText()
				if strings.HasPrefix(nextText, "(") && strings.HasSuffix(nextText, ")") {
					n.InsertAfter(&ast.Node{Type: ast.NodeText, Tokens: []byte(editor.Zwsp)})
				}
			}
			return ast.WalkContinue
		})

		md := treenode.FormatNode(tree.Root, luteEngine)
		tree = parse.Parse("", []byte(md), luteEngine.ParseOptions)
		// Replace style variables with the actual theme style values. Use real theme style value replace var in preview mode https://github.com/siyuan-note/siyuan/issues/11458
		if fillCSSVar {
			fillThemeStyleVar(tree)
		}
		luteEngine.RenderOptions.ProtyleMarkNetImg = false
		retStdHTML = luteEngine.ProtylePreview(tree, luteEngine.RenderOptions, luteEngine.ParseOptions)

		if footnotesDefBlock := tree.Root.ChildByType(ast.NodeFootnotesDefBlock); nil != footnotesDefBlock {
			footnotesDefBlock.Unlink()
		}
		return nil
	}); exportErr != nil {
		logging.LogErrorf("export preview [%s] failed: %s", id, exportErr)
		return
	}
	return
}

func ExportDocx(id, savePath string, removeAssets, merge bool) (fullPath string, err error) {
	err = withExportReadLockByBlockID(id, func() error {
		if !util.IsValidPandocBin(Conf.Export.PandocBin) {
			Conf.Export.PandocBin = util.PandocBinPath
			Conf.Save()
			if !util.IsValidPandocBin(Conf.Export.PandocBin) {
				return errors.New(Conf.Language(115))
			}
		}

		tmpDir := filepath.Join(util.TempDir, "export", gulu.Rand.String(7))
		if bt := treenode.GetBlockTree(id); bt != nil && IsEncryptedBox(bt.BoxID) {
			exportID, idErr := newManagedEncryptedExportID()
			if idErr != nil {
				return idErr
			}
			tmpDir = filepath.Join(util.TempDir, "export", bt.BoxID, "docx", exportID)
		}
		if mkdirErr := os.MkdirAll(tmpDir, 0755); mkdirErr != nil {
			return mkdirErr
		}
		defer os.RemoveAll(tmpDir)
		name, content := ExportMarkdownHTML(id, tmpDir, true, merge)
		content = strings.ReplaceAll(content, "  \n", "<br>\n")

		tmpDocxPath := filepath.Join(tmpDir, name+".docx")
		args := []string{
			"-f", "html+tex_math_dollars",
			"--resource-path", tmpDir,
			"-o", tmpDocxPath,
		}

		params := util.ReplaceNewline(Conf.Export.PandocParams, " ")
		if "" != params {
			customArgs, parseErr := shellquote.Split(params)
			if nil != parseErr {
				logging.LogErrorf("parse pandoc custom params [%s] failed: %s", params, parseErr)
			} else {
				args = append(args, customArgs...)
			}
		}

		hasLuaFilter := false
		for i := 0; i < len(args)-1; i++ {
			if "--lua-filter" == args[i] {
				hasLuaFilter = true
				break
			}
		}
		if !hasLuaFilter {
			args = append(args, "--lua-filter", util.PandocColorFilterPath)
		}

		hasReferenceDoc := false
		for i := 0; i < len(args)-1; i++ {
			if "--reference-doc" == args[i] {
				hasReferenceDoc = true
				break
			}
		}
		if !hasReferenceDoc {
			args = append(args, "--reference-doc", util.PandocTemplatePath)
		}

		pandoc := exec.Command(Conf.Export.PandocBin, args...)
		gulu.CmdAttr(pandoc)
		pandoc.Stdin = bytes.NewBufferString(content)
		output, pandocErr := pandoc.CombinedOutput()
		if pandocErr != nil {
			argStr := strings.Join(args, " ")
			msg := gulu.DecodeCmdOutput(output)
			logging.LogErrorf("export docx [%s] failed: %s", argStr, msg)
			return fmt.Errorf(Conf.Language(14), msg)
		}

		fullPath = filepath.Join(savePath, name+".docx")
		fullPath = util.GetUniqueFilename(fullPath)
		if copyErr := filelock.Copy(tmpDocxPath, fullPath); copyErr != nil {
			logging.LogErrorf("export docx failed: %s", copyErr)
			return fmt.Errorf(Conf.Language(14), copyErr)
		}

		if tmpAssets := filepath.Join(tmpDir, "assets"); !removeAssets && gulu.File.IsDir(tmpAssets) {
			if copyErr := filelock.Copy(tmpAssets, filepath.Join(savePath, "assets")); copyErr != nil {
				logging.LogErrorf("export docx failed: %s", copyErr)
				return fmt.Errorf(Conf.Language(14), copyErr)
			}
		}
		return nil
	})
	return
}

func ExportMarkdownHTML(id, savePath string, docx, merge bool) (name, dom string) {
	if exportErr := withExportReadLockByBlockID(id, func() error {
		bt := treenode.GetBlockTree(id)
		if nil == bt {
			return nil
		}

		tree := prepareExportTree(bt)

		if merge {
			var mergeErr error
			tree, mergeErr = mergeSubDocs(tree)
			if nil != mergeErr {
				logging.LogErrorf("merge sub docs failed: %s", mergeErr)
				return nil
			}
		}

		blockRefMode := Conf.Export.BlockRefMode
		tree = exportTree(tree, true, false, true,
			blockRefMode, Conf.Export.BlockEmbedMode, Conf.Export.FileAnnotationRefMode,
			Conf.Export.TagOpenMarker, Conf.Export.TagCloseMarker,
			Conf.Export.BlockRefTextLeft, Conf.Export.BlockRefTextRight,
			Conf.Export.AddTitle, Conf.Export.InlineMemo, true, true)
		name = path.Base(tree.HPath)
		name = util.FilterFileName(name) // unsupported file name characters were not removed when exporting PDF, HTML and Word https://github.com/siyuan-note/siyuan/issues/5614
		savePath = strings.TrimSpace(savePath)

		if err := os.MkdirAll(savePath, 0755); err != nil {
			logging.LogErrorf("mkdir [%s] failed: %s", savePath, err)
			return nil
		}

		if docx {
			netAssets2LocalAssets0(tree, true, "", filepath.Join(savePath, "assets"), false)
		}

		assets := getAssetsLinkDests(tree.Root, docx)
		for _, asset := range assets {
			if !util.IsAssetLinkDest([]byte(asset), docx) {
				continue
			}

			srcAbsPath, err := GetAssetAbsPathInBox(asset, tree.Box)
			if err != nil {
				logging.LogWarnf("resolve path of asset [%s] failed: %s", asset, err)
				continue
			}
			targetAbsPath := filepath.Join(savePath, AssetPathWithoutQuery(asset))
			if err = copyAssetDecryptIfEncrypted(srcAbsPath, targetAbsPath); err != nil {
				logging.LogWarnf("copy asset from [%s] to [%s] failed: %s", srcAbsPath, targetAbsPath, err)
			}
		}

		srcs := []string{"stage/build/export", "stage/protyle"}
		for _, src := range srcs {
			from := filepath.Join(util.WorkingDir, src)
			to := filepath.Join(savePath, src)
			if err := filelock.Copy(from, to); err != nil {
				logging.LogWarnf("copy stage from [%s] to [%s] failed: %s", from, savePath, err)
			}
		}

		theme := Conf.Appearance.ThemeLight
		if 1 == Conf.Appearance.Mode {
			theme = Conf.Appearance.ThemeDark
		}
		// Copy the theme folder
		srcs = []string{"themes/" + theme}
		appearancePath := util.AppearancePath
		if util.IsSymlinkPath(util.AppearancePath) {
			// Support for symlinked theme folder when exporting HTML https://github.com/siyuan-note/siyuan/issues/9173
			var readErr error
			appearancePath, readErr = filepath.EvalSymlinks(util.AppearancePath)
			if nil != readErr {
				logging.LogErrorf("readlink [%s] failed: %s", util.AppearancePath, readErr)
				return nil
			}
		}

		for _, src := range srcs {
			from := filepath.Join(appearancePath, src)
			to := filepath.Join(savePath, "appearance", src)
			if err := filelock.Copy(from, to); err != nil {
				logging.LogErrorf("copy appearance from [%s] to [%s] failed: %s", from, savePath, err)
				return nil
			}
		}

		// Only copy the icon.js file from the icon folder
		iconName := Conf.Appearance.Icon
		// If the icon in use is not the built-in one (litheness), copy litheness as a fallback
		if iconName != "litheness" && iconName != "" {
			srcIconFile := filepath.Join(appearancePath, "icons", "litheness", "icon.js")
			toIconDir := filepath.Join(savePath, "appearance", "icons", "litheness")
			if err := os.MkdirAll(toIconDir, 0755); err != nil {
				logging.LogErrorf("mkdir [%s] failed: %s", toIconDir, err)
				return nil
			}
			toIconFile := filepath.Join(toIconDir, "icon.js")
			if err := filelock.Copy(srcIconFile, toIconFile); err != nil {
				logging.LogWarnf("copy icon file from [%s] to [%s] failed: %s", srcIconFile, toIconFile, err)
			}
		}
		// Copy the currently used icon file
		if iconName != "" {
			srcIconFile := filepath.Join(appearancePath, "icons", iconName, "icon.js")
			toIconDir := filepath.Join(savePath, "appearance", "icons", iconName)
			if err := os.MkdirAll(toIconDir, 0755); err != nil {
				logging.LogErrorf("mkdir [%s] failed: %s", toIconDir, err)
				return nil
			}
			toIconFile := filepath.Join(toIconDir, "icon.js")
			if err := filelock.Copy(srcIconFile, toIconFile); err != nil {
				logging.LogWarnf("copy icon file from [%s] to [%s] failed: %s", srcIconFile, toIconFile, err)
			}
		}

		// Copy custom emoji images
		emojis := emojisInTree(tree)
		for _, emoji := range emojis {
			from := filepath.Join(util.DataDir, emoji)
			to := filepath.Join(savePath, emoji)
			if err := filelock.Copy(from, to); err != nil {
				logging.LogErrorf("copy emojis from [%s] to [%s] failed: %s", from, to, err)
			}
		}

		if docx {
			processIFrame(tree)
			fillThemeStyleVar(tree)
		}

		luteEngine := NewLute()
		luteEngine.SetFootnotes(true)
		luteEngine.SetExportNormalizeTaskListMarker(true)

		ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
			if !entering {
				return ast.WalkContinue
			}
			if ast.NodeEmojiImg == n.Type {
				// Strip the leading / from custom emoji image addresses
				n.Tokens = bytes.ReplaceAll(n.Tokens, []byte("src=\"/emojis"), []byte("src=\"emojis"))
			} else if ast.NodeList == n.Type {
				if nil != n.ListData && 1 == n.ListData.Typ {
					if 0 == n.ListData.Start {
						n.ListData.Start = 1
					}
					if li := n.ChildByType(ast.NodeListItem); nil != li && nil != li.ListData {
						n.ListData.Start = li.ListData.Num
					}
				}
			} else if n.IsTextMarkType("code") {
				if nil != n.Next && ast.NodeText == n.Next.Type {
					// there is an extra zero-width space after exporting inline code to word https://github.com/siyuan-note/siyuan/issues/14825
					n.Next.Tokens = bytes.TrimPrefix(n.Next.Tokens, []byte(editor.Zwsp))
				}
			}
			return ast.WalkContinue
		})

		if docx {
			renderer := render.NewProtyleExportDocxRenderer(tree, luteEngine.RenderOptions, luteEngine.ParseOptions)
			output := renderer.Render()
			dom = gulu.Str.FromBytes(output)
		} else {
			dom = luteEngine.ProtylePreview(tree, luteEngine.RenderOptions, luteEngine.ParseOptions)
		}
		return nil
	}); exportErr != nil {
		logging.LogErrorf("export markdown html [%s] failed: %s", id, exportErr)
		return
	}
	return
}

func ExportHTML(id, savePath string, pdf, keepFold, merge bool) (name, dom string, node *ast.Node) {
	if exportErr := withExportReadLockByBlockID(id, func() error {
		savePath = strings.TrimSpace(savePath)

		bt := treenode.GetBlockTree(id)
		if nil == bt {
			return nil
		}

		tree := prepareExportTree(bt)
		node = treenode.GetNodeInTree(tree, id)
		if ast.NodeDocument == node.Type {
			node.RemoveIALAttr("style")
		}

		if merge {
			var mergeErr error
			tree, mergeErr = mergeSubDocs(tree)
			if nil != mergeErr {
				logging.LogErrorf("merge sub docs failed: %s", mergeErr)
				return nil
			}
		}

		blockRefMode := Conf.Export.BlockRefMode
		var headings []*ast.Node
		if pdf { // exporting PDF needs table-of-contents bookmarks marked
			ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
				if entering && ast.NodeHeading == n.Type && !n.ParentIs(ast.NodeBlockquote) && !n.ParentIs(ast.NodeCallout) {
					headings = append(headings, n)
					return ast.WalkSkipChildren
				}
				return ast.WalkContinue
			})

			for _, h := range headings {
				link := &ast.Node{Type: ast.NodeLink}
				link.AppendChild(&ast.Node{Type: ast.NodeOpenBracket})
				link.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(" ")})
				link.AppendChild(&ast.Node{Type: ast.NodeCloseBracket})
				link.AppendChild(&ast.Node{Type: ast.NodeOpenParen})
				link.AppendChild(&ast.Node{Type: ast.NodeLinkDest, Tokens: []byte(PdfOutlineScheme + "://" + h.ID)})
				link.AppendChild(&ast.Node{Type: ast.NodeCloseParen})
				h.PrependChild(link)
			}
		}

		tree = exportTree(tree, true, keepFold, true,
			blockRefMode, Conf.Export.BlockEmbedMode, Conf.Export.FileAnnotationRefMode,
			Conf.Export.TagOpenMarker, Conf.Export.TagCloseMarker,
			Conf.Export.BlockRefTextLeft, Conf.Export.BlockRefTextRight,
			Conf.Export.AddTitle, Conf.Export.InlineMemo, true, true)
		adjustHeadingLevel(bt, tree)
		name = path.Base(tree.HPath)
		name = util.FilterFileName(name) // unsupported file name characters were not removed when exporting PDF, HTML and Word https://github.com/siyuan-note/siyuan/issues/5614

		if "" != savePath {
			if err := os.MkdirAll(savePath, 0755); err != nil {
				logging.LogErrorf("mkdir [%s] failed: %s", savePath, err)
				return nil
			}

			assets := getAssetsLinkDests(tree.Root, false)
			for _, asset := range assets {
				srcAbsPath, err := GetAssetAbsPathInBox(asset, tree.Box)
				if err != nil {
					logging.LogWarnf("resolve path of asset [%s] failed: %s", asset, err)
					continue
				}
				targetAbsPath := filepath.Join(savePath, AssetPathWithoutQuery(asset))
				if err = copyAssetDecryptIfEncrypted(srcAbsPath, targetAbsPath); err != nil {
					logging.LogWarnf("copy asset from [%s] to [%s] failed: %s", srcAbsPath, targetAbsPath, err)
				}
			}
		}

		if !pdf && "" != savePath { // exporting HTML needs static resources copied
			srcs := []string{"stage/build/export", "stage/protyle"}
			for _, src := range srcs {
				from := filepath.Join(util.WorkingDir, src)
				to := filepath.Join(savePath, src)
				if err := filelock.Copy(from, to); err != nil {
					logging.LogErrorf("copy stage from [%s] to [%s] failed: %s", from, savePath, err)
					return nil
				}
			}

			theme := Conf.Appearance.ThemeLight
			if 1 == Conf.Appearance.Mode {
				theme = Conf.Appearance.ThemeDark
			}
			// Copy the theme folder
			srcs = []string{"themes/" + theme}
			appearancePath := util.AppearancePath
			if util.IsSymlinkPath(util.AppearancePath) {
				// Support for symlinked theme folder when exporting HTML https://github.com/siyuan-note/siyuan/issues/9173
				var readErr error
				appearancePath, readErr = filepath.EvalSymlinks(util.AppearancePath)
				if nil != readErr {
					logging.LogErrorf("readlink [%s] failed: %s", util.AppearancePath, readErr)
					return nil
				}
			}
			for _, src := range srcs {
				from := filepath.Join(appearancePath, src)
				to := filepath.Join(savePath, "appearance", src)
				if err := filelock.Copy(from, to); err != nil {
					logging.LogErrorf("copy appearance from [%s] to [%s] failed: %s", from, savePath, err)
				}
			}

			// Only copy the icon.js file from the icon folder
			iconName := Conf.Appearance.Icon
			// If the icon in use is not the built-in one (litheness), copy litheness as a fallback
			if iconName != "litheness" && iconName != "" {
				srcIconFile := filepath.Join(appearancePath, "icons", "litheness", "icon.js")
				toIconDir := filepath.Join(savePath, "appearance", "icons", "litheness")
				if err := os.MkdirAll(toIconDir, 0755); err != nil {
					logging.LogErrorf("mkdir [%s] failed: %s", toIconDir, err)
					return nil
				}
				toIconFile := filepath.Join(toIconDir, "icon.js")
				if err := filelock.Copy(srcIconFile, toIconFile); err != nil {
					logging.LogWarnf("copy icon file from [%s] to [%s] failed: %s", srcIconFile, toIconFile, err)
				}
			}
			// Copy the currently used icon file
			if iconName != "" {
				srcIconFile := filepath.Join(appearancePath, "icons", iconName, "icon.js")
				toIconDir := filepath.Join(savePath, "appearance", "icons", iconName)
				if err := os.MkdirAll(toIconDir, 0755); err != nil {
					logging.LogErrorf("mkdir [%s] failed: %s", toIconDir, err)
					return nil
				}
				toIconFile := filepath.Join(toIconDir, "icon.js")
				if err := filelock.Copy(srcIconFile, toIconFile); err != nil {
					logging.LogWarnf("copy icon file from [%s] to [%s] failed: %s", srcIconFile, toIconFile, err)
				}
			}

			// Copy custom emoji images
			emojis := emojisInTree(tree)
			for _, emoji := range emojis {
				from := filepath.Join(util.DataDir, emoji)
				to := filepath.Join(savePath, emoji)
				if err := filelock.Copy(from, to); err != nil {
					logging.LogErrorf("copy emojis from [%s] to [%s] failed: %s", from, to, err)
				}
			}
		}

		if pdf {
			processIFrame(tree)
		}

		luteEngine := NewLute()
		luteEngine.SetFootnotes(true)
		luteEngine.RenderOptions.ProtyleContenteditable = false
		luteEngine.SetProtyleMarkNetImg(false)

		// Don't run security sanitization, because all HTML tags need to be preserved on export
		// The `<style></style>` tag was lost when exporting with the `data-export-html` attribute https://github.com/siyuan-note/siyuan/issues/6228
		luteEngine.SetSanitize(false)

		renderer := render.NewProtyleExportRenderer(tree, luteEngine.RenderOptions, luteEngine.ParseOptions)
		dom = gulu.Str.FromBytes(renderer.Render())
		return nil
	}); exportErr != nil {
		logging.LogErrorf("export html [%s] failed: %s", id, exportErr)
		return
	}
	return
}

func prepareExportTree(bt *treenode.BlockTree) (ret *parse.Tree) {
	luteEngine := NewLute()
	ret, _ = filesys.LoadTree(bt.BoxID, bt.Path, luteEngine)
	if "d" != bt.Type {
		node := treenode.GetNodeInTree(ret, bt.ID)
		nodes := []*ast.Node{node}
		if "h" == bt.Type {
			children := treenode.HeadingChildren(node)
			for _, child := range children {
				nodes = append(nodes, child)
			}
		}

		oldRoot := ret.Root
		ret = parse.Parse("", []byte(""), luteEngine.ParseOptions)
		first := ret.Root.FirstChild
		for _, n := range nodes {
			first.InsertBefore(n)
		}
		ret.Root.KramdownIAL = oldRoot.KramdownIAL
	}
	ret.Path = bt.Path
	ret.HPath = bt.HPath
	ret.Box = bt.BoxID
	ret.ID = bt.RootID
	return
}

func processIFrame(tree *parse.Tree) {
	// Use hyperlinks for IFrame blocks when exporting PDF/Word https://github.com/siyuan-note/siyuan/issues/4035
	ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering || ast.NodeIFrame != n.Type {
			return ast.WalkContinue
		}

		n.Type = ast.NodeParagraph
		index := bytes.Index(n.Tokens, []byte("src=\""))
		if 0 > index {
			n.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: n.Tokens})
		} else {
			src := n.Tokens[index+len("src=\""):]
			src = src[:bytes.Index(src, []byte("\""))]
			src = html.UnescapeHTML(src)
			link := &ast.Node{Type: ast.NodeLink}
			link.AppendChild(&ast.Node{Type: ast.NodeOpenBracket})
			link.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: src})
			link.AppendChild(&ast.Node{Type: ast.NodeCloseBracket})
			link.AppendChild(&ast.Node{Type: ast.NodeOpenParen})
			link.AppendChild(&ast.Node{Type: ast.NodeLinkDest, Tokens: src})
			link.AppendChild(&ast.Node{Type: ast.NodeCloseParen})
			n.AppendChild(link)
		}
		return ast.WalkContinue
	})
}

func ProcessPDF(id, p string, merge, removeAssets, watermark bool) (err error) {
	err = withExportReadLockByBlockID(id, func() error {
		tree, _ := LoadTreeByBlockID(id)
		if nil == tree {
			return nil
		}

		if merge {
			var mergeErr error
			tree, mergeErr = mergeSubDocs(tree)
			if nil != mergeErr {
				logging.LogErrorf("merge sub docs failed: %s", mergeErr)
				return nil
			}
		}

		var headings []*ast.Node
		assetDests := getAssetsLinkDests(tree.Root, false)
		ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
			if !entering {
				return ast.WalkContinue
			}

			if ast.NodeHeading == n.Type && !n.ParentIs(ast.NodeBlockquote) && !n.ParentIs(ast.NodeCallout) {
				headings = append(headings, n)
				return ast.WalkSkipChildren
			}
			return ast.WalkContinue
		})

		api.DisableConfigDir()
		font.UserFontDir = filepath.Join(util.HomeDir, ".config", "siyuan", "fonts")
		if mkdirErr := os.MkdirAll(font.UserFontDir, 0755); nil != mkdirErr {
			logging.LogErrorf("mkdir [%s] failed: %s", font.UserFontDir, mkdirErr)
			return nil
		}
		if loadErr := api.LoadUserFonts(); nil != loadErr {
			logging.LogErrorf("load user fonts failed: %s", loadErr)
		}

		pdfCtx, ctxErr := api.ReadContextFile(p)
		if nil != ctxErr {
			logging.LogErrorf("read pdf context failed: %s", ctxErr)
			return nil
		}

		processPDFBookmarks(pdfCtx, headings)
		processPDFLinkEmbedAssets(pdfCtx, assetDests, tree.Box, removeAssets)
		processPDFWatermark(pdfCtx, watermark)

		pdfcpuVer := model.VersionStr
		model.VersionStr = "SiYuan v" + util.Ver + " (pdfcpu " + pdfcpuVer + ")"
		if writeErr := api.WriteContextFile(pdfCtx, p); nil != writeErr {
			logging.LogErrorf("write pdf context failed: %s", writeErr)
			return nil
		}
		return nil
	})
	return
}

func processPDFWatermark(pdfCtx *model.Context, watermark bool) {
	// Support adding the watermark on export PDF https://github.com/siyuan-note/siyuan/issues/9961
	// https://pdfcpu.io/core/watermark

	if !watermark {
		return
	}

	str := Conf.Export.PDFWatermarkStr
	if "" == str {
		return
	}

	mode := "text"
	if gulu.File.IsExist(str) {
		if ".pdf" == strings.ToLower(filepath.Ext(str)) {
			mode = "pdf"
		} else {
			mode = "image"
		}
	}

	desc := Conf.Export.PDFWatermarkDesc
	if "text" == mode && util.ContainsCJK(str) {
		// A CJK text watermark requires a font file to be installed
		descParts := strings.Split(desc, ",")
		m := map[string]string{}
		for _, descPart := range descParts {
			kv := strings.Split(descPart, ":")
			if 2 != len(kv) {
				continue
			}
			m[kv[0]] = kv[1]
		}

		useDefaultFont := true
		if "" != m["fontname"] {
			listFonts, e := api.ListFonts()
			var builtInFontNames []string
			if nil != e {
				logging.LogInfof("listFont failed: %s", e)
			} else {
				for _, f := range listFonts {
					if strings.Contains(f, "(") {
						f = f[:strings.Index(f, "(")]
					}
					f = strings.TrimSpace(f)
					if strings.Contains(f, ":") || "" == f || strings.Contains(f, "Corefonts") || strings.Contains(f, "Userfonts") {
						continue
					}

					builtInFontNames = append(builtInFontNames, f)
				}

				if slices.Contains(builtInFontNames, m["fontname"]) {
					useDefaultFont = false
				}
			}
		}
		if useDefaultFont {
			m["fontname"] = "LXGWWenKaiLite-Regular"
			fontPath := filepath.Join(util.AppearancePath, "fonts", "LxgwWenKai-Lite-1.501", "LXGWWenKaiLite-Regular.ttf")
			err := api.InstallFonts([]string{fontPath})
			if err != nil {
				logging.LogErrorf("install font [%s] failed: %s", fontPath, err)
			}
		}

		descBuilder := bytes.Buffer{}
		for k, v := range m {
			descBuilder.WriteString(k)
			descBuilder.WriteString(":")
			descBuilder.WriteString(v)
			descBuilder.WriteString(",")
		}
		desc = descBuilder.String()
		desc = desc[:len(desc)-1]
	}

	logging.LogInfof("add PDF watermark [mode=%s, str=%s, desc=%s]", mode, str, desc)

	var wm *model.Watermark
	var err error
	switch mode {
	case "text":
		wm, err = pdfcpu.ParseTextWatermarkDetails(str, desc, false, types.POINTS)
	case "image":
		wm, err = pdfcpu.ParseImageWatermarkDetails(str, desc, false, types.POINTS)
	case "pdf":
		wm, err = pdfcpu.ParsePDFWatermarkDetails(str, desc, false, types.POINTS)
	}

	if err != nil {
		logging.LogErrorf("parse watermark failed: %s", err)
		util.PushErrMsg(err.Error(), 7000)
		return
	}

	wm.OnTop = true // Export PDF and add watermarks no longer covered by images https://github.com/siyuan-note/siyuan/issues/10818
	err = pdfcpu.AddWatermarks(pdfCtx, nil, wm)
	if err != nil {
		logging.LogErrorf("add watermark failed: %s", err)
		return
	}
}

func processPDFBookmarks(pdfCtx *model.Context, headings []*ast.Node) {
	links, err := PdfListToCLinks(pdfCtx)
	if err != nil {
		return
	}

	sort.Slice(links, func(i, j int) bool {
		return links[i].Page < links[j].Page
	})

	titles := map[string]bool{}
	bms := map[string]*pdfcpu.Bookmark{}
	for _, link := range links {
		linkID := link.URI[strings.LastIndex(link.URI, "/")+1:]
		b := sql.GetBlock(linkID)
		if nil == b {
			logging.LogWarnf("pdf outline block [%s] not found", linkID)
			continue
		}
		title := b.Content
		title, _ = url.QueryUnescape(title)
		for {
			if _, ok := titles[title]; ok {
				title += "\x01"
			} else {
				titles[title] = true
				break
			}
		}
		bm := &pdfcpu.Bookmark{
			Title:    title,
			PageFrom: link.Page,
			AbsPos:   link.Rect.UR.Y,
		}
		bms[linkID] = bm
	}

	if 1 > len(bms) {
		return
	}

	var topBms []*pdfcpu.Bookmark
	stack := linkedliststack.New()
	for _, h := range headings {
	L:
		for ; ; stack.Pop() {
			cur, ok := stack.Peek()
			if !ok {
				bm, ok := bms[h.ID]
				if !ok {
					break L
				}
				bm.Level = h.HeadingLevel
				stack.Push(bm)
				topBms = append(topBms, bm)
				break L
			}

			tip := cur.(*pdfcpu.Bookmark)
			if tip.Level < h.HeadingLevel {
				bm := bms[h.ID]
				bm.Level = h.HeadingLevel
				bm.Parent = tip
				tip.Kids = append(tip.Kids, bm)
				stack.Push(bm)
				break L
			}
		}
	}

	err = pdfcpu.AddBookmarks(pdfCtx, topBms, true)
	if err != nil {
		logging.LogErrorf("add bookmark failed: %s", err)
		return
	}
}

// processPDFLinkEmbedAssets processes hyperlinks to asset files, deciding based on the removeAssets parameter
// whether to embed asset files into the PDF.
// Supports embedding asset files as attachments when exporting PDF https://github.com/siyuan-note/siyuan/issues/7414
func processPDFLinkEmbedAssets(pdfCtx *model.Context, assetDests []string, boxID string, removeAssets bool) {
	var assetAbsPaths []string
	for _, dest := range assetDests {
		if absPath, _ := GetAssetAbsPathInBox(dest, boxID); "" != absPath {
			assetAbsPaths = append(assetAbsPaths, absPath)
		}
	}

	if 1 > len(assetAbsPaths) {
		return
	}

	assetLinks, otherLinks, listErr := PdfListLinks(pdfCtx)
	if nil != listErr {
		logging.LogErrorf("list asset links failed: %s", listErr)
		return
	}

	if 1 > len(assetLinks) {
		return
	}

	if _, removeErr := pdfcpu.RemoveAnnotations(pdfCtx, nil, nil, nil, false); nil != removeErr {
		logging.LogWarnf("remove annotations failed: %s", removeErr)
	}

	linkMap := map[int][]model.AnnotationRenderer{}
	for _, link := range otherLinks {
		link.URI, _ = url.PathUnescape(link.URI)
		if 1 > len(linkMap[link.Page]) {
			linkMap[link.Page] = []model.AnnotationRenderer{link}
		} else {
			linkMap[link.Page] = append(linkMap[link.Page], link)
		}
	}

	attachmentMap := map[int][]*types.IndirectRef{}
	now := types.StringLiteral(types.DateString(time.Now()))
	for _, link := range assetLinks {
		link.URI = strings.ReplaceAll(link.URI, "http://"+util.LocalHost+":"+util.ServerPort+"/export/temp/", "")
		link.URI = strings.ReplaceAll(link.URI, "http://"+util.LocalHost+":6806/export/temp/", "")
		link.URI = strings.ReplaceAll(link.URI, "http://"+util.LocalHost+":"+util.ServerPort+"/", "") // Exporting PDF embedded asset files as attachments fails https://github.com/siyuan-note/siyuan/issues/7414#issuecomment-1704573557
		link.URI = strings.ReplaceAll(link.URI, "http://"+util.LocalHost+":6806/", "")
		link.URI, _ = url.PathUnescape(link.URI)
		sourceURI := link.URI
		if idx := strings.Index(link.URI, "?"); 0 < idx {
			link.URI = link.URI[:idx]
		}

		if !removeAssets {
			// If the resource folder isn't removed, point the hyperlink at the resource folder
			// For encrypted resources, link.URI needs to keep the ?box= context, otherwise the exported PDF link
			// cannot be resolved
			if idx := strings.Index(sourceURI, "?"); 0 < idx {
				if strings.Contains(sourceURI[idx:], "box=") {
					link.URI = sourceURI
				}
			}
			if 1 > len(linkMap[link.Page]) {
				linkMap[link.Page] = []model.AnnotationRenderer{link}
			} else {
				linkMap[link.Page] = append(linkMap[link.Page], link)
			}

			continue
		}

		// If the resource folder is removed, use an embedded attachment instead

		absPath, getErr := GetAssetAbsPathInBox(sourceURI, boxID)
		if nil != getErr {
			continue
		}
		embedPath := absPath
		if IsEncryptedAssetPath(absPath) {
			assetBoxID := ExtractBoxIDFromAssetsPath(absPath)
			plain, readErr := ReadAssetBytesInBox(assetBoxID, sourceURI)
			if nil != readErr {
				logging.LogWarnf("read encrypted asset [%s] failed: %s", sourceURI, readErr)
				continue
			}
			// Temp resources for encrypted notebooks go under a boxID subdirectory, so LockBox cleanup and
			// server-side lock verification work correctly
			pdfAssetsDir := filepath.Join(util.TempDir, "export", assetBoxID, "pdf-assets")
			if mkErr := os.MkdirAll(pdfAssetsDir, 0755); mkErr != nil {
				logging.LogWarnf("mkdir pdf-assets [%s] failed: %s", pdfAssetsDir, mkErr)
				continue
			}
			embedPath = filepath.Join(pdfAssetsDir, gulu.Rand.String(7)+"-"+filepath.Base(AssetPathWithoutQuery(sourceURI)))
			if writeErr := filelock.WriteFile(embedPath, plain); nil != writeErr {
				logging.LogWarnf("write temp embedded asset [%s] failed: %s", embedPath, writeErr)
				continue
			}
			defer os.Remove(embedPath)
		}

		ir, newErr := pdfCtx.XRefTable.NewEmbeddedFileStreamDict(embedPath)
		if nil != newErr {
			logging.LogWarnf("new embedded file stream dict failed: %s", newErr)
			continue
		}

		fn := filepath.Base(AssetPathWithoutQuery(sourceURI))
		fileSpecDict, newErr := pdfCtx.XRefTable.NewFileSpecDict(fn, fn, "attached by SiYuan", *ir)
		if nil != newErr {
			logging.LogWarnf("new file spec dict failed: %s", newErr)
			continue
		}

		ir, indErr := pdfCtx.XRefTable.IndRefForNewObject(fileSpecDict)
		if nil != indErr {
			logging.LogWarnf("ind ref for new object failed: %s", indErr)
			continue
		}

		lx := link.Rect.LL.X + link.Rect.Width()
		ly := link.Rect.LL.Y + link.Rect.Height()/2
		w := link.Rect.Height() / 2
		h := link.Rect.Height() / 2

		d := types.Dict(
			map[string]types.Object{
				"Type":         types.Name("Annot"),
				"Subtype":      types.Name("FileAttachment"),
				"Contents":     types.StringLiteral(""),
				"Rect":         types.RectForWidthAndHeight(lx, ly, w, h).Array(),
				"P":            link.P,
				"M":            now,
				"F":            types.Integer(0),
				"Border":       types.NewIntegerArray(0, 0, 1),
				"C":            types.NewNumberArray(0.5, 0.0, 0.5),
				"CA":           types.Float(0.95),
				"CreationDate": now,
				"Name":         types.Name("FileAttachment"),
				"FS":           *ir,
				"NM":           types.StringLiteral(""),
			},
		)

		ann, indErr := pdfCtx.XRefTable.IndRefForNewObject(d)
		if nil != indErr {
			logging.LogWarnf("ind ref for new object failed: %s", indErr)
			continue
		}

		pageDictIndRef, pageErr := pdfCtx.PageDictIndRef(link.Page)
		if nil != pageErr {
			logging.LogWarnf("page dict ind ref failed: %s", pageErr)
			continue
		}

		d, defErr := pdfCtx.DereferenceDict(*pageDictIndRef)
		if nil != defErr {
			logging.LogWarnf("dereference dict failed: %s", defErr)
			continue
		}

		if 1 > len(attachmentMap[link.Page]) {
			attachmentMap[link.Page] = []*types.IndirectRef{ann}
		} else {
			attachmentMap[link.Page] = append(attachmentMap[link.Page], ann)
		}
	}

	if 0 < len(linkMap) {
		if _, addErr := pdfcpu.AddAnnotationsMap(pdfCtx, linkMap, false); nil != addErr {
			logging.LogErrorf("add annotations map failed: %s", addErr)
		}
	}

	// Add attachment annotations pointing to the embedded attachments
	for page, anns := range attachmentMap {
		pageDictIndRef, pageErr := pdfCtx.PageDictIndRef(page)
		if nil != pageErr {
			logging.LogWarnf("page dict ind ref failed: %s", pageErr)
			continue
		}

		pageDict, defErr := pdfCtx.DereferenceDict(*pageDictIndRef)
		if nil != defErr {
			logging.LogWarnf("dereference dict failed: %s", defErr)
			continue
		}

		array := types.Array{}
		for _, ann := range anns {
			array = append(array, *ann)
		}

		obj, found := pageDict.Find("Annots")
		if !found {
			pageDict.Insert("Annots", array)
			pdfCtx.EnsureVersionForWriting()
			continue
		}

		ir, ok := obj.(types.IndirectRef)
		if !ok {
			pageDict.Update("Annots", append(obj.(types.Array), array...))
			pdfCtx.EnsureVersionForWriting()
			continue
		}

		// Annots array is an IndirectReference.

		o, err := pdfCtx.Dereference(ir)
		if err != nil || o == nil {
			continue
		}

		annots, _ := o.(types.Array)
		entry, ok := pdfCtx.FindTableEntryForIndRef(&ir)
		if !ok {
			continue
		}
		entry.Object = append(annots, array...)
		pdfCtx.EnsureVersionForWriting()
	}
}

func ExportStdMarkdown(id string, assetsDestSpace2Underscore, fillCSSVar, adjustHeadingLevel, imgTag bool) string {
	var ret string
	if exportErr := withExportReadLockByBlockID(id, func() error {
		bt := treenode.GetBlockTree(id)
		if nil == bt {
			logging.LogErrorf("block tree [%s] not found", id)
			return nil
		}

		tree := prepareExportTree(bt)
		cloudAssetsBase := ""
		if IsSubscriber() {
			cloudAssetsBase = util.GetCloudAssetsServer() + Conf.GetUser().UserId + "/"
		}

		var defBlockIDs []string
		if 4 == Conf.Export.BlockRefMode { // footnote + anchor hash
			// Exporting anchor hashes, so record the IDs of all definition blocks here first
			ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
				if !entering {
					return ast.WalkContinue
				}

				var defID string
				if treenode.IsBlockLink(n) {
					defID = strings.TrimPrefix(n.TextMarkAHref, "siyuan://blocks/")
				} else if treenode.IsBlockRef(n) {
					defID, _, _ = treenode.GetBlockRef(n)
				}

				if "" != defID {
					if defBt := treenode.GetBlockTree(defID); nil != defBt {
						defBlockIDs = append(defBlockIDs, defID)
						defBlockIDs = gulu.Str.RemoveDuplicatedElem(defBlockIDs)
					}
				}
				return ast.WalkContinue
			})
		}
		defBlockIDs = gulu.Str.RemoveDuplicatedElem(defBlockIDs)

		ret = exportMarkdownContent0(id, tree, cloudAssetsBase, assetsDestSpace2Underscore, adjustHeadingLevel, imgTag,
			".md", Conf.Export.BlockRefMode, Conf.Export.BlockEmbedMode, Conf.Export.FileAnnotationRefMode,
			Conf.Export.TagOpenMarker, Conf.Export.TagCloseMarker,
			Conf.Export.BlockRefTextLeft, Conf.Export.BlockRefTextRight,
			Conf.Export.AddTitle, Conf.Export.InlineMemo, defBlockIDs, true, fillCSSVar)
		return nil
	}); exportErr != nil {
		logging.LogErrorf("export std markdown [%s] failed: %s", id, exportErr)
		return ""
	}
	return ret
}

// ExportOptions provides temporary options for a single export; fields left at their zero value (nil) fall back
// to the global Conf.Export values.
// The common section can be reused by every export format, while the Markdown-specific section is only used by
// Markdown export.
// Used by the "export Markdown parameters dialog" https://github.com/siyuan-note/siyuan/issues/17031
type ExportOptions struct {
	// Common section (read by PDF/Word/HTML/Markdown)
	AddTitle              *bool   `json:"addTitle"`              // whether to add the document title
	InlineMemo            *bool   `json:"inlineMemo"`            // whether to export inline memos
	BlockRefMode          *int    `json:"blockRefMode"`          // content block reference export mode
	BlockEmbedMode        *int    `json:"blockEmbedMode"`        // content block embed export mode
	FileAnnotationRefMode *int    `json:"fileAnnotationRefMode"` // file annotation reference export mode
	BlockRefTextLeft      *string `json:"blockRefTextLeft"`      // symbol to the left of block ref anchor text
	BlockRefTextRight     *string `json:"blockRefTextRight"`     // symbol to the right of block ref anchor text
	TagOpenMarker         *string `json:"tagOpenMarker"`         // tag opening marker
	TagCloseMarker        *string `json:"tagCloseMarker"`        // tag closing marker
	// Markdown-specific section
	IncludeSubDocs     *bool `json:"includeSubDocs"`     // whether to include sub-documents
	IncludeRelatedDocs *bool `json:"includeRelatedDocs"` // whether to include related documents
	MarkdownYFM        *bool `json:"markdownYFM"`        // whether to add YAML Front Matter
	RemoveAssetsID     *bool `json:"removeAssetsID"`     // whether to strip the ID from asset file names
}

// applyExportOptions temporarily overrides Conf.Export with opts, returning a restore function (make sure to
// defer the call).
// Used by the "export Markdown parameters dialog" #17031; exports run serially, so there's no concurrency risk
// during the override.
func applyExportOptions(opts *ExportOptions) func() {
	snapshot := *Conf.Export // save the original state via a value copy
	if nil != opts {
		if nil != opts.AddTitle {
			Conf.Export.AddTitle = *opts.AddTitle
		}
		if nil != opts.InlineMemo {
			Conf.Export.InlineMemo = *opts.InlineMemo
		}
		if nil != opts.BlockRefMode {
			Conf.Export.BlockRefMode = *opts.BlockRefMode
		}
		if nil != opts.BlockEmbedMode {
			Conf.Export.BlockEmbedMode = *opts.BlockEmbedMode
		}
		if nil != opts.FileAnnotationRefMode {
			Conf.Export.FileAnnotationRefMode = *opts.FileAnnotationRefMode
		}
		if nil != opts.BlockRefTextLeft {
			Conf.Export.BlockRefTextLeft = *opts.BlockRefTextLeft
		}
		if nil != opts.BlockRefTextRight {
			Conf.Export.BlockRefTextRight = *opts.BlockRefTextRight
		}
		if nil != opts.TagOpenMarker {
			Conf.Export.TagOpenMarker = *opts.TagOpenMarker
		}
		if nil != opts.TagCloseMarker {
			Conf.Export.TagCloseMarker = *opts.TagCloseMarker
		}
		if nil != opts.IncludeSubDocs {
			Conf.Export.IncludeSubDocs = *opts.IncludeSubDocs
		}
		if nil != opts.IncludeRelatedDocs {
			Conf.Export.IncludeRelatedDocs = *opts.IncludeRelatedDocs
		}
		if nil != opts.MarkdownYFM {
			Conf.Export.MarkdownYFM = *opts.MarkdownYFM
		}
		if nil != opts.RemoveAssetsID {
			Conf.Export.RemoveAssetsID = *opts.RemoveAssetsID
		}
	}
	return func() { *Conf.Export = snapshot }
}

// ExportPandocConvertZipWithOptions builds on ExportPandocConvertZip by accepting per-export options #17031.
func ExportPandocConvertZipWithOptions(ids []string, pandocTo, ext string, opts *ExportOptions) (name, zipPath string) {
	restore := applyExportOptions(opts)
	defer restore()
	return ExportPandocConvertZip(ids, pandocTo, ext)
}

// ExportNotebookMarkdownWithOptions builds on ExportNotebookMarkdown by accepting per-export options #17031.
func ExportNotebookMarkdownWithOptions(boxID string, opts *ExportOptions) (zipPath string) {
	restore := applyExportOptions(opts)
	defer restore()
	return ExportNotebookMarkdown(boxID)
}

// ParseExportOptions parses export options from JSON request parameters; fields that aren't passed in stay nil
// (falling back to the global config) #17031.
func ParseExportOptions(arg map[string]any) (opts *ExportOptions) {
	opts = &ExportOptions{}
	// Common section
	if nil != arg["addTitle"] {
		v := arg["addTitle"].(bool)
		opts.AddTitle = &v
	}
	if nil != arg["inlineMemo"] {
		v := arg["inlineMemo"].(bool)
		opts.InlineMemo = &v
	}
	if nil != arg["blockRefMode"] {
		v := int(arg["blockRefMode"].(float64))
		opts.BlockRefMode = &v
	}
	if nil != arg["blockEmbedMode"] {
		v := int(arg["blockEmbedMode"].(float64))
		opts.BlockEmbedMode = &v
	}
	if nil != arg["fileAnnotationRefMode"] {
		v := int(arg["fileAnnotationRefMode"].(float64))
		opts.FileAnnotationRefMode = &v
	}
	if nil != arg["blockRefTextLeft"] {
		v := arg["blockRefTextLeft"].(string)
		opts.BlockRefTextLeft = &v
	}
	if nil != arg["blockRefTextRight"] {
		v := arg["blockRefTextRight"].(string)
		opts.BlockRefTextRight = &v
	}
	if nil != arg["tagOpenMarker"] {
		v := arg["tagOpenMarker"].(string)
		opts.TagOpenMarker = &v
	}
	if nil != arg["tagCloseMarker"] {
		v := arg["tagCloseMarker"].(string)
		opts.TagCloseMarker = &v
	}
	// Markdown-specific section
	if nil != arg["includeSubDocs"] {
		v := arg["includeSubDocs"].(bool)
		opts.IncludeSubDocs = &v
	}
	if nil != arg["includeRelatedDocs"] {
		v := arg["includeRelatedDocs"].(bool)
		opts.IncludeRelatedDocs = &v
	}
	if nil != arg["markdownYFM"] {
		v := arg["markdownYFM"].(bool)
		opts.MarkdownYFM = &v
	}
	if nil != arg["removeAssetsID"] {
		v := arg["removeAssetsID"].(bool)
		opts.RemoveAssetsID = &v
	}
	return
}

func ExportPandocConvertZip(ids []string, pandocTo, ext string) (name, zipPath string) {
	block := treenode.GetBlockTree(ids[0])
	if nil != block && IsEncryptedBox(block.BoxID) && !IsBoxUnlocked(block.BoxID) {
		logging.LogErrorf("export pandoc zip [%s] failed: encrypted notebook locked", ids[0])
		return
	}
	box := Conf.Box(block.BoxID)
	baseFolderName := path.Base(block.HPath)
	if "." == baseFolderName {
		baseFolderName = path.Base(block.Path)
	}

	var docPaths []string
	bts := treenode.GetBlockTrees(ids)
	for _, bt := range bts {
		docPaths = append(docPaths, bt.Path)

		if Conf.Export.IncludeSubDocs {
			listPath := strings.TrimSuffix(bt.Path, ".sy")
			if IsBoxDoc(bt.BoxID, bt.RootID) {
				listPath = "/"
			}
			docFiles := box.ListFiles(listPath)
			for _, docFile := range docFiles {
				if docFile.path == bt.Path {
					continue
				}
				docPaths = append(docPaths, docFile.path)
			}
		}
	}

	defBlockIDs, docPaths := prepareExportTrees(docPaths)
	zipPath = exportPandocConvertZip(block.BoxID, baseFolderName, docPaths, defBlockIDs, "gfm+footnotes+hard_line_breaks", pandocTo, ext)
	name = util.GetTreeID(block.Path)
	return
}

func ExportNotebookMarkdown(boxID string) (zipPath string) {
	if IsEncryptedBox(boxID) && !IsBoxUnlocked(boxID) {
		logging.LogErrorf("export notebook markdown [%s] failed: encrypted notebook locked", boxID)
		return
	}
	util.PushEndlessProgress(Conf.Language(65))
	defer util.ClearPushProgress(100)

	box := Conf.Box(boxID)
	if nil == box {
		logging.LogErrorf("not found box [%s]", boxID)
		return
	}

	var docPaths []string
	docFiles := box.ListFiles("/")
	for _, docFile := range docFiles {
		docPaths = append(docPaths, docFile.path)
	}

	defBlockIDs, docPaths := prepareExportTrees(docPaths)
	zipPath = exportPandocConvertZip(boxID, box.Name, docPaths, defBlockIDs, "", "", ".md")
	return
}

func yfm(docIAL map[string]string) string {
	// Prepend some metadata at the beginning when exporting a Markdown file https://github.com/siyuan-note/siyuan/issues/6880

	buf := bytes.Buffer{}
	buf.WriteString("---\n")
	var title, created, updated, tags string
	for k, v := range docIAL {
		if "id" == k {
			createdTime, parseErr := time.ParseInLocation("20060102150405", util.TimeFromID(v), time.Local)
			if nil == parseErr {
				created = createdTime.Format(time.RFC3339)
			}
			continue
		}
		if "title" == k {
			title = v
			continue
		}
		if "updated" == k {
			updatedTime, parseErr := time.ParseInLocation("20060102150405", v, time.Local)
			if nil == parseErr {
				updated = updatedTime.Format(time.RFC3339)
			}
			continue
		}
		if "tags" == k {
			tags = v
			continue
		}
	}
	if "" != title {
		buf.WriteString("title: ")
		buf.WriteString(title)
		buf.WriteString("\n")
	}
	if "" == updated {
		updated = time.Now().Format(time.RFC3339)
	}
	if "" == created {
		created = updated
	}
	buf.WriteString("date: ")
	buf.WriteString(created)
	buf.WriteString("\n")
	buf.WriteString("lastmod: ")
	buf.WriteString(updated)
	buf.WriteString("\n")
	if "" != tags {
		buf.WriteString("tags:\n")
		tagLines := strings.SplitSeq(tags, ",")
		for tag := range tagLines {
			buf.WriteString("  - '")
			tag = strings.ReplaceAll(tag, "'", "''")
			buf.WriteString(tag)
			buf.WriteString("'\n")
		}
	}
	buf.WriteString("---\n\n")
	return buf.String()
}

// treeToSYJSON serializes an in-memory tree to plaintext .sy-format JSON bytes.
// Used for exporting .sy.zip: for an encrypted notebook, the tree has already been transparently decrypted to
// plaintext by filesys.LoadTree, so it's re-serialized here (rather than reading the disk directly via
// filelock.ReadFile, which would return ciphertext).
// Difference from filesys.prepareWriteTree: no UpsertBlockTree DB write side effect, no path dependency, pure
// serialization only.
func treeToSYJSON(tree *parse.Tree) (data []byte) {
	treenode.UpgradeSpec(tree)
	luteEngine := util.NewLute()
	renderer := render.NewJSONRenderer(tree, luteEngine.RenderOptions, luteEngine.ParseOptions)
	data = renderer.Render()
	if !util.UseSingleLineSave {
		buf := bytes.Buffer{}
		buf.Grow(1024 * 1024 * 2)
		if err := json.Indent(&buf, data, "", "\t"); err != nil {
			logging.LogErrorf("json indent failed: %s", err)
			return
		}
		data = buf.Bytes()
	}
	return
}

func exportBoxSYZip(boxID string) (zipPath string) {
	util.PushEndlessProgress(Conf.Language(65))
	defer util.ClearPushProgress(100)

	box := Conf.Box(boxID)
	if nil == box {
		logging.LogErrorf("not found box [%s]", boxID)
		return
	}
	baseFolderName := box.Name

	var docPaths []string
	docFiles := box.ListFiles("/")
	for _, docFile := range docFiles {
		docPaths = append(docPaths, docFile.path)
	}
	zipPath = exportSYZip(boxID, "/", baseFolderName, docPaths, true)
	return
}

func exportSYZip(boxID, rootDirPath, baseFolderName string, docPaths []string, includeBoxConf bool) (zipPath string) {
	defer util.ClearPushProgress(100)

	dir, name := path.Split(baseFolderName)
	name = util.FilterFileName(name)
	if strings.HasSuffix(name, "..") {
		// Markdown cannot be exported when the document title ends with `..` https://github.com/siyuan-note/siyuan/issues/4698
		// This appears to be an os.MkdirAll bug: a path ending in .. cannot be created, so a trailing _ is appended here
		name += "_"
	}
	baseFolderName = path.Join(dir, name)
	box := Conf.Box(boxID)

	// For an encrypted notebook, hold the read lock for the entire export, writing the plaintext intermediate
	// directory and the resulting artifact under the controlled temp/export/<boxID>/sy/<exportID>/ directory,
	// so LockBox cleanup and managed-download verification work. A regular notebook keeps its existing export
	// path named after boxName, unchanged.
	encrypted := IsEncryptedBox(boxID)
	var exportID string
	if encrypted {
		HoldBoxReadLock(boxID)
		defer ReleaseBoxReadLock(boxID)
		if _, dekErr := GetDEKIfUnlocked(boxID); dekErr != nil {
			logging.LogErrorf("export .sy.zip of encrypted box [%s] failed: locked", boxID)
			return
		}
		var idErr error
		exportID, idErr = newManagedEncryptedExportID()
		if idErr != nil {
			logging.LogErrorf("new export id failed: %s", idErr)
			return
		}
	}
	exportDir := filepath.Join(util.TempDir, "export", baseFolderName)
	if encrypted {
		exportDir = filepath.Join(util.TempDir, "export", boxID, "sy", exportID)
	}
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		logging.LogErrorf("create export temp folder failed: %s", err)
		return
	}
	if includeBoxConf {
		boxConf := box.GetConf()
		boxConf.Sort = 0
		boxConf.Closed = true
		boxConf.RefCreateSaveBox = ""
		boxConf.DocCreateSaveBox = ""
		boxConf.Encrypted = false
		boxConf.BoxCrypt = nil
		confData, marshalErr := gulu.JSON.MarshalIndentJSON(boxConf, "", "  ")
		if marshalErr != nil {
			logging.LogErrorf("marshal export notebook conf failed: %s", marshalErr)
			return
		}
		confDir := filepath.Join(exportDir, ".siyuan")
		if mkdirErr := os.MkdirAll(confDir, 0755); mkdirErr != nil {
			logging.LogErrorf("create export notebook conf dir failed: %s", mkdirErr)
			return
		}
		if writeErr := os.WriteFile(filepath.Join(confDir, "conf.json"), confData, 0644); writeErr != nil {
			logging.LogErrorf("write export notebook conf failed: %s", writeErr)
			return
		}
		sourceBoxDocMetaPath := boxDocMetaPath(boxID)
		if filelock.IsExist(sourceBoxDocMetaPath) {
			if copyErr := filelock.Copy(sourceBoxDocMetaPath, filepath.Join(confDir, boxDocMetaName)); copyErr != nil {
				logging.LogErrorf("copy export notebook document metadata failed: %s", copyErr)
				return
			}
		}
	}

	trees := map[string]*parse.Tree{}
	refTrees := map[string]*parse.Tree{}
	luteEngine := util.NewLute()
	for i, p := range docPaths {
		if !strings.HasSuffix(p, ".sy") {
			continue
		}

		tree, err := filesys.LoadTree(boxID, p, luteEngine)
		if err != nil {
			continue
		}
		trees[tree.ID] = tree

		util.PushEndlessProgress(Conf.language(65) + " " + fmt.Sprintf(Conf.language(70), fmt.Sprintf("%d/%d %s", i+1, len(docPaths), tree.Root.IALAttr("title"))))
	}

	count := 1
	for _, tree := range trees {
		util.PushEndlessProgress(Conf.language(65) + " " + fmt.Sprintf(Conf.language(70), fmt.Sprintf("%d/%d %s", count, len(docPaths), tree.Root.IALAttr("title"))))

		refs := map[string]*parse.Tree{}
		exportRefTrees(tree, &[]string{}, refs)
		for refTreeID, refTree := range refs {
			if nil == trees[refTreeID] {
				refTrees[refTreeID] = refTree
			}
		}
		count++
	}

	util.PushEndlessProgress(Conf.Language(65))
	count = 0

	// Copy the selected trees preserving the folder structure
	// Note: tree has already been transparently decrypted to plaintext by filesys.LoadTree, so it's serialized
	// to plaintext JSON and written here (it cannot be read directly from disk via filelock.ReadFile, since the
	// on-disk .sy of an encrypted notebook is ciphertext).
	total := len(trees) + len(refTrees)
	for _, tree := range trees {
		writePath := strings.TrimPrefix(tree.Path, rootDirPath)
		writePath = filepath.Join(exportDir, writePath)
		writeFolder := filepath.Dir(writePath)
		if mkdirErr := os.MkdirAll(writeFolder, 0755); nil != mkdirErr {
			logging.LogErrorf("create export temp folder [%s] failed: %s", writeFolder, mkdirErr)
			continue
		}
		if writeErr := os.WriteFile(writePath, treeToSYJSON(tree), 0644); nil != writeErr {
			logging.LogErrorf("write export file [%s] failed: %s", writePath, writeErr)
			continue
		}
		count++

		util.PushEndlessProgress(Conf.language(65) + " " + fmt.Sprintf(Conf.Language(66), fmt.Sprintf("%d/%d ", count, total)+tree.HPath))
	}

	count = 0
	// Referenced trees are placed at the root of the export folder
	for treeID, tree := range refTrees {
		writePath := filepath.Join(exportDir, treeID+".sy")
		if writeErr := os.WriteFile(writePath, treeToSYJSON(tree), 0644); nil != writeErr {
			logging.LogErrorf("write export file [%s] failed: %s", writePath, writeErr)
			continue
		}
		count++

		util.PushEndlessProgress(Conf.language(65) + " " + fmt.Sprintf(Conf.Language(66), fmt.Sprintf("%d/%d ", count, total)+tree.HPath))
	}

	// Merge the referenced trees into the selected trees, so their asset files can be exported together afterward
	maps.Copy(trees, refTrees)

	// Export referenced asset files
	assetPathMap, err := allAssetAbsPaths()
	if nil != err {
		logging.LogWarnf("get assets abs path failed: %s", err)
		return
	}
	copiedAssets := hashset.New()
	for _, tree := range trees {
		var assets []string
		assets = append(assets, getAssetsLinkDests(tree.Root, false)...)
		titleImgPath := treenode.GetDocTitleImgPath(tree.Root) // Export .sy.zip doc title image is not exported https://github.com/siyuan-note/siyuan/issues/8748
		if "" != titleImgPath {
			if util.IsAssetLinkDest([]byte(titleImgPath), false) {
				assets = append(assets, titleImgPath)
			}
		}

		for _, asset := range assets {
			util.PushEndlessProgress(Conf.language(65) + " " + fmt.Sprintf(Conf.language(70), asset))

			asset = string(html.DecodeDestination([]byte(asset)))
			cleanAsset := AssetPathWithoutQuery(asset)

			copyKey := tree.Box + "\x00" + cleanAsset
			if copiedAssets.Contains(copyKey) {
				continue
			}

			srcPath := ""
			if IsEncryptedBox(tree.Box) {
				srcPath, _ = GetAssetAbsPathInBox(asset, tree.Box)
			}
			if "" == srcPath {
				srcPath = assetPathMap[cleanAsset]
			}
			if "" == srcPath {
				logging.LogWarnf("get asset [%s] abs path failed", asset)
				continue
			}

			destPath := filepath.Join(exportDir, cleanAsset)
			assetErr := copyAssetDecryptIfEncrypted(srcPath, destPath)
			if nil != assetErr {
				logging.LogErrorf("copy asset from [%s] to [%s] failed: %s", srcPath, destPath, assetErr)
				continue
			}
			copiedAssets.Add(copyKey)

			if !gulu.File.IsDir(srcPath) && strings.HasSuffix(strings.ToLower(srcPath), ".pdf") {
				sya := srcPath + ".sya"
				if filelock.IsExist(sya) {
					// Related PDF annotation information is not exported when exporting .sy.zip https://github.com/siyuan-note/siyuan/issues/7836
					if syaErr := copyAssetDecryptIfEncrypted(sya, destPath+".sya"); nil != syaErr {
						logging.LogErrorf("copy sya from [%s] to [%s] failed: %s", sya, destPath+".sya", syaErr)
					}
				}
			}

			copiedAssets.Add(asset)
		}

		// Copy custom emoji images
		emojis := emojisInTree(tree)
		for _, emoji := range emojis {
			from := filepath.Join(util.DataDir, emoji)
			to := filepath.Join(exportDir, emoji)
			if copyErr := filelock.Copy(from, to); copyErr != nil {
				logging.LogErrorf("copy emojis from [%s] to [%s] failed: %s", from, to, copyErr)
			}
		}
	}

	// Export the database. Attribute View export https://github.com/siyuan-note/siyuan/issues/8710
	exportStorageAvDir := filepath.Join(exportDir, "storage", "av")
	var avIDs []string
	avBoxes := map[string]string{}
	for _, tree := range trees {
		ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
			if !entering || !n.IsBlock() {
				return ast.WalkContinue
			}

			if ast.NodeAttributeView == n.Type {
				avIDs = append(avIDs, n.AttributeViewID)
				if IsEncryptedBox(tree.Box) {
					avBoxes[n.AttributeViewID] = tree.Box
				}
			}

			avs := n.IALAttr(av.NodeAttrNameAvs)
			if "" == avs {
				return ast.WalkContinue
			}

			for avID := range strings.SplitSeq(avs, ",") {
				avID = strings.TrimSpace(avID)
				avIDs = append(avIDs, avID)
				if IsEncryptedBox(tree.Box) {
					avBoxes[avID] = tree.Box
				}
			}
			return ast.WalkContinue
		})
	}
	avIDs = gulu.Str.RemoveDuplicatedElem(avIDs)
	for _, avID := range avIDs {
		if !ast.IsNodeIDPattern(avID) {
			continue
		}

		exportAv(avID, avBoxes[avID], exportStorageAvDir, exportDir, assetPathMap)
	}

	// Export flashcards. Export related flashcard data when exporting .sy.zip https://github.com/siyuan-note/siyuan/issues/9372
	exportStorageRiffDir := filepath.Join(exportDir, "storage", "riff")
	deck, loadErr := riff.LoadDeck(exportStorageRiffDir, builtinDeckID, Conf.Flashcard.RequestRetention, Conf.Flashcard.MaximumInterval, Conf.Flashcard.Weights)
	if nil != loadErr {
		logging.LogErrorf("load deck [%s] failed: %s", name, loadErr)
	} else {
		for _, tree := range trees {
			cards := getTreeFlashcards(tree.ID)

			for _, card := range cards {
				deck.AddCard(card.ID(), card.BlockID())
			}
		}
		if 0 < deck.CountCards() {
			if saveErr := deck.Save(); nil != saveErr {
				logging.LogErrorf("save deck [%s] failed: %s", name, saveErr)
			}
		}
	}

	// Export the custom sort order
	sortPath := filepath.Join(util.DataDir, box.ID, ".siyuan", "sort.json")
	fullSortIDs := map[string]int{}
	sortIDs := map[string]int{}
	var sortData []byte
	var sortErr error
	if filelock.IsExist(sortPath) {
		sortData, sortErr = filelock.ReadFile(sortPath)
		if nil != sortErr {
			logging.LogErrorf("read sort conf failed: %s", sortErr)
		}

		if sortErr = gulu.JSON.UnmarshalJSON(sortData, &fullSortIDs); nil != sortErr {
			logging.LogErrorf("unmarshal sort conf failed: %s", sortErr)
		}

		if 0 < len(fullSortIDs) {
			for _, tree := range trees {
				if v, ok := fullSortIDs[tree.ID]; ok {
					sortIDs[tree.ID] = v
				}
			}
		}

		if 0 < len(sortIDs) {
			sortData, sortErr = gulu.JSON.MarshalJSON(sortIDs)
			if nil != sortErr {
				logging.LogErrorf("marshal sort conf failed: %s", sortErr)
			}
			if 0 < len(sortData) {
				confDir := filepath.Join(exportDir, ".siyuan")
				if mkdirErr := os.MkdirAll(confDir, 0755); nil != mkdirErr {
					logging.LogErrorf("create export conf folder [%s] failed: %s", confDir, mkdirErr)
				} else {
					sortPath = filepath.Join(confDir, "sort.json")
					if writeErr := os.WriteFile(sortPath, sortData, 0644); nil != writeErr {
						logging.LogErrorf("write sort conf failed: %s", writeErr)
					}
				}
			}
		}
	}

	// For encrypted notebooks, write a .partial file first and then rename it atomically, registering the
	// artifact as a managed download token; regular notebooks keep the original behavior.
	zipBaseName := baseFolderName + ".sy.zip"
	zipPath = exportDir + ".sy.zip"
	zipPartialPath := zipPath + ".partial"
	if encrypted {
		zipPath = filepath.Join(util.TempDir, "export", boxID, "sy", exportID+"-"+zipBaseName)
		zipPartialPath = zipPath + ".partial"
	}
	zip, err := gulu.Zip.Create(zipPartialPath)
	if err != nil {
		logging.LogErrorf("create export .sy.zip [%s] failed: %s", exportDir, err)
		return ""
	}

	zipCallback := func(filename string) {
		util.PushEndlessProgress(Conf.language(65) + " " + fmt.Sprintf(Conf.language(253), filename))
	}

	if err = zip.AddDirectory(baseFolderName, exportDir, zipCallback); err != nil {
		logging.LogErrorf("create export .sy.zip [%s] failed: %s", exportDir, err)
		return ""
	}

	if err = zip.Close(); err != nil {
		logging.LogErrorf("close export .sy.zip failed: %s", err)
	}
	if err = os.Rename(zipPartialPath, zipPath); err != nil {
		logging.LogErrorf("publish export .sy.zip [%s] failed: %s", zipPath, err)
		return ""
	}

	os.RemoveAll(exportDir)
	if encrypted {
		zipPath = "/export/" + registerManagedEncryptedExport(boxID, "sy", zipPath)
	} else {
		zipPath = "/export/" + url.PathEscape(filepath.Base(zipPath))
	}
	return
}

func exportAv(avID, boxID, exportStorageAvDir, exportFolder string, assetPathMap map[string]string) {
	// Use box-aware path resolution plus automatic decryption to read the plaintext AV definition (for an
	// encrypted notebook the AV lives at <boxID>/storage/av/; GetAttributeViewDataPath, which only checks the
	// global path, would miss it, and filelock.Copy would copy the ciphertext).
	var avData []byte
	var readErr error
	if boxID != "" {
		avData, readErr = av.ReadAttributeViewDataInBox(avID, boxID)
	} else {
		avData, readErr = av.ReadAttributeViewData(avID)
	}
	if readErr != nil {
		logging.LogErrorf("read attribute view [%s] failed: %s", avID, readErr)
		return
	}
	if avData != nil {
		if mkdirErr := os.MkdirAll(exportStorageAvDir, 0755); mkdirErr != nil {
			logging.LogErrorf("create export av folder [%s] failed: %s", exportStorageAvDir, mkdirErr)
			return
		}
		if writeErr := os.WriteFile(filepath.Join(exportStorageAvDir, avID+".json"), avData, 0644); writeErr != nil {
			logging.LogErrorf("write av json failed: %s", writeErr)
		}
	}

	var attrView *av.AttributeView
	var parseErr error
	if boxID != "" {
		attrView, parseErr = av.ParseAttributeViewInBox(avID, boxID)
	} else {
		attrView, parseErr = av.ParseAttributeView(avID)
	}
	if parseErr != nil {
		logging.LogErrorf("parse attribute view [%s] failed: %s", avID, parseErr)
		return
	}

	for _, keyValues := range attrView.KeyValues {
		switch keyValues.Key.Type {
		case av.KeyTypeMAsset: // export the asset file column https://github.com/siyuan-note/siyuan/issues/9919
			for _, value := range keyValues.Values {
				for _, asset := range value.MAsset {
					if !util.IsAssetLinkDest([]byte(asset.Content), false) {
						continue
					}

					destPath := filepath.Join(exportFolder, AssetPathWithoutQuery(asset.Content))
					srcPath := ""
					if boxID != "" {
						srcPath, _ = GetAssetAbsPathInBox(asset.Content, boxID)
					}
					if "" == srcPath {
						srcPath = assetPathMap[AssetPathWithoutQuery(asset.Content)]
					}
					if "" == srcPath {
						logging.LogWarnf("get asset [%s] abs path failed", asset.Content)
						continue
					}

					if copyErr := copyAssetDecryptIfEncrypted(srcPath, destPath); nil != copyErr {
						logging.LogErrorf("copy asset failed: %s", copyErr)
					}
				}
			}
		}
	}

	// Cascade-export the databases linked via relation columns
	exportRelationAvs(avID, boxID, exportStorageAvDir)
}

func exportRelationAvs(avID, boxID, exportStorageAvDir string) {
	avIDs := hashset.New()
	walkRelationAvs(avID, boxID, avIDs)

	for _, v := range avIDs.Values() {
		relAvID := v.(string)
		var relAvData []byte
		var readErr error
		if boxID != "" {
			relAvData, readErr = av.ReadAttributeViewDataInBox(relAvID, boxID)
		} else {
			relAvData, readErr = av.ReadAttributeViewData(relAvID)
		}
		if readErr != nil {
			logging.LogErrorf("read relation attribute view [%s] failed: %s", relAvID, readErr)
			continue
		}
		if relAvData == nil {
			continue
		}
		if writeErr := os.WriteFile(filepath.Join(exportStorageAvDir, relAvID+".json"), relAvData, 0644); writeErr != nil {
			logging.LogErrorf("write av json failed: %s", writeErr)
		}
	}
}

func walkRelationAvs(avID, boxID string, exportAvIDs *hashset.Set) {
	if exportAvIDs.Contains(avID) {
		return
	}

	var attrView *av.AttributeView
	if boxID != "" {
		attrView, _ = av.ParseAttributeViewInBox(avID, boxID)
	} else {
		attrView, _ = av.ParseAttributeView(avID)
	}
	if nil == attrView {
		return
	}

	exportAvIDs.Add(avID)
	for _, keyValues := range attrView.KeyValues {
		switch keyValues.Key.Type {
		case av.KeyTypeRelation: // export the relation column
			if nil == keyValues.Key.Relation {
				break
			}

			walkRelationAvs(keyValues.Key.Relation.AvID, boxID, exportAvIDs)
		}
	}
}

func ExportMarkdownContent(id string, refMode, embedMode int, addYfm, fillCSSVar, adjustHeadingLv, imgTag, addTitle bool) (hPath, exportedMd string) {
	if exportErr := withExportReadLockByBlockID(id, func() error {
		bt := treenode.GetBlockTree(id)
		if nil == bt {
			return nil
		}

		tree := prepareExportTree(bt)
		hPath = tree.HPath
		exportedMd = exportMarkdownContent0(id, tree, "", false, adjustHeadingLv, imgTag,
			".md", refMode, embedMode, Conf.Export.FileAnnotationRefMode,
			Conf.Export.TagOpenMarker, Conf.Export.TagCloseMarker,
			Conf.Export.BlockRefTextLeft, Conf.Export.BlockRefTextRight,
			addTitle, Conf.Export.InlineMemo, nil, true, fillCSSVar)
		docIAL := parse.IAL2Map(tree.Root.KramdownIAL)
		if addYfm {
			exportedMd = yfm(docIAL) + exportedMd
		}
		return nil
	}); exportErr != nil {
		logging.LogErrorf("export markdown content [%s] failed: %s", id, exportErr)
		return
	}
	return
}

func exportMarkdownContent(rootID, ext string, exportRefMode int, defBlockIDs []string, singleFile bool) (tree *parse.Tree, exportedMd string, isEmpty bool) {
	tree, err := LoadTreeByBlockID(rootID)
	if err != nil {
		logging.LogErrorf("load tree by block id [%s] failed: %s", rootID, err)
		return
	}

	refCount := sql.QueryRootChildrenRefCount(tree.ID)
	if !Conf.Export.MarkdownYFM && treenode.ContainOnlyDefaultIAL(tree) && 1 > len(refCount) {
		for c := tree.Root.FirstChild; nil != c; c = c.Next {
			if ast.NodeParagraph == c.Type {
				isEmpty = nil == c.FirstChild
				if !isEmpty {
					break
				}
			} else {
				isEmpty = false
				break
			}
		}
	}

	exportedMd = exportMarkdownContent0(rootID, tree, "", false, false, false,
		ext, exportRefMode, Conf.Export.BlockEmbedMode, Conf.Export.FileAnnotationRefMode,
		Conf.Export.TagOpenMarker, Conf.Export.TagCloseMarker,
		Conf.Export.BlockRefTextLeft, Conf.Export.BlockRefTextRight,
		Conf.Export.AddTitle, Conf.Export.InlineMemo, defBlockIDs, singleFile, false)
	docIAL := parse.IAL2Map(tree.Root.KramdownIAL)
	if Conf.Export.MarkdownYFM {
		// Toggle for adding YFM at the top of the document when exporting Markdown https://github.com/siyuan-note/siyuan/issues/7727
		exportedMd = yfm(docIAL) + exportedMd
	}
	return
}

func exportMarkdownContent0(id string, tree *parse.Tree, cloudAssetsBase string, assetsDestSpace2Underscore, adjustHeadingLv, imgTag bool,
	ext string, blockRefMode, blockEmbedMode, fileAnnotationRefMode int,
	tagOpenMarker, tagCloseMarker string, blockRefTextLeft, blockRefTextRight string,
	addTitle, inlineMemo bool, defBlockIDs []string, singleFile, fillCSSVar bool) (ret string) {
	tree = exportTree(tree, false, false, false,
		blockRefMode, blockEmbedMode, fileAnnotationRefMode,
		tagOpenMarker, tagCloseMarker,
		blockRefTextLeft, blockRefTextRight,
		addTitle, inlineMemo, 0 < len(defBlockIDs), singleFile)
	if adjustHeadingLv {
		bt := treenode.GetBlockTree(id)
		adjustHeadingLevel(bt, tree)
	}

	luteEngine := NewLute()
	luteEngine.SetFootnotes(true)
	luteEngine.SetKramdownIAL(false)
	luteEngine.SetExportNormalizeTaskListMarker(true)
	if "" != cloudAssetsBase {
		luteEngine.RenderOptions.LinkBase = cloudAssetsBase
	}
	if assetsDestSpace2Underscore { // asset files uploaded to the community image host have spaces converted to underscores, so the document content needs the same conversion here
		ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
			if !entering {
				return ast.WalkContinue
			}

			if ast.NodeLinkDest == n.Type {
				if util.IsAssetLinkDest(n.Tokens, false) {
					n.Tokens = bytes.ReplaceAll(n.Tokens, []byte(" "), []byte("_"))
				}
			} else if n.IsTextMarkType("a") {
				href := n.TextMarkAHref
				if util.IsAssetLinkDest([]byte(href), false) {
					n.TextMarkAHref = strings.ReplaceAll(href, " ", "_")
				}
			} else if ast.NodeIFrame == n.Type || ast.NodeAudio == n.Type || ast.NodeVideo == n.Type {
				dest := treenode.GetNodeSrcTokens(n)
				if util.IsAssetLinkDest([]byte(dest), false) {
					setAssetsLinkDest(n, dest, strings.ReplaceAll(dest, " ", "_"))
				}
			}
			return ast.WalkContinue
		})
	}

	currentDocDir := path.Dir(tree.HPath)
	currentDocDir = util.FilterFilePath(currentDocDir)

	var unlinks []*ast.Node
	ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.WalkContinue
		}

		if ast.NodeBr == n.Type {
			if !n.ParentIs(ast.NodeTableCell) {
				// When exporting Markdown, `<br />` nodes in non-tables are replaced with `\n` text nodes https://github.com/siyuan-note/siyuan/issues/9509
				n.InsertBefore(&ast.Node{Type: ast.NodeText, Tokens: []byte("\n")})
				unlinks = append(unlinks, n)
			}
		}

		if 4 == blockRefMode { // footnote + anchor hash
			if n.IsBlock() && gulu.Str.Contains(n.ID, defBlockIDs) {
				// If this is a definition block, add an anchor at the beginning
				anchorSpan := treenode.NewSpanAnchor(n.ID)
				if ast.NodeDocument != n.Type {
					firstLeaf := treenode.FirstLeafBlock(n)
					if nil != firstLeaf {
						if ast.NodeTable == firstLeaf.Type {
							firstLeaf.InsertBefore(anchorSpan)
							firstLeaf.InsertBefore(&ast.Node{Type: ast.NodeHardBreak})
						} else {
							if nil != firstLeaf.FirstChild {
								firstLeaf.FirstChild.InsertBefore(anchorSpan)
							} else {
								firstLeaf.AppendChild(anchorSpan)
							}
						}
					} else {
						n.AppendChild(anchorSpan)
					}
				}
			}

			if treenode.IsBlockRef(n) {
				// If this is a reference element, convert it to a hyperlink pointing at xxx.md#block-id
				defID, linkText := getExportBlockRefLinkText(n, blockRefTextLeft, blockRefTextRight)
				if gulu.Str.Contains(defID, defBlockIDs) {
					var href string
					bt := treenode.GetBlockTree(defID)
					if nil != bt {
						href += bt.HPath + ext
						if "d" != bt.Type {
							href += "#" + defID
						}
						if tree.ID == bt.RootID {
							href = "#" + defID
						}
					}

					sameDir := path.Dir(href) == currentDocDir
					if strings.HasPrefix(href, "#") {
						sameDir = true
					}
					href = util.FilterFilePath(href)
					if !sameDir {
						var relErr error
						href, relErr = filepath.Rel(currentDocDir, href)
						if nil != relErr {
							logging.LogWarnf("get relative path from [%s] to [%s] failed: %s", currentDocDir, href, relErr)
						}
						href = filepath.ToSlash(href)
					} else {
						href = strings.TrimPrefix(href, currentDocDir+"/")
					}
					blockRefLink := &ast.Node{Type: ast.NodeTextMark, TextMarkType: "a", TextMarkTextContent: linkText, TextMarkAHref: href}
					blockRefLink.KramdownIAL = n.KramdownIAL
					n.InsertBefore(blockRefLink)
					unlinks = append(unlinks, n)
				}
			}
		}
		return ast.WalkContinue
	})
	for _, unlink := range unlinks {
		unlink.Unlink()
	}

	if fillCSSVar {
		fillThemeStyleVar(tree)
	}

	luteEngine.SetUnorderedListMarker("-")
	luteEngine.SetImgTag(imgTag)
	renderer := render.NewProtyleExportMdRenderer(tree, luteEngine.RenderOptions, luteEngine.ParseOptions)
	ret = gulu.Str.FromBytes(renderer.Render())
	return
}

func exportTree(tree *parse.Tree, wysiwyg, keepFold, avHiddenCol bool,
	blockRefMode, blockEmbedMode, fileAnnotationRefMode int,
	tagOpenMarker, tagCloseMarker string,
	blockRefTextLeft, blockRefTextRight string,
	addTitle, inlineMemo, addDocAnchorSpan, singleFile bool) (ret *parse.Tree) {
	luteEngine := NewLute()
	ret = tree
	id := tree.Root.ID

	// Resolve query embed nodes
	depth := 0
	resolveEmbedR(ret.Root, blockEmbedMode, luteEngine, &[]string{}, &depth)

	// Convert block hyperlinks in the current document into references
	blockLink2Ref(ret)

	// Collect references to convert into footnote + anchor hash (may recurse across documents)
	var refFootnoteOrder []string // stores defIDs in order
	refFootnotesByID := make(map[string]*refAsFootnotes)
	if 4 == blockRefMode && singleFile {
		depth = 0
		collectFootnotesDefs(ret, ret.ID, &refFootnoteOrder, refFootnotesByID, &depth)
	}

	currentTreeNodeIDs := map[string]bool{}
	ast.Walk(ret.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.WalkContinue
		}

		if "" != n.ID {
			currentTreeNodeIDs[n.ID] = true
		}
		return ast.WalkContinue
	})

	var unlinks []*ast.Node
	ast.Walk(ret.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.WalkContinue
		}

		switch n.Type {
		case ast.NodeSuperBlockOpenMarker, ast.NodeSuperBlockLayoutMarker, ast.NodeSuperBlockCloseMarker:
			if !wysiwyg {
				unlinks = append(unlinks, n)
				return ast.WalkContinue
			}
		case ast.NodeHeading:
			n.SetIALAttr("id", n.ID)
		case ast.NodeMathBlockContent:
			n.Tokens = bytes.TrimSpace(n.Tokens) // strip leading/trailing whitespace from formula content when exporting Markdown https://github.com/siyuan-note/siyuan/issues/4666
			return ast.WalkContinue
		case ast.NodeTextMark:
			if n.IsTextMarkType("inline-memo") {
				if !inlineMemo {
					n.TextMarkInlineMemoContent = ""
				}
			}

			if n.IsTextMarkType("inline-math") {
				n.TextMarkInlineMathContent = strings.TrimSpace(n.TextMarkInlineMathContent)
				return ast.WalkContinue
			} else if treenode.IsFileAnnotationRef(n) {
				refID := n.TextMarkFileAnnotationRefID
				if !strings.Contains(refID, "/") {
					return ast.WalkSkipChildren
				}

				status := processFileAnnotationRef(refID, n, fileAnnotationRefMode, tree.Box)
				unlinks = append(unlinks, n)
				return status
			} else if n.IsTextMarkType("tag") {
				if !wysiwyg {
					n.Type = ast.NodeText
					n.Tokens = []byte(tagOpenMarker + n.TextMarkTextContent + tagCloseMarker)
					return ast.WalkContinue
				}
			}
		}

		if !treenode.IsBlockRef(n) {
			return ast.WalkContinue
		}

		// Process reference nodes
		defID, linkText := getExportBlockRefLinkText(n, blockRefTextLeft, blockRefTextRight)

		switch blockRefMode {
		case 2: // anchor-text block link
			blockRefLink := &ast.Node{Type: ast.NodeTextMark, TextMarkTextContent: linkText, TextMarkAHref: "siyuan://blocks/" + defID}
			blockRefLink.KramdownIAL = n.KramdownIAL
			blockRefLink.TextMarkType = "a " + n.TextMarkType
			blockRefLink.TextMarkInlineMemoContent = n.TextMarkInlineMemoContent
			n.InsertBefore(blockRefLink)
			unlinks = append(unlinks, n)
		case 3: // anchor text only
			blockRefLink := &ast.Node{Type: ast.NodeTextMark, TextMarkType: strings.TrimSpace(strings.ReplaceAll(n.TextMarkType, "block-ref", "")), TextMarkTextContent: linkText}
			blockRefLink.KramdownIAL = n.KramdownIAL
			blockRefLink.TextMarkInlineMemoContent = n.TextMarkInlineMemoContent
			n.InsertBefore(blockRefLink)
			unlinks = append(unlinks, n)
		case 4: // footnote + anchor hash
			if currentTreeNodeIDs[defID] {
				// Don't convert to a footnote within the current document; use the anchor hash directly https://github.com/siyuan-note/siyuan/issues/13283
				n.TextMarkType = "a " + n.TextMarkType
				n.TextMarkTextContent = linkText
				n.TextMarkAHref = "#" + defID
				return ast.WalkContinue
			}

			refFoot := refFootnotesByID[defID]
			if nil == refFoot {
				return ast.WalkContinue
			}

			text := &ast.Node{Type: ast.NodeText, Tokens: []byte(linkText)}
			if "block-ref" != n.TextMarkType {
				text.Type = ast.NodeTextMark
				text.TextMarkType = strings.TrimSpace(strings.ReplaceAll(n.TextMarkType, "block-ref", ""))
				text.TextMarkTextContent = linkText
				text.TextMarkInlineMemoContent = n.TextMarkInlineMemoContent
			}
			n.InsertBefore(text)
			n.InsertBefore(&ast.Node{Type: ast.NodeFootnotesRef, Tokens: []byte("^" + refFoot.refNum), FootnotesRefId: refFoot.refNum, FootnotesRefLabel: []byte("^" + refFoot.refNum)})
			unlinks = append(unlinks, n)
		}
		return ast.WalkSkipChildren
	})
	for _, n := range unlinks {
		n.Unlink()
	}

	if 4 == blockRefMode { // footnote + anchor hash
		unlinks = nil
		footnotesDefBlock := resolveFootnotesDefs(&refFootnoteOrder, refFootnotesByID, ret, currentTreeNodeIDs, blockRefTextLeft, blockRefTextRight)
		if nil != footnotesDefBlock {
			// For a focused export there may be unused footnote definition blocks, so clean them up here
			// Improve focus export conversion of block refs to footnotes https://github.com/siyuan-note/siyuan/issues/10647
			footnotesRefs := ret.Root.ChildrenByType(ast.NodeFootnotesRef)
			for footnotesDef := footnotesDefBlock.FirstChild; nil != footnotesDef; footnotesDef = footnotesDef.Next {
				fnRefsInDef := footnotesDef.ChildrenByType(ast.NodeFootnotesRef)
				footnotesRefs = append(footnotesRefs, fnRefsInDef...)
			}

			for footnotesDef := footnotesDefBlock.FirstChild; nil != footnotesDef; footnotesDef = footnotesDef.Next {
				exist := false
				for _, ref := range footnotesRefs {
					if ref.FootnotesRefId == footnotesDef.FootnotesRefId {
						exist = true
						break
					}
				}
				if !exist {
					unlinks = append(unlinks, footnotesDef)
				}
			}

			for _, n := range unlinks {
				n.Unlink()
			}

			ret.Root.AppendChild(footnotesDefBlock)
		}
	}

	if addTitle {
		if root, _ := getBlock(id, tree); nil != root {
			root.IAL["type"] = "doc"
			title := &ast.Node{Type: ast.NodeHeading, HeadingLevel: 1}
			for k, v := range root.IAL {
				if "type" == k || "style" == k {
					continue
				}
				title.SetIALAttr(k, v)
			}
			title.InsertAfter(&ast.Node{Type: ast.NodeKramdownBlockIAL, Tokens: parse.IAL2Tokens(title.KramdownIAL)})
			content := html.UnescapeString(root.Content)
			title.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(content)})
			ret.Root.PrependChild(title)
		}
	} else {
		if 4 == blockRefMode { // footnote + anchor hash
			refRoot := slices.Contains(refFootnoteOrder, id)

			footnotesDefs := tree.Root.ChildrenByType(ast.NodeFootnotesDef)
			for _, footnotesDef := range footnotesDefs {
				ast.Walk(footnotesDef, func(n *ast.Node, entering bool) ast.WalkStatus {
					if !entering {
						return ast.WalkContinue
					}

					if id == n.TextMarkBlockRefID {
						refRoot = true
						return ast.WalkStop
					}
					return ast.WalkContinue
				})
			}

			if refRoot && addDocAnchorSpan {
				anchorSpan := treenode.NewSpanAnchor(id)
				ret.Root.PrependChild(anchorSpan)
			}
		}
	}

	// Support exporting the document title image https://github.com/siyuan-note/siyuan/issues/4372
	titleImgPath := treenode.GetDocTitleImgPath(ret.Root)
	if "" != titleImgPath {
		p := &ast.Node{Type: ast.NodeParagraph}
		titleImg := &ast.Node{Type: ast.NodeImage}
		titleImg.AppendChild(&ast.Node{Type: ast.NodeBang})
		titleImg.AppendChild(&ast.Node{Type: ast.NodeOpenBracket})
		titleImg.AppendChild(&ast.Node{Type: ast.NodeLinkText, Tokens: []byte("image")})
		titleImg.AppendChild(&ast.Node{Type: ast.NodeCloseBracket})
		titleImg.AppendChild(&ast.Node{Type: ast.NodeOpenParen})
		titleImg.AppendChild(&ast.Node{Type: ast.NodeLinkDest, Tokens: []byte(titleImgPath)})
		titleImg.AppendChild(&ast.Node{Type: ast.NodeCloseParen})
		p.AppendChild(titleImg)
		ret.Root.PrependChild(p)
	}

	unlinks = nil
	var emptyParagraphs []*ast.Node
	ast.Walk(ret.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.WalkContinue
		}

		// Support exporting PDF according to the existing fold state https://github.com/siyuan-note/siyuan/issues/5941
		if !keepFold {
			// Folded blocks are always expanded when exporting HTML/PDF https://github.com/siyuan-note/siyuan/issues/4064
			n.RemoveIALAttr("fold")
			n.RemoveIALAttr("heading-fold")
		} else {
			if "1" == n.IALAttr("heading-fold") {
				unlinks = append(unlinks, n)
				return ast.WalkContinue
			}
		}

		// Strip the flashcard style from content blocks on export https://github.com/siyuan-note/siyuan/issues/7374
		if n.IsBlock() {
			n.RemoveIALAttr(NodeAttrRiffDecks)
		}

		switch n.Type {
		case ast.NodeParagraph:
			if nil == n.FirstChild {
				// An empty paragraph block needs a placeholder text node, otherwise re-parsing the tree after
				// subsequent formatting would be semantically inconsistent https://github.com/siyuan-note/siyuan/issues/5806
				emptyParagraphs = append(emptyParagraphs, n)
			}
		case ast.NodeWidget:
			// Widget block export https://github.com/siyuan-note/siyuan/issues/3834 https://github.com/siyuan-note/siyuan/issues/6188

			if wysiwyg {
				exportHtmlVal := n.IALAttr("data-export-html")
				if "" != exportHtmlVal {
					htmlBlock := &ast.Node{Type: ast.NodeHTMLBlock, Tokens: []byte(exportHtmlVal)}
					n.InsertBefore(htmlBlock)
					unlinks = append(unlinks, n)
					return ast.WalkContinue
				}
			}

			exportMdVal := n.IALAttr("data-export-md")
			exportMdVal = html.UnescapeString(exportMdVal) // escaped characters inside code blocks and inline code were not parsed when exporting `data-export-md` https://github.com/siyuan-note/siyuan/issues/4180
			if "" != exportMdVal {
				luteEngine0 := util.NewLute()
				luteEngine0.SetYamlFrontMatter(true) // the widget export attribute `data-export-md` supports YFM https://github.com/siyuan-note/siyuan/issues/7752
				exportMdTree := parse.Parse("", []byte(exportMdVal), luteEngine0.ParseOptions)
				var insertNodes []*ast.Node
				for c := exportMdTree.Root.FirstChild; nil != c; c = c.Next {
					if ast.NodeKramdownBlockIAL != c.Type {
						insertNodes = append(insertNodes, c)
					}
				}
				for _, insertNode := range insertNodes {
					n.InsertBefore(insertNode)
				}
				unlinks = append(unlinks, n)
			}
		case ast.NodeSuperBlockOpenMarker, ast.NodeSuperBlockLayoutMarker, ast.NodeSuperBlockCloseMarker:
			if !wysiwyg {
				unlinks = append(unlinks, n)
			}
		}

		if ast.NodeText != n.Type {
			return ast.WalkContinue
		}

		// Shift+Enter line breaks use a hard line break when exporting to Markdown https://github.com/siyuan-note/siyuan/issues/3458
		n.Tokens = bytes.ReplaceAll(n.Tokens, []byte("\n"), []byte("  \n"))
		return ast.WalkContinue
	})
	for _, n := range unlinks {
		n.Unlink()
	}
	for _, emptyParagraph := range emptyParagraphs {
		emptyParagraph.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(editor.Zwj)})
	}

	unlinks = nil
	// Attribute View export https://github.com/siyuan-note/siyuan/issues/8710
	ast.Walk(ret.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.WalkContinue
		}

		if ast.NodeAttributeView != n.Type {
			return ast.WalkContinue
		}

		avID := n.AttributeViewID
		// Use box-aware path resolution (for an encrypted notebook the AV lives at <boxID>/storage/av/;
		// GetAttributeViewDataPath, which only checks the global path, would miss it)
		if avJSONPath, _ := av.FindAttributeViewPath(avID); "" == avJSONPath {
			return ast.WalkContinue
		}

		attrView, err := av.ParseAttributeView(avID)
		if err != nil {
			logging.LogErrorf("parse attribute view [%s] failed: %s", avID, err)
			return ast.WalkContinue
		}

		viewID := n.IALAttr(av.NodeAttrView)
		view, err := attrView.GetCurrentView(viewID)
		if err != nil {
			logging.LogErrorf("get attribute view [%s] failed: %s", avID, err)
			return ast.WalkContinue
		}

		table := getAttrViewTable(attrView, view, "")

		// Follow the view's filter and sort rules. Use filtering and sorting of current view settings when exporting database blocks https://github.com/siyuan-note/siyuan/issues/10474
		cachedAttrViews := map[string]*av.AttributeView{}
		rollupFurtherCollections := sql.GetFurtherCollections(attrView, cachedAttrViews)
		av.Filter(table, attrView, rollupFurtherCollections, cachedAttrViews)
		av.Sort(table, attrView)

		aligns := getAttrViewTableAligns(table, avHiddenCol)
		mdTable := &ast.Node{Type: ast.NodeTable, TableAligns: aligns}
		mdTableHead := &ast.Node{Type: ast.NodeTableHead}
		mdTable.AppendChild(mdTableHead)
		mdTableHeadRow := &ast.Node{Type: ast.NodeTableRow, TableAligns: aligns}
		mdTableHead.AppendChild(mdTableHeadRow)
		for _, col := range table.Columns {
			if avHiddenCol && col.Hidden {
				// Skip hidden columns as needed. Improve database table view exporting https://github.com/siyuan-note/siyuan/issues/12232
				continue
			}

			cell := &ast.Node{Type: ast.NodeTableCell}
			name := col.Name
			if !wysiwyg {
				name = string(lex.EscapeProtyleMarkers([]byte(col.Name)))
				name = strings.ReplaceAll(name, "\\|", "|")
				name = strings.ReplaceAll(name, "|", "\\|")
			}
			cell.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(name)})
			mdTableHeadRow.AppendChild(cell)
		}

		rowNum := 1
		for _, row := range table.Rows {
			mdTableRow := &ast.Node{Type: ast.NodeTableRow, TableAligns: aligns}
			mdTable.AppendChild(mdTableRow)
			for _, cell := range row.Cells {
				if avHiddenCol && nil != cell.Value {
					if col := table.GetColumn(cell.Value.KeyID); nil != col && col.Hidden {
						continue
					}
				}

				mdTableCell := &ast.Node{Type: ast.NodeTableCell}
				mdTableRow.AppendChild(mdTableCell)
				var val string
				if nil != cell.Value {
					if av.KeyTypeBlock == cell.Value.Type {
						if nil != cell.Value.Block {
							val = cell.Value.Block.Content
							if !wysiwyg {
								val = string(lex.EscapeProtyleMarkers([]byte(val)))
								val = strings.ReplaceAll(val, "\\|", "|")
								val = strings.ReplaceAll(val, "|", "\\|")
							}
							col := table.GetColumn(cell.Value.KeyID)
							if nil != col && col.Wrap {
								lines := strings.SplitSeq(val, "\n")
								for line := range lines {
									mdTableCell.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(line)})
									mdTableCell.AppendChild(&ast.Node{Type: ast.NodeHardBreak})
								}
							} else {
								val = strings.ReplaceAll(val, "\n", " ")
								mdTableCell.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(val)})
							}
							continue
						}
					} else if av.KeyTypeText == cell.Value.Type {
						if nil != cell.Value.Text {
							val = cell.Value.Text.Content
							if !wysiwyg {
								val = string(lex.EscapeProtyleMarkers([]byte(val)))
								val = strings.ReplaceAll(val, "\\|", "|")
								val = strings.ReplaceAll(val, "|", "\\|")
							}
							col := table.GetColumn(cell.Value.KeyID)
							if nil != col && col.Wrap {
								lines := strings.SplitSeq(val, "\n")
								for line := range lines {
									mdTableCell.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(line)})
									mdTableCell.AppendChild(&ast.Node{Type: ast.NodeHardBreak})
								}
							} else {
								val = strings.ReplaceAll(val, "\n", " ")
								mdTableCell.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(val)})
							}
							continue
						}
					} else if av.KeyTypeTemplate == cell.Value.Type {
						if nil != cell.Value.Template {
							val = cell.Value.Template.Content
							val = strings.ReplaceAll(val, "\\|", "|")
							val = strings.ReplaceAll(val, "|", "\\|")
							col := table.GetColumn(cell.Value.KeyID)
							if nil != col && col.Wrap {
								lines := strings.SplitSeq(val, "\n")
								for line := range lines {
									mdTableCell.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(line)})
									mdTableCell.AppendChild(&ast.Node{Type: ast.NodeHardBreak})
								}
							} else {
								val = strings.ReplaceAll(val, "\n", " ")
								mdTableCell.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(val)})
							}
							continue
						}
					} else if av.KeyTypeDate == cell.Value.Type {
						if nil != cell.Value.Date {
							cell.Value.Date = av.NewFormattedValueDate(cell.Value.Date.Content, cell.Value.Date.Content2, av.DateFormatNone, cell.Value.Date.IsNotTime, cell.Value.Date.HasEndDate)
						}
					} else if av.KeyTypeCreated == cell.Value.Type {
						if nil != cell.Value.Created {
							key, _ := attrView.GetKey(cell.Value.KeyID)
							isNotTime := false
							if nil != key && nil != key.Created {
								isNotTime = !key.Created.IncludeTime
							}

							cell.Value.Created = av.NewFormattedValueCreated(cell.Value.Created.Content, 0, av.CreatedFormatNone, isNotTime)
						}
					} else if av.KeyTypeUpdated == cell.Value.Type {
						if nil != cell.Value.Updated {
							key, _ := attrView.GetKey(cell.Value.KeyID)
							isNotTime := false
							if nil != key && nil != key.Updated {
								isNotTime = !key.Updated.IncludeTime
							}

							cell.Value.Updated = av.NewFormattedValueUpdated(cell.Value.Updated.Content, 0, av.UpdatedFormatNone, isNotTime)
						}
					} else if av.KeyTypeURL == cell.Value.Type {
						if nil != cell.Value.URL {
							if "" != strings.TrimSpace(cell.Value.URL.Content) {
								link := &ast.Node{Type: ast.NodeLink}
								link.AppendChild(&ast.Node{Type: ast.NodeOpenBracket})
								link.AppendChild(&ast.Node{Type: ast.NodeLinkText, Tokens: []byte(cell.Value.URL.Content)})
								link.AppendChild(&ast.Node{Type: ast.NodeCloseBracket})
								link.AppendChild(&ast.Node{Type: ast.NodeOpenParen})
								link.AppendChild(&ast.Node{Type: ast.NodeLinkDest, Tokens: []byte(cell.Value.URL.Content)})
								link.AppendChild(&ast.Node{Type: ast.NodeCloseParen})
								mdTableCell.AppendChild(link)
							}
							continue
						}
					} else if av.KeyTypeMAsset == cell.Value.Type {
						if nil != cell.Value.MAsset {
							for i, a := range cell.Value.MAsset {
								if av.AssetTypeImage == a.Type {
									img := &ast.Node{Type: ast.NodeImage}
									img.AppendChild(&ast.Node{Type: ast.NodeBang})
									img.AppendChild(&ast.Node{Type: ast.NodeOpenBracket})
									img.AppendChild(&ast.Node{Type: ast.NodeLinkText, Tokens: []byte(a.Name)})
									img.AppendChild(&ast.Node{Type: ast.NodeCloseBracket})
									img.AppendChild(&ast.Node{Type: ast.NodeOpenParen})
									img.AppendChild(&ast.Node{Type: ast.NodeLinkDest, Tokens: []byte(a.Content)})
									img.AppendChild(&ast.Node{Type: ast.NodeCloseParen})
									mdTableCell.AppendChild(img)
									img.SetIALAttr("style", "max-height: 128px;")

									width, height := GetAssetImgSizeInBox(a.Content, tree.Box)
									if height > 128 {
										img.SetIALAttr("height", "128px")
										newWidth := int(float64(width) * (128.0 / float64(height)))
										img.SetIALAttr("width", strconv.Itoa(newWidth)+"px")
									}
								} else if av.AssetTypeFile == a.Type {
									linkText := strings.TrimSpace(a.Name)
									if "" == linkText {
										linkText = a.Content
									}

									if "" != strings.TrimSpace(a.Content) {
										file := &ast.Node{Type: ast.NodeLink}
										file.AppendChild(&ast.Node{Type: ast.NodeOpenBracket})
										file.AppendChild(&ast.Node{Type: ast.NodeLinkText, Tokens: []byte(linkText)})
										file.AppendChild(&ast.Node{Type: ast.NodeCloseBracket})
										file.AppendChild(&ast.Node{Type: ast.NodeOpenParen})
										file.AppendChild(&ast.Node{Type: ast.NodeLinkDest, Tokens: []byte(a.Content)})
										file.AppendChild(&ast.Node{Type: ast.NodeCloseParen})
										mdTableCell.AppendChild(file)
									} else {
										mdTableCell.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(linkText)})
									}
								}
								if i < len(cell.Value.MAsset)-1 {
									mdTableCell.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(" ")})
								}
							}
							continue
						}
					} else if av.KeyTypeLineNumber == cell.Value.Type {
						val = strconv.Itoa(rowNum)
						rowNum++
					} else if av.KeyTypeRelation == cell.Value.Type {
						for i, v := range cell.Value.Relation.Contents {
							if nil == v {
								continue
							}

							if av.KeyTypeBlock == v.Type && nil != v.Block {
								val = v.Block.Content
								if !wysiwyg {
									val = string(lex.EscapeProtyleMarkers([]byte(val)))
									val = strings.ReplaceAll(val, "\\|", "|")
									val = strings.ReplaceAll(val, "|", "\\|")
								}

								col := table.GetColumn(cell.Value.KeyID)
								if nil != col && col.Wrap {
									lines := strings.SplitSeq(val, "\n")
									for line := range lines {
										mdTableCell.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(line)})
										mdTableCell.AppendChild(&ast.Node{Type: ast.NodeHardBreak})
									}
								} else {
									val = strings.ReplaceAll(val, "\n", " ")
									mdTableCell.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(val)})
								}
							}
							if i < len(cell.Value.Relation.Contents)-1 {
								mdTableCell.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(", ")})
							}
						}
						continue
					} else if av.KeyTypeRollup == cell.Value.Type {
						for i, v := range cell.Value.Rollup.Contents {
							if nil == v {
								continue
							}

							if av.KeyTypeBlock == v.Type {
								if nil != v.Block {
									val = v.Block.Content
									if !wysiwyg {
										val = string(lex.EscapeProtyleMarkers([]byte(val)))
										val = strings.ReplaceAll(val, "\\|", "|")
										val = strings.ReplaceAll(val, "|", "\\|")
									}

									col := table.GetColumn(cell.Value.KeyID)
									if nil != col && col.Wrap {
										lines := strings.SplitSeq(val, "\n")
										for line := range lines {
											mdTableCell.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(line)})
											mdTableCell.AppendChild(&ast.Node{Type: ast.NodeHardBreak})
										}
									} else {
										val = strings.ReplaceAll(val, "\n", " ")
										mdTableCell.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(val)})
									}
								}
							} else if av.KeyTypeText == v.Type {
								val = v.Text.Content
								if !wysiwyg {
									val = string(lex.EscapeProtyleMarkers([]byte(val)))
									val = strings.ReplaceAll(val, "\\|", "|")
									val = strings.ReplaceAll(val, "|", "\\|")
								}

								col := table.GetColumn(cell.Value.KeyID)
								if nil != col && col.Wrap {
									lines := strings.SplitSeq(val, "\n")
									for line := range lines {
										mdTableCell.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(line)})
										mdTableCell.AppendChild(&ast.Node{Type: ast.NodeHardBreak})
									}
								} else {
									val = strings.ReplaceAll(val, "\n", " ")
									mdTableCell.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(val)})
								}
							} else {
								mdTableCell.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(v.String(true))})
							}

							if i < len(cell.Value.Rollup.Contents)-1 {
								mdTableCell.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(", ")})
							}
						}
						continue
					}

					if "" == val {
						val = cell.Value.String(true)
					}
				}
				mdTableCell.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(val)})
			}
		}

		n.InsertBefore(mdTable)
		unlinks = append(unlinks, n)
		return ast.WalkContinue
	})
	for _, n := range unlinks {
		n.Unlink()
	}
	return ret
}

func resolveFootnotesDefs(refFootnoteOrder *[]string, refFootnotesByID map[string]*refAsFootnotes, currentTree *parse.Tree, currentTreeNodeIDs map[string]bool, blockRefTextLeft, blockRefTextRight string) (footnotesDefBlock *ast.Node) {
	if 1 > len(*refFootnoteOrder) {
		return nil
	}

	footnotesDefBlock = &ast.Node{Type: ast.NodeFootnotesDefBlock}
	var rendered []string

	bts := treenode.GetBlockTrees(*refFootnoteOrder)
	for _, defID := range *refFootnoteOrder {
		foot := refFootnotesByID[defID]
		if nil == foot {
			continue
		}
		bt := bts[defID]
		if nil == bt {
			logging.LogWarnf("not found block tree for footnote def [%s] refNum [%s]", defID, foot.refNum)
			continue
		}

		t, err := LoadTreeByBlockID(bt.RootID)
		if nil != err {
			logging.LogWarnf("load tree for footnote def [%s] refNum [%s] failed: %s", defID, foot.refNum, err)
			continue
		}

		defNode := treenode.GetNodeInTree(t, defID)
		if nil == defNode {
			logging.LogErrorf("not found node [%s] in tree for footnote refNum [%s]", defID, foot.refNum)
			continue
		}

		docID := util.GetTreeID(defNode.Path)
		var nodes []*ast.Node
		if ast.NodeHeading == defNode.Type {
			nodes = append(nodes, defNode)
			if currentTree.ID != docID {
				// Consider container blocks and heading blocks when abbreviating the footnote definition for a
				// same-document block ref https://github.com/siyuan-note/siyuan/issues/5917
				children := treenode.HeadingChildren(defNode)
				nodes = append(nodes, children...)
			}
		} else if ast.NodeDocument == defNode.Type {
			docTitle := &ast.Node{ID: defNode.ID, Type: ast.NodeHeading, HeadingLevel: 1}
			docTitle.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(defNode.IALAttr("title"))})
			nodes = append(nodes, docTitle)
			for c := defNode.FirstChild; nil != c; c = c.Next {
				nodes = append(nodes, c)
			}
		} else {
			nodes = append(nodes, defNode)
		}

		var newNodes []*ast.Node
		for _, node := range nodes {
			var unlinks []*ast.Node

			ast.Walk(node, func(n *ast.Node, entering bool) ast.WalkStatus {
				if !entering {
					return ast.WalkContinue
				}

				if treenode.IsBlockRef(n) {
					defID, _, _ := treenode.GetBlockRef(n)
					if f := refFootnotesByID[defID]; nil != f {
						n.InsertBefore(&ast.Node{Type: ast.NodeText, Tokens: []byte(blockRefTextLeft + f.refAnchorText + blockRefTextRight)})
						n.InsertBefore(&ast.Node{Type: ast.NodeFootnotesRef, Tokens: []byte("^" + f.refNum), FootnotesRefId: f.refNum, FootnotesRefLabel: []byte("^" + f.refNum)})
						unlinks = append(unlinks, n)
					} else {
						if isNodeInTree(defID, currentTree) {
							if currentTreeNodeIDs[defID] {
								// Don't convert to a footnote within the current document; use the anchor hash directly https://github.com/siyuan-note/siyuan/issues/13283
								n.TextMarkType = "a"
								n.TextMarkTextContent = blockRefTextLeft + n.TextMarkTextContent + blockRefTextRight
								n.TextMarkAHref = "#" + defID
								return ast.WalkSkipChildren
							}
						}
					}
					return ast.WalkSkipChildren
				} else if ast.NodeBlockQueryEmbed == n.Type {
					stmt := n.ChildByType(ast.NodeBlockQueryEmbedScript).TokensStr()
					stmt = html.UnescapeString(stmt)
					stmt = strings.ReplaceAll(stmt, editor.IALValEscNewLine, "\n")
					sqlBlocks := sql.SelectBlocksRawStmt(stmt, 1, Conf.Search.Limit)
					for _, b := range sqlBlocks {
						subNodes := renderBlockMarkdownR(b.ID, &rendered)
						for _, subNode := range subNodes {
							if ast.NodeListItem == subNode.Type {
								parentList := &ast.Node{Type: ast.NodeList, ListData: &ast.ListData{Typ: subNode.ListData.Typ}}
								parentList.AppendChild(subNode)
								newNodes = append(newNodes, parentList)
							} else {
								newNodes = append(newNodes, subNode)
							}
						}
					}
					unlinks = append(unlinks, n)
					return ast.WalkSkipChildren
				}
				return ast.WalkContinue
			})
			for _, n := range unlinks {
				n.Unlink()
			}

			if ast.NodeBlockQueryEmbed != node.Type {
				if ast.NodeListItem == node.Type {
					parentList := &ast.Node{Type: ast.NodeList, ListData: &ast.ListData{Typ: node.ListData.Typ}}
					parentList.AppendChild(node)
					newNodes = append(newNodes, parentList)
				} else {
					newNodes = append(newNodes, node)
				}
			}
		}

		footnotesDef := &ast.Node{Type: ast.NodeFootnotesDef, Tokens: []byte("^" + foot.refNum), FootnotesRefId: foot.refNum, FootnotesRefLabel: []byte("^" + foot.refNum)}
		for _, node := range newNodes {
			ast.Walk(node, func(n *ast.Node, entering bool) ast.WalkStatus {
				if !entering {
					return ast.WalkContinue
				}
				if ast.NodeParagraph != n.Type {
					return ast.WalkContinue
				}

				docID := util.GetTreeID(n.Path)
				if currentTree.ID == docID {
					// Abbreviate the footnote definition for a same-document block ref https://github.com/siyuan-note/siyuan/issues/3299
					if text := sql.GetRefText(n.ID); 64 < utf8.RuneCountInString(text) {
						var unlinkChildren []*ast.Node
						for c := n.FirstChild; nil != c; c = c.Next {
							unlinkChildren = append(unlinkChildren, c)
						}
						for _, c := range unlinkChildren {
							c.Unlink()
						}
						text = gulu.Str.SubStr(text, 64) + "..."
						n.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(text)})
						return ast.WalkSkipChildren
					}
				}
				return ast.WalkContinue
			})

			footnotesDef.AppendChild(node)
		}
		footnotesDefBlock.AppendChild(footnotesDef)
	}
	return
}

func blockLink2Ref(currentTree *parse.Tree) {
	ast.Walk(currentTree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.WalkContinue
		}

		if treenode.IsBlockLink(n) {
			n.TextMarkType = strings.TrimSpace(strings.TrimPrefix(n.TextMarkType, "a") + " block-ref")
			n.TextMarkBlockRefID = strings.TrimPrefix(n.TextMarkAHref, "siyuan://blocks/")
			n.TextMarkBlockRefSubtype = "s"
		}
		return ast.WalkContinue
	})
}

func collectFootnotesDefs(currentTree *parse.Tree, id string, refFootnoteOrder *[]string, refFootnotesByID map[string]*refAsFootnotes, depth *int) {
	*depth++
	if 4096 < *depth {
		return
	}
	b := treenode.GetBlockTree(id)
	if nil == b {
		return
	}
	t, err := LoadTreeByBlockID(b.RootID)
	if nil != err {
		return
	}

	node := treenode.GetNodeInTree(t, b.ID)
	if nil == node {
		logging.LogErrorf("not found node [%s] in tree [%s]", b.ID, t.Root.ID)
		return
	}
	collectFootnotesDefs0(currentTree, node, refFootnoteOrder, refFootnotesByID, depth)
	if ast.NodeHeading == node.Type {
		children := treenode.HeadingChildren(node)
		for _, c := range children {
			collectFootnotesDefs0(currentTree, c, refFootnoteOrder, refFootnotesByID, depth)
		}
	}
}

func addRefFootnoteAndRecurse(currentTree *parse.Tree, defID, anchorText string, refFootnoteOrder *[]string, refFootnotesByID map[string]*refAsFootnotes, depth *int) {
	if nil != refFootnotesByID[defID] {
		return
	}
	if isNodeInTree(defID, currentTree) {
		// Don't convert to a footnote within the current document; use the anchor hash directly https://github.com/siyuan-note/siyuan/issues/13283
		return
	}
	if Conf.Editor.BlockRefDynamicAnchorTextMaxLen < utf8.RuneCountInString(anchorText) {
		anchorText = gulu.Str.SubStr(anchorText, Conf.Editor.BlockRefDynamicAnchorTextMaxLen) + "..."
	}
	*refFootnoteOrder = append(*refFootnoteOrder, defID)
	refFootnotesByID[defID] = &refAsFootnotes{
		refNum:        strconv.Itoa(len(*refFootnoteOrder)),
		refAnchorText: anchorText,
	}
	collectFootnotesDefs(currentTree, defID, refFootnoteOrder, refFootnotesByID, depth)
}

func collectFootnotesDefs0(currentTree *parse.Tree, node *ast.Node, refFootnoteOrder *[]string, refFootnotesByID map[string]*refAsFootnotes, depth *int) {
	ast.Walk(node, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.WalkContinue
		}

		if treenode.IsBlockRef(n) {
			defID, refText, _ := treenode.GetBlockRef(n)
			addRefFootnoteAndRecurse(currentTree, defID, refText, refFootnoteOrder, refFootnotesByID, depth)
			return ast.WalkSkipChildren
		} else if treenode.IsBlockLink(n) {
			defID := strings.TrimPrefix(n.TextMarkAHref, "siyuan://blocks/")
			anchorText := n.TextMarkTextContent
			if "" == anchorText {
				anchorText = sql.GetRefText(defID)
			}
			addRefFootnoteAndRecurse(currentTree, defID, anchorText, refFootnoteOrder, refFootnotesByID, depth)
			return ast.WalkSkipChildren
		}
		return ast.WalkContinue
	})
}

func isNodeInTree(id string, tree *parse.Tree) (ret bool) {
	ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.WalkContinue
		}

		if n.ID == id {
			ret = true
			return ast.WalkStop
		}
		return ast.WalkContinue
	})
	return
}

type refAsFootnotes struct {
	refNum        string
	refAnchorText string
}

func processFileAnnotationRef(refID string, n *ast.Node, fileAnnotationRefMode int, boxID string) ast.WalkStatus {
	p := refID[:strings.LastIndex(refID, "/")]
	absPath, err := GetAssetAbsPathInBox(p, boxID)
	if err != nil {
		logging.LogWarnf("get assets abs path by rel path [%s] failed: %s", p, err)
		return ast.WalkSkipChildren
	}
	sya := absPath + ".sya"
	syaData, readErr := os.ReadFile(sya)
	if readErr != nil {
		logging.LogErrorf("read file [%s] failed: %s", sya, readErr)
		return ast.WalkSkipChildren
	}
	// The .sya of an encrypted box is ciphertext and needs to be decrypted first
	if IsEncryptedBox(boxID) {
		HoldBoxReadLock(boxID)
		defer ReleaseBoxReadLock(boxID)
		dek, dekErr := GetDEKIfUnlocked(boxID)
		if dekErr != nil {
			logging.LogWarnf("get DEK for file annotation [%s] failed: %s", sya, dekErr)
			return ast.WalkSkipChildren
		}
		plain, decErr := DecryptAsset(boxID, filepath.Base(sya), dek, syaData)
		if decErr != nil {
			logging.LogWarnf("decrypt file annotation [%s] failed: %s", sya, decErr)
			return ast.WalkSkipChildren
		}
		syaData = plain
	}
	syaJSON := map[string]any{}
	if err = gulu.JSON.UnmarshalJSON(syaData, &syaJSON); err != nil {
		logging.LogErrorf("unmarshal file [%s] failed: %s", sya, err)
		return ast.WalkSkipChildren
	}
	annotationID := refID[strings.LastIndex(refID, "/")+1:]
	annotationData := syaJSON[annotationID]
	if nil == annotationData {
		logging.LogErrorf("not found annotation [%s] in .sya", annotationID)
		return ast.WalkSkipChildren
	}
	pages := annotationData.(map[string]any)["pages"].([]any)
	page := int(pages[0].(map[string]any)["index"].(float64)) + 1
	pageStr := strconv.Itoa(page)

	refText := n.TextMarkTextContent
	ext := filepath.Ext(p)
	file := p[7:len(p)-23-len(ext)] + ext
	fileAnnotationRefLink := &ast.Node{Type: ast.NodeLink}
	fileAnnotationRefLink.AppendChild(&ast.Node{Type: ast.NodeOpenBracket})
	if 0 == fileAnnotationRefMode {
		fileAnnotationRefLink.AppendChild(&ast.Node{Type: ast.NodeLinkText, Tokens: []byte(file + " - p" + pageStr + " - " + refText)})
	} else {
		fileAnnotationRefLink.AppendChild(&ast.Node{Type: ast.NodeLinkText, Tokens: []byte(refText)})
	}
	fileAnnotationRefLink.AppendChild(&ast.Node{Type: ast.NodeCloseBracket})
	fileAnnotationRefLink.AppendChild(&ast.Node{Type: ast.NodeOpenParen})
	dest := p + "#page=" + pageStr // https://github.com/siyuan-note/siyuan/issues/11780
	fileAnnotationRefLink.AppendChild(&ast.Node{Type: ast.NodeLinkDest, Tokens: []byte(dest)})
	fileAnnotationRefLink.AppendChild(&ast.Node{Type: ast.NodeCloseParen})
	n.InsertBefore(fileAnnotationRefLink)
	return ast.WalkSkipChildren
}

func exportPandocConvertZip(boxID, baseFolderName string, docPaths, defBlockIDs []string, pandocFrom, pandocTo, ext string) (zipPath string) {
	defer util.ClearPushProgress(100)

	dir, name := path.Split(baseFolderName)
	name = util.FilterFileName(name)
	if strings.HasSuffix(name, "..") {
		// Markdown cannot be exported when the document title ends with `..` https://github.com/siyuan-note/siyuan/issues/4698
		// This appears to be an os.MkdirAll bug: a path ending in .. cannot be created, so a trailing _ is appended here
		name += "_"
	}
	baseFolderName = path.Join(dir, name)

	// For an encrypted notebook, hold the read lock for the entire export, writing the plaintext intermediate
	// directory and the resulting artifact under the controlled temp/export/<boxID>/markdown/<exportID>/
	// directory, so LockBox cleanup and managed-download verification work. A regular notebook keeps its
	// existing export path named after baseFolderName, unchanged.
	encrypted := IsEncryptedBox(boxID)
	var exportID string
	if encrypted {
		HoldBoxReadLock(boxID)
		defer ReleaseBoxReadLock(boxID)
		if _, dekErr := GetDEKIfUnlocked(boxID); dekErr != nil {
			logging.LogErrorf("export markdown of encrypted box [%s] failed: locked", boxID)
			return
		}
		var idErr error
		exportID, idErr = newManagedEncryptedExportID()
		if idErr != nil {
			logging.LogErrorf("new export id failed: %s", idErr)
			return
		}
	}
	exportFolder := filepath.Join(util.TempDir, "export", baseFolderName+ext)
	if encrypted {
		exportFolder = filepath.Join(util.TempDir, "export", boxID, "markdown", exportID)
	}
	os.RemoveAll(exportFolder)
	if err := os.MkdirAll(exportFolder, 0755); err != nil {
		logging.LogErrorf("create export temp folder failed: %s", err)
		return
	}

	exportRefMode := Conf.Export.BlockRefMode
	wrotePathHash := map[string]string{}
	assetsPathMap, err := allAssetAbsPaths()
	if nil != err {
		logging.LogWarnf("get assets abs path failed: %s", err)
		return
	}

	assetsOldNew, assetsNewOld := map[string]string{}, map[string]string{}
	luteEngine := util.NewLute()
	luteEngine.SetExportNormalizeTaskListMarker(true)
	for i, p := range docPaths {
		rootID := util.GetTreeID(p)
		tree, md, isEmpty := exportMarkdownContent(rootID, ext, exportRefMode, defBlockIDs, false)
		if nil == tree {
			continue
		}
		hPath := tree.HPath
		dir, name = path.Split(hPath)
		dir = util.FilterFilePath(dir) // unsupported file name characters were not removed when exporting documents https://github.com/siyuan-note/siyuan/issues/4590
		name = util.FilterFileName(name)
		hPath = path.Join(dir, name)
		p = hPath + ext
		if 1 == len(docPaths) {
			// If only a single document is being exported, use the document title as the file name instead of the parent path https://github.com/siyuan-note/siyuan/issues/13635#issuecomment-3794560233
			p = name + ext
		}

		writePath := filepath.Join(exportFolder, p)
		hash := fmt.Sprintf("%x", sha1.Sum([]byte(md)))
		if gulu.File.IsExist(writePath) && hash != wrotePathHash[writePath] {
			// Append the ID for documents with the same name
			p = hPath + "-" + rootID + ext
			writePath = filepath.Join(exportFolder, p)
		}
		writeFolder := filepath.Dir(writePath)
		if err := os.MkdirAll(writeFolder, 0755); err != nil {
			logging.LogErrorf("create export temp folder [%s] failed: %s", writeFolder, err)
			continue
		}

		if isEmpty {
			entries, readErr := os.ReadDir(filepath.Join(util.DataDir, tree.Box, strings.TrimSuffix(tree.Path, ".sy")))
			if nil == readErr && 0 < len(entries) {
				// If the document content is empty and sub-documents exist, only export the folder
				// Improve export of empty documents with subdocuments https://github.com/siyuan-note/siyuan/issues/15009
				continue
			}
		}

		// Parse the exported standard Markdown and collect assets
		treeBoxID := tree.Box
		tree = parse.Parse("", gulu.Str.ToBytes(md), luteEngine.ParseOptions)
		removeAssetsID(tree, assetsOldNew, assetsNewOld)

		newAssets := getAssetsLinkDests(tree.Root, false)
		for _, newAsset := range newAssets {
			newAsset = string(html.DecodeDestination([]byte(newAsset)))
			cleanNewAsset := AssetPathWithoutQuery(newAsset)

			if !strings.HasPrefix(cleanNewAsset, "assets/") {
				continue
			}

			// When exporting Markdown, spaces in link paths are encoded as `%20`, which must be decoded back to
			// spaces before the original resource path can be resolved correctly
			// Improve export of Markdown hyperlink spaces https://github.com/siyuan-note/siyuan/issues/9792
			// No assets were exported when exporting Markdown https://github.com/siyuan-note/siyuan/issues/17046
			spaceEncodedNewAsset := strings.ReplaceAll(newAsset, " ", "%20")
			oldAsset := assetsNewOld[spaceEncodedNewAsset]
			if "" == oldAsset {
				spaceEncodedCleanNewAsset := strings.ReplaceAll(cleanNewAsset, " ", "%20")
				oldAsset = assetsNewOld[spaceEncodedCleanNewAsset]
			}
			if "" == oldAsset {
				logging.LogWarnf("get asset old path for new asset [%s] failed", spaceEncodedNewAsset)
				continue
			}

			spaceDecodedOldAsset := strings.ReplaceAll(oldAsset, "%20", " ")
			srcPath := ""
			if IsEncryptedBox(treeBoxID) {
				srcPath, _ = GetAssetAbsPathInBox(spaceDecodedOldAsset, treeBoxID)
			}
			if "" == srcPath {
				srcPath = assetsPathMap[AssetPathWithoutQuery(spaceDecodedOldAsset)]
			}
			if "" == srcPath {
				logging.LogWarnf("get asset [%s] abs path failed", spaceDecodedOldAsset)
				continue
			}

			destPath := filepath.Join(writeFolder, cleanNewAsset)
			if copyErr := copyAssetDecryptIfEncrypted(srcPath, destPath); copyErr != nil {
				logging.LogErrorf("copy asset from [%s] to [%s] failed: %s", srcPath, destPath, copyErr)
				continue
			}
		}

		for assetsOld, assetsNew := range assetsOldNew {
			md = strings.ReplaceAll(md, assetsOld, assetsNew)
		}

		// Call Pandoc to perform the format conversion
		pandocErr := util.Pandoc(pandocFrom, pandocTo, writePath, md)
		if pandocErr != nil {
			logging.LogErrorf("pandoc failed: %s", pandocErr)
			continue
		}

		wrotePathHash[writePath] = hash
		util.PushEndlessProgress(Conf.language(65) + " " + fmt.Sprintf(Conf.language(70), fmt.Sprintf("%d/%d %s", i+1, len(docPaths), name)))
	}

	// For encrypted notebooks, write a .partial file first and then rename it atomically, registering the
	// artifact as a managed download token; regular notebooks keep the original behavior.
	zipBaseName := baseFolderName + ext + ".zip"
	zipPath = exportFolder + ".zip"
	zipPartialPath := zipPath + ".partial"
	if encrypted {
		zipPath = filepath.Join(util.TempDir, "export", boxID, "markdown", exportID+"-"+zipBaseName)
		zipPartialPath = zipPath + ".partial"
	}
	zip, err := gulu.Zip.Create(zipPartialPath)
	if err != nil {
		logging.LogErrorf("create export markdown zip [%s] failed: %s", exportFolder, err)
		return ""
	}

	// The exported Markdown zip package doesn't contain a wrapping folder https://github.com/siyuan-note/siyuan/issues/6869
	entries, err := os.ReadDir(exportFolder)
	if err != nil {
		logging.LogErrorf("read export markdown folder [%s] failed: %s", exportFolder, err)
		return ""
	}

	zipCallback := func(filename string) {
		util.PushEndlessProgress(Conf.language(65) + " " + fmt.Sprintf(Conf.language(253), filename))
	}
	for _, entry := range entries {
		entryName := entry.Name()
		entryPath := filepath.Join(exportFolder, entryName)
		if gulu.File.IsDir(entryPath) {
			err = zip.AddDirectory(entryName, entryPath, zipCallback)
		} else {
			err = zip.AddEntry(entryName, entryPath, zipCallback)
		}
		if err != nil {
			logging.LogErrorf("add entry [%s] to zip failed: %s", entryName, err)
			return ""
		}
	}

	if err = zip.Close(); err != nil {
		logging.LogErrorf("close export markdown zip failed: %s", err)
	}
	if err = os.Rename(zipPartialPath, zipPath); err != nil {
		logging.LogErrorf("publish export markdown zip [%s] failed: %s", zipPath, err)
		return ""
	}

	os.RemoveAll(exportFolder)
	if encrypted {
		zipPath = "/export/" + registerManagedEncryptedExport(boxID, "markdown", zipPath)
	} else {
		zipPath = "/export/" + url.PathEscape(filepath.Base(zipPath))
	}
	return
}

func removeAssetsID(tree *parse.Tree, assetsOldNew, assetsNewOld map[string]string) {
	assetNodes := getAssetsLinkDestsInTree(tree, false)
	for _, node := range assetNodes {
		dests := getAssetsLinkDests(node, false)
		if 1 > len(dests) {
			continue
		}

		for _, dest := range dests {
			if !Conf.Export.RemoveAssetsID {
				assetsOldNew[dest] = dest
				assetsNewOld[dest] = dest
				continue
			}

			if newDest := assetsOldNew[dest]; "" != newDest {
				setAssetsLinkDest(node, dest, newDest)
				continue
			}

			name := path.Base(dest)
			name = util.RemoveID(name)
			newDest := "assets/" + name
			if existOld := assetsNewOld[newDest]; "" != existOld {
				if existOld == dest { // the same resource path already exists
					setAssetsLinkDest(node, dest, newDest)
				} else {
					// A resource file with the same name but different content exists, so keep the ID
					assetsNewOld[dest] = dest
					assetsOldNew[dest] = dest
				}
				continue
			}

			setAssetsLinkDest(node, dest, newDest)
			assetsOldNew[dest] = newDest
			assetsNewOld[newDest] = dest
		}
	}
}

func getExportBlockRefLinkText(blockRef *ast.Node, blockRefTextLeft, blockRefTextRight string) (defID, linkText string) {
	defID, linkText, _ = treenode.GetBlockRef(blockRef)
	if "" == linkText {
		linkText = sql.GetRefText(defID)
	}
	linkText = util.UnescapeHTML(linkText) // `&` was turned into the `&amp;` entity when exporting block ref anchor text https://github.com/siyuan-note/siyuan/issues/7659
	if Conf.Editor.BlockRefDynamicAnchorTextMaxLen < utf8.RuneCountInString(linkText) {
		linkText = gulu.Str.SubStr(linkText, Conf.Editor.BlockRefDynamicAnchorTextMaxLen) + "..."
	}
	linkText = blockRefTextLeft + linkText + blockRefTextRight
	return
}

func prepareExportTrees(docPaths []string) (defBlockIDs []string, relatedDocPaths []string) {
	trees := map[string]*parse.Tree{}
	defBlockIDs = []string{}
	for i, p := range docPaths {
		rootID := strings.TrimSuffix(path.Base(p), ".sy")
		if !ast.IsNodeIDPattern(rootID) {
			continue
		}

		tree, err := LoadTreeByBlockID(rootID)
		if err != nil {
			continue
		}
		exportRefTrees(tree, &defBlockIDs, trees)

		util.PushEndlessProgress(Conf.language(65) + " " + fmt.Sprintf(Conf.language(70), fmt.Sprintf("%d/%d %s", i+1, len(docPaths), tree.Root.IALAttr("title"))))
	}

	for _, tree := range trees {
		relatedDocPaths = append(relatedDocPaths, tree.Path)
	}
	relatedDocPaths = gulu.Str.RemoveDuplicatedElem(relatedDocPaths)
	return
}

func exportRefTrees(tree *parse.Tree, defBlockIDs *[]string, retTrees map[string]*parse.Tree) {
	if nil != retTrees[tree.ID] {
		return
	}
	retTrees[tree.ID] = tree

	ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.WalkContinue
		}

		if treenode.IsBlockRef(n) {
			defID, _, _ := treenode.GetBlockRef(n)
			if "" == defID {
				return ast.WalkContinue
			}
			defBlock := treenode.GetBlockTree(defID)
			if nil == defBlock {
				return ast.WalkSkipChildren
			}

			defTree, err := LoadTreeByBlockID(defBlock.RootID)
			if err != nil {
				return ast.WalkSkipChildren
			}
			*defBlockIDs = append(*defBlockIDs, defID)

			if !Conf.Export.IncludeRelatedDocs {
				return ast.WalkSkipChildren
			}
			exportRefTrees(defTree, defBlockIDs, retTrees)
		} else if treenode.IsBlockLink(n) {
			defID := strings.TrimPrefix(n.TextMarkAHref, "siyuan://blocks/")
			if "" == defID {
				return ast.WalkContinue
			}
			defBlock := treenode.GetBlockTree(defID)
			if nil == defBlock {
				return ast.WalkSkipChildren
			}

			defTree, err := LoadTreeByBlockID(defBlock.RootID)
			if err != nil {
				return ast.WalkSkipChildren
			}
			*defBlockIDs = append(*defBlockIDs, defID)

			if !Conf.Export.IncludeRelatedDocs {
				return ast.WalkSkipChildren
			}
			exportRefTrees(defTree, defBlockIDs, retTrees)
		} else if ast.NodeAttributeView == n.Type {
			// Also export the documents containing bound blocks when exporting the document that hosts the database
			// Export the binding block docs when exporting the doc where the database is located https://github.com/siyuan-note/siyuan/issues/11486

			avID := n.AttributeViewID
			if "" == avID {
				return ast.WalkContinue
			}

			attrView, _ := av.ParseAttributeView(avID)
			if nil == attrView {
				return ast.WalkContinue
			}

			blockKeyValues := attrView.GetBlockKeyValues()
			if nil == blockKeyValues || nil == blockKeyValues.Values {
				return ast.WalkContinue
			}

			for _, val := range blockKeyValues.Values {
				if val.IsDetached || nil == val.Block {
					continue
				}

				blockID := val.Block.ID
				if "" == blockID {
					continue
				}

				defBlock := treenode.GetBlockTree(blockID)
				if nil == defBlock {
					continue
				}

				defTree, err := LoadTreeByBlockID(defBlock.RootID)
				if err != nil {
					continue
				}
				*defBlockIDs = append(*defBlockIDs, val.BlockID)

				if !Conf.Export.IncludeRelatedDocs {
					return ast.WalkSkipChildren
				}
				exportRefTrees(defTree, defBlockIDs, retTrees)
			}
		}
		return ast.WalkContinue
	})

	*defBlockIDs = gulu.Str.RemoveDuplicatedElem(*defBlockIDs)
}

func getAttrViewTable(attrView *av.AttributeView, view *av.View, query string) (ret *av.Table) {
	switch view.LayoutType {
	case av.LayoutTypeGallery:
		view.Table = av.NewLayoutTable()
		for _, field := range view.Gallery.CardFields {
			view.Table.Columns = append(view.Table.Columns, &av.ViewTableColumn{BaseField: &av.BaseField{ID: field.ID}})
		}
	case av.LayoutTypeKanban:
		view.Table = av.NewLayoutTable()
		for _, field := range view.Kanban.Fields {
			view.Table.Columns = append(view.Table.Columns, &av.ViewTableColumn{BaseField: &av.BaseField{ID: field.ID}})
		}
	}

	depth := 1
	ret = sql.RenderAttributeViewTable(attrView, view, query, &depth, map[string]*av.AttributeView{}, false)
	return
}

func getAttrViewTableAligns(table *av.Table, hiddenCol bool) (ret []int) {
	for _, column := range table.Columns {
		if hiddenCol && column.Hidden {
			continue
		}

		align := 0
		switch column.Align {
		case av.TableColumnAlignLeft:
			align = 1
		case av.TableColumnAlignCenter:
			align = 2
		case av.TableColumnAlignRight:
			align = 3
		}
		ret = append(ret, align)
	}
	return
}

// adjustHeadingLevel promotes the first heading to level 1 when doing a focused export (i.e. not a document
// block), or to level 2 if adding the document title is enabled.
// Export preview mode supports focus use https://github.com/siyuan-note/siyuan/issues/15340
func adjustHeadingLevel(bt *treenode.BlockTree, tree *parse.Tree) {
	if "d" == bt.Type {
		return
	}

	level := 1
	var firstHeading *ast.Node
	if !Conf.Export.AddTitle {
		for n := tree.Root.FirstChild; nil != n; n = n.Next {
			if ast.NodeHeading == n.Type && !n.ParentIs(ast.NodeBlockquote) && !n.ParentIs(ast.NodeCallout) {
				firstHeading = n
				break
			}
		}
	} else {
		for n := tree.Root.FirstChild.Next; nil != n; n = n.Next {
			if ast.NodeHeading == n.Type && !n.ParentIs(ast.NodeBlockquote) && !n.ParentIs(ast.NodeCallout) {
				firstHeading = n
				break
			}
		}
		level = 2
	}
	if nil != firstHeading {
		hLevel := firstHeading.HeadingLevel
		diff := level - hLevel
		var children, childrenHeadings []*ast.Node
		children = append(children, firstHeading)
		children = append(children, treenode.HeadingChildren(firstHeading)...)
		for _, c := range children {
			ccH := c.ChildrenByType(ast.NodeHeading)
			childrenHeadings = append(childrenHeadings, ccH...)
		}
		for _, h := range childrenHeadings {
			h.HeadingLevel += diff
			if 6 < h.HeadingLevel {
				h.HeadingLevel = 6
			} else if 1 > h.HeadingLevel {
				h.HeadingLevel = 1
			}
		}
	}
}
