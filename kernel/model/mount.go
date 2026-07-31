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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/88250/gulu"
	"github.com/88250/lute/ast"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/cache"
	"github.com/siyuan-note/siyuan/kernel/sql"
	"github.com/siyuan-note/siyuan/kernel/task"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func GetBoxByName(name string) (ret *Box) {
	for _, box := range Conf.GetOpenedBoxes() {
		if box.Name == name {
			ret = box
			return
		}
	}
	return
}

func CreateBox(name string) (id string, err error) {
	name = normalizeBoxName(name)
	if 512 < utf8.RuneCountInString(name) {
		// Limit the maximum length of notebook and document names to `512` https://github.com/siyuan-note/siyuan/issues/6299
		err = errors.New(Conf.Language(106))
		return
	}
	FlushTxQueue()

	createDocLock.Lock()
	defer createDocLock.Unlock()

	boxes, _ := ListNotebooks()
	for i, b := range boxes {
		c := b.GetConf()
		c.Sort = i + 1
		if err := b.SaveConf(c); err != nil {
			logging.LogErrorf("save box conf [%s] failed: %s", b.ID, err)
		}
	}

	id = ast.NewNodeID()
	boxLocalPath := filepath.Join(util.DataDir, id)
	err = os.MkdirAll(boxLocalPath, 0755)
	if err != nil {
		return
	}

	box := &Box{ID: id, Name: name}
	boxConf := box.GetConf()
	boxConf.Name = name
	if err := box.SaveConf(boxConf); err != nil {
		logging.LogErrorf("save box conf [%s] failed: %s", id, err)
	}
	if _, err = ensureBoxDoc0(id); err != nil {
		treenode.RemoveBlockTreesByBoxID(id)
		sql.DeleteBoxQueue(id)
		if removeErr := filelock.Remove(boxLocalPath); nil != removeErr {
			logging.LogErrorf("remove box [%s] after initializing box document failed: %s", id, removeErr)
		}
		return "", err
	}
	IncSync()
	logging.LogInfof("created box [%s]", id)
	return
}

func RenameBox(boxID, name string) (err error) {
	box := Conf.Box(boxID)
	if nil == box {
		return errors.New(Conf.Language(0))
	}

	name = normalizeBoxName(name)
	if 512 < utf8.RuneCountInString(name) {
		// Limit the maximum length of notebook and document names to `512` https://github.com/siyuan-note/siyuan/issues/6299
		err = errors.New(Conf.Language(106))
		return
	}

	boxConf := box.GetConf()
	boxConf.Name = name
	box.Name = name
	if err = box.SaveConf(boxConf); err != nil {
		logging.LogErrorf("save box conf [%s] failed: %s", boxID, err)
		return
	}
	if err = renameBoxDoc(boxID, name); err != nil {
		logging.LogErrorf("rename box document [box=%s] failed: %s", boxID, err)
		return
	}
	IncSync()
	logging.LogInfof("renamed box [%s] to [%s]", boxID, name)
	return
}

func normalizeBoxName(name string) string {
	name = normalizeDocTitle(name)
	if "" == name {
		name = normalizeDocTitle(Conf.language(105))
	}
	return name
}

var boxLock = sync.Map{}

func RemoveBox(boxID string) (err error) {
	if _, ok := boxLock.Load(boxID); ok {
		err = errors.New(Conf.language(239))
		return
	}

	boxLock.Store(boxID, true)
	defer boxLock.Delete(boxID)

	if util.IsReservedFilename(boxID) {
		return fmt.Errorf("can not remove [%s] caused by it is a reserved file", boxID)
	}

	FlushTxQueue()
	isUserGuide := IsUserGuide(boxID)
	createDocLock.Lock()
	defer createDocLock.Unlock()

	localPath := filepath.Join(util.DataDir, boxID)
	if !filelock.IsExist(localPath) {
		return
	}
	if !gulu.File.IsDir(localPath) {
		return fmt.Errorf("can not remove [%s] caused by it is not a dir", boxID)
	}

	unmount0(boxID)

	// Cache the encrypted state before removing the dir: once the dir is removed conf.json no longer
	// exists, so IsEncryptedBox would return false
	isEncrypted := IsEncryptedBox(boxID)

	if !isUserGuide {
		var historyDir string
		historyDir, err = getHistoryDir(HistoryOpDelete)
		if err != nil {
			logging.LogErrorf("get history dir failed: %s", err)
			return
		}
		// Back up to the history dir before deletion (the ciphertext is copied as-is; the entire dir of an
		// encrypted notebook stays encrypted)
		p := strings.TrimPrefix(localPath, util.DataDir)
		historyPath := filepath.Join(historyDir, p)
		if err = filelock.Copy(localPath, historyPath); err != nil {
			logging.LogErrorf("gen sync history failed: %s", err)
			return
		}

		// Do not promote an encrypted notebook's assets to the global data/assets, to avoid polluting the
		// global assets with ciphertext or having them picked up by the global index
		if !isEncrypted {
			copyBoxAssetsToDataAssets(boxID)
		}
	}

	// Before removing an encrypted notebook, clean up its export temp dir and revoke any managed
	// download registrations. This must run before filelock.Remove: even if removing the box dir
	// fails and we return early, the export cleanup has already happened, avoiding a fail-open
	// download of plaintext artifacts once IsEncryptedBox starts returning false
	if isEncrypted {
		if rmErr := os.RemoveAll(filepath.Join(util.TempDir, "export", boxID)); rmErr != nil {
			logging.LogWarnf("remove export/[%s] dir failed: %s", boxID, rmErr)
		}
		RevokeManagedEncryptedExportsForBox(boxID)
	}

	if err = filelock.Remove(localPath); err != nil {
		return
	}
	// When removing an encrypted notebook, also clean up its dedicated encrypted db files
	// (including WAL/SHM) to avoid leftovers
	if isEncrypted {
		sql.RemoveEncryptedDBFile(boxID)
		treenode.RemoveEncryptedBlockTreeDBFile(boxID)
	}

	if isUserGuide {
		if avFiles, readAvErr := getUserGuideAVJSONFiles(boxID); nil == readAvErr {
			for _, avName := range avFiles {
				avFilePath := filepath.Join(util.DataDir, "storage", "av", avName)
				if removeErr := filelock.Remove(avFilePath); nil != removeErr {
					logging.LogErrorf("remove av file [%s] failed: %s", avFilePath, removeErr)
				} else {
					logging.LogDebugf("removed av file [%s]", avFilePath)
				}
			}
		}
	}

	IncSync()

	logging.LogInfof("removed box [%s]", boxID)
	return
}

func Unmount(boxID string) {
	FlushTxQueue()

	unmount0(boxID)

	cmdName := "closeBox"
	if IsUserGuide(boxID) {
		if err := RemoveBox(boxID); err == nil {
			cmdName = "removeBox"
		} else {
			logging.LogErrorf("close user guide box [%s] failed, fallback to unmount: %s", boxID, err)
		}
	}
	evt := util.NewCmdResult(cmdName, 0, util.PushModeBroadcast)
	evt.Data = map[string]any{
		"box": boxID,
	}
	util.PushEvent(evt)
	if cmdName == "removeBox" {
		TriggerOnboardingIfEmpty()
	}
}

// clearDEKIfUnlockedEncryptedBox clears the DEK of an encrypted notebook that has been unlocked
// but never mounted. unmount0 calls this when the box isn't mounted (Conf.Box returns nil), which
// covers the case where unlockBox unlocked it but it was locked again before ever being mounted:
// the DEK is still in memory, and if it isn't cleared, the authenticated API could still read
// plaintext after locking.
func clearDEKIfUnlockedEncryptedBox(boxID string) {
	if IsEncryptedBox(boxID) && IsBoxUnlocked(boxID) {
		ClearDEK(boxID)
	}
}

func unmount0(boxID string) {
	box := Conf.Box(boxID)
	if nil == box {
		// The notebook is not mounted (Closed). If it is an unlocked encrypted notebook (DEK is in
		// memory), we still need to call ClearDEK to wipe the leftover key material, otherwise the
		// authenticated API could still read plaintext after locking.
		clearDEKIfUnlockedEncryptedBox(boxID)
		return
	}

	boxConf := box.GetConf()
	boxConf.Closed = true
	if err := box.SaveConf(boxConf); err != nil {
		logging.LogErrorf("save box conf [%s] failed: %s", box.ID, err)
	}
	if IsEncryptedBox(box.ID) {
		// Closing an encrypted notebook: skip Unindex (the index db is about to be deleted anyway, so
		// removing entries one by one would be wasted work). First wait for the tx queue and the SQL
		// index queue to flush (to make sure pending writes are persisted to the encrypted .sy files),
		// generate the file history, then call ClearDEK (= LockBox) to clear the DEK and delete the
		// encrypted db files. The encrypted index can always be fully rebuilt by box.Index(), so
		// deleting the files on close avoids stale index data stacking duplicate rows on the next unlock.
		FlushTxQueue()
		sql.FlushQueue()
		// Generate a file history once before closing: once locked, the timer can no longer generate
		// history for an encrypted notebook (it's not in GetOpenedBoxes)
		GenerateFileHistoryForBox(box)
		ClearDEK(boxID)
	} else {
		box.Unindex()
	}
}

func Mount(boxID string) (alreadyMount bool, err error) {
	if _, ok := boxLock.Load(boxID); ok {
		err = errors.New(Conf.language(239))
		return
	}

	boxLock.Store(boxID, true)
	defer boxLock.Delete(boxID)

	FlushTxQueue()
	isUserGuide := IsUserGuide(boxID)

	localPath := filepath.Join(util.DataDir, boxID)
	var reMountGuide bool
	if isUserGuide {
		// Remount the user guide

		guideBox := Conf.Box(boxID)
		if nil != guideBox {
			unmount0(guideBox.ID)
			reMountGuide = true
		}

		if err = filelock.Remove(localPath); err != nil {
			return
		}

		boxes, _ := ListNotebooks()
		var sort int
		if len(boxes) > 0 {
			sort = boxes[0].Sort - 1
		}

		p := filepath.Join(util.WorkingDir, "guide", boxID)
		if err = filelock.Copy(p, localPath); err != nil {
			return
		}

		// Clear all caches to make sure the data is fresh when the user guide is reopened
		cache.ClearTreeCache()
		cache.ClearDocsIAL()
		cache.ClearBlocksIAL()
		cache.ClearAVCache()

		avDirPath := filepath.Join(util.WorkingDir, "guide", boxID, "storage", "av")
		if filelock.IsExist(avDirPath) {
			if err = filelock.Copy(avDirPath, filepath.Join(util.DataDir, "storage", "av")); err != nil {
				return
			}
		}

		if box := Conf.Box(boxID); nil != box {
			boxConf := box.GetConf()
			boxConf.Closed = true
			boxConf.Sort = sort
			box.SaveConf(boxConf)
		}

		task.AppendAsyncTaskWithDelay(task.PushMsg, 3*time.Second, util.PushErrMsg, Conf.Language(244), 7000)
		go func() {
			// Automatically check for a version update and notify the user every time the user guide is
			// opened https://github.com/siyuan-note/siyuan/issues/5057
			time.Sleep(time.Second * 10)
			CheckUpdate(true)
		}()
	}

	if !gulu.File.IsDir(localPath) {
		return false, errors.New("can not open file, just support open folder only")
	}

	for _, box := range Conf.GetOpenedBoxes() {
		if box.ID == boxID {
			return true, nil
		}
	}

	// An encrypted notebook must have its DEK unlocked via UnlockBox first, otherwise mounting is
	// refused. Mount itself does not accept a password; the frontend flow is: call
	// /api/notebook/unlockBox to unlock, then call openNotebook to mount.
	// IsEncryptedBox is used as the single source of truth for this check (it includes a backup
	// fallback and does not depend on conf integrity).
	if IsEncryptedBox(boxID) && !IsBoxUnlocked(boxID) {
		return false, errors.New("encrypted notebook locked, please unlock it first")
	}

	box := &Box{ID: boxID}
	boxConf := box.GetConf()
	boxConf.Closed = false
	if err := box.SaveConf(boxConf); err != nil {
		logging.LogErrorf("save box conf [%s] failed: %s", boxID, err)
	}
	if _, ensureErr := EnsureBoxDoc(boxID); nil != ensureErr {
		logging.LogErrorf("ensure box document [%s] failed: %s", boxID, ensureErr)
	}

	// Cache the expansion of the root-level document tree
	files, _, _ := ListDocTree(box.ID, "/", util.SortModeUnassigned, false, false, Conf.FileTree.MaxListCount)
	box = Conf.Box(boxID)
	if 0 < len(files) || (nil != box && box.Exist(boxDocPath(box.ID))) {
		box.Index()
	}

	if reMountGuide {
		return true, nil
	}
	return false, nil
}

func IsUserGuide(boxID string) bool {
	return "20210808180117-czj9bvb" == boxID || "20210808180117-6v0mkxr" == boxID || "20211226090932-5lcq56f" == boxID || "20240530133126-axarxgx" == boxID
}

func getUserGuideAVJSONFiles(boxID string) (ret []string, err error) {
	guideAVDirPath := filepath.Join(util.WorkingDir, "guide", boxID, "storage", "av")
	if !filelock.IsExist(guideAVDirPath) {
		logging.LogErrorf("guide av dir [%s] not exist", guideAVDirPath)
		return
	}

	avEntries, err := os.ReadDir(guideAVDirPath)
	if nil != err {
		logging.LogErrorf("read guide av dir [%s] failed: %s", guideAVDirPath, err)
		return
	}

	for _, avEntry := range avEntries {
		avName := avEntry.Name()
		if avEntry.IsDir() || !strings.HasSuffix(avName, ".json") || !ast.IsNodeIDPattern(strings.TrimSuffix(avName, ".json")) {
			continue
		}
		ret = append(ret, avName)
	}
	return
}

func getAllUserGuideAVJSONFiles() (ret []string) {
	guideDirPath := filepath.Join(util.WorkingDir, "guide")
	guideEntries, err := os.ReadDir(guideDirPath)
	if nil != err {
		return
	}

	for _, guideEntry := range guideEntries {
		boxID := guideEntry.Name()
		if !guideEntry.IsDir() || !IsUserGuide(boxID) {
			continue
		}

		avFiles, err := getUserGuideAVJSONFiles(boxID)
		if nil != err {
			continue
		}
		ret = append(ret, avFiles...)
	}
	return
}
