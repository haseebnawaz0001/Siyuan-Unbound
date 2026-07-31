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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/88250/gulu"
	"github.com/88250/lute/ast"
	"github.com/gin-gonic/gin"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/cache"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

// InsertAssetBytes writes an in-memory asset directly into the target document's assets directory, avoiding
// generated content passing through a plaintext temp file.
func InsertAssetBytes(id, fileName string, data []byte) (assetPath string, created bool, err error) {
	bt := treenode.GetBlockTree(id)
	if bt == nil {
		return "", false, errors.New(Conf.Language(71))
	}
	if len(data) == 0 {
		return "", false, errors.New("asset data is empty")
	}

	baseName := filepath.Base(fileName)
	fName := util.FilterUploadFileName(baseName)
	ext := strings.ToLower(filepath.Ext(fName))
	fName = strings.TrimSuffix(fName, filepath.Ext(fName)) + ext
	if fName == "" || fName == "." || ext == "" {
		return "", false, errors.New("invalid asset filename")
	}

	docDirLocalPath := filepath.Join(util.DataDir, bt.BoxID, path.Dir(bt.Path))
	assetsDirPath := getAssetsDir(filepath.Join(util.DataDir, bt.BoxID), docDirLocalPath)
	if err = os.MkdirAll(assetsDirPath, 0755); err != nil {
		return "", false, err
	}

	reader := bytes.NewReader(data)
	hash, err := util.GetEtagByHandle(reader, int64(len(data)))
	if err != nil {
		return "", false, err
	}
	if existAssetPath := GetAssetPathByHash(hash, bt.BoxID); existAssetPath != "" {
		originalName := util.RemoveID(filepath.Base(existAssetPath))
		if strings.EqualFold(fName, originalName) {
			return strings.TrimPrefix(existAssetPath, "/"), false, nil
		}
		hash = "random_2_" + gulu.Rand.String(12)
	}

	blockID := ast.NewNodeID()
	if IsEncryptedBox(bt.BoxID) {
		fName = encryptedAssetName(util.Ext(fName), blockID)
		if err = writeAssetNameMapping(bt.BoxID, fName, baseName); err != nil {
			return "", false, err
		}
	} else {
		fName = util.AssetName(fName, blockID)
	}
	writePath := filepath.Join(assetsDirPath, fName)
	if err = writeAssetFile(writePath, bytes.NewReader(data), bt.BoxID); err != nil {
		if IsEncryptedBox(bt.BoxID) {
			_ = removeAssetNameMapping(bt.BoxID, fName)
		}
		return "", false, err
	}

	assetPath = "assets/" + fName
	if IsEncryptedBox(bt.BoxID) {
		assetPath += "?box=" + bt.BoxID
	} else {
		cache.SetAssetHash(hash, assetPath)
	}
	IncSync()
	return assetPath, true, nil
}

func InsertLocalAssets(id string, assetAbsPaths []string, isUpload bool) (succMap map[string]any, err error) {
	succMap = map[string]any{}

	bt := treenode.GetBlockTree(id)
	if nil == bt {
		err = errors.New(Conf.Language(71))
		return
	}

	docDirLocalPath := filepath.Join(util.DataDir, bt.BoxID, path.Dir(bt.Path))
	assetsDirPath := getAssetsDir(filepath.Join(util.DataDir, bt.BoxID), docDirLocalPath)
	if !gulu.File.IsExist(assetsDirPath) {
		if err = os.MkdirAll(assetsDirPath, 0755); err != nil {
			return
		}
	}

	for _, assetAbsPath := range assetAbsPaths {
		baseName := filepath.Base(assetAbsPath)
		fName := baseName
		fName = util.FilterUploadFileName(fName)
		ext := filepath.Ext(fName)
		fName = strings.TrimSuffix(fName, ext)
		ext = strings.ToLower(ext)
		fName += ext
		if gulu.File.IsDir(assetAbsPath) || !isUpload {
			if !strings.HasPrefix(assetAbsPath, "\\\\") {
				assetAbsPath = "file://" + assetAbsPath
			}
			succMap[baseName] = assetAbsPath
			continue
		}

		if gulu.File.IsSubPath(assetsDirPath, assetAbsPath) {
			// Skip asset files that are already located under the assets directory
			// Dragging a file from the assets folder into the editor causes the kernel to exit https://github.com/siyuan-note/siyuan/issues/15355
			succMap[baseName] = "assets/" + baseName
			continue
		}

		fi, statErr := os.Stat(assetAbsPath)
		if nil != statErr {
			err = statErr
			return
		}
		f, openErr := os.Open(assetAbsPath)
		if nil != openErr {
			err = openErr
			return
		}

		hash, hashErr := util.GetEtagByHandle(f, fi.Size())
		if nil != hashErr {
			f.Close()
			return
		}

		if 1 > fi.Size() {
			hash = "random_1_" + gulu.Rand.String(12)
		}

		existAssetPath := GetAssetPathByHash(hash, bt.BoxID)
		if "" != existAssetPath {
			originalName := util.RemoveID(filepath.Base(existAssetPath))
			if strings.ToLower(fName) != strings.ToLower(originalName) {
				hash = "random_2_" + gulu.Rand.String(12)
			}
		}

		if "" != existAssetPath && !strings.HasPrefix(hash, "random_") {
			succMap[baseName] = strings.TrimPrefix(existAssetPath, "/")
			f.Close()
		} else {
			blockID := ast.NewNodeID()
			if IsEncryptedBox(bt.BoxID) {
				// Encrypted box: the on-disk filename is anonymized to uuid-blockID.ext, and the original name is
				// stored in the encrypted mapping
				fName = encryptedAssetName(util.Ext(fName), blockID)
				// If writing the mapping fails, do not write the asset, to avoid producing an "orphaned ciphertext
				// asset with no mapping" (see design doc section 7)
				if mapErr := writeAssetNameMapping(bt.BoxID, fName, baseName); mapErr != nil {
					err = mapErr
					f.Close()
					return
				}
			} else {
				fName = util.AssetName(fName, blockID)
			}
			writePath := filepath.Join(assetsDirPath, fName)
			if _, err = f.Seek(0, io.SeekStart); err != nil {
				f.Close()
				return
			}
			if err = writeAssetFile(writePath, f, bt.BoxID); err != nil {
				f.Close()
				return
			}
			f.Close()

			p := "assets/" + fName
			if IsEncryptedBox(bt.BoxID) {
				p += "?box=" + bt.BoxID
			}
			succMap[baseName] = p
			if !IsEncryptedBox(bt.BoxID) {
				cache.SetAssetHash(hash, p) // do not write to the global cache for an encrypted notebook, to avoid cross-boundary dedup pollution
			}
		}
	}
	IncSync()
	return
}

func Upload(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(200, ret)

	form, err := c.MultipartForm()
	if err != nil {
		logging.LogErrorf("insert asset failed: %s", err)
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}
	assetsDirPath := filepath.Join(util.DataDir, "assets")
	var uploadBoxID string // records the upload target boxID, used by writeAssetFile to decide whether encryption is needed
	if nil != form.Value["id"] {
		id := form.Value["id"][0]
		bt := treenode.GetBlockTree(id)
		if nil == bt {
			// If not found in the global block tree, iterate over the opened encrypted notebooks to look for it
			for _, encBoxID := range treenode.GetOpenedEncryptedBoxIDs() {
				if encBT := treenode.GetBlockTreeInBox(id, encBoxID); nil != encBT {
					bt = encBT
					break
				}
			}
		}
		if nil == bt {
			ret.Code = -1
			ret.Msg = Conf.Language(71)
			return
		}
		uploadBoxID = bt.BoxID
		docDirLocalPath := filepath.Join(util.DataDir, bt.BoxID, path.Dir(bt.Path))
		assetsDirPath = getAssetsDir(filepath.Join(util.DataDir, bt.BoxID), docDirLocalPath)
	}

	relAssetsDirPath := "assets"
	if nil != form.Value["assetsDirPath"] {
		relAssetsDirPath = form.Value["assetsDirPath"][0]
		assetsDirPath = filepath.Join(util.DataDir, relAssetsDirPath)
		if !util.IsAbsPathInWorkspace(assetsDirPath) {
			ret.Code = -1
			ret.Msg = "Path [" + assetsDirPath + "] is not in workspace"
			return
		}
		// assetsDirPath may point to an encrypted box (the caller did not pass id); resolve the boxID so filename
		// anonymization / .names.json takes effect
		if pathBox := ExtractBoxIDFromAssetsPath(assetsDirPath); pathBox != "" && IsEncryptedBox(pathBox) {
			uploadBoxID = pathBox
		}
	}
	if !gulu.File.IsExist(assetsDirPath) {
		if err = os.MkdirAll(assetsDirPath, 0755); err != nil {
			ret.Code = -1
			ret.Msg = err.Error()
			return
		}
	}

	var errFiles []string
	succMap := map[string]any{}
	files := form.File["file[]"]
	skipIfDuplicated := false // by default duplicate files are not skipped, but some scenarios need to skip them, e.g. uploading PDF annotation images https://github.com/siyuan-note/siyuan/issues/10666
	if nil != form.Value["skipIfDuplicated"] {
		skipIfDuplicated = "true" == form.Value["skipIfDuplicated"][0]
	}

	for _, file := range files {
		baseName := file.Filename
		_, lastID := util.LastID(baseName)
		if !ast.IsNodeIDPattern(lastID) {
			lastID = ""
		}

		needUnzip2Dir := false
		if gulu.OS.IsDarwin() {
			if strings.HasSuffix(baseName, ".rtfd.zip") {
				needUnzip2Dir = true
			}
		}

		fName := baseName
		fName = util.FilterUploadFileName(fName)
		ext := filepath.Ext(fName)
		fName = strings.TrimSuffix(fName, ext)
		ext = strings.ToLower(ext)
		fName += ext
		f, openErr := file.Open()
		if nil != openErr {
			errFiles = append(errFiles, fName)
			ret.Msg = openErr.Error()
			break
		}
		if needUnzip2Dir && IsEncryptedBox(uploadBoxID) {
			errFiles = append(errFiles, fName)
			ret.Msg = "directory assets are not supported in encrypted notebooks"
			f.Close()
			break
		}

		hash, hashErr := util.GetEtagByHandle(f, file.Size)
		if nil != hashErr {
			errFiles = append(errFiles, fName)
			ret.Msg = err.Error()
			f.Close()
			break
		}

		if 1 > file.Size {
			hash = "random_1_" + gulu.Rand.String(12)
		}

		existAssetPath := GetAssetPathByHash(hash, uploadBoxID)
		if "" != existAssetPath {
			originalName := util.RemoveID(filepath.Base(existAssetPath))
			if strings.ToLower(fName) != strings.ToLower(originalName) {
				hash = "random_2_" + gulu.Rand.String(12)
			}
		}

		if "" != existAssetPath && !strings.HasPrefix(hash, "random_") {
			succMap[baseName] = strings.TrimPrefix(existAssetPath, "/")
			f.Close()
		} else {
			if skipIfDuplicated {
				// No longer insert the image repeatedly when copying a PDF rectangle annotation. No longer upload image repeatedly when copying PDF rectangle annotation https://github.com/siyuan-note/siyuan/issues/10666
				pattern := assetsDirPath + string(os.PathSeparator) + strings.TrimSuffix(fName, ext)
				_, patternLastID := util.LastID(fName)
				if lastID != "" && lastID != patternLastID {
					// The filename was truncated for being too long; match using the previous lastID instead. PDF files with too long file names cannot generate annotated images https://github.com/siyuan-note/siyuan/issues/15739
					pattern = assetsDirPath + string(os.PathSeparator) + "*" + lastID + ext
				} else {
					pattern += "*" + ext
				}

				matches, globErr := filepath.Glob(pattern)
				if nil != globErr {
					logging.LogErrorf("glob failed: %s", globErr)
				} else {
					if 0 < len(matches) {
						fName = filepath.Base(matches[0])
						succMap[baseName] = strings.TrimPrefix(path.Join(relAssetsDirPath, fName), "/")
						f.Close()
						break
					}
				}
			}

			if "" == lastID {
				lastID = ast.NewNodeID()
			}
			if IsEncryptedBox(uploadBoxID) {
				// Encrypted box: the on-disk filename is anonymized to uuid-blockID.ext, and the original name is
				// stored in the encrypted mapping
				fName = encryptedAssetName(util.Ext(fName), lastID)
				// If writing the mapping fails, do not write the asset, to avoid producing an "orphaned ciphertext
				// asset with no mapping" (see design doc section 7)
				if mapErr := writeAssetNameMapping(uploadBoxID, fName, baseName); mapErr != nil {
					errFiles = append(errFiles, fName)
					ret.Msg = mapErr.Error()
					f.Close()
					break
				}
			} else {
				fName = util.AssetName(fName, lastID)
			}
			writePath := filepath.Join(assetsDirPath, fName)
			tmpDir := filepath.Join(util.TempDir, "convert", "zip", gulu.Rand.String(7))
			if needUnzip2Dir {
				if err = os.MkdirAll(tmpDir, 0755); err != nil {
					errFiles = append(errFiles, fName)
					ret.Msg = err.Error()
					f.Close()
					break
				}
				writePath = filepath.Join(tmpDir, fName)
			}

			if _, err = f.Seek(0, io.SeekStart); err != nil {
				logging.LogErrorf("seek failed: %s", err)
				errFiles = append(errFiles, fName)
				ret.Msg = err.Error()
				f.Close()
				break
			}
			if err = writeAssetFile(writePath, f, uploadBoxID); err != nil {
				logging.LogErrorf("write file failed: %s", err)
				errFiles = append(errFiles, fName)
				ret.Msg = err.Error()
				f.Close()
				break
			}
			f.Close()

			if needUnzip2Dir {
				baseName = strings.TrimSuffix(file.Filename, ".rtfd.zip") + ".rtfd"
				fName = baseName
				fName = util.FilterUploadFileName(fName)
				ext = filepath.Ext(fName)
				fName = strings.TrimSuffix(fName, ext)
				ext = strings.ToLower(ext)
				fName += ext
				fName = util.AssetName(fName, ast.NewNodeID())
				tmpDir2 := filepath.Join(util.TempDir, "convert", "zip", gulu.Rand.String(7))
				if err = gulu.Zip.Unzip(writePath, tmpDir2); err != nil {
					errFiles = append(errFiles, fName)
					ret.Msg = err.Error()
					break
				}

				entries, readErr := os.ReadDir(tmpDir2)
				if nil != readErr {
					logging.LogErrorf("read dir [%s] failed: %s", tmpDir2, readErr)
					errFiles = append(errFiles, fName)
					ret.Msg = readErr.Error()
					break
				}
				if 1 > len(entries) {
					logging.LogErrorf("read dir [%s] failed: no entry", tmpDir2)
					errFiles = append(errFiles, fName)
					ret.Msg = "no entry"
					break
				}
				dirName := entries[0].Name()
				srcDir := filepath.Join(tmpDir2, dirName)
				entries, readErr = os.ReadDir(srcDir)
				if nil != readErr {
					logging.LogErrorf("read dir [%s] failed: %s", filepath.Join(tmpDir2, entries[0].Name()), readErr)
					errFiles = append(errFiles, fName)
					ret.Msg = readErr.Error()
					break
				}
				destDir := filepath.Join(assetsDirPath, fName)
				for _, entry := range entries {
					from := filepath.Join(srcDir, entry.Name())
					to := filepath.Join(destDir, entry.Name())
					if copyErr := gulu.File.Copy(from, to); nil != copyErr {
						logging.LogErrorf("copy [%s] to [%s] failed: %s", from, to, copyErr)
						errFiles = append(errFiles, fName)
						ret.Msg = copyErr.Error()
						break
					}
				}
				os.RemoveAll(tmpDir)
				os.RemoveAll(tmpDir2)
			}

			p := strings.TrimPrefix(path.Join(relAssetsDirPath, fName), "/")
			if uploadBoxID != "" && IsEncryptedBox(uploadBoxID) {
				p += "?box=" + uploadBoxID
			}
			succMap[baseName] = p
			if uploadBoxID == "" || !IsEncryptedBox(uploadBoxID) {
				cache.SetAssetHash(hash, p) // do not write to the global cache for an encrypted notebook
			}
		}
	}

	ret.Data = map[string]any{
		"errFiles": errFiles,
		"succMap":  succMap,
	}

	IncSync()
}

func getAssetsDir(boxLocalPath, docDirLocalPath string) (assets string) {
	assets = filepath.Join(docDirLocalPath, "assets")
	if !filelock.IsExist(assets) {
		assets = filepath.Join(boxLocalPath, "assets")
		if !filelock.IsExist(assets) {
			// Falling back to the global data/assets directory is disabled for encrypted notebooks; force use of
			// notebook-level assets to avoid leaking plaintext resources into the global directory
			boxID := filepath.Base(boxLocalPath)
			if IsEncryptedBox(boxID) {
				_ = os.MkdirAll(assets, 0755)
				return
			}
			assets = filepath.Join(util.DataDir, "assets")
		}
	}
	return
}

// writeAssetFile writes the content of src to writePath. The real boxID is resolved from writePath to decide
// whether encryption is needed -- the passed-in boxID is not trusted blindly (the caller may not have passed one,
// or assetsDirPath may point to an encrypted notebook while id is empty).
// An encrypted notebook must already be unlocked (DEK in memory) before writing; if encrypted but not unlocked,
// an error is returned (fail-closed, to avoid writing plaintext to disk).
// A non-encrypted notebook is written directly via the reader (goes through the original
// filelock.WriteFileByReader path, preserving lock semantics).
func writeAssetFile(writePath string, src io.Reader, boxID string) (err error) {
	// Resolve the real boxID from writePath and cross-check it against the passed-in boxID
	pathBoxID := ExtractBoxIDFromAssetsPath(writePath)
	// Both the passed-in boxID and the path's box are non-empty but differ: the path points to another box, reject
	// it (to prevent cross-box writes)
	if boxID != "" && pathBoxID != "" && boxID != pathBoxID {
		return fmt.Errorf("boxID mismatch: param=%s, path=%s", boxID, pathBoxID)
	}
	// The path is not under a box but the passed-in box is encrypted: encrypted content may only be written inside
	// the box, reject writing to global assets
	if pathBoxID == "" && boxID != "" && IsEncryptedBox(boxID) {
		return fmt.Errorf("encrypted box asset must be written inside the box directory, got global path: %s", writePath)
	}
	actualBoxID := pathBoxID
	if actualBoxID == "" {
		actualBoxID = boxID // the path is not under a box (e.g. global assets), fall back to the passed-in value
	}
	if actualBoxID != "" && IsEncryptedBox(actualBoxID) {
		HoldBoxReadLock(actualBoxID)
		defer ReleaseBoxReadLock(actualBoxID)
		dek, dekErr := GetDEKIfUnlocked(actualBoxID)
		if dekErr != nil {
			// The encrypted notebook is not unlocked: reject the write to avoid writing plaintext to disk
			// (defense in depth, see issue #18034)
			return dekErr
		}
		// Unlocked encrypted box: read all -> encrypt -> write to disk
		raw, readErr := io.ReadAll(src)
		if readErr != nil {
			return readErr
		}
		enc, encErr := EncryptAsset(actualBoxID, filepath.Base(writePath), dek, raw)
		if encErr != nil {
			return encErr
		}
		return filelock.WriteFile(writePath, enc)
	}
	return filelock.WriteFileByReader(writePath, src)
}

// StoreAssetForBox is the unified asset write entry point: it decides encrypted vs. plaintext write based on
// boxID, and returns the on-disk filename (without the path prefix).
// Encrypted box: generate an anonymized name -> writeAssetNameMapping records the mapping -> EncryptAsset
// encrypts -> filelock.WriteFile.
// Regular box: util.AssetName generates the name -> filelock.WriteFile writes plaintext.
// When boxID is empty, it is treated as a regular box (written to global assets).
func StoreAssetForBox(boxID, assetDirPath, originalName string, data []byte) (diskName string, err error) {
	return storeAssetForBox(boxID, assetDirPath, originalName, data)
}

// storeAssetForBox is the unified asset write entry point: it decides encrypted vs. plaintext write based on
// boxID, and returns the on-disk filename (without the path prefix).
// Encrypted box: generate an anonymized name -> writeAssetNameMapping records the mapping -> EncryptAsset
// encrypts -> filelock.WriteFile.
// Regular box: util.AssetName generates the name -> filelock.WriteFile writes plaintext.
// When boxID is empty, it is treated as a regular box (written to global assets).
func storeAssetForBox(boxID, assetDirPath, originalName string, data []byte) (diskName string, err error) {
	if IsEncryptedBox(boxID) {
		HoldBoxReadLock(boxID)
		defer ReleaseBoxReadLock(boxID)

		ext := filepath.Ext(originalName)
		blockID := ast.NewNodeID()
		diskName = encryptedAssetName(ext, blockID)
		// If writing the mapping fails, do not write the asset, to avoid producing an "orphaned ciphertext asset
		// with no mapping" (see design doc section 7)
		if mapErr := writeAssetNameMappingLocked(boxID, diskName, originalName); mapErr != nil {
			return "", mapErr
		}

		dek, dekErr := GetDEKIfUnlocked(boxID)
		if dekErr != nil {
			return "", dekErr
		}
		enc, encErr := EncryptAsset(boxID, diskName, dek, data)
		if encErr != nil {
			return "", encErr
		}
		writePath := filepath.Join(assetDirPath, diskName)
		if err = filelock.WriteFile(writePath, enc); err != nil {
			return "", err
		}
		return diskName, nil
	}

	// Regular box: generate a filename with an ID, write plaintext
	diskName = util.AssetName(originalName, ast.NewNodeID())
	writePath := filepath.Join(assetDirPath, diskName)
	if err = filelock.WriteFile(writePath, data); err != nil {
		return "", err
	}
	return diskName, nil
}

// encryptedAssetName generates a meaningless asset filename dedicated to encrypted notebooks: uuid-blockID.ext.
// The original meaningful filename (e.g. "contract.pdf") is stored in the encrypted mapping via
// writeAssetNameMapping, leaving only a random name on disk.
func encryptedAssetName(ext, blockID string) string {
	return gulu.Rand.String(16) + "-" + blockID + ext
}

// assetNameMappingPath returns the path to an encrypted notebook's asset name mapping file: <boxID>/assets/.names.json.
func assetNameMappingPath(boxID string) string {
	return filepath.Join(util.DataDir, boxID, "assets", ".names.json")
}

// assetNameMappingLocks holds mutexes grouped by boxID, protecting the concurrency safety of .names.json
// read-modify-write operations
var assetNameMappingLocks sync.Map // map[string]*sync.Mutex

// writeAssetNameMapping writes a "disk filename -> original filename" mapping into an encrypted notebook's
// .names.json (encrypted with the DEK before writing to disk).
// If it returns an error, the caller must not proceed to write the asset ciphertext, to avoid producing an
// "orphaned ciphertext asset with no mapping" (see design doc section 7).
func writeAssetNameMapping(boxID, diskName, originalName string) error {
	if boxID == "" || !IsEncryptedBox(boxID) {
		return nil
	}
	HoldBoxReadLock(boxID)
	defer ReleaseBoxReadLock(boxID)
	return writeAssetNameMappingLocked(boxID, diskName, originalName)
}

func writeAssetNameMappingLocked(boxID, diskName, originalName string) error {
	muI, _ := assetNameMappingLocks.LoadOrStore(boxID, &sync.Mutex{})
	mu := muI.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	mapping := readAssetNameMappingLocked(boxID)
	mapping[diskName] = originalName
	data, err := json.Marshal(mapping)
	if err != nil {
		return fmt.Errorf("marshal asset name mapping failed: %w", err)
	}
	dek, err := GetDEK(boxID)
	if err != nil || dek == nil {
		return fmt.Errorf("get DEK for asset name mapping failed: %w", err)
	}
	enc, err := EncryptAssetNameMapping(boxID, dek, data)
	if err != nil {
		return fmt.Errorf("encrypt asset name mapping failed: %w", err)
	}
	// Atomic write (temp+rename): prevents a partially-written mapping from lingering, and avoids a lost update
	// from racing with a concurrent writer on the same file
	if err = atomicWriteFile(assetNameMappingPath(boxID), enc); err != nil {
		return fmt.Errorf("write asset name mapping failed: %w", err)
	}
	return nil
}

func removeAssetNameMapping(boxID, diskName string) error {
	if boxID == "" || !IsEncryptedBox(boxID) {
		return nil
	}
	HoldBoxReadLock(boxID)
	defer ReleaseBoxReadLock(boxID)

	muI, _ := assetNameMappingLocks.LoadOrStore(boxID, &sync.Mutex{})
	mu := muI.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	mapping := readAssetNameMappingLocked(boxID)
	if _, exists := mapping[diskName]; !exists {
		return nil
	}
	delete(mapping, diskName)
	data, err := json.Marshal(mapping)
	if err != nil {
		return fmt.Errorf("marshal asset name mapping failed: %w", err)
	}
	dek, err := GetDEK(boxID)
	if err != nil || dek == nil {
		return fmt.Errorf("get DEK for asset name mapping failed: %w", err)
	}
	enc, err := EncryptAssetNameMapping(boxID, dek, data)
	if err != nil {
		return fmt.Errorf("encrypt asset name mapping failed: %w", err)
	}
	if err = atomicWriteFile(assetNameMappingPath(boxID), enc); err != nil {
		return fmt.Errorf("write asset name mapping failed: %w", err)
	}
	return nil
}

// readAssetNameMapping reads an encrypted notebook's asset name mapping (decrypted with the DEK). Returns an
// empty map if not unlocked or the file does not exist.
func readAssetNameMapping(boxID string) map[string]string {
	ret := map[string]string{}
	if boxID == "" || !IsEncryptedBox(boxID) {
		return ret
	}
	HoldBoxReadLock(boxID)
	defer ReleaseBoxReadLock(boxID)
	return readAssetNameMappingLocked(boxID)
}

func readAssetNameMappingLocked(boxID string) map[string]string {
	ret := map[string]string{}
	p := assetNameMappingPath(boxID)
	enc, err := filelock.ReadFile(p)
	if err != nil {
		return ret
	}
	dek, err := GetDEK(boxID)
	if err != nil || dek == nil {
		return ret
	}
	data, err := DecryptAssetNameMapping(boxID, dek, enc)
	if err != nil {
		logging.LogErrorf("decrypt asset name mapping failed: %s", err)
		return ret
	}
	if err = json.Unmarshal(data, &ret); err != nil {
		logging.LogErrorf("unmarshal asset name mapping failed: %s", err)
		return map[string]string{}
	}
	return ret
}

// LookupAssetOriginalName looks up the original filename of an encrypted notebook's asset (used for download
// Content-Disposition and similar display purposes).
// Returns an empty string if not found.
func LookupAssetOriginalName(boxID, diskName string) string {
	return readAssetNameMapping(boxID)[diskName]
}

// LookupAssetOriginalNameLocked looks up the original asset name when the caller already holds the box's read lock.
func LookupAssetOriginalNameLocked(boxID, diskName string) string {
	return readAssetNameMappingLocked(boxID)[diskName]
}
