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
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/88250/gulu"
	"github.com/88250/lute"
	"github.com/88250/lute/ast"
	"github.com/88250/lute/editor"
	"github.com/88250/lute/parse"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/av"
	"github.com/siyuan-note/siyuan/kernel/cache"
	"github.com/siyuan-note/siyuan/kernel/filesys"
	"github.com/siyuan-note/siyuan/kernel/sql"
	"github.com/siyuan-note/siyuan/kernel/task"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func IsMoveOutlineHeading(transactions *[]*Transaction) bool {
	for _, tx := range *transactions {
		for _, op := range tx.DoOperations {
			if "moveOutlineHeading" == op.Action {
				return true
			}
		}
	}
	return false
}

func FlushTxQueue() {
	time.Sleep(time.Duration(50) * time.Millisecond)
	for 0 < txQueueSize() || isFlushing.Load() {
		time.Sleep(10 * time.Millisecond)
	}
}

// PerformTxSync synchronously executes a single transaction and returns an error, for use by undo/redo replay.
// Unlike the asynchronously queued PerformTransactions, this holds flushLock directly and runs performTx serially;
// on failure it returns the raw error (rather than converting it into a push message) so the caller can roll back the undo stack state accordingly.
func PerformTxSync(tx *Transaction) (err error) {
	defer logging.Recover()
	// Initialize the transaction mutex (the async path initializes it before enqueueing in PerformTransactions; the sync path fills it in here)
	if nil == tx.m {
		tx.m = &sync.Mutex{}
	}
	flushLock.Lock()
	isFlushing.Store(true)
	defer func() {
		isFlushing.Store(false)
		flushLock.Unlock()
	}()
	if txErr := performTx(tx); nil != txErr {
		return txErr
	}
	return
}

var (
	txQueue     []*Transaction
	txQueueLock sync.Mutex
	flushLock   sync.Mutex
	isFlushing  atomic.Bool
)

func init() {
	go flushQueue()
}

func flushQueue() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		flushLock.Lock()
		isFlushing.Store(true)
		transactions := takeQueuedTransactions()
		for _, tx := range transactions {
			flushTx(tx)
		}
		isFlushing.Store(false)
		flushLock.Unlock()
	}
}

func flushTx(tx *Transaction) {
	defer logging.Recover()

	start := time.Now()
	if txErr := performTx(tx); nil != txErr {
		switch txErr.code {
		case TxErrCodeSkipTx:
			// The operation was already skipped; the message was pushed via PushMsg inside the specific function, so no error state is raised
			return
		case TxErrCodeBlockNotFound, TxErrCodePushMsg:
			pushMsg := txErr.msg
			if pushMsg == "" {
				if TxErrCodeBlockNotFound == txErr.code {
					pushMsg = "Transaction failed: block not found"
				} else {
					pushMsg = "Transaction failed"
				}
			}
			if txErr.id != "" && !strings.Contains(pushMsg, txErr.id) {
				pushMsg += fmt.Sprintf(" [%s]", txErr.id)
			}
			util.PushTxErr(pushMsg, txErr.code, nil)
			return
		case TxErrCodeDataIsSyncing:
			util.PushMsg(Conf.Language(222), 5000)
		case TxErrHandleAttributeView:
			util.PushMsg(Conf.language(258), 5000)
			logging.LogErrorf("handle attribute view failed: %s", txErr.msg)
		default:
			txData, _ := gulu.JSON.MarshalJSON(tx)
			logging.LogFatalf(logging.ExitCodeFatal, "transaction failed [%d]: %s\n  tx [%s]", txErr.code, txErr.msg, txData)
		}
	}
	elapsed := time.Since(start).Milliseconds()
	if 0 < len(tx.DoOperations) {
		if 2000 < elapsed {
			logging.LogWarnf("op tx [%dms]", elapsed)
		}
	}
}

func PerformTransactions(transactions *[]*Transaction) {
	txQueueLock.Lock()
	defer txQueueLock.Unlock()
	for _, tx := range *transactions {
		tx.m = &sync.Mutex{}
		txQueue = append(txQueue, tx)
	}
	sort.SliceStable(txQueue, func(i, j int) bool {
		return txQueue[i].Timestamp < txQueue[j].Timestamp
	})
}

func takeQueuedTransactions() (ret []*Transaction) {
	txQueueLock.Lock()
	defer txQueueLock.Unlock()
	ret = txQueue
	txQueue = nil
	return
}

func txQueueSize() int {
	txQueueLock.Lock()
	defer txQueueLock.Unlock()
	return len(txQueue)
}

const (
	TxErrCodeBlockNotFound   = 0
	TxErrCodeDataIsSyncing   = 1
	TxErrCodeWriteTree       = 2
	TxErrHandleAttributeView = 3
	TxErrCodePushMsg         = 4
	TxErrCodeSkipTx          = 5 // The operation was skipped (e.g. moving across an encryption boundary); a PushMsg has already been shown, so no error state is raised
)

type TxErr struct {
	code int
	msg  string
	id   string
}

// Error implements the error interface, so other packages (e.g. the undo API) can read the transaction error message.
func (e *TxErr) Error() string {
	if "" != e.id {
		return e.msg + " [" + e.id + "]"
	}
	return e.msg
}

// Code returns the transaction error code.
func (e *TxErr) Code() int {
	return e.code
}

func performTx(tx *Transaction) (ret *TxErr) {
	if 1 > len(tx.DoOperations) {
		return
	}

	//os.MkdirAll("pprof", 0755)
	//cpuProfile, _ := os.Create("pprof/cpu_profile_tx")
	//pprof.StartCPUProfile(cpuProfile)
	//defer pprof.StopCPUProfile()

	var err error
	if err = tx.begin(); err != nil {
		if strings.Contains(err.Error(), "database is closed") {
			return
		}
		logging.LogErrorf("begin tx failed: %s", err)
		ret = &TxErr{code: TxErrCodePushMsg, msg: err.Error()}
		return
	}

	defer func() {
		if e := recover(); nil != e {
			msg := fmt.Sprintf("PANIC RECOVERED: %v\n\t%s\n", e, logging.ShortStack())
			logging.LogError(msg)

			state := tx.state.Load()
			if 1 == state {
				tx.rollback()
			}
			ret = txErrFromPanic(state, e)
		}
	}()

	isLargeInsert := tx.processLargeInsert()
	isLargeDelete := tx.processLargeDelete()
	if !isLargeInsert {
		for _, op := range tx.DoOperations {
			if isLargeDelete && "delete" == op.Action {
				continue
			}
			switch op.Action {
			case "create":
				ret = tx.doCreate(op)
			case "restoreCreatedDoc":
				ret = tx.doRestoreCreatedDoc(op)
			case "removeCreatedDoc":
				ret = tx.doRemoveCreatedDoc(op)
			case "update":
				ret = tx.doUpdate(op)
			case "insert":
				ret = tx.doInsert(op)
			case "delete":
				ret = tx.doDelete(op)
			case "move":
				ret = tx.doMove(op)
			case "moveOutlineHeading":
				ret = tx.doMoveOutlineHeading(op)
			case "append":
				ret = tx.doAppend(op)
			case "appendInsert":
				ret = tx.doAppendInsert(op)
			case "prependInsert":
				ret = tx.doPrependInsert(op)
			case "foldHeading":
				ret = tx.doFoldHeading(op)
			case "unfoldHeading":
				ret = tx.doUnfoldHeading(op)
			case "setAttrs":
				ret = tx.doSetAttrs(op)
			case "doUpdateUpdated":
				ret = tx.doUpdateUpdated(op)
			case "addFlashcards":
				ret = tx.doAddFlashcards(op)
			case "removeFlashcards":
				ret = tx.doRemoveFlashcards(op)
			case "setAttrViewName":
				ret = tx.doSetAttrViewName(op)
			case "setAttrViewNewItemTemplates":
				ret = tx.doSetAttrViewNewItemTemplates(op)
			case "setAttrViewFilters":
				ret = tx.doSetAttrViewFilters(op)
			case "setAttrViewSorts":
				ret = tx.doSetAttrViewSorts(op)
			case "setAttrViewPageSize":
				ret = tx.doSetAttrViewPageSize(op)
			case "setAttrViewColWidth":
				ret = tx.doSetAttrViewColumnWidth(op)
			case "setAttrViewColAlign":
				ret = tx.doSetAttrViewColumnAlign(op)
			case "setAttrViewColWrap":
				ret = tx.doSetAttrViewColumnWrap(op)
			case "setAttrViewColHidden":
				ret = tx.doSetAttrViewColumnHidden(op)
			case "setAttrViewColPin":
				ret = tx.doSetAttrViewColumnPin(op)
			case "setAttrViewColIcon":
				ret = tx.doSetAttrViewColumnIcon(op)
			case "setAttrViewColDesc":
				ret = tx.doSetAttrViewColumnDesc(op)
			case "insertAttrViewBlock":
				ret = tx.doInsertAttrViewBlock(op)
			case "removeAttrViewBlock":
				ret = tx.doRemoveAttrViewBlock(op)
			case "addAttrViewCol":
				ret = tx.doAddAttrViewColumn(op)
			case "updateAttrViewCol":
				ret = tx.doUpdateAttrViewColumn(op)
			case "removeAttrViewCol":
				ret = tx.doRemoveAttrViewColumn(op)
			case "sortAttrViewRow":
				ret = tx.doSortAttrViewRow(op)
			case "sortAttrViewCol":
				ret = tx.doSortAttrViewColumn(op)
			case "sortAttrViewKey":
				ret = tx.doSortAttrViewKey(op)
			case "updateAttrViewCell":
				ret = tx.doUpdateAttrViewCell(op)
			case "updateAttrViewColOptions":
				ret = tx.doUpdateAttrViewColOptions(op)
			case "removeAttrViewColOption":
				ret = tx.doRemoveAttrViewColOption(op)
			case "updateAttrViewColOption":
				ret = tx.doUpdateAttrViewColOption(op)
			case "setAttrViewColOptionDesc":
				ret = tx.doSetAttrViewColOptionDesc(op)
			case "setAttrViewColCalc":
				ret = tx.doSetAttrViewColCalc(op)
			case "updateAttrViewColNumberFormat":
				ret = tx.doUpdateAttrViewColNumberFormat(op)
			case "replaceAttrViewBlock":
				ret = tx.doReplaceAttrViewBlock(op)
			case "updateAttrViewColTemplate":
				ret = tx.doUpdateAttrViewColTemplate(op)
			case "addAttrViewView":
				ret = tx.doAddAttrViewView(op)
			case "removeAttrViewView":
				ret = tx.doRemoveAttrViewView(op)
			case "setAttrViewViewName":
				ret = tx.doSetAttrViewViewName(op)
			case "setAttrViewViewIcon":
				ret = tx.doSetAttrViewViewIcon(op)
			case "setAttrViewViewDesc":
				ret = tx.doSetAttrViewViewDesc(op)
			case "duplicateAttrViewView":
				ret = tx.doDuplicateAttrViewView(op)
			case "duplicateAttrViewRow":
				ret = tx.doDuplicateAttrViewRow(op)
			case "sortAttrViewView":
				ret = tx.doSortAttrViewView(op)
			case "updateAttrViewColRelation":
				ret = tx.doUpdateAttrViewColRelation(op)
			case "updateAttrViewColRollup":
				ret = tx.doUpdateAttrViewColRollup(op)
			case "hideAttrViewName":
				ret = tx.doHideAttrViewName(op)
			case "setAttrViewColDateFillCreated":
				ret = tx.doSetAttrViewColDateFillCreated(op)
			case "setAttrViewColDateFillSpecificTime":
				ret = tx.doSetAttrViewColDateFillSpecificTime(op)
			case "setAttrViewCreatedIncludeTime":
				ret = tx.doSetAttrViewCreatedIncludeTime(op)
			case "setAttrViewUpdatedIncludeTime":
				ret = tx.doSetAttrViewUpdatedIncludeTime(op)
			case "duplicateAttrViewKey":
				ret = tx.doDuplicateAttrViewKey(op)
			case "setAttrViewCoverFrom":
				ret = tx.doSetAttrViewCoverFrom(op)
			case "setAttrViewCoverFromAssetKeyID":
				ret = tx.doSetAttrViewCoverFromAssetKeyID(op)
			case "setAttrViewCardSize":
				ret = tx.doSetAttrViewCardSize(op)
			case "setAttrViewFitImage":
				ret = tx.doSetAttrViewFitImage(op)
			case "setAttrViewDisplayFieldName":
				ret = tx.doSetAttrViewDisplayFieldName(op)
			case "setAttrViewFillColBackgroundColor":
				ret = tx.doSetAttrViewFillColBackgroundColor(op)
			case "setAttrViewShowIcon":
				ret = tx.doSetAttrViewShowIcon(op)
			case "setAttrViewWrapField":
				ret = tx.doSetAttrViewWrapField(op)
			case "changeAttrViewLayout":
				ret = tx.doChangeAttrViewLayout(op)
			case "setAttrViewBlockView":
				ret = tx.doSetAttrViewBlockView(op)
			case "setAttrViewCardAspectRatio":
				ret = tx.doSetAttrViewCardAspectRatio(op)
			case "setAttrViewGroup":
				ret = tx.doSetAttrViewGroup(op)
			case "hideAttrViewGroup":
				ret = tx.doHideAttrViewGroup(op)
			case "hideAttrViewAllGroups":
				ret = tx.doHideAttrViewAllGroups(op)
			case "foldAttrViewGroup":
				ret = tx.doFoldAttrViewGroup(op)
			case "syncAttrViewTableColWidth":
				ret = tx.doSyncAttrViewTableColWidth(op)
			case "removeAttrViewGroup":
				ret = tx.doRemoveAttrViewGroup(op)
			case "sortAttrViewGroup":
				ret = tx.doSortAttrViewGroup(op)
			}

			if nil != ret {
				tx.rollback()
				return
			}
		}
	}

	if cr := tx.commit(); nil != cr {
		logging.LogErrorf("commit tx failed: %s", cr)
		if 1 == tx.state.Load() {
			tx.rollback()
		}
		return &TxErr{code: TxErrCodePushMsg, msg: cr.Error()}
	}
	return
}

func txErrFromPanic(state int32, recovered any) *TxErr {
	if 2 == state {
		return nil
	}
	return &TxErr{code: TxErrCodePushMsg, msg: fmt.Sprintf("transaction panic: %v", recovered)}
}

func (tx *Transaction) processLargeDelete() bool {
	opSize := len(tx.DoOperations)
	if 32 > opSize {
		return false
	}

	var deleteOps []*Operation
	for i, op := range tx.DoOperations {
		if "delete" != op.Action {
			if i != opSize-1 {
				return false
			}

			continue
		}

		deleteOps = append(deleteOps, op)
	}

	if 1 > len(deleteOps) {
		return false
	}

	tx.doLargeDelete(deleteOps)
	return true
}

func (tx *Transaction) processLargeInsert() bool {
	opSize := len(tx.DoOperations)
	if 32 > opSize {
		return false
	}

	var insertOps []*Operation
	var firstDeleteOp, lastDeleteOp *Operation
	for i, op := range tx.DoOperations {
		if "insert" != op.Action {
			if 0 != i && i != opSize-1 {
				return false
			}

			if "delete" == op.Action {
				if 0 == i {
					firstDeleteOp = op
				} else {
					lastDeleteOp = op
				}
			}
			continue
		}

		insertOps = append(insertOps, op)
	}

	if 1 > len(insertOps) {
		return false
	}

	if nil != firstDeleteOp {
		tx.doDelete(firstDeleteOp)
	}
	tx.doLargeInsert(insertOps)
	if nil != lastDeleteOp {
		tx.doDelete(lastDeleteOp)
	}
	return true
}

func (tx *Transaction) doMove(operation *Operation) (ret *TxErr) {
	var err error
	id := operation.ID
	srcTree, err := tx.loadTree(id)
	if err != nil {
		logging.LogErrorf("load tree [%s] failed: %s", id, err)
		return &TxErr{code: TxErrCodeBlockNotFound, id: id}
	}

	srcNode := treenode.GetNodeInTree(srcTree, id)
	if nil == srcNode {
		logging.LogErrorf("get node [%s] in tree [%s] failed", id, srcTree.Root.ID)
		return &TxErr{code: TxErrCodeBlockNotFound, id: id}
	}

	// Generate document history https://github.com/siyuan-note/siyuan/issues/14359
	generateOpTypeHistory(srcTree, HistoryOpUpdate)

	var headingChildren []*ast.Node
	if isMovingFoldHeading := ast.NodeHeading == srcNode.Type && "1" == srcNode.IALAttr("fold"); isMovingFoldHeading {
		headingChildren = treenode.HeadingChildren(srcNode)
		// Blocks below other non-folded headings are no longer moved when moving a folded heading https://github.com/siyuan-note/siyuan/issues/8321
		headingChildren = treenode.GetHeadingFold(headingChildren)
	}

	var srcEmptyList *ast.Node
	if ast.NodeListItem == srcNode.Type && srcNode.Parent.FirstChild == srcNode && srcNode.Parent.LastChild == srcNode {
		// After removing the only list item in the list, the list becomes empty
		srcEmptyList = srcNode.Parent
	}

	if nil != operation.Context && "true" == operation.Context["removeFold"] {
		srcNode.RemoveIALAttr("heading-fold")
		srcNode.RemoveIALAttr("fold")
	}

	targetPreviousID := operation.PreviousID
	targetParentID := operation.ParentID
	if "" != targetPreviousID {
		if id == targetPreviousID {
			return
		}

		var targetTree *parse.Tree
		targetTree, err = tx.loadTree(targetPreviousID)
		if err != nil {
			logging.LogErrorf("load tree [%s] failed: %s", targetPreviousID, err)
			return &TxErr{code: TxErrCodeBlockNotFound, id: targetPreviousID}
		}
		isSameTree := srcTree.ID == targetTree.ID
		if isSameTree {
			targetTree = srcTree
		}
		// Forbid moving blocks across an encryption boundary: an encrypted notebook is an isolated island, and moving across boxes would break that isolation (content leaking from A to B)
		if !isSameTree && !IsSameCryptoBoundary(srcTree.Box, targetTree.Box) {
			util.PushMsg(Conf.Language(313), 5000)
			return &TxErr{code: TxErrCodeSkipTx}
		}

		targetNode := treenode.GetNodeInTree(targetTree, targetPreviousID)
		if nil == targetNode {
			logging.LogErrorf("get node [%s] in tree [%s] failed", targetPreviousID, targetTree.Root.ID)
			return &TxErr{code: TxErrCodeBlockNotFound, id: targetPreviousID}
		}

		if ast.NodeHeading == targetNode.Type && "1" == targetNode.IALAttr("fold") {
			targetChildren := treenode.HeadingChildren(targetNode)
			targetChildren = treenode.GetHeadingFold(targetChildren)

			if l := len(targetChildren); 0 < l {
				targetNode = targetChildren[l-1]
			}
		}

		if isMovingFoldHeadingIntoSelf(targetNode, headingChildren) {
			return
		}

		if isMovingParentIntoChild(srcNode, targetNode) {
			return
		}

		if 0 < len(headingChildren) {
			// When a folded heading is edited into an outer list (e.g. by prefixing "* "), the tx sequence from the frontend would form an infinite loop; unwind it here
			// Nested lists cause hang after collapsing headings https://github.com/siyuan-note/siyuan/issues/15943
			lastChild := headingChildren[len(headingChildren)-1]
			if "1" == lastChild.IALAttr("heading-fold") && ast.NodeList == lastChild.Type &&
				nil != lastChild.FirstChild && nil != lastChild.FirstChild.FirstChild && lastChild.FirstChild.FirstChild.ID == targetPreviousID {
				ast.Walk(lastChild, func(n *ast.Node, entering bool) ast.WalkStatus {
					if !entering || !n.IsBlock() {
						return ast.WalkContinue
					}

					n.RemoveIALAttr("heading-fold")
					n.RemoveIALAttr("fold")
					return ast.WalkContinue
				})
				headingChildren = headingChildren[:len(headingChildren)-1]
			}
		}

		for i := len(headingChildren) - 1; -1 < i; i-- {
			c := headingChildren[i]
			targetNode.InsertAfter(c)
		}
		targetNode.InsertAfter(srcNode)
		if nil != srcEmptyList {
			srcEmptyList.Unlink()
		}

		treenode.RefreshUpdated(srcNode)
		tx.nodes[srcNode.ID] = srcNode
		treenode.RefreshUpdated(srcTree.Root)
		tx.writeTree(srcTree)
		if !isSameTree {
			tx.writeTree(targetTree)
			task.AppendAsyncTaskWithDelay(task.SetDefRefCount, util.SQLFlushInterval, refreshRefCount, srcTree.ID)
			task.AppendAsyncTaskWithDelay(task.SetDefRefCount, util.SQLFlushInterval, refreshRefCount, srcNode.ID)
		}
		return
	}

	if id == targetParentID {
		return
	}

	targetTree, err := tx.loadTree(targetParentID)
	if err != nil {
		logging.LogErrorf("load tree [%s] failed: %s", targetParentID, err)
		return &TxErr{code: TxErrCodeBlockNotFound, id: targetParentID}
	}
	isSameTree := srcTree.ID == targetTree.ID
	if isSameTree {
		targetTree = srcTree
	}
	// Forbid moving blocks across an encryption boundary (same as the doMove targetPreviousID branch)
	if !isSameTree && !IsSameCryptoBoundary(srcTree.Box, targetTree.Box) {
		util.PushMsg(Conf.Language(313), 5000)
		return &TxErr{code: TxErrCodeSkipTx}
	}

	targetNode := treenode.GetNodeInTree(targetTree, targetParentID)
	if nil == targetNode {
		logging.LogErrorf("get node [%s] in tree [%s] failed", targetParentID, targetTree.Root.ID)
		return &TxErr{code: TxErrCodeBlockNotFound, id: targetParentID}
	}

	if isMovingFoldHeadingIntoSelf(targetNode, headingChildren) {
		return
	}

	if isMovingParentIntoChild(srcNode, targetNode) {
		return
	}

	processed := false
	if ast.NodeSuperBlock == targetNode.Type {
		// Insert after the layout node
		targetNode = targetNode.FirstChild.Next
		for i := len(headingChildren) - 1; -1 < i; i-- {
			c := headingChildren[i]
			targetNode.InsertAfter(c)
		}
		targetNode.InsertAfter(srcNode)
		if nil != srcEmptyList {
			srcEmptyList.Unlink()
		}
		processed = true
	} else if ast.NodeListItem == targetNode.Type {
		if 3 == targetNode.ListData.Typ {
			// Insert after the task list marker node
			targetNode = targetNode.FirstChild
			for i := len(headingChildren) - 1; -1 < i; i-- {
				c := headingChildren[i]
				targetNode.InsertAfter(c)
			}
			targetNode.InsertAfter(srcNode)
			if nil != srcEmptyList {
				srcEmptyList.Unlink()
			}
			processed = true
		}
	}

	if !processed {
		for i := len(headingChildren) - 1; -1 < i; i-- {
			c := headingChildren[i]
			targetNode.PrependChild(c)
		}

		targetNode.PrependChild(srcNode)
		if nil != srcEmptyList {
			srcEmptyList.Unlink()
		}
	}

	treenode.RefreshUpdated(srcNode)
	tx.nodes[srcNode.ID] = srcNode
	treenode.RefreshUpdated(srcTree.Root)
	tx.writeTree(srcTree)
	if !isSameTree {
		tx.writeTree(targetTree)
		task.AppendAsyncTaskWithDelay(task.SetDefRefCount, util.SQLFlushInterval, refreshRefCount, srcTree.ID)
		task.AppendAsyncTaskWithDelay(task.SetDefRefCount, util.SQLFlushInterval, refreshRefCount, srcNode.ID)
	}
	return
}

func isMovingFoldHeadingIntoSelf(targetNode *ast.Node, headingChildren []*ast.Node) bool {
	for _, headingChild := range headingChildren {
		if headingChild.ID == targetNode.ID {
			// A folded heading cannot be moved to before or after a node beneath itself https://github.com/siyuan-note/siyuan/issues/7163
			return true
		}
	}
	return false
}

func isMovingParentIntoChild(srcNode, targetNode *ast.Node) bool {
	for parent := targetNode.Parent; nil != parent; parent = parent.Parent {
		if parent.ID == srcNode.ID {
			return true
		}
	}
	return false
}

func (tx *Transaction) doPrependInsert(operation *Operation) (ret *TxErr) {
	var err error
	block := treenode.GetBlockTree(operation.ParentID)
	if nil == block {
		logging.LogWarnf("not found block [%s]", operation.ParentID)
		util.ReloadUI() // e.g. after splitting the view the editor state can become inconsistent; force a UI reload here
		return
	}
	tree, err := tx.loadTree(block.ID)
	if err != nil {
		msg := fmt.Sprintf("load tree [%s] failed: %s", block.ID, err)
		logging.LogError(msg)
		return &TxErr{code: TxErrCodeBlockNotFound, id: block.ID}
	}

	data := strings.ReplaceAll(operation.Data.(string), editor.FrontEndCaret, "")
	subTree := tx.luteEngine.BlockDOM2Tree(data)
	// Fallback safety check: forbid block refs that cross an encryption boundary (paste/drag/direct API calls may carry cross-boundary refs)
	// subTree.Box has not been set yet at this point, so use the target tree's box as srcBox
	degradeCrossBoundaryBlockRefs(subTree.Root, tree.Box)
	insertedNode := subTree.Root.FirstChild
	if nil == insertedNode {
		return &TxErr{code: TxErrCodeBlockNotFound, msg: "invalid data tree", id: block.ID}
	}
	if "" == insertedNode.ID {
		insertedNode.ID = ast.NewNodeID()
		insertedNode.SetIALAttr("id", insertedNode.ID)
	}
	var toInserts []*ast.Node
	for toInsert := insertedNode; nil != toInsert; toInsert = toInsert.Next {
		if ast.NodeKramdownBlockIAL != toInsert.Type {
			if "" == toInsert.ID {
				toInsert.ID = ast.NewNodeID()
				toInsert.SetIALAttr("id", toInsert.ID)
			}
			toInserts = append(toInserts, toInsert)
		}
	}

	node := treenode.GetNodeInTree(tree, operation.ParentID)
	if nil == node {
		logging.LogErrorf("get node [%s] in tree [%s] failed", operation.ParentID, tree.Root.ID)
		return &TxErr{code: TxErrCodeBlockNotFound, id: operation.ParentID}
	}
	isContainer := node.IsContainerBlock()
	slices.Reverse(toInserts)

	for _, toInsert := range toInserts {
		if isContainer {
			if ast.NodeList == node.Type {
				// A list can only contain list items as children, so this needs special handling
				if ast.NodeList == toInsert.Type {
					var childLis []*ast.Node
					for childLi := toInsert.FirstChild; nil != childLi; childLi = childLi.Next {
						childLis = append(childLis, childLi)
					}
					for i := len(childLis) - 1; -1 < i; i-- {
						node.PrependChild(childLis[i])
					}
				} else {
					newLiID := ast.NewNodeID()
					newLi := &ast.Node{ID: newLiID, Type: ast.NodeListItem, ListData: &ast.ListData{Typ: node.ListData.Typ}}
					newLi.SetIALAttr("id", newLiID)
					node.PrependChild(newLi)
					newLi.AppendChild(toInsert)
				}
			} else if ast.NodeSuperBlock == node.Type {
				layout := node.ChildByType(ast.NodeSuperBlockLayoutMarker)
				if nil != layout {
					layout.InsertAfter(toInsert)
				} else {
					node.FirstChild.InsertAfter(toInsert)
				}
			} else {
				node.PrependChild(toInsert)
			}
		} else {
			node.InsertAfter(toInsert)
		}

		treenode.CreatedUpdated(toInsert)
		tx.nodes[toInsert.ID] = toInsert
	}

	treenode.CreatedUpdated(insertedNode)
	tx.nodes[insertedNode.ID] = insertedNode
	tx.writeTree(tree)

	operation.ID = insertedNode.ID
	operation.ParentID = insertedNode.Parent.ID

	// Convert prependInsert into insert before pushing
	operation.Action = "insert"
	if nil != insertedNode.Previous {
		operation.PreviousID = insertedNode.Previous.ID
	}
	return
}

func (tx *Transaction) doAppendInsert(operation *Operation) (ret *TxErr) {
	var err error
	block := treenode.GetBlockTree(operation.ParentID)
	if nil == block {
		logging.LogWarnf("not found block [%s]", operation.ParentID)
		util.ReloadUI() // e.g. after splitting the view the editor state can become inconsistent; force a UI reload here
		return
	}
	tree, err := tx.loadTree(block.ID)
	if err != nil {
		msg := fmt.Sprintf("load tree [%s] failed: %s", block.ID, err)
		logging.LogError(msg)
		return &TxErr{code: TxErrCodeBlockNotFound, id: block.ID}
	}

	data := strings.ReplaceAll(operation.Data.(string), editor.FrontEndCaret, "")
	subTree := tx.luteEngine.BlockDOM2Tree(data)
	// Fallback safety check: forbid block refs that cross an encryption boundary (paste/drag/direct API calls may carry cross-boundary refs)
	// subTree.Box has not been set yet at this point, so use the target tree's box as srcBox
	degradeCrossBoundaryBlockRefs(subTree.Root, tree.Box)
	insertedNode := subTree.Root.FirstChild
	if nil == insertedNode {
		return &TxErr{code: TxErrCodeBlockNotFound, msg: "invalid data tree", id: block.ID}
	}
	if "" == insertedNode.ID {
		insertedNode.ID = ast.NewNodeID()
		insertedNode.SetIALAttr("id", insertedNode.ID)
	}
	var toInserts []*ast.Node
	for toInsert := insertedNode; nil != toInsert; toInsert = toInsert.Next {
		if ast.NodeKramdownBlockIAL != toInsert.Type {
			if "" == toInsert.ID {
				toInsert.ID = ast.NewNodeID()
				toInsert.SetIALAttr("id", toInsert.ID)
			}
			toInserts = append(toInserts, toInsert)
		}
	}

	node := treenode.GetNodeInTree(tree, operation.ParentID)
	if nil == node {
		logging.LogErrorf("get node [%s] in tree [%s] failed", operation.ParentID, tree.Root.ID)
		return &TxErr{code: TxErrCodeBlockNotFound, id: operation.ParentID}
	}
	isContainer := node.IsContainerBlock()
	if !isContainer {
		slices.Reverse(toInserts)
	}
	var lastChildBelowHeading *ast.Node
	if ast.NodeHeading == node.Type {
		if children := treenode.HeadingChildren(node); 0 < len(children) {
			lastChildBelowHeading = children[len(children)-1]
		}
	}

	for _, toInsert := range toInserts {
		if isContainer {
			if ast.NodeList == node.Type {
				// A list can only contain list items as children, so this needs special handling https://github.com/siyuan-note/siyuan/issues/9955
				if ast.NodeList == toInsert.Type {
					var childLis []*ast.Node
					for childLi := toInsert.FirstChild; nil != childLi; childLi = childLi.Next {
						childLis = append(childLis, childLi)
					}
					for _, childLi := range childLis {
						node.AppendChild(childLi)
					}
				} else {
					newLiID := ast.NewNodeID()
					newLi := &ast.Node{ID: newLiID, Type: ast.NodeListItem, ListData: &ast.ListData{Typ: node.ListData.Typ}}
					newLi.SetIALAttr("id", newLiID)
					node.AppendChild(newLi)
					newLi.AppendChild(toInsert)
				}
			} else if ast.NodeSuperBlock == node.Type {
				node.LastChild.InsertBefore(toInsert)
			} else {
				node.AppendChild(toInsert)
			}
		} else {
			if ast.NodeHeading == node.Type {
				if nil != lastChildBelowHeading {
					lastChildBelowHeading.InsertAfter(toInsert)
				} else {
					node.InsertAfter(toInsert)
				}
			} else {
				node.InsertAfter(toInsert)
			}
		}

		treenode.CreatedUpdated(toInsert)
		tx.nodes[toInsert.ID] = toInsert
	}

	treenode.CreatedUpdated(insertedNode)
	tx.nodes[insertedNode.ID] = insertedNode
	tx.writeTree(tree)

	operation.ID = insertedNode.ID
	operation.ParentID = insertedNode.Parent.ID

	// Convert appendInsert into insert before pushing
	operation.Action = "insert"
	if nil != insertedNode.Previous {
		operation.PreviousID = insertedNode.Previous.ID
	}
	return
}

func (tx *Transaction) doAppend(operation *Operation) (ret *TxErr) {
	var err error
	id := operation.ID
	srcTree, err := tx.loadTree(id)
	if err != nil {
		logging.LogErrorf("load tree [%s] failed: %s", id, err)
		return &TxErr{code: TxErrCodeBlockNotFound, id: id}
	}

	srcNode := treenode.GetNodeInTree(srcTree, id)
	if nil == srcNode {
		logging.LogErrorf("get node [%s] in tree [%s] failed", id, srcTree.Root.ID)
		return &TxErr{code: TxErrCodeBlockNotFound, id: id}
	}

	if ast.NodeDocument == srcNode.Type {
		logging.LogWarnf("can't append a root to another root")
		return
	}

	var headingChildren []*ast.Node
	if isMovingFoldHeading := ast.NodeHeading == srcNode.Type && "1" == srcNode.IALAttr("fold"); isMovingFoldHeading {
		headingChildren = treenode.HeadingChildren(srcNode)
	}
	var srcEmptyList, targetNewList *ast.Node
	if ast.NodeListItem == srcNode.Type {
		targetNewListID := ast.NewNodeID()
		targetNewList = &ast.Node{ID: targetNewListID, Type: ast.NodeList, ListData: &ast.ListData{Typ: srcNode.ListData.Typ}}
		targetNewList.SetIALAttr("id", targetNewListID)
		if srcNode.Parent.FirstChild == srcNode && srcNode.Parent.LastChild == srcNode {
			// After removing the only list item in the list, the list becomes empty
			srcEmptyList = srcNode.Parent
		}
	}

	targetRootID := operation.ParentID
	if id == targetRootID {
		logging.LogWarnf("target root id is nil")
		return
	}

	targetTree, err := tx.loadTree(targetRootID)
	if err != nil {
		logging.LogErrorf("load tree [%s] failed: %s", targetRootID, err)
		return &TxErr{code: TxErrCodeBlockNotFound, id: targetRootID}
	}
	isSameTree := srcTree.ID == targetTree.ID
	if isSameTree {
		targetTree = srcTree
	}
	// Forbid inserting blocks across an encryption boundary (same guard as doMove)
	if !isSameTree && !IsSameCryptoBoundary(srcTree.Box, targetTree.Box) {
		util.PushMsg(Conf.Language(313), 5000)
		return &TxErr{code: TxErrCodeSkipTx}
	}

	targetRoot := targetTree.Root
	if nil != targetNewList {
		if nil != targetRoot.LastChild {
			if ast.NodeList != targetRoot.LastChild.Type {
				targetNewList.AppendChild(srcNode)
				targetRoot.AppendChild(targetNewList)
			} else {
				targetRoot.LastChild.AppendChild(srcNode)
			}
		} else {
			targetRoot.AppendChild(srcNode)
		}
	} else {
		targetRoot.AppendChild(srcNode)
	}
	for _, c := range headingChildren {
		targetRoot.AppendChild(c)
	}
	if nil != srcEmptyList {
		srcEmptyList.Unlink()
	}

	tx.writeTree(srcTree)
	if !isSameTree {
		tx.writeTree(targetTree)
	}
	return
}

func (tx *Transaction) doLargeDelete(operations []*Operation) {
	tree, err := tx.loadTree(operations[0].ID)
	if err != nil {
		logging.LogErrorf("load tree [%s] failed: %s", operations[0].ID, err)
		return
	}

	var ids []string
	for _, operation := range operations {
		deletedNode := tx.doDelete0(operation, tree)
		ids = append(ids, deletedNode.BlockIDs()...)
	}
	ids = gulu.Str.RemoveDuplicatedElem(ids)
	treenode.RemoveBlockTreesByIDs(tree.Box, ids)
	tx.writeTree(tree)
}

func (tx *Transaction) doDelete(operation *Operation) (ret *TxErr) {
	var err error
	id := operation.ID
	tree, err := tx.loadTree(id)
	if err != nil {
		if errors.Is(err, ErrBlockNotFound) {
			// After a move this can be empty here, which is treated as normal
			return
		}

		msg := fmt.Sprintf("load tree [%s] failed: %s", id, err)
		logging.LogError(msg)
		return &TxErr{code: TxErrCodeBlockNotFound, id: id}
	}

	deletedNode := tx.doDelete0(operation, tree)
	if nil == deletedNode {
		return
	}
	// Synchronously clean up the index entries for the deleted container block and its children; otherwise, after deleting a container block such as a list or super block, its children would still exist and ExistBlockTree would still return true
	// Improve editor state synchronization when deleting blocks https://github.com/siyuan-note/siyuan/issues/17742
	deletedIDs := deletedNode.BlockIDs()
	treenode.RemoveBlockTreesByIDs(tree.Box, deletedIDs)
	tx.writeTree(tree)
	return
}

func (tx *Transaction) doDelete0(operation *Operation, tree *parse.Tree) (deletedNode *ast.Node) {
	node := treenode.GetNodeInTree(tree, operation.ID)
	if nil == node {
		return // This happens after a move: an abnormal state caused by moving a list item https://github.com/siyuan-note/insider/issues/961
	}

	// Collect the IDs of the referenced definition blocks
	refDefIDs := getRefDefIDs(node)
	// Push the ref count update for the definition node
	for _, defID := range refDefIDs {
		task.AppendAsyncTaskWithDelay(task.SetDefRefCount, util.SQLFlushInterval, refreshRefCount, defID)
	}
	// After deleting a referenced block, the ref count of its containing document must be refreshed; otherwise the document-level count badge on the source document won't update
	task.AppendAsyncTaskWithDelay(task.SetDefRefCount, util.SQLFlushInterval, refreshRefCount, tree.Root.ID)

	parent := node.Parent
	if nil != node.Next && ast.NodeKramdownBlockIAL == node.Next.Type && bytes.Contains(node.Next.Tokens, []byte(node.ID)) {
		// Abnormal undo state for list blocks https://github.com/siyuan-note/siyuan/issues/3985
		node.Next.Unlink()
	}

	node.Unlink()

	if nil != parent && ast.NodeListItem == parent.Type && nil == parent.FirstChild {
		needAppendEmptyListItem := true
		for _, op := range tx.DoOperations {
			if "insert" == op.Action && op.ParentID == parent.ID {
				needAppendEmptyListItem = false
				break
			}
		}

		if needAppendEmptyListItem {
			parent.AppendChild(treenode.NewParagraph(ast.NewNodeID()))
		}
	}

	delete(tx.nodes, node.ID)

	// If this is a delete-list-item transaction from unwrapping a list, there's no need to remove the database-bound block, because the unwrap-list transaction will re-insert a list item with the same ID afterward
	// List item disconnection no longer affects database binding blocks https://github.com/siyuan-note/siyuan/issues/12235
	needSyncDel2AvBlock := true
	if ast.NodeListItem == node.Type {
		for _, op := range tx.DoOperations {
			// It's impossible for the same ID to be inserted and then deleted; only delete-then-insert can happen, so we only need to look for the insert operation here
			if "insert" == op.Action {
				data := strings.ReplaceAll(op.Data.(string), editor.FrontEndCaret, "")
				subTree := tx.luteEngine.BlockDOM2Tree(data)
				ast.Walk(subTree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
					if !entering || ast.NodeListItem != n.Type {
						return ast.WalkContinue
					}

					if n.ID == operation.ID {
						needSyncDel2AvBlock = false
						return ast.WalkStop
					}
					return ast.WalkContinue
				})

				break
			}
		}
	}

	if needSyncDel2AvBlock {
		syncDelete2AvBlock(node, tree, true, tx)
	}

	deletedNode = node
	return
}

func syncDelete2AvBlock(node *ast.Node, nodeTree *parse.Tree, delChildrenWhenDelParent bool, tx *Transaction) {
	changedAvIDs := syncDelete2AttributeView(node, delChildrenWhenDelParent)
	avIDs := tx.syncDelete2Block(node, nodeTree)
	changedAvIDs = append(changedAvIDs, avIDs...)
	changedAvIDs = gulu.Str.RemoveDuplicatedElem(changedAvIDs)

	for _, avID := range changedAvIDs {
		ReloadAttrView(avID)
	}
}

func (tx *Transaction) syncDelete2Block(node *ast.Node, nodeTree *parse.Tree) (changedAvIDs []string) {
	ast.Walk(node, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering || ast.NodeAttributeView != n.Type {
			return ast.WalkContinue
		}

		avID := n.AttributeViewID
		isMirror := av.IsMirror(avID)
		if changed := av.RemoveBlockRel(avID, n.ID, treenode.ExistBlockTree); changed {
			changedAvIDs = append(changedAvIDs, avID)
		}

		if isMirror {
			// After deleting a mirrored database node there's no need to unbind the block, because other mirror nodes are still using it
			return ast.WalkContinue
		}

		attrView, err := av.ParseAttributeView(avID)
		if err != nil {
			return ast.WalkContinue
		}

		trees, nodes := tx.getAttrViewBoundNodes(attrView)
		for _, toChangNode := range nodes {
			avs := toChangNode.IALAttr(av.NodeAttrNameAvs)
			if "" != avs {
				avIDs := strings.Split(avs, ",")
				avIDs = gulu.Str.RemoveElem(avIDs, avID)
				if 1 > len(avIDs) {
					toChangNode.RemoveIALAttr(av.NodeAttrNameAvs)
				} else {
					toChangNode.SetIALAttr(av.NodeAttrNameAvs, strings.Join(avIDs, ","))
				}
			}
			avNames := getAvNames(toChangNode.IALAttr(av.NodeAttrNameAvs))
			oldAttrs := parse.IAL2Map(toChangNode.KramdownIAL)
			toChangNode.SetIALAttr(av.NodeAttrViewNames, avNames)
			pushBlockAttrs(oldAttrs, toChangNode)
		}

		for _, tree := range trees {
			if nodeTree.ID != tree.ID {
				indexWriteTreeUpsertQueue(tree)
			}
		}
		return ast.WalkContinue
	})

	changedAvIDs = gulu.Str.RemoveDuplicatedElem(changedAvIDs)
	return
}

func syncDelete2AttributeView(node *ast.Node, delChildrenWhenDelParent bool) (changedAvIDs []string) {
	if !delChildrenWhenDelParent {
		changedAvIDs = deleteAttrView(node, changedAvIDs)
		return
	}

	ast.Walk(node, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering || !n.IsBlock() {
			return ast.WalkContinue
		}

		changedAvIDs = append(changedAvIDs, deleteAttrView(n, changedAvIDs)...)
		return ast.WalkContinue
	})

	changedAvIDs = gulu.Str.RemoveDuplicatedElem(changedAvIDs)
	return
}

func deleteAttrView(n *ast.Node, changedAvIDs []string) []string {
	avs := n.IALAttr(av.NodeAttrNameAvs)
	if "" == avs {
		return nil
	}

	avIDs := strings.SplitSeq(avs, ",")
	for avID := range avIDs {
		attrView, parseErr := av.ParseAttributeView(avID)
		if nil != parseErr {
			continue
		}

		changedAv := false
		blockValues := attrView.GetBlockKeyValues()
		if nil == blockValues {
			continue
		}

		for i, blockValue := range blockValues.Values {
			if nil == blockValue.Block {
				continue
			}

			if blockValue.Block.ID == n.ID {
				blockValues.Values = append(blockValues.Values[:i], blockValues.Values[i+1:]...)
				changedAv = true
				break
			}
		}

		if changedAv {
			regenAttrViewGroups(attrView)
			av.SaveAttributeView(attrView)
			changedAvIDs = append(changedAvIDs, avID)
		}
	}
	return changedAvIDs
}

func (tx *Transaction) doLargeInsert(operations []*Operation) {
	tree, _ := tx.loadTree(operations[0].ID)
	if nil == tree {
		tree, _ = tx.loadTree(operations[0].PreviousID)
		if nil == tree {
			tree, _ = tx.loadTree(operations[0].ParentID)
		}
		if nil == tree {
			tree, _ = tx.loadTree(operations[0].NextID)
		}
	}

	if nil == tree {
		logging.LogErrorf("load tree [%s] failed", operations[0].ID)
		return
	}

	for _, operation := range operations {
		if txErr := tx.doInsert0(operation, tree); nil != txErr {
			return
		}
	}

	tx.writeTree(tree)
}

func (tx *Transaction) doInsert(operation *Operation) (ret *TxErr) {
	var bt *treenode.BlockTree
	bts := treenode.GetBlockTrees([]string{operation.ParentID, operation.PreviousID, operation.NextID})
	for _, b := range bts {
		if "" != b.ID {
			bt = b
			break
		}
	}
	if nil == bt {
		// When it can't be found in the global block tree, search through the opened encrypted notebooks
		for _, encBoxID := range treenode.GetOpenedEncryptedBoxIDs() {
			encBTs := treenode.GetBlockTreesInBox([]string{operation.ParentID, operation.PreviousID, operation.NextID}, encBoxID)
			for _, b := range encBTs {
				if "" != b.ID {
					bt = b
					break
				}
			}
			if nil != bt {
				break
			}
		}
	}
	if nil == bt {
		logging.LogWarnf("not found block tree [%s, %s, %s]", operation.ParentID, operation.PreviousID, operation.NextID)
		util.ReloadUI() // e.g. after splitting the view the editor state can become inconsistent; force a UI reload here
		return
	}

	var err error
	tree, err := tx.loadTreeByBlockTree(bt)
	if err != nil {
		msg := fmt.Sprintf("load tree [%s] failed: %s", bt.ID, err)
		logging.LogError(msg)
		return &TxErr{code: TxErrCodeBlockNotFound, id: bt.ID}
	}

	if ret = tx.doInsert0(operation, tree); nil != ret {
		return
	}
	tx.writeTree(tree)
	return
}

func (tx *Transaction) doInsert0(operation *Operation, tree *parse.Tree) (ret *TxErr) {
	data := strings.ReplaceAll(operation.Data.(string), editor.FrontEndCaret, "")
	subTree := tx.luteEngine.BlockDOM2Tree(data)
	subTree.Box, subTree.Path = tree.Box, tree.Path
	tx.processGlobalAssets(subTree)
	// Fallback safety check: forbid block refs that cross an encryption boundary (paste/drag/direct API calls may carry cross-boundary refs)
	degradeCrossBoundaryBlockRefs(subTree.Root, subTree.Box)

	insertedNode := subTree.Root.FirstChild
	if nil == insertedNode {
		logging.LogErrorf("invalid data tree: insert op id[%s] parent[%s] previous[%s] next[%s] root[%s]",
			operation.ID, operation.ParentID, operation.PreviousID, operation.NextID, tree.Root.ID)
		return &TxErr{code: TxErrCodeBlockNotFound, msg: "invalid data tree"}
	}
	var remains []*ast.Node
	for remain := insertedNode.Next; nil != remain; remain = remain.Next {
		if ast.NodeKramdownBlockIAL != remain.Type {
			if "" == remain.ID {
				remain.ID = ast.NewNodeID()
				remain.SetIALAttr("id", remain.ID)
			}
			remains = append(remains, remain)
		}
	}
	if !ast.IsNodeIDPattern(insertedNode.ID) {
		insertedNode.ID = ast.NewNodeID()
		insertedNode.SetIALAttr("id", insertedNode.ID)
	}
	if ast.NodeAttributeView == insertedNode.Type {
		if !ast.IsNodeIDPattern(insertedNode.AttributeViewID) {
			insertedNode.AttributeViewID = ast.NewNodeID()
		}
	}

	var node *ast.Node
	nextID := operation.NextID
	previousID := operation.PreviousID
	if "" != nextID {
		node = treenode.GetNodeInTree(tree, nextID)
		if nil == node {
			logging.LogErrorf("get node [%s] in tree [%s] failed", nextID, tree.Root.ID)
			return &TxErr{code: TxErrCodeBlockNotFound, id: nextID}
		}

		if ast.NodeList == insertedNode.Type && nil != node.Parent && ast.NodeList == node.Parent.Type {
			insertedNode = insertedNode.FirstChild
		}
		node.InsertBefore(insertedNode)
		for _, remain := range remains {
			node.InsertBefore(remain)
		}
	} else if "" != previousID {
		node = treenode.GetNodeInTree(tree, previousID)
		if nil == node {
			logging.LogErrorf("get node [%s] in tree [%s] failed", previousID, tree.Root.ID)
			return &TxErr{code: TxErrCodeBlockNotFound, id: previousID}
		}

		if ast.NodeHeading == node.Type && "1" == node.IALAttr("fold") {
			children := treenode.HeadingChildren(node)
			if l := len(children); 0 < l {
				node = children[l-1]
			}
		}
		if ast.NodeList == insertedNode.Type && nil != node.Parent && ast.NodeList == node.Parent.Type {
			insertedNode = insertedNode.FirstChild
		}
		for i := len(remains) - 1; 0 <= i; i-- {
			remain := remains[i]
			node.InsertAfter(remain)
		}
		node.InsertAfter(insertedNode)
	} else {
		node = treenode.GetNodeInTree(tree, operation.ParentID)
		if nil == node {
			logging.LogErrorf("get node [%s] in tree [%s] failed", operation.ParentID, tree.Root.ID)
			return &TxErr{code: TxErrCodeBlockNotFound, id: operation.ParentID}
		}
		if ast.NodeSuperBlock == node.Type {
			// Insert after the layout node
			node.FirstChild.Next.InsertAfter(insertedNode)
		} else {
			if ast.NodeList == insertedNode.Type && nil != insertedNode.FirstChild && operation.ID == insertedNode.FirstChild.ID && operation.ID != insertedNode.ID {
				// When moving a list item to become the first item of another list https://github.com/siyuan-note/siyuan/issues/2341
				insertedNode = insertedNode.FirstChild
			}

			if ast.NodeListItem == node.Type && 3 == node.ListData.Typ {
				// Insert after the task list marker node
				node.FirstChild.InsertAfter(insertedNode)
				for _, remain := range remains {
					node.FirstChild.InsertAfter(remain)
				}
			} else {
				if !node.IsContainerBlock() {
					for i := len(remains) - 1; 0 <= i; i-- {
						remain := remains[i]
						node.InsertAfter(remain)
					}
					node.InsertAfter(insertedNode)
				} else {
					for i := len(remains) - 1; 0 <= i; i-- {
						remain := remains[i]
						node.PrependChild(remain)
					}
					node.PrependChild(insertedNode)
				}
			}
		}
	}

	treenode.CreatedUpdated(insertedNode)
	tx.nodes[insertedNode.ID] = insertedNode

	// Collect the IDs of the referenced definition blocks
	refDefIDs := getRefDefIDs(insertedNode)
	// Push the ref count update for the definition node
	for _, defID := range refDefIDs {
		task.AppendAsyncTaskWithDelay(task.SetDefRefCount, util.SQLFlushInterval, refreshRefCount, defID)
	}
	// The refs in the newly inserted block are all newly added here, so refresh their last-referenced time for the block-ref "recently referenced" ordering
	TouchRefUsed(refDefIDs)
	// After pasting a referenced block, the target document's ref count must be refreshed; otherwise the document-level count badge on the target document won't update
	task.AppendAsyncTaskWithDelay(task.SetDefRefCount, util.SQLFlushInterval, refreshRefCount, tree.Root.ID)

	upsertAvBlockRel(insertedNode)

	// Remove the database-binding state when copying as a duplicate https://github.com/siyuan-note/siyuan/issues/12294
	insertedNode.RemoveIALAttr(av.NodeAttrNameAvs)
	insertedNode.RemoveIALAttr(av.NodeAttrViewNames)
	insertedNode.RemoveIALAttrsByPrefix(av.NodeAttrViewStaticText)

	// Remove flashcard-related attributes when copying as a duplicate https://github.com/siyuan-note/siyuan/issues/13987
	insertedNode.RemoveIALAttr(NodeAttrRiffDecks)

	if ast.NodeAttributeView == insertedNode.Type {
		// When inserting a database block, the blocks that already exist within it need to be re-bound
		// For example, during a cut operation a delete first unbinds the database block, so it needs to be re-bound here https://github.com/siyuan-note/siyuan/issues/13031
		attrView, parseErr := av.ParseAttributeView(insertedNode.AttributeViewID)
		if nil == parseErr {
			trees, toBindNodes := tx.getAttrViewBoundNodes(attrView)
			for _, toBindNode := range toBindNodes {
				t := trees[toBindNode.ID]
				bindBlockAv0(tx, insertedNode.AttributeViewID, toBindNode, t)
			}

			// Set the view https://github.com/siyuan-note/siyuan/issues/15279
			v := attrView.GetView(attrView.ViewID)
			if nil != v {
				insertedNode.AttributeViewType = string(v.LayoutType)
				attrs := parse.IAL2Map(insertedNode.KramdownIAL)
				if "" == attrs[av.NodeAttrView] {
					attrs[av.NodeAttrView] = v.ID
					err := setNodeAttrs(insertedNode, tree, attrs)
					if err != nil {
						logging.LogWarnf("set node [%s] attrs failed: %s", operation.BlockID, err)
						return
					}
				}
			}
		}
	}

	operation.ID = insertedNode.ID
	operation.ParentID = insertedNode.Parent.ID
	return
}

func (tx *Transaction) processGlobalAssets(tree *parse.Tree) {
	if !tx.isGlobalAssetsInit {
		tx.assetsDir = getAssetsDir(filepath.Join(util.DataDir, tree.Box), filepath.Dir(filepath.Join(util.DataDir, tree.Box, tree.Path)))
		tx.isGlobalAssets = strings.HasPrefix(tx.assetsDir, filepath.Join(util.DataDir, "assets"))
		tx.isGlobalAssetsInit = true
	}

	if tx.isGlobalAssets {
		return
	}

	// Local asset files need to be moved under the assets directory the user manually created https://github.com/siyuan-note/siyuan/issues/2410
	ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.WalkContinue
		}

		if ast.NodeLinkDest == n.Type && bytes.HasPrefix(n.Tokens, []byte("assets/")) {
			assetP := gulu.Str.FromBytes(n.Tokens)
			assetPath, e := GetAssetAbsPathInBox(assetP, tree.Box)
			if nil != e {
				logging.LogErrorf("get path of asset [%s] failed: %s", assetP, e)
				return ast.WalkContinue
			}

			if !strings.HasPrefix(assetPath, filepath.Join(util.DataDir, "assets")) {
				// Skip if it's not in the global assets directory
				return ast.WalkContinue
			}

			// Only move to the relative assets directory if it's in the global assets directory
			targetP := filepath.Join(tx.assetsDir, filepath.Base(assetPath))
			if e = filelock.Rename(assetPath, targetP); e != nil {
				logging.LogErrorf("copy path of asset from [%s] to [%s] failed: %s", assetPath, targetP, e)
				return ast.WalkContinue
			}
		}
		return ast.WalkContinue
	})
}

func (tx *Transaction) doUpdate(operation *Operation) (ret *TxErr) {
	id := operation.ID
	updateData, ok := operation.Data.(string)
	if !ok || "" == updateData {
		msg := "update data is invalid"
		logging.LogError(msg)
		return &TxErr{code: TxErrCodePushMsg, msg: msg, id: id}
	}

	tree, err := tx.loadTree(id)
	if err != nil {
		logging.LogErrorf("load tree [%s] failed: %s", id, err)
		return &TxErr{code: TxErrCodeBlockNotFound, id: id}
	}

	data := strings.ReplaceAll(updateData, editor.FrontEndCaret, "")
	if "" == data {
		msg := "update data is invalid"
		logging.LogError(msg)
		return &TxErr{code: TxErrCodePushMsg, msg: msg, id: id}
	}

	subTree := tx.luteEngine.BlockDOM2Tree(data)
	subTree.ID, subTree.Box, subTree.Path = tree.ID, tree.Box, tree.Path
	oldNode := treenode.GetNodeInTree(tree, id)
	if nil == oldNode {
		logging.LogErrorf("get node [%s] in tree [%s] failed", id, tree.Root.ID)
		return &TxErr{code: TxErrCodeBlockNotFound, msg: ErrBlockNotFound.Error(), id: id}
	}

	// Collect the IDs of the referenced definition blocks
	oldDefIDs := getRefDefIDs(oldNode)
	var newDefIDs []string

	// Fallback safety check: forbid block refs that cross an encryption boundary (between an encrypted notebook and a regular box, or between different encrypted notebooks)
	degradeCrossBoundaryBlockRefs(subTree.Root, subTree.Box)

	var unlinks []*ast.Node
	ast.Walk(subTree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.WalkContinue
		}

		if ast.NodeTextMark == n.Type {
			if n.IsTextMarkType("inline-math") {
				if "" == strings.TrimSpace(n.TextMarkInlineMathContent) {
					// Remove blank inline formulas
					unlinks = append(unlinks, n)
				}
			} else if n.IsTextMarkType("block-ref") {
				if "" == n.TextMarkBlockRefID {
					// Already degraded to plain text by degradeCrossBoundaryBlockRefs, so skip ref processing
					return ast.WalkContinue
				}

				sql.CacheRef(subTree, n)

				if "d" == n.TextMarkBlockRefSubtype {
					// Occasionally, after editing a document title, the dynamic anchor text at the ref site doesn't update https://github.com/siyuan-note/siyuan/issues/5891
					// Force-overwrite the dynamic anchor text of the ref node in the current block with the cached dynamic anchor text
					if dRefText := treenode.GetDynamicRefText(n.TextMarkBlockRefID, tree.Box); "" != dRefText {
						n.TextMarkTextContent = dRefText
					}
				}

				newDefIDs = append(newDefIDs, n.TextMarkBlockRefID)
			}
		}
		return ast.WalkContinue
	})
	for _, n := range unlinks {
		n.Unlink()
	}

	oldDefIDs = gulu.Str.RemoveDuplicatedElem(oldDefIDs)
	newDefIDs = gulu.Str.RemoveDuplicatedElem(newDefIDs)
	refDefIDs := oldDefIDs

	if !slices.Equal(oldDefIDs, newDefIDs) { // if the refs changed, push the ref count update for the definition node
		refDefIDs = append(refDefIDs, newDefIDs...)
		refDefIDs = gulu.Str.RemoveDuplicatedElem(refDefIDs)
		for _, defID := range refDefIDs {
			task.AppendAsyncTaskWithDelay(task.SetDefRefCount, util.SQLFlushInterval, refreshRefCount, defID)
		}

		// For target blocks newly referenced this time, refresh their last-referenced time for the block-ref "recently referenced" ordering
		var newRefDefIDs []string
		for _, defID := range newDefIDs {
			if !gulu.Str.Contains(defID, oldDefIDs) {
				newRefDefIDs = append(newRefDefIDs, defID)
			}
		}
		TouchRefUsed(newRefDefIDs)
	}

	updatedNode := subTree.Root.FirstChild
	if nil == updatedNode {
		logging.LogErrorf("get fist node in sub tree [%s] failed", subTree.Root.ID)
		return &TxErr{code: TxErrCodeBlockNotFound, msg: ErrBlockNotFound.Error(), id: id}
	}
	if ast.NodeList == updatedNode.Type && ast.NodeList == oldNode.Parent.Type {
		updatedNode = updatedNode.FirstChild
	}

	if oldNode.IsContainerBlock() {
		// When updating a container block, account for any folded headings among its children, and move the blocks beneath those folded headings under the new node
		treenode.MoveFoldHeading(updatedNode, oldNode)
	}

	cache.PutBlockIALInBox(updatedNode.ID, tree.Box, parse.IAL2Map(updatedNode.KramdownIAL))

	if ast.NodeHTMLBlock == updatedNode.Type {
		content := string(updatedNode.Tokens)
		// Remove consecutive blank lines (including blank lines that contain only spaces) https://github.com/siyuan-note/siyuan/issues/15377
		var newLines []string
		lines := strings.SplitSeq(content, "\n")
		for line := range lines {
			if strings.TrimSpace(line) != "" {
				newLines = append(newLines, line)
			}
		}
		updatedNode.Tokens = []byte(strings.Join(newLines, "\n"))
	}

	removedNodes := getRemovedNodes(oldNode, updatedNode)
	for _, n := range removedNodes {
		syncDelete2AvBlock(n, tree, false, tx)
	}

	// When a block that doesn't belong under a folded heading is moved beneath one, the folded heading needs to be unfolded
	needUnfoldParentHeading := 0 < oldNode.HeadingLevel && (0 == updatedNode.HeadingLevel || oldNode.HeadingLevel < updatedNode.HeadingLevel)

	oldParentFoldedHeading := treenode.GetParentFoldedHeading(oldNode)
	// When a block previously under a folded heading is promoted to a heading at the same level as, or higher than, the folded heading, the promoted heading block needs to be inserted after the folded heading (only a UI insert needs to be pushed)
	needInsertAfterParentHeading := nil != oldParentFoldedHeading && 0 != updatedNode.HeadingLevel && updatedNode.HeadingLevel <= oldParentFoldedHeading.HeadingLevel

	oldNode.InsertAfter(updatedNode)
	oldNode.Unlink()

	if needUnfoldParentHeading {
		newParentFoldedHeading := treenode.GetParentFoldedHeading(updatedNode)
		if nil == oldParentFoldedHeading || (nil != newParentFoldedHeading && oldParentFoldedHeading.ID != newParentFoldedHeading.ID) {
			unfoldHeading(newParentFoldedHeading, updatedNode)
		}
	}

	if needInsertAfterParentHeading {
		insertDom := data
		if 2 == len(tx.DoOperations) && "foldHeading" == tx.DoOperations[1].Action {
			children := treenode.HeadingChildren(updatedNode)
			for _, child := range children {
				ast.Walk(child, func(n *ast.Node, entering bool) ast.WalkStatus {
					if !entering || !n.IsBlock() {
						return ast.WalkContinue
					}

					n.SetIALAttr("fold", "1")
					n.SetIALAttr("heading-fold", "1")
					return ast.WalkContinue
				})
			}
			updatedNode.SetIALAttr("fold", "1")
			insertDom = tx.luteEngine.RenderNodeBlockDOM(updatedNode)
		}

		evt := util.NewCmdResult("transactions", 0, util.PushModeBroadcast)
		evt.Data = []*Transaction{{
			DoOperations:   []*Operation{{Action: "insert", ID: updatedNode.ID, PreviousID: oldParentFoldedHeading.ID, Data: insertDom}},
			UndoOperations: []*Operation{{Action: "delete", ID: updatedNode.ID}},
		}}
		util.PushEvent(evt)
	}

	if avNames := getAvNames(updatedNode.IALAttr(av.NodeAttrNameAvs)); "" != avNames {
		// updateBlock clears the database badge https://github.com/siyuan-note/siyuan/issues/16549
		go func() {
			time.Sleep(200 * time.Millisecond)
			oldAttrs := parse.IAL2Map(updatedNode.KramdownIAL)
			updatedNode.SetIALAttr(av.NodeAttrViewNames, avNames)
			pushBlockAttrs(oldAttrs, updatedNode)
		}()
	}

	treenode.CreatedUpdated(updatedNode)
	tx.nodes[updatedNode.ID] = updatedNode
	tx.writeTree(tree)

	upsertAvBlockRel(updatedNode)

	if ast.NodeAttributeView == updatedNode.Type {
		// Set the view https://github.com/siyuan-note/siyuan/issues/15279
		attrView, parseErr := av.ParseAttributeView(updatedNode.AttributeViewID)
		if nil == parseErr {
			v := attrView.GetView(attrView.ViewID)
			if nil != v {
				updatedNode.AttributeViewType = string(v.LayoutType)
				attrs := parse.IAL2Map(updatedNode.KramdownIAL)
				if "" == attrs[av.NodeAttrView] {
					attrs[av.NodeAttrView] = v.ID
					err = setNodeAttrs(updatedNode, tree, attrs)
					if err != nil {
						logging.LogWarnf("set node [%s] attrs failed: %s", operation.BlockID, err)
						return &TxErr{code: TxErrCodeBlockNotFound, id: id}
					}
				}
			}
		}
	}
	return
}

func unfoldHeading(heading, currentNode *ast.Node) {
	if nil == heading {
		return
	}

	children := treenode.HeadingChildren(heading)
	for _, child := range children {
		ast.Walk(child, func(n *ast.Node, entering bool) ast.WalkStatus {
			if !entering || !n.IsBlock() {
				return ast.WalkContinue
			}

			n.RemoveIALAttr("fold")
			n.RemoveIALAttr("heading-fold")
			return ast.WalkContinue
		})
	}
	heading.RemoveIALAttr("fold")
	heading.RemoveIALAttr("heading-fold")

	util.BroadcastByType("protyle", "unfoldHeading", 0, "", map[string]any{"id": heading.ID, "currentNodeID": currentNode.ID})
}

func getRefDefIDs(node *ast.Node) (refDefIDs []string) {
	ast.Walk(node, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.WalkContinue
		}

		if treenode.IsBlockRef(n) {
			refDefIDs = append(refDefIDs, n.TextMarkBlockRefID)
		} else if treenode.IsEmbedBlockRef(n) {
			defID := treenode.GetEmbedBlockRef(n)
			refDefIDs = append(refDefIDs, defID)
		}
		return ast.WalkContinue
	})
	refDefIDs = gulu.Str.RemoveDuplicatedElem(refDefIDs)
	return
}

// degradeCrossBoundaryBlockRefs walks the tree and degrades any block-ref nodes that cross an encryption boundary to plain text.
// Encrypted notebooks forbid cross-boundary block refs in both directions: this prevents manual input/drag/paste/direct API calls from bypassing the frontend search filtering.
// Returns the number of refs that were degraded.
func degradeCrossBoundaryBlockRefs(root *ast.Node, srcBox string) int {
	degraded := 0
	localBlockIDs := map[string]struct{}{}
	ast.Walk(root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if entering && n.IsBlock() && n.ID != "" {
			localBlockIDs[n.ID] = struct{}{}
		}
		return ast.WalkContinue
	})
	ast.Walk(root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.WalkContinue
		}

		if ast.NodeTextMark == n.Type && n.IsTextMarkType("block-ref") {
			if _, local := localBlockIDs[n.TextMarkBlockRefID]; local {
				return ast.WalkContinue
			}
			if IsBlockRefCrossingBoundary(srcBox, n.TextMarkBlockRefID) {
				logging.LogWarnf("block ref crosses encrypted boundary, src box [%s] -> def block [%s], degrade to text", srcBox, n.TextMarkBlockRefID)
				n.TextMarkBlockRefID = ""
				n.TextMarkBlockRefSubtype = ""
				n.TextMarkTextContent = strings.TrimSpace(n.TextMarkTextContent)
				degraded++
			}
		}
		return ast.WalkContinue
	})
	return degraded
}

func getRemovedNodes(oldNode, newNode *ast.Node) (ret []*ast.Node) {
	oldNodes := map[string]*ast.Node{}
	ast.Walk(oldNode, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering || !n.IsBlock() {
			return ast.WalkContinue
		}
		oldNodes[n.ID] = n
		return ast.WalkContinue
	})
	ast.Walk(newNode, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering || !n.IsBlock() {
			return ast.WalkContinue
		}
		if _, ok := oldNodes[n.ID]; ok {
			delete(oldNodes, n.ID)
		}
		return ast.WalkContinue
	})
	for _, n := range oldNodes {
		ret = append(ret, n)
	}
	return
}

func upsertAvBlockRel(node *ast.Node) {
	var affectedAvIDs []string
	ast.Walk(node, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.WalkContinue
		}

		if ast.NodeAttributeView == n.Type {
			avID := n.AttributeViewID
			if changed := av.UpsertBlockRel(avID, n.ID); changed {
				affectedAvIDs = append(affectedAvIDs, avID)
			}
		}
		return ast.WalkContinue
	})

	updatedNodes := []*ast.Node{node}
	var parents []*ast.Node
	for parent := node.Parent; nil != parent && ast.NodeDocument != parent.Type; parent = parent.Parent {
		parents = append(parents, parent)
	}
	updatedNodes = append(updatedNodes, parents...)
	for _, updatedNode := range updatedNodes {
		ast.Walk(updatedNode, func(n *ast.Node, entering bool) ast.WalkStatus {
			avs := n.IALAttr(av.NodeAttrNameAvs)
			if "" == avs {
				return ast.WalkContinue
			}

			avIDs := strings.Split(avs, ",")
			affectedAvIDs = append(affectedAvIDs, avIDs...)
			return ast.WalkContinue
		})
	}

	go func() {
		time.Sleep(100 * time.Millisecond)

		affectedAvIDs = gulu.Str.RemoveDuplicatedElem(affectedAvIDs)
		var relatedAvIDs []string
		for _, avID := range affectedAvIDs {
			relatedAvIDs = append(relatedAvIDs, av.GetSrcAvIDs(avID)...)
		}
		affectedAvIDs = append(affectedAvIDs, relatedAvIDs...)
		affectedAvIDs = gulu.Str.RemoveDuplicatedElem(affectedAvIDs)
		for _, avID := range affectedAvIDs {
			attrView, _ := av.ParseAttributeView(avID)
			if nil != attrView {
				regenAttrViewGroups(attrView)
				av.SaveAttributeView(attrView)
			}

			ReloadAttrView(avID)
		}
	}()
}

func (tx *Transaction) doUpdateUpdated(operation *Operation) (ret *TxErr) {
	id := operation.ID
	tree, err := tx.loadTree(id)
	if err != nil {
		if errors.Is(err, ErrBlockNotFound) {
			logging.LogWarnf("not found block [%s]", id)
			return
		}

		logging.LogErrorf("load tree [%s] failed: %s", id, err)
		return &TxErr{code: TxErrCodeBlockNotFound, id: id}
	}

	node := treenode.GetNodeInTree(tree, id)
	if nil == node {
		logging.LogErrorf("get node [%s] in tree [%s] failed", id, tree.Root.ID)
		return &TxErr{code: TxErrCodeBlockNotFound, msg: ErrBlockNotFound.Error(), id: id}
	}

	node.SetIALAttr("updated", operation.Data.(string))
	treenode.CreatedUpdated(node)
	tx.nodes[node.ID] = node
	tx.writeTree(tree)
	return
}

func (tx *Transaction) doCreate(operation *Operation) (ret *TxErr) {
	tree := operation.Data.(*parse.Tree)
	// Fallback safety check: forbid block refs that cross an encryption boundary (creating a document may carry cross-boundary refs)
	// Must run before getRefDefIDs, to avoid cross-boundary refs being collected into the ref cache
	degradeCrossBoundaryBlockRefs(tree.Root, tree.Box)
	tx.writeTree(tree)
	// The refs in a newly created document are all newly added here, so refresh their last-referenced time for the block-ref "recently referenced" ordering
	TouchRefUsed(getRefDefIDs(tree.Root))
	return
}

// doRestoreCreatedDoc registers the newly created document on first execution, and restores it from the snapshot on redo.
func (tx *Transaction) doRestoreCreatedDoc(operation *Operation) (ret *TxErr) {
	tree := operation.Tree
	if nil == tree || nil == tree.Root || operation.ID != tree.Root.ID {
		return &TxErr{code: TxErrCodePushMsg, msg: "invalid created doc snapshot", id: operation.ID}
	}
	if existing, err := LoadTreeByBlockID(tree.Root.ID); nil == err && nil != existing {
		if tx.isReplay || existing.Box != tree.Box || existing.Path != tree.Path {
			return &TxErr{code: TxErrCodePushMsg, msg: "created doc already exists", id: operation.ID}
		}
		tx.writeTree(existing)
		return
	}
	if ret = tx.doCreate(&Operation{Action: "create", Data: tree}); nil == ret {
		tx.restoredCreatedDocs = append(tx.restoredCreatedDocs, tree)
	}
	return
}

// doRemoveCreatedDoc registers the document generated by this newly added entry for deletion at commit time; it goes through document history before being deleted.
func (tx *Transaction) doRemoveCreatedDoc(operation *Operation) (ret *TxErr) {
	if nil == operation.Tree || nil == operation.Tree.Root || operation.ID != operation.Tree.Root.ID {
		return &TxErr{code: TxErrCodePushMsg, msg: "invalid created doc snapshot", id: operation.ID}
	}
	tree, err := LoadTreeByBlockID(operation.ID)
	if nil != err {
		if errors.Is(err, ErrBlockNotFound) {
			return
		}
		return &TxErr{code: TxErrCodeBlockNotFound, msg: err.Error(), id: operation.ID}
	}
	tx.removedCreatedDocs = append(tx.removedCreatedDocs, tree)
	return
}

func (tx *Transaction) doSetAttrs(operation *Operation) (ret *TxErr) {
	id := operation.ID
	tree, err := tx.loadTree(id)
	if err != nil {
		logging.LogErrorf("load tree [%s] failed: %s", id, err)
		return &TxErr{code: TxErrCodeBlockNotFound, id: id}
	}

	node := treenode.GetNodeInTree(tree, id)
	if nil == node {
		logging.LogErrorf("get node [%s] in tree [%s] failed", id, tree.Root.ID)
		return &TxErr{code: TxErrCodeBlockNotFound, id: id}
	}

	attrs := map[string]string{}
	if err = gulu.JSON.UnmarshalJSON([]byte(operation.Data.(string)), &attrs); err != nil {
		logging.LogErrorf("unmarshal attrs failed: %s", err)
		return &TxErr{code: TxErrCodeBlockNotFound, id: id}
	}
	if IsBoxDoc(tree.Box, tree.ID) {
		attrs[DocHiddenAttr] = "true"
		if icon, ok := attrs["icon"]; ok {
			icon = filterBoxIcon(icon)
			attrs["icon"] = icon
			tx.boxIcons[tree.Box] = icon
		}
	}

	if _, setErr := setNodeAttrs0(node, attrs, tree.Box); nil != setErr {
		logging.LogErrorf("set attrs failed: %s", setErr)
		return &TxErr{code: TxErrCodePushMsg, msg: setErr.Error(), id: id}
	}

	tx.writeTree(tree)
	cache.PutBlockIALInBox(id, tree.Box, parse.IAL2Map(node.KramdownIAL))
	return
}

type Operation struct {
	Action     string   `json:"action"`
	Data       any      `json:"data"`
	ID         string   `json:"id"`
	RootID     string   `json:"rootID"` // Not currently used internally by SiYuan https://github.com/siyuan-note/siyuan/issues/17179#issuecomment-4051604916
	ParentID   string   `json:"parentID"`
	PreviousID string   `json:"previousID"`
	NextID     string   `json:"nextID"`
	RetData    any      `json:"retData"`
	BlockIDs   []string `json:"blockIDs"`
	BlockID    string   `json:"blockID"`

	DeckID string      `json:"deckID"` // Used for adding/removing flashcards
	Tree   *parse.Tree `json:"-"`      // Used only for kernel transaction replay; not sent to the frontend

	AvID              string           `json:"avID"`              // Attribute view ID
	SrcIDs            []string         `json:"srcIDs"`            // Used to remove rows from the attribute view
	Srcs              []map[string]any `json:"srcs"`              // Used to add attribute view rows (including bound blocks) {id, content, isDetached}
	IsDetached        bool             `json:"isDetached"`        // Indicates whether this is an unbound block; only meaningful within an attribute view
	Name              string           `json:"name"`              // Attribute view column name
	Typ               string           `json:"type"`              // Attribute view column type
	Format            string           `json:"format"`            // Attribute view column format
	KeyID             string           `json:"keyID"`             // Attribute view field ID
	RowID             string           `json:"rowID"`             // Attribute view row ID
	IsTwoWay          bool             `json:"isTwoWay"`          // Whether the attribute view relation column is a two-way relation
	BackRelationKeyID string           `json:"backRelationKeyID"` // ID of the back-relation column linked by the attribute view relation column
	RemoveDest        bool             `json:"removeDest"`        // Remove the attribute view's relation target
	Layout            av.LayoutType    `json:"layout"`            // Attribute view layout type
	GroupID           string           `json:"groupID"`           // Attribute view group view ID
	TargetGroupID     string           `json:"targetGroupID"`     // Attribute view target group view ID
	ViewID            string           `json:"viewID"`            // Attribute view view ID
	IgnoreDefaultFill bool             `json:"ignoreDefaultFill"` // Whether to ignore the default fill

	Context map[string]any `json:"context"` // Context information
}

type Transaction struct {
	Timestamp      int64        `json:"timestamp"`
	DoOperations   []*Operation `json:"doOperations"`
	UndoOperations []*Operation `json:"undoOperations"`

	trees          map[string]*parse.Tree // Trees changed within the transaction
	nodes          map[string]*ast.Node   // Nodes changed within the transaction
	relatedAvIDs   []string               // Attribute view IDs changed within the transaction
	changedRootIDs []string               // List of changed tree IDs (includes trees containing dynamic anchor text affected by changed definition blocks)
	boxIcons       map[string]string      // Notebook icons that need to be synced after the transaction commits

	isGlobalAssetsInit  bool   // Whether the global-assets determination has been initialized
	isGlobalAssets      bool   // Whether this belongs to the global assets directory
	assetsDir           string // Path to the assets directory
	removedCreatedDocs  []*parse.Tree
	restoredCreatedDocs []*parse.Tree

	fromAPI  bool // Whether this came from the /api/transactions HTTP endpoint (used to determine undo log capture)
	isReplay bool // Whether this transaction was constructed by undo/redo replay (replayed transactions are not recorded again in the undo log)

	luteEngine *lute.Lute
	m          *sync.Mutex
	state      atomic.Int32 // 0: initialized, 1: not yet committed, 2: committed, 3: rolled back
}

func (tx *Transaction) GetChangedRootIDs() (ret []string) {
	for t := range tx.trees {
		ret = append(ret, t)
	}

	for _, id := range tx.changedRootIDs {
		ret = append(ret, id)
	}
	ret = gulu.Str.RemoveDuplicatedElem(ret)
	return
}

// MarkFromAPI marks the transaction as coming from the /api/transactions HTTP endpoint, for the global undo log capture check.
func (tx *Transaction) MarkFromAPI() {
	tx.fromAPI = true
}

// MarkReplay marks the transaction as constructed by undo/redo replay; replayed transactions are not recorded again in the undo log.
func (tx *Transaction) MarkReplay() {
	tx.isReplay = true
}

// GetMutatedRootIDs returns the rootIDs of trees whose structure was actually written to disk, excluding reference trees refreshed by refreshDynamicRefTexts.
// Used for cross-document undo determination: a single-document edit returns 1 rootID, a cross-document move returns multiple, and reference text refreshes don't count.
func (tx *Transaction) GetMutatedRootIDs() (ret []string) {
	for t := range tx.trees {
		ret = append(ret, t)
	}
	ret = gulu.Str.RemoveDuplicatedElem(ret)
	return
}

func (tx *Transaction) WaitForCommit() {
	for {
		if 1 == tx.state.Load() {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		return
	}
}

func (tx *Transaction) begin() (err error) {
	tx.trees = map[string]*parse.Tree{}
	tx.nodes = map[string]*ast.Node{}
	tx.boxIcons = map[string]string{}
	tx.removedCreatedDocs = nil
	tx.restoredCreatedDocs = nil
	tx.luteEngine = util.NewLute()
	tx.m.Lock()
	tx.state.Store(1)
	return
}

func (tx *Transaction) commit() (err error) {
	for _, tree := range tx.trees {
		if err = writeTreeUpsertQueue(tree); err != nil {
			return
		}

		var sources []any
		sources = append(sources, tx)
		util.PushSaveDoc(tree.ID, "tx", sources)

		checkUpsertInUserGuide(tree)
	}
	for boxID, icon := range tx.boxIcons {
		box := &Box{ID: boxID}
		boxConf := box.GetConf()
		boxConf.Icon = icon
		if err = box.SaveConf(boxConf); err != nil {
			return
		}
	}
	if 0 < len(tx.boxIcons) {
		ReloadFiletree()
	}
	tx.changedRootIDs = refreshDynamicRefTexts(tx.nodes, tx.trees)

	tx.relatedAvIDs = gulu.Str.RemoveDuplicatedElem(tx.relatedAvIDs)
	for _, avID := range tx.relatedAvIDs {
		destAv, _ := av.ParseAttributeView(avID)
		if nil == destAv {
			continue
		}

		regenAttrViewGroups(destAv)
		av.SaveAttributeView(destAv)
		ReloadAttrView(avID)
	}
	for _, tree := range tx.removedCreatedDocs {
		box := Conf.Box(tree.Box)
		if nil == box {
			return ErrBoxNotFound
		}
		removedTree, err := removeDoc(box, tree.Path, util.NewLute())
		if nil != err {
			return err
		}
		refreshBoxDocInfo(removedTree)
	}
	for _, tree := range tx.restoredCreatedDocs {
		box := Conf.Box(tree.Box)
		if nil == box {
			return ErrBoxNotFound
		}
		box.setSortByConf(path.Dir(tree.Path), tree.ID)
		PushCreate(box, tree.Path, nil)
	}

	IncSync()
	tx.state.Store(2)
	// Record to the global undo log once committed and trees are stable (rollback does not record)
	GlobalUndoLog.Record(tx)
	tx.m.Unlock()
	return
}

func (tx *Transaction) rollback() {
	tx.trees, tx.nodes, tx.boxIcons, tx.removedCreatedDocs, tx.restoredCreatedDocs = nil, nil, nil, nil, nil
	tx.state.Store(3)
	tx.m.Unlock()
	return
}

func (tx *Transaction) loadTreeByBlockTree(bt *treenode.BlockTree) (ret *parse.Tree, err error) {
	if nil == bt {
		return nil, ErrBlockNotFound
	}

	ret = tx.trees[bt.RootID]
	if nil != ret {
		return
	}

	ret, err = filesys.LoadTree(bt.BoxID, bt.Path, tx.luteEngine)
	if err != nil {
		return
	}
	tx.trees[bt.RootID] = ret
	return
}

func (tx *Transaction) loadTree(id string) (ret *parse.Tree, err error) {
	var rootID, box, p string
	bt := treenode.GetBlockTree(id)
	if nil == bt {
		// When it can't be found in the global block tree, search through the opened encrypted notebooks
		for _, encBoxID := range treenode.GetOpenedEncryptedBoxIDs() {
			if encBT := treenode.GetBlockTreeInBox(id, encBoxID); nil != encBT {
				bt = encBT
				break
			}
		}
	}
	if nil == bt {
		return nil, ErrBlockNotFound
	}
	rootID = bt.RootID
	box = bt.BoxID
	p = bt.Path

	ret = tx.trees[rootID]
	if nil != ret {
		return
	}

	ret, err = filesys.LoadTree(box, p, tx.luteEngine)
	if err != nil {
		return
	}
	tx.trees[rootID] = ret
	return
}

func (tx *Transaction) writeTree(tree *parse.Tree) {
	tx.trees[tree.ID] = tree
	treenode.UpsertBlockTree(tree)
	return
}

func getRefsCacheByDefNode(updateNode *ast.Node) (ret []*sql.Ref, changedNodes []*ast.Node) {
	changedNodesMap := map[string]*ast.Node{}
	ret = sql.GetRefsCacheByDefIDInBox(updateNode.ID, updateNode.Box)
	if nil != updateNode.Parent && ast.NodeDocument != updateNode.Parent.Type &&
		updateNode.Parent.IsContainerBlock() && updateNode == treenode.FirstLeafBlock(updateNode.Parent) {
		// If this is the first leaf block under a container block, refs need to be looked up upward
		for parent := updateNode.Parent; nil != parent; parent = parent.Parent {
			if ast.NodeDocument == parent.Type {
				break
			}

			parentRefs := sql.GetRefsCacheByDefIDInBox(parent.ID, updateNode.Box)
			if 0 < len(parentRefs) {
				ret = append(ret, parentRefs...)
				if _, ok := changedNodesMap[parent.ID]; !ok {
					changedNodesMap[parent.ID] = parent
				}
			}
		}
	}
	if ast.NodeDocument != updateNode.Type && updateNode.IsContainerBlock() {
		// If this is a container block, refs need to be looked up downward
		ast.Walk(updateNode, func(n *ast.Node, entering bool) ast.WalkStatus {
			if !entering || !n.IsBlock() {
				return ast.WalkContinue
			}

			childRefs := sql.GetRefsCacheByDefIDInBox(n.ID, updateNode.Box)
			if 0 < len(childRefs) {
				ret = append(ret, childRefs...)
				changedNodesMap[n.ID] = n
			}
			return ast.WalkContinue
		})
	}
	if ast.NodeHeading == updateNode.Type && "1" == updateNode.IALAttr("fold") {
		// If this is a folded heading, refs need to be looked up downward
		children := treenode.HeadingChildren(updateNode)
		for _, child := range children {
			childRefs := sql.GetRefsCacheByDefIDInBox(child.ID, updateNode.Box)
			if 0 < len(childRefs) {
				ret = append(ret, childRefs...)
				changedNodesMap[child.ID] = child
			}
		}
	}
	for _, n := range changedNodesMap {
		changedNodes = append(changedNodes, n)
	}
	return
}

var updateRefTextRenameDocs = map[string]*parse.Tree{}
var updateRefTextRenameDocLock = sync.Mutex{}

func updateRefTextRenameDoc(renamedTree *parse.Tree) {
	updateRefTextRenameDocLock.Lock()
	updateRefTextRenameDocs[renamedTree.ID] = renamedTree
	updateRefTextRenameDocLock.Unlock()
}

func FlushUpdateRefTextRenameDocJob() {
	sql.WaitFlushTx()
	flushUpdateRefTextRenameDoc()
}

func flushUpdateRefTextRenameDoc() {
	updateRefTextRenameDocLock.Lock()
	defer updateRefTextRenameDocLock.Unlock()

	for _, tree := range updateRefTextRenameDocs {
		refreshDynamicRefText(tree.Root, tree)
	}
	updateRefTextRenameDocs = map[string]*parse.Tree{}
}

type changedDefNode struct {
	id      string
	refText string
	refType string // ref-d/ref-s/embed
}

func updateRefText(refNode *ast.Node, changedDefNodes map[string]*ast.Node) (changed bool, defNodes []*changedDefNode) {
	ast.Walk(refNode, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.WalkContinue
		}
		if treenode.IsBlockRef(n) {
			defID, refText, subtype := treenode.GetBlockRef(n)
			if "" == defID {
				return ast.WalkContinue
			}

			defNode := changedDefNodes[defID]
			if nil == defNode {
				return ast.WalkSkipChildren
			}

			if "d" == subtype {
				newRefText := strings.TrimSpace(getNodeRefText(defNode))
				if "" == newRefText {
					newRefText = n.TextMarkBlockRefID
				}
				if strings.TrimSpace(refText) == newRefText {
					return ast.WalkContinue
				}
				treenode.SetDynamicBlockRefText(n, newRefText)
				changed = true
				refText = newRefText
				defNodes = append(defNodes, &changedDefNode{id: defID, refText: refText, refType: "ref-" + subtype})
			}
		}
		return ast.WalkContinue
	})
	return
}

func checkUpsertInUserGuide(tree *parse.Tree) {
	// In production mode, data reset warning pops up when editing data in the user guide https://github.com/siyuan-note/siyuan/issues/9757
	if "prod" == util.Mode && IsUserGuide(tree.Box) {
		util.PushErrMsg(Conf.Language(52), 7000)
	}
}
