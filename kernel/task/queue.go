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

package task

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/88250/gulu"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/util"
)

var (
	taskQueue []*Task
	queueLock = sync.Mutex{}
)

type Task struct {
	Action  string
	Handler reflect.Value
	Args    []any
	Created time.Time
	Async   bool // true means it's an async task, which doesn't block the task queue and runs immediately once the Delay condition is satisfied
	Delay   time.Duration
	Timeout time.Duration
}

func AppendTask(action string, handler any, args ...any) {
	appendTaskWithDelayTimeout(action, false, 0, 24*time.Hour, handler, args...)
}

func AppendAsyncTaskWithDelay(action string, delay time.Duration, handler any, args ...any) {
	appendTaskWithDelayTimeout(action, true, delay, 24*time.Hour, handler, args...)
}

func AppendTaskWithTimeout(action string, timeout time.Duration, handler any, args ...any) {
	appendTaskWithDelayTimeout(action, false, 0, timeout, handler, args...)
}

func appendTaskWithDelayTimeout(action string, async bool, delay, timeout time.Duration, handler any, args ...any) {
	if util.IsExiting.Load() {
		//logging.LogWarnf("task queue is paused, action [%s] will be ignored", action)
		return
	}

	task := &Task{
		Action:  action,
		Handler: reflect.ValueOf(handler),
		Args:    args,
		Created: time.Now(),
		Async:   async,
		Delay:   delay,
		Timeout: timeout,
	}

	if gulu.Str.Contains(action, uniqueActions) {
		if currentTasks := getCurrentTasks(); containTask(task, currentTasks) {
			//logging.LogWarnf("task [%s] is already in queue, will be ignored", action)
			return
		}
	}

	queueLock.Lock()
	defer queueLock.Unlock()
	taskQueue = append(taskQueue, task)
}

func containTask(task *Task, tasks []*Task) bool {
	for _, t := range tasks {
		if t.Action == task.Action {
			if len(t.Args) != len(task.Args) {
				return false
			}

			for i, arg := range t.Args {
				if !areArgsEqual(arg, task.Args[i]) {
					return false
				}
			}
			return true
		}
	}
	return false
}

// areArgsEqual compares whether two arguments are equal
func areArgsEqual(a, b any) bool {

	// If both arguments are nil
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Fast path for common basic types
	switch av := a.(type) {
	case string:
		if bv, ok := b.(string); ok {
			return av == bv
		}
	case int:
		if bv, ok := b.(int); ok {
			return av == bv
		}
	case int64:
		if bv, ok := b.(int64); ok {
			return av == bv
		}
	case int32:
		if bv, ok := b.(int32); ok {
			return av == bv
		}
	case bool:
		if bv, ok := b.(bool); ok {
			return av == bv
		}
	case float64:
		if bv, ok := b.(float64); ok {
			return av == bv
		}
	case float32:
		if bv, ok := b.(float32); ok {
			return av == bv
		}
	case uint:
		if bv, ok := b.(uint); ok {
			return av == bv
		}
	case uint64:
		if bv, ok := b.(uint64); ok {
			return av == bv
		}
	case uint32:
		if bv, ok := b.(uint32); ok {
			return av == bv
		}
	case []string:
		if bv, ok := b.([]string); ok {
			if len(av) != len(bv) {
				return false
			}
			for i := range av {
				if av[i] != bv[i] {
					return false
				}
			}
			return true
		}
	case []int:
		if bv, ok := b.([]int); ok {
			if len(av) != len(bv) {
				return false
			}
			for i := range av {
				if av[i] != bv[i] {
					return false
				}
			}
			return true
		}
	}

	// For unhandled complex types, fall back to reflect.DeepEqual
	return reflect.DeepEqual(a, b)
}

func getCurrentTasks() (ret []*Task) {
	queueLock.Lock()
	defer queueLock.Unlock()

	currentTaskLock.Lock()
	if nil != currentTask {
		ret = append(ret, currentTask)
	}
	currentTaskLock.Unlock()

	for _, task := range taskQueue {
		ret = append(ret, task)
	}
	return
}

const (
	RepoCheckout        = "task.repo.checkout"         // checkout from a snapshot
	RepoAutoPurge       = "task.repo.autoPurge"        // automatically purge the data repository
	DatabaseIndexFull   = "task.database.index.full"   // rebuild the index
	DatabaseIndexFTS    = "task.database.index.fts"    // rebuild the search index
	DatabaseIndex       = "task.database.index"        // database index
	DatabaseIndexCommit = "task.database.index.commit" // database index commit
	DatabaseIndexRef    = "task.database.index.ref"    // database index reference

	OCRImage                          = "task.ocr.image"                            // extract text from an image via OCR
	HistoryGenerateFile               = "task.history.generateFile"                 // generate the file history
	HistoryDatabaseIndexFull          = "task.history.database.index.full"          // rebuild the history database index
	HistoryDatabaseIndexCommit        = "task.history.database.index.commit"        // history database index commit
	DatabaseIndexEmbedBlock           = "task.database.index.embedBlock"            // database index embed block
	ReloadUI                          = "task.reload.ui"                            // reload the UI
	AssetContentDatabaseIndexFull     = "task.asset.database.index.full"            // rebuild the asset file database index
	AssetContentDatabaseIndexCommit   = "task.asset.database.index.commit"          // asset file database index commit
	DatabaseIndexEmbeddingFull        = "task.database.index.embedding.full"        // rebuild the embedding vector index
	DatabaseIndexEmbeddingRetryFailed = "task.database.index.embedding.retryFailed" // retry failed blocks for embedding vectors
	CacheVirtualBlockRef              = "task.cache.virtualBlockRef"                // cache virtual block references
	ReloadAttributeView               = "task.reload.attributeView"                 // reload the attribute view
	ReloadProtyle                     = "task.reload.protyle"                       // reload the editor
	ReloadTag                         = "task.reload.tag"                           // reload the tag panel
	ReloadFiletree                    = "task.reload.filetree"                      // reload the file tree panel
	SetRefDynamicText                 = "task.ref.setDynamicText"                   // set the dynamic anchor text of a reference
	SetDefRefCount                    = "task.def.setRefCount"                      // set the reference count of a definition
	UpdateIDs                         = "task.update.ids"                           // update IDs
	PushMsg                           = "task.push.msg"                             // push a message
)

// uniqueActions describes unique tasks, meaning at most one instance of the task can exist executing in the queue at a time.
var uniqueActions = []string{
	RepoCheckout,
	RepoAutoPurge,
	DatabaseIndexFull,
	DatabaseIndexFTS,
	DatabaseIndexCommit,
	OCRImage,
	HistoryGenerateFile,
	HistoryDatabaseIndexFull,
	HistoryDatabaseIndexCommit,
	AssetContentDatabaseIndexFull,
	AssetContentDatabaseIndexCommit,
	DatabaseIndexEmbeddingFull,
	DatabaseIndexEmbeddingRetryFailed,
	ReloadAttributeView,
	ReloadProtyle,
	ReloadTag,
	ReloadFiletree,
	SetRefDynamicText,
	SetDefRefCount,
	UpdateIDs,
}

func ContainIndexTask() bool {
	tasks := getCurrentTasks()
	for _, task := range tasks {
		if gulu.Str.Contains(task.Action, []string{DatabaseIndexFull, DatabaseIndex}) {
			return true
		}
	}
	return false
}

func StatusJob() {
	var items []map[string]any
	count := map[string]int{}
	actionLangs := util.TaskActionLangs[util.Lang]

	queueLock.Lock()
	for _, task := range taskQueue {
		action := task.Action
		if c := count[action]; 7 < c {
			logging.LogWarnf("too many tasks [%s], ignore show its status", action)
			continue
		}
		count[action]++

		if skipPushTaskAction(action) {
			continue
		}

		if nil != actionLangs {
			if label := actionLangs[task.Action]; nil != label {
				action = label.(string)
			} else {
				continue
			}
		}

		item := map[string]any{"action": action}
		items = append(items, item)
	}
	defer queueLock.Unlock()

	currentTaskLock.Lock()
	if nil != currentTask && nil != actionLangs && !skipPushTaskAction(currentTask.Action) {
		if label := actionLangs[currentTask.Action]; nil != label {
			items = append([]map[string]any{{"action": label.(string)}}, items...)
		}
	}
	currentTaskLock.Unlock()

	if 1 > len(items) {
		items = []map[string]any{}
	}
	data := map[string]any{}
	data["tasks"] = items
	util.PushBackgroundTask(data)
}

func skipPushTaskAction(action string) bool {
	switch action {
	case DatabaseIndexCommit:
		return util.StatusBarCfg.MsgTaskDatabaseIndexCommitDisabled
	case HistoryDatabaseIndexCommit:
		return util.StatusBarCfg.MsgTaskHistoryDatabaseIndexCommitDisabled
	case AssetContentDatabaseIndexCommit:
		return util.StatusBarCfg.MsgTaskAssetDatabaseIndexCommitDisabled
	case HistoryGenerateFile:
		return util.StatusBarCfg.MsgTaskHistoryGenerateFileDisabled
	default:
		return false
	}
}

func ExecTaskJob() {
	task := popTask()
	if nil == task {
		return
	}

	if util.IsExiting.Load() {
		return
	}

	execTask(task)
}

func popTask() (ret *Task) {
	queueLock.Lock()
	defer queueLock.Unlock()

	if 1 > len(taskQueue) {
		return
	}

	for i, task := range taskQueue {
		if time.Since(task.Created) <= task.Delay {
			continue
		}

		if !task.Async {
			ret = task
			taskQueue = append(taskQueue[:i], taskQueue[i+1:]...)
			return
		}
	}
	return
}

func ExecAsyncTaskJob() {
	tasks := popAsyncTasks()
	if 1 > len(tasks) {
		return
	}

	if util.IsExiting.Load() {
		return
	}

	for _, task := range tasks {
		go func() {
			execTask(task)
		}()
	}
}

func popAsyncTasks() (ret []*Task) {
	queueLock.Lock()
	defer queueLock.Unlock()

	if 1 > len(taskQueue) {
		return
	}

	// writeIdx points to the next position to write
	writeIdx := 0
	for readIdx := 0; readIdx < len(taskQueue); readIdx++ {
		task := taskQueue[readIdx]

		// Determine whether this task should be popped
		shouldPop := task.Async && time.Since(task.Created) > task.Delay
		if shouldPop {
			ret = append(ret, task)
			// Not written back to taskQueue, effectively deleting it
		} else {
			// Keep this task, moving it to the writeIdx position
			if writeIdx != readIdx {
				taskQueue[writeIdx] = task
			}
			writeIdx++
		}
	}

	// Clear the references at the tail of the queue, to prevent a memory leak
	for i := writeIdx; i < len(taskQueue); i++ {
		taskQueue[i] = nil
	}
	taskQueue = taskQueue[:writeIdx]
	return
}

var (
	currentTask     *Task
	currentTaskLock = sync.Mutex{}
)

func execTask(task *Task) {
	if nil == task {
		return
	}

	defer logging.Recover()

	args := make([]reflect.Value, len(task.Args))
	for i, v := range task.Args {
		if nil == v {
			args[i] = reflect.New(task.Handler.Type().In(i)).Elem()
		} else {
			args[i] = reflect.ValueOf(v)
		}
	}

	if !task.Async {
		currentTaskLock.Lock()
		currentTask = task
		currentTaskLock.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), task.Timeout)
	defer cancel()
	ch := make(chan bool, 1)
	go func() {
		task.Handler.Call(args)
		ch <- true
	}()

	select {
	case <-ctx.Done():
		logging.LogWarnf("task [%s] timeout", task.Action)
	case <-ch:
		//logging.LogInfof("task [%s] done", task.Action)
	}

	if !task.Async {
		currentTaskLock.Lock()
		currentTask = nil
		currentTaskLock.Unlock()
	}
}
