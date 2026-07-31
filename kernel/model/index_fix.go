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
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/88250/gulu"
	"github.com/88250/lute"
	"github.com/88250/lute/ast"
	"github.com/88250/lute/html"
	"github.com/88250/lute/parse"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/filesys"
	"github.com/siyuan-note/siyuan/kernel/sql"
	"github.com/siyuan-note/siyuan/kernel/task"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

var (
	checkIndexOnce = sync.Once{}

	// fixIndexMu ensures checkIndex and AutoFixIndex are mutually exclusive so the same fix pass never runs concurrently.
	fixIndexMu sync.Mutex
	// lastFixedAt records when the last fix completed, used for AutoFixIndex's cooldown check.
	lastFixedAt time.Time
)

const (
	// idleFixThreshold is the idle duration a user must exceed before an idle fix can be triggered.
	idleFixThreshold = 7 * time.Minute
	// fixCooldown is the minimum interval since the last fix before another idle fix is allowed.
	fixCooldown = 120 * time.Minute
)

// checkIndex automatically verifies the database index, running exactly once after data sync completes.
// Index fixing should not be performed before data synchronization https://github.com/siyuan-note/siyuan/issues/10761
func checkIndex() {
	checkIndexOnce.Do(func() {
		if util.IsMobileContainer() {
			// Skip verification on mobile https://ld246.com/article/1734939896061
			return
		}

		// Acquire the lock blockingly: if AutoFixIndex is running, wait for it to finish so this one-time check never
		// runs concurrently with it
		fixIndexMu.Lock()
		defer fixIndexMu.Unlock()

		runFixIndexPipeline()
	})
}

// runFixIndexPipeline runs the index fix pipeline and finishes up (clears the dirty flag, records the fix time).
// The caller must hold fixIndexMu.
func runFixIndexPipeline() {
	fixIndexPipeline()
	// Finish up: clear the dirty flag and record the fix time to avoid AutoFixIndex re-triggering during the cooldown
	util.MarkIndexClean()
	lastFixedAt = time.Now()
}

// fixIndexPipeline runs the index fix pipeline.
// Shared by checkIndex (one-time after sync) and AutoFixIndex (triggered on idle); the caller is responsible for
// holding the fixIndexMu mutex.
func fixIndexPipeline() {
	logging.LogInfof("start fixing index...")

	removeDuplicateDatabaseIndex()
	sql.FlushQueue()

	resetDuplicateBlocksOnFileSys()
	sql.FlushQueue()

	fixBlockTreeByFileSys()
	sql.FlushQueue()

	fixDatabaseIndexByBlockTree()
	sql.FlushQueue()

	removeDuplicateDatabaseRefs()

	// If a task is added later, remember to update the status bar progress push util.PushStatusBar(fmt.Sprintf(Conf.Language(58), 1, 5))

	debug.FreeOSMemory()
	util.PushStatusBar(Conf.Language(185))
	logging.LogInfof("finish fixing index")
}

// AutoFixIndex automatically fixes the index when the user is idle and there are unfixed changes. Called by cron
// every minute.
// Triggering requires all of: idle for at least idleFixThreshold, unfixed changes exist (dirty), and the cooldown
// has elapsed.
func AutoFixIndex() {
	defer logging.Recover()

	if util.IsMobileContainer() {
		return
	}
	if !util.IsIdle(idleFixThreshold) {
		return
	}
	if !util.IsIndexFixDirty() {
		return
	}
	if !lastFixedAt.IsZero() && time.Since(lastFixedAt) < fixCooldown {
		return
	}
	// TryLock is non-blocking: if checkIndex is running or the previous run hasn't finished, skip immediately instead
	// of piling up goroutines
	if !fixIndexMu.TryLock() {
		return
	}
	defer fixIndexMu.Unlock()

	// double-check: after acquiring the lock, verify idleness again to avoid the user resuming activity while we
	// waited for the lock
	if !util.IsIdle(idleFixThreshold) {
		return
	}

	logging.LogInfof("start auto fixing index on idle...")
	runFixIndexPipeline()
	logging.LogInfof("finish auto fixing index on idle")
}

// removeDuplicateDatabaseRefs removes duplicate database reference relations.
func removeDuplicateDatabaseRefs() {
	defer logging.Recover()

	util.PushStatusBar(fmt.Sprintf(Conf.Language(58), 5, 5))
	duplicatedRootIDs := sql.GetRefDuplicatedDefRootIDs()
	for _, rootID := range duplicatedRootIDs {
		refreshRefsByDefID(rootID)
	}

	for _, rootID := range duplicatedRootIDs {
		logging.LogWarnf("exist more than one ref duplicated [%s], reindex it", rootID)
	}
}

// removeDuplicateDatabaseIndex removes duplicate database indexes.
func removeDuplicateDatabaseIndex() {
	defer logging.Recover()

	util.PushStatusBar(fmt.Sprintf(Conf.Language(58), 1, 5))
	duplicatedRootIDs := sql.GetDuplicatedRootIDs("blocks")
	if 1 > len(duplicatedRootIDs) {
		duplicatedRootIDs = sql.GetDuplicatedRootIDs("blocks_fts")
	}

	roots := sql.GetBlocks(duplicatedRootIDs)
	rootMap := map[string]*sql.Block{}
	for _, root := range roots {
		if nil == root {
			continue
		}
		rootMap[root.ID] = root
	}

	var toRemoveRootIDs []string
	var deletes int
	for _, rootID := range duplicatedRootIDs {
		root := rootMap[rootID]
		if nil == root {
			continue
		}
		deletes++
		toRemoveRootIDs = append(toRemoveRootIDs, rootID)
		if util.IsExiting.Load() {
			break
		}
	}
	toRemoveRootIDs = gulu.Str.RemoveDuplicatedElem(toRemoveRootIDs)
	sql.BatchRemoveTreeQueue(toRemoveRootIDs)

	if 0 < deletes {
		logging.LogWarnf("exist more than one tree duplicated [%d], reindex it", deletes)
	}
}

// resetDuplicateBlocksOnFileSys resets blocks with duplicate IDs. https://github.com/siyuan-note/siyuan/issues/7357
func resetDuplicateBlocksOnFileSys() {
	defer logging.Recover()

	util.PushStatusBar(fmt.Sprintf(Conf.Language(58), 2, 5))
	boxes := Conf.GetBoxes()
	luteEngine := lute.New()
	blockIDs := map[string]bool{}
	needRefreshUI := false
	for _, box := range boxes {
		// A closed encrypted notebook can't decrypt .sy files, so skip it (avoid moving ciphertext away as corrupted)
		if IsEncryptedBox(box.ID) && !IsBoxUnlocked(box.ID) {
			continue
		}
		// During index verification, automatically remove the notebook's legacy history folder
		legacyHistory := filepath.Join(util.DataDir, box.ID, ".siyuan", "history")
		if gulu.File.IsDir(legacyHistory) {
			if removeErr := os.RemoveAll(legacyHistory); nil != removeErr {
				logging.LogErrorf("remove legacy history failed: %s", removeErr)
			} else {
				logging.LogInfof("removed legacy history [%s]", legacyHistory)
			}
		}

		boxPath := filepath.Join(util.DataDir, box.ID)
		var duplicatedTrees []*parse.Tree
		filelock.Walk(boxPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil || nil == d {
				return nil
			}

			if d.IsDir() {
				if boxPath == path {
					// Skip the notebook folder
					return nil
				}

				if strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}

				if !ast.IsNodeIDPattern(d.Name()) {
					return nil
				}
				return nil
			}

			if filepath.Ext(path) != ".sy" || strings.Contains(filepath.ToSlash(path), "/assets/") {
				return nil
			}

			if !ast.IsNodeIDPattern(strings.TrimSuffix(d.Name(), ".sy")) {
				logging.LogWarnf("invalid .sy file name [%s]", path)
				box.moveCorruptedData(path)
				return nil
			}

			p := path[len(boxPath):]
			p = filepath.ToSlash(p)
			tree, loadErr := filesys.LoadTree(box.ID, p, luteEngine)
			if nil != loadErr {
				logging.LogErrorf("load tree [%s] failed: %s", p, loadErr)
				return nil
			}

			needOverwrite := false
			ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
				if !entering || !n.IsBlock() {
					return ast.WalkContinue
				}

				if "" == n.ID {
					needOverwrite = true
					treenode.ResetNodeID(n)
					return ast.WalkContinue
				}

				if !blockIDs[n.ID] {
					blockIDs[n.ID] = true
					return ast.WalkContinue
				}

				// A duplicate block ID exists

				if ast.NodeDocument == n.Type {
					// If it's the document root node, reset the whole tree.
					// This can't be reset during iteration, because if the document has child documents, resetting
					// it renames the child document folder, and subsequent iterations could then produce duplicate
					// child document IDs.
					duplicatedTrees = append(duplicatedTrees, tree)
					return ast.WalkStop
				}

				// Otherwise, reset the node ID
				needOverwrite = true
				treenode.ResetNodeID(n)
				needRefreshUI = true
				return ast.WalkContinue
			})

			if needOverwrite {
				logging.LogWarnf("exist more than one node with the same id in tree [%s], reset it", box.ID+p)
				if _, writeErr := filesys.WriteTree(tree); nil != writeErr {
					logging.LogErrorf("write tree [%s] failed: %s", p, writeErr)
				}
			}
			return nil
		})

		for _, tree := range duplicatedTrees {
			absPath := filepath.Join(boxPath, tree.Path)
			logging.LogWarnf("exist more than one tree with the same id [%s], reset it", absPath)
			recreateTree(tree, absPath)
			needRefreshUI = true
		}
	}

	if needRefreshUI {
		util.ReloadUI()
		task.AppendAsyncTaskWithDelay(task.PushMsg, 3*time.Second, util.PushMsg, Conf.Language(190), 5000)
	}
}

func recreateTree(tree *parse.Tree, absPath string) {
	// Remove all block tree data for this tree; fixBlockTreeByFileSys() will be called afterward to fix and rebuild it
	treenode.RemoveBlockTreesByPathPrefix(tree.Box, strings.TrimSuffix(tree.Path, ".sy"))
	treenode.RemoveBlockTreesByRootID(tree.Box, tree.ID)

	resetTree(tree, "", true)
	if _, err := filesys.WriteTree(tree); err != nil {
		logging.LogWarnf("write tree [%s] failed: %s", tree.Path, err)
		return
	}

	if gulu.File.IsDir(strings.TrimSuffix(absPath, ".sy")) {
		// Rename the child document folder
		from := strings.TrimSuffix(absPath, ".sy")
		to := filepath.Join(filepath.Dir(absPath), tree.ID)
		if renameErr := os.Rename(from, to); nil != renameErr {
			logging.LogWarnf("rename [%s] failed: %s", from, renameErr)
			return
		}
	}

	if err := filelock.Remove(absPath); err != nil {
		logging.LogWarnf("remove [%s] failed: %s", absPath, err)
		return
	}
}

// fixBlockTreeByFileSys fixes the block tree using the filesystem.
func fixBlockTreeByFileSys() {
	defer logging.Recover()

	util.PushStatusBar(fmt.Sprintf(Conf.Language(58), 3, 5))
	boxes := Conf.GetOpenedBoxes()
	luteEngine := lute.New()
	for _, box := range boxes {
		boxPath := filepath.Join(util.DataDir, box.ID)
		var paths []string
		filelock.Walk(boxPath, func(path string, d fs.DirEntry, err error) error {
			if nil != err || nil == d {
				return nil
			}

			if boxPath == path {
				// Skip the root path (the notebook folder)
				return nil
			}

			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}

			if filepath.Ext(path) != ".sy" || strings.Contains(filepath.ToSlash(path), "/assets/") {
				return nil
			}

			p := path[len(boxPath):]
			p = filepath.ToSlash(p)
			paths = append(paths, p)
			return nil
		})

		size := len(paths)

		// Clean up redundant data in the block tree
		treenode.ClearRedundantBlockTrees(box.ID, paths)

		// Reindex missing block trees
		missingPaths := treenode.GetNotExistPaths(box.ID, paths)
		for i, p := range missingPaths {
			id := path.Base(p)
			id = strings.TrimSuffix(id, ".sy")
			if !ast.IsNodeIDPattern(id) {
				continue
			}

			reindexTreeByPath(box.ID, p, i, size, luteEngine)
			if util.IsExiting.Load() {
				break
			}
		}

		if util.IsExiting.Load() {
			break
		}
	}

	// Clean up block trees for closed notebooks
	boxes = Conf.GetClosedBoxes()
	for _, box := range boxes {
		treenode.RemoveBlockTreesByBoxID(box.ID)
	}
}

// fixDatabaseIndexByBlockTree fixes the database index using the block tree.
func fixDatabaseIndexByBlockTree() {
	defer logging.Recover()

	util.PushStatusBar(fmt.Sprintf(Conf.Language(58), 4, 5))
	rootUpdatedMap := treenode.GetRootUpdated()
	dbRootUpdatedMap, err := sql.GetRootUpdated()
	if err == nil {
		reindexTreeByUpdated(rootUpdatedMap, dbRootUpdatedMap)
	}
}

func reindexTreeByUpdated(rootUpdatedMap, dbRootUpdatedMap map[string]string) {
	i := -1
	size := len(rootUpdatedMap)
	luteEngine := util.NewLute()
	for rootID, updated := range rootUpdatedMap {
		i++

		if util.IsExiting.Load() {
			break
		}

		rootUpdated := dbRootUpdatedMap[rootID]
		if "" == rootUpdated {
			//logging.LogWarnf("not found tree [%s] in database, reindex it", rootID)
			reindexTree(rootID, i, size, luteEngine)
			continue
		}

		if "" == updated {
			// BlockTree migration: there was no updated field before v2.6.3
			reindexTree(rootID, i, size, luteEngine)
			continue
		}

		btUpdated, _ := time.Parse("20060102150405", updated)
		dbUpdated, _ := time.Parse("20060102150405", rootUpdated)
		if dbUpdated.Before(btUpdated.Add(-10 * time.Minute)) {
			logging.LogWarnf("tree [%s] is not up to date, reindex it", rootID)
			reindexTree(rootID, i, size, luteEngine)
			continue
		}

		if util.IsExiting.Load() {
			break
		}
	}

	var rootIDs []string
	for rootID := range dbRootUpdatedMap {
		if _, ok := rootUpdatedMap[rootID]; !ok {
			rootIDs = append(rootIDs, rootID)
		}

		if util.IsExiting.Load() {
			break
		}
	}
	rootIDs = gulu.Str.RemoveDuplicatedElem(rootIDs)
	roots := map[string]*sql.Block{}
	blocks := sql.GetBlocks(rootIDs)
	for _, block := range blocks {
		roots[block.RootID] = block
	}
	var toRemoveRootIDs []string
	for id, root := range roots {
		if nil == root {
			continue
		}

		toRemoveRootIDs = append(toRemoveRootIDs, id)
		if util.IsExiting.Load() {
			break
		}
	}
	toRemoveRootIDs = gulu.Str.RemoveDuplicatedElem(toRemoveRootIDs)
	//logging.LogWarnf("tree [%s] is not in block tree, remove it from [%s]", id, root.Box)
	sql.BatchRemoveTreeQueue(toRemoveRootIDs)
}

func reindexTreeByPath(box, p string, i, size int, luteEngine *lute.Lute) {
	tree, err := filesys.LoadTree(box, p, luteEngine)
	if err != nil {
		return
	}

	reindexTree0(tree, i, size)
}

func reindexTree(rootID string, i, size int, luteEngine *lute.Lute) {
	root := treenode.GetBlockTree(rootID)
	if nil == root {
		logging.LogWarnf("root block [%s] not found", rootID)
		return
	}

	tree, err := filesys.LoadTree(root.BoxID, root.Path, luteEngine)
	if err != nil {
		if os.IsNotExist(err) {
			// The .sy file was not found on the filesystem, so fix the block tree
			treenode.RemoveBlockTreesByRootID(root.BoxID, rootID)
		}
		return
	}

	reindexTree0(tree, i, size)
}

func reindexTree0(tree *parse.Tree, i, size int) {
	updated := tree.Root.IALAttr("updated")
	if "" == updated {
		updated = util.TimeFromID(tree.Root.ID)
		tree.Root.SetIALAttr("updated", updated)
		indexWriteTreeUpsertQueue(tree)
	} else {
		treenode.UpsertBlockTree(tree)
		sql.IndexTreeQueue(tree)
	}

	if 0 == i%64 {
		util.PushStatusBar(fmt.Sprintf(Conf.Language(183), i, size, html.EscapeString(path.Base(tree.HPath))))
	}
}
