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
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/88250/gulu"
	"github.com/88250/lute/ast"
	"github.com/siyuan-note/siyuan/kernel/treenode"
)

// UndoEntry is a single record in the undo stack. An entry for a cross-document operation (MutatedRootIDs
// containing multiple rootIDs) is attached to the stacks of all those rootIDs via the same pointer; undoing
// from any one of them cascades removal of the reference from the others.
type UndoEntry struct {
	id             string
	doOperations   []*Operation
	undoOperations []*Operation
	timestamp      int64
	mutatedRootIDs []string // rootIDs of trees actually modified on disk, used for cascading removal and cross-document detection
}

// DoOperationsForReplay returns a copy of the forward operations, for building a transaction during redo replay.
func (e *UndoEntry) DoOperationsForReplay() []*Operation {
	return cloneOperations(e.doOperations)
}

// UndoOperationsForReplay returns a copy of the reverse operations, for building a transaction during undo replay.
func (e *UndoEntry) UndoOperationsForReplay() []*Operation {
	return cloneOperations(e.undoOperations)
}

// MutatedRootIDs returns a copy of the list of rootIDs affected by this entry.
func (e *UndoEntry) MutatedRootIDs() []string {
	if nil == e.mutatedRootIDs {
		return nil
	}
	ret := make([]string, len(e.mutatedRootIDs))
	copy(ret, e.mutatedRootIDs)
	return ret
}

// undoStack is the undo/redo stack for a single rootID.
type undoStack struct {
	undoStack []*UndoEntry
	redoStack []*UndoEntry
	hasUndo   bool // mirrors the frontend's hasUndo state machine: set to true after an undo; if true when adding, the redo stack is cleared
}

// UndoLog is the global undo log, split into stacks by rootID; all windows/clients share the same authoritative instance.
type UndoLog struct {
	mu     sync.Mutex
	stacks map[string]*undoStack
	max    int
}

// GlobalUndoLog is the global undo log singleton. It is in-memory state and is cleared on restart.
var GlobalUndoLog = newUndoLog(64)

var undoEntrySeq uint64

func newUndoLog(max int) *UndoLog {
	return &UndoLog{
		stacks: map[string]*undoStack{},
		max:    max,
	}
}

func newUndoEntryID() string {
	seq := atomic.AddUint64(&undoEntrySeq, 1)
	return fmt.Sprintf("undo-%d-%d", time.Now().UnixNano(), seq)
}

// stack returns the stack for rootID, or nil if it doesn't exist.
func (l *UndoLog) stack(rootID string) *undoStack {
	return l.stacks[rootID]
}

// stackOrCreate returns the stack for rootID, creating one if it doesn't exist.
func (l *UndoLog) stackOrCreate(rootID string) *undoStack {
	s := l.stacks[rootID]
	if nil == s {
		s = &undoStack{}
		l.stacks[rootID] = s
	}
	return s
}

// Record records a committed editor transaction. It only records when the transaction comes from
// /api/transactions (fromAPI), carries non-empty UndoOperations, and is not an undo/redo replay (isReplay).
func (l *UndoLog) Record(tx *Transaction) {
	if !tx.fromAPI || 0 == len(tx.UndoOperations) || tx.isReplay {
		return
	}

	rootIDs := tx.GetMutatedRootIDs()
	if 0 == len(rootIDs) {
		// transactions that don't write to the block tree, such as pure attribute-view cell edits, are not pushed onto the stack
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	entry := &UndoEntry{
		id:             newUndoEntryID(),
		doOperations:   cloneOperations(tx.DoOperations),
		undoOperations: cloneOperations(tx.UndoOperations),
		timestamp:      time.Now().UnixMilli(),
		mutatedRootIDs: rootIDs,
	}

	for _, rootID := range rootIDs {
		s := l.stackOrCreate(rootID)
		s.undoStack = append(s.undoStack, entry)
		if s.hasUndo {
			s.redoStack = nil
			s.hasUndo = false
		}
		if l.max < len(s.undoStack) {
			s.undoStack = s.undoStack[len(s.undoStack)-l.max:]
		}
	}
}

// Peek returns the top of rootID's undo stack (without popping); returns nil if the stack is empty.
func (l *UndoLog) Peek(rootID string) *UndoEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	s := l.stack(rootID)
	if nil == s || 0 == len(s.undoStack) {
		return nil
	}
	return s.undoStack[len(s.undoStack)-1]
}

// Undo pops the top of rootID's undo stack, pushes it onto that stack's redo stack, and sets hasUndo.
// It only touches the acting stack, without cascading the removal to others. After the reverse operation
// executes successfully, call UndoCommit to complete the cascade; on failure, call UndoRollback to roll
// back precisely (since only the acting stack was touched). Returns the popped entry, or nil if the stack
// is empty.
func (l *UndoLog) Undo(rootID string) *UndoEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	s := l.stack(rootID)
	if nil == s || 0 == len(s.undoStack) {
		return nil
	}

	entry := s.undoStack[len(s.undoStack)-1]
	s.undoStack = s.undoStack[:len(s.undoStack)-1]
	// push only onto the acting stack's redo stack, matching semantic B: pressing Ctrl+Y on document B will not redo this entry
	s.redoStack = append(s.redoStack, entry)
	if l.max < len(s.redoStack) {
		s.redoStack = s.redoStack[len(s.redoStack)-l.max:]
	}
	s.hasUndo = true
	return entry
}

// UndoCommit, after the reverse operation executes successfully, cascades removal of this entry from other related stacks (matched by id).
func (l *UndoLog) UndoCommit(entry *UndoEntry, rootID string) {
	if nil == entry {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, r := range entry.mutatedRootIDs {
		if r == rootID {
			continue
		}
		l.removeEntry(r, entry.id)
	}
}

// UndoRollback rolls back the acting stack when the reverse operation fails to execute: it moves the entry
// from the redo stack back onto the top of the undo stack and resets hasUndo.
// Since Undo only touched the acting stack, this rollback is precise.
func (l *UndoLog) UndoRollback(entry *UndoEntry, rootID string) {
	if nil == entry {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	s := l.stack(rootID)
	if nil == s {
		return
	}
	// remove the entry from the top of the acting stack's redo stack (the one Undo pushed)
	if 0 < len(s.redoStack) && s.redoStack[len(s.redoStack)-1].id == entry.id {
		s.redoStack = s.redoStack[:len(s.redoStack)-1]
	}
	// push it back onto the top of the acting stack's undo stack (restoring the position before Undo popped it)
	s.undoStack = append(s.undoStack, entry)
	s.hasUndo = false
}

// Redo pops the top of rootID's redo stack and pushes it back onto the acting stack's undo stack. It only
// touches the acting stack, without cascading the re-attachment to others.
// It does not change hasUndo (mirroring the frontend's asymmetric redo behavior). Call RedoCommit on
// success; call RedoRollback on failure.
func (l *UndoLog) Redo(rootID string) *UndoEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	s := l.stack(rootID)
	if nil == s || 0 == len(s.redoStack) {
		return nil
	}

	entry := s.redoStack[len(s.redoStack)-1]
	s.redoStack = s.redoStack[:len(s.redoStack)-1]
	s.undoStack = append(s.undoStack, entry)
	return entry
}

// RedoCommit, after redo executes successfully, cascades re-attaching the entry onto the top of other related stacks.
func (l *UndoLog) RedoCommit(entry *UndoEntry, rootID string) {
	if nil == entry {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, r := range entry.mutatedRootIDs {
		if r == rootID {
			continue
		}
		rs := l.stackOrCreate(r)
		rs.undoStack = append(rs.undoStack, entry)
		if l.max < len(rs.undoStack) {
			rs.undoStack = rs.undoStack[len(rs.undoStack)-l.max:]
		}
	}
}

// RedoRollback rolls back the acting stack when redo fails to execute: it moves the entry from the undo
// stack back onto the top of the redo stack.
// Since Redo only touched the acting stack, this rollback is precise.
func (l *UndoLog) RedoRollback(entry *UndoEntry, rootID string) {
	if nil == entry {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	s := l.stack(rootID)
	if nil == s {
		return
	}
	// remove the entry from the top of the acting stack's undo stack (the one Redo pushed)
	if 0 < len(s.undoStack) && s.undoStack[len(s.undoStack)-1].id == entry.id {
		s.undoStack = s.undoStack[:len(s.undoStack)-1]
	}
	// push it back onto the top of the acting stack's redo stack
	s.redoStack = append(s.redoStack, entry)
	if l.max < len(s.redoStack) {
		s.redoStack = s.redoStack[len(s.redoStack)-l.max:]
	}
}

// State returns whether undo/redo is available for rootID, along with the mutatedRootIDs associated with the top entry.
func (l *UndoLog) State(rootID string) (canUndo, canRedo bool, peekMutatedRootIDs []string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	s := l.stack(rootID)
	if nil == s {
		return
	}
	canUndo = 0 < len(s.undoStack)
	canRedo = 0 < len(s.redoStack)
	if canUndo {
		top := s.undoStack[len(s.undoStack)-1]
		peekMutatedRootIDs = append(peekMutatedRootIDs, top.mutatedRootIDs...)
		peekMutatedRootIDs = gulu.Str.RemoveDuplicatedElem(peekMutatedRootIDs)
	}
	return
}

// Clear clears the undo log. If rootID is non-empty, it clears that document's stack and cascades removal
// of related entries from other stacks; if empty, it clears everything.
func (l *UndoLog) Clear(rootID string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if "" == rootID {
		l.stacks = map[string]*undoStack{}
		return
	}

	s := l.stacks[rootID]
	if nil == s {
		return
	}
	// collect the ids of all cross-document entries in this stack, to cascade their removal from other stacks
	linkedIDs := map[string]bool{}
	for _, e := range s.undoStack {
		for _, r := range e.mutatedRootIDs {
			if r != rootID {
				linkedIDs[e.id] = true
			}
		}
	}
	for _, e := range s.redoStack {
		for _, r := range e.mutatedRootIDs {
			if r != rootID {
				linkedIDs[e.id] = true
			}
		}
	}
	delete(l.stacks, rootID)
	for otherID, other := range l.stacks {
		for id := range linkedIDs {
			other.undoStack = removeEntryByID(other.undoStack, id)
			other.redoStack = removeEntryByID(other.redoStack, id)
		}
		_ = otherID
	}
}

// removeEntry removes an entry from rootID's stack by id (used for undo cascading).
func (l *UndoLog) removeEntry(rootID, id string) {
	s := l.stacks[rootID]
	if nil == s {
		return
	}
	s.undoStack = removeEntryByID(s.undoStack, id)
}

func removeEntryByID(stack []*UndoEntry, id string) []*UndoEntry {
	for i, e := range stack {
		if e.id == id {
			return append(stack[:i], stack[i+1:]...)
		}
	}
	return stack
}

// cloneOperations deep-copies a slice of operations (value-copying each Operation), decoupling the log
// entry from subsequent transactions. Functions like doInsert0 in performTx rewrite scalar fields such as
// operation.ID/Action in place; a shallow copy of the pointer would let an already-recorded entry be
// overwritten, invalidating the ID on redo replay. The value copy duplicates scalar fields, while
// Data (any) shares its reference (performTx does not modify its content).
func cloneOperations(ops []*Operation) []*Operation {
	if nil == ops {
		return nil
	}
	ret := make([]*Operation, len(ops))
	for i, op := range ops {
		cloned := *op // value-copy scalar fields (ID/Action/ParentID/PreviousID/NextID/AvID, etc.)
		ret[i] = &cloned
	}
	return ret
}

var dataNodeIDPattern = regexp.MustCompile(`data-node-id="([^"]+)"`)
var refcountAttrPattern = regexp.MustCompile(`\s*refcount="[^"]*"`)
var refcountDivPattern = regexp.MustCompile(`<div class="protyle-attr--refcount[^"]*"[^>]*>.*?</div>`)

// ResolveReplayDuplicateIds resolves block ID conflicts before replaying an undo/redo transaction.
// Scenario: block X is cut and pasted elsewhere (keeping its original ID); undoing the cut then inserts X,
// but X already exists where it was pasted, producing a duplicate ID.
// This checks the insert operations about to be replayed -- if an ID they introduce already exists in the
// block tree, it is uniformly replaced with a new ID across both the forward and reverse operations and
// their related fields (ID/ParentID/PreviousID/NextID and inline IDs in Data).
// The replacement is applied to both the do and undo operation sets: replay only executes doOperations, but
// if undoOperations is not updated in sync, a later redo of the same entry would reuse the old ID and
// collide again.
func ResolveReplayDuplicateIds(tx *Transaction) {
	if nil == tx || !tx.isReplay {
		return
	}

	// collect the block IDs introduced by all insert operations (op.ID plus inline data-node-id in Data)
	ids := map[string]struct{}{}
	collect := func(ops []*Operation) {
		for _, op := range ops {
			if "insert" != op.Action {
				continue
			}
			if "" != op.ID && ast.IsNodeIDPattern(op.ID) {
				ids[op.ID] = struct{}{}
			}
			data, ok := op.Data.(string)
			if !ok {
				continue
			}
			for _, m := range dataNodeIDPattern.FindAllStringSubmatch(data, -1) {
				if ast.IsNodeIDPattern(m[1]) {
					ids[m[1]] = struct{}{}
				}
			}
		}
	}
	collect(tx.DoOperations)
	// Note: only DoOperations (the operations actually being executed) are checked, not UndoOperations.
	// On redo, UndoOperations becomes the new DoOperations and goes through ResolveReplayDuplicateIds again.
	// If inserts in UndoOperations were also checked during undo, the ID used for redo would get replaced,
	// polluting the corresponding delete in DoOperations (do/undo share the same replacements map), causing
	// undo to delete the wrong ID.
	if 0 == len(ids) {
		return
	}

	idList := make([]string, 0, len(ids))
	for id := range ids {
		idList = append(idList, id)
	}
	exist := treenode.ExistBlockTrees(idList)

	// generate a replacement for each ID that already exists
	replacements := map[string]string{}
	for _, id := range idList {
		if exist[id] {
			replacements[id] = ast.NewNodeID()
		}
	}
	if 0 == len(replacements) {
		return
	}

	// uniformly replace IDs and related fields across both the do and undo operation sets
	apply := func(ops []*Operation) {
		for _, op := range ops {
			// record whether this operation's ID was replaced (checked before op.ID is modified, otherwise the replacements map's key is the old ID and the lookup would miss)
			_, idReplaced := replacements[op.ID]
			// Only insert operations have op.ID replaced. The ID declared by a delete operation is the old
			// block to be deleted itself; replacing it with a new ID would make doDelete silently skip it
			// for not finding the node, leaving the old block behind and producing a duplicate block after replay.
			// Typical scenario: undoing a list-to-paragraph conversion -- undo first deletes the flattened
			// child blocks, then inserts the original list (whose HTML inlines the same batch of child block
			// IDs); those child blocks are cleaned up by the preceding delete and should not participate in
			// conflict replacement.
			// https://github.com/siyuan-note/siyuan/issues/18012
			if "insert" == op.Action {
				if newID, ok := replacements[op.ID]; ok {
					op.ID = newID
				}
			}
			if newID, ok := replacements[op.ParentID]; ok {
				op.ParentID = newID
			}
			if newID, ok := replacements[op.PreviousID]; ok {
				op.PreviousID = newID
			}
			if newID, ok := replacements[op.NextID]; ok {
				op.NextID = newID
			}
			data, ok := op.Data.(string)
			if !ok {
				continue
			}
			for oldID, newID := range replacements {
				data = dataNodeIDPattern.ReplaceAllStringFunc(data, func(match string) string {
					if sub := dataNodeIDPattern.FindStringSubmatch(match); len(sub) > 1 && sub[1] == oldID {
						return `data-node-id="` + newID + `"`
					}
					return match
				})
			}
			// For a block whose ID was replaced (a copy restored by undoing a cut-and-paste), clear the
			// reference-count badge to avoid showing the stale refcount. The badge is rebuilt with the
			// correct value asynchronously by the kernel (refreshRefCount).
			if idReplaced {
				data = refcountDivPattern.ReplaceAllString(data, "")
				data = refcountAttrPattern.ReplaceAllString(data, "")
			}
			op.Data = data
		}
	}
	apply(tx.DoOperations)
}
