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

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/88250/gulu"
	"github.com/siyuan-note/filelock"
)

const (
	toolNotExecutedResult = "Tool was not executed because the turn was interrupted."
	toolUnknownResult     = "Tool execution was interrupted; the result is unknown. Do not retry automatically."
)

type agentRuntime struct {
	SchemaVersion int                `json:"schemaVersion"`
	Revision      int64              `json:"revision"`
	SessionID     string             `json:"sessionID"`
	AlwaysAllow   bool               `json:"alwaysAllow,omitempty"`
	ActiveTurn    *agentRuntimeTurn  `json:"activeTurn,omitempty"`
	Compaction    *runtimeCompaction `json:"compaction,omitempty"`
}

type agentRuntimeTurn struct {
	TurnID            string         `json:"turnID"`
	Mode              string         `json:"mode"`
	UserEntryID       string         `json:"userEntryID"`
	TargetUserEntryID string         `json:"targetUserEntryID,omitempty"`
	UserContent       string         `json:"userContent,omitempty"`
	UserReferences    *[]Reference   `json:"userReferences,omitempty"`
	UserEditorContext *EditorContext `json:"userEditorContext,omitempty"`
	BaseRevision      int64          `json:"baseRevision"`
	State             string         `json:"state"`
	Delta             []AgentMessage `json:"delta,omitempty"`
	DraftContent      string         `json:"draftContent,omitempty"`
	SnapshotIDs       []string       `json:"snapshotIDs,omitempty"`
	PromptTokens      int            `json:"promptTokens,omitempty"`
	CompletionTokens  int            `json:"completionTokens,omitempty"`
	LastPromptTokens  int            `json:"lastPromptTokens,omitempty"`
	CachedTokens      int            `json:"cachedTokens,omitempty"`
	ContextLimit      int            `json:"contextLimit,omitempty"`
	TokenBreakdown    map[string]int `json:"tokenBreakdown,omitempty"`
	UpdatedAt         int64          `json:"updatedAt"`
}

type runtimeCompaction struct {
	Summary           string `json:"summary"`
	CoveredEntryCount int    `json:"coveredEntryCount"`
	CoveredDigest     string `json:"coveredDigest"`
}

func runtimePath(sessionID string) string {
	return filepath.Join(sessionsDir(), sessionID, "runtime.json")
}

func loadRuntimeLocked(sessionID string) (*agentRuntime, error) {
	data, err := os.ReadFile(runtimePath(sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return &agentRuntime{SchemaVersion: 1, SessionID: sessionID}, nil
		}
		return nil, err
	}
	var runtime agentRuntime
	if err := gulu.JSON.UnmarshalJSON(data, &runtime); err != nil {
		return nil, err
	}
	if runtime.SchemaVersion > 1 {
		return nil, fmt.Errorf("unsupported agent runtime schema version: %d", runtime.SchemaVersion)
	}
	if runtime.SessionID != "" && runtime.SessionID != sessionID {
		return nil, fmt.Errorf("agent runtime session id mismatch")
	}
	if runtime.Revision < 0 {
		return nil, fmt.Errorf("invalid agent runtime revision")
	}
	if runtime.ActiveTurn != nil {
		if runtime.ActiveTurn.TurnID == "" {
			return nil, fmt.Errorf("invalid agent runtime turn id")
		}
		switch runtime.ActiveTurn.State {
		case "running", "finished", "interrupted":
		default:
			return nil, fmt.Errorf("invalid agent runtime turn state")
		}
	}
	if runtime.SchemaVersion == 0 {
		runtime.SchemaVersion = 1
	}
	if runtime.SessionID == "" {
		runtime.SessionID = sessionID
	}
	return &runtime, nil
}

func writeRuntimeLocked(sessionID string, runtime *agentRuntime) error {
	if runtime == nil {
		return nil
	}
	// runtime can only attach to an already-existing session, to avoid a late checkpoint reviving a deleted session.
	if _, err := os.Stat(filepath.Join(sessionsDir(), sessionID, "session.json")); err != nil {
		return err
	}
	runtime.SchemaVersion = 1
	runtime.SessionID = sessionID
	runtime.Revision++
	data, err := gulu.JSON.MarshalIndentJSON(runtime, "", "\t")
	if err != nil {
		return err
	}
	return filelock.WriteFile(runtimePath(sessionID), data)
}

func beginRuntimeTurn(sessionID string, turn *agentRuntimeTurn, alwaysAllow bool) error {
	if sessionID == "" || turn == nil {
		return nil
	}
	if !isValidSessionID(sessionID) {
		return fmt.Errorf("invalid session id")
	}
	if turn.TurnID == "" || turn.State != "running" {
		return fmt.Errorf("invalid agent runtime turn")
	}
	lock := sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	runtime, err := loadRuntimeLocked(sessionID)
	if err != nil {
		return err
	}
	if runtime.ActiveTurn != nil && runtime.ActiveTurn.TurnID != turn.TurnID {
		committed, err := isTurnCommittedLocked(sessionID, runtime.ActiveTurn.TurnID)
		if err != nil {
			return err
		}
		if !committed {
			return fmt.Errorf("agent session has an uncommitted turn")
		}
		runtime.ActiveTurn = nil
	}
	data, err := os.ReadFile(filepath.Join(sessionsDir(), sessionID, "session.json"))
	if err != nil {
		return err
	}
	var session map[string]any
	if err := gulu.JSON.UnmarshalJSON(data, &session); err != nil {
		return err
	}
	if turn.BaseRevision >= 0 && numberToInt64(session["revision"]) != turn.BaseRevision {
		return ErrSessionConflict
	}
	if findRuntimeUserAnchor(session, turn.UserEntryID) < 0 {
		return fmt.Errorf("agent runtime user entry not found")
	}
	runtime.AlwaysAllow = runtime.AlwaysAllow || alwaysAllow
	runtime.ActiveTurn = turn
	return writeRuntimeLocked(sessionID, runtime)
}

func isTurnCommittedLocked(sessionID, turnID string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(sessionsDir(), sessionID, "session.json"))
	if err != nil {
		return false, err
	}
	var meta sessionMeta
	if err := gulu.JSON.UnmarshalJSON(data, &meta); err != nil {
		return false, err
	}
	return meta.LastCommittedTurnID == turnID, nil
}

func saveRuntimeTurn(sessionID string, turn *agentRuntimeTurn, alwaysAllow bool) error {
	if sessionID == "" || turn == nil {
		return nil
	}
	if !isValidSessionID(sessionID) {
		return fmt.Errorf("invalid session id")
	}
	lock := sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	committed, err := isTurnCommittedLocked(sessionID, turn.TurnID)
	if err != nil {
		return err
	}
	if committed {
		return nil
	}
	runtime, err := loadRuntimeLocked(sessionID)
	if err != nil {
		return err
	}
	if runtime.ActiveTurn != nil && runtime.ActiveTurn.TurnID != turn.TurnID {
		return fmt.Errorf("agent runtime turn changed")
	}
	runtime.AlwaysAllow = runtime.AlwaysAllow || alwaysAllow
	turn.UpdatedAt = time.Now().UnixMilli()
	runtime.ActiveTurn = turn
	return writeRuntimeLocked(sessionID, runtime)
}

func loadRuntimeState(sessionID string) (*agentRuntime, error) {
	if sessionID == "" || !isValidSessionID(sessionID) {
		return nil, nil
	}
	lock := sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	return loadRuntimeLocked(sessionID)
}

func FinalizeOrphanedTurn(sessionID string) error {
	if sessionID == "" || !isValidSessionID(sessionID) {
		return nil
	}
	lock := sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	runtime, err := loadRuntimeLocked(sessionID)
	if err != nil || runtime.ActiveTurn == nil || runtime.ActiveTurn.State != "running" {
		return err
	}
	runtime.ActiveTurn.State = "interrupted"
	runtime.ActiveTurn.UpdatedAt = time.Now().UnixMilli()
	return writeRuntimeLocked(sessionID, runtime)
}

func HasUncommittedTurn(sessionID string) (bool, error) {
	if sessionID == "" || !isValidSessionID(sessionID) {
		return false, nil
	}
	lock := sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	runtime, err := loadRuntimeLocked(sessionID)
	if err != nil || runtime.ActiveTurn == nil {
		return false, err
	}
	committed, err := isTurnCommittedLocked(sessionID, runtime.ActiveTurn.TurnID)
	if err != nil {
		return false, err
	}
	return !committed, nil
}

func RecoverableTurnID(sessionID string) (string, error) {
	if sessionID == "" || !isValidSessionID(sessionID) {
		return "", nil
	}
	lock := sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	runtime, err := loadRuntimeLocked(sessionID)
	if err != nil || runtime.ActiveTurn == nil || !isRuntimeTurnTerminal(runtime.ActiveTurn) {
		return "", err
	}
	committed, err := isTurnCommittedLocked(sessionID, runtime.ActiveTurn.TurnID)
	if err != nil || committed {
		return "", err
	}
	return runtime.ActiveTurn.TurnID, nil
}

func markRuntimeCommittedLocked(sessionID, turnID string) error {
	if turnID == "" {
		return nil
	}
	runtime, err := loadRuntimeLocked(sessionID)
	if err != nil {
		return err
	}
	if runtime.ActiveTurn == nil || runtime.ActiveTurn.TurnID != turnID {
		return nil
	}
	runtime.ActiveTurn = nil
	return writeRuntimeLocked(sessionID, runtime)
}

func isRuntimeTurnTerminal(turn *agentRuntimeTurn) bool {
	return turn != nil && (turn.State == "finished" || turn.State == "interrupted")
}

func findRuntimeUserAnchor(session map[string]any, userEntryID string) int {
	entries, _ := session["entries"].([]any)
	for i := len(entries) - 1; i >= 0; i-- {
		entry, _ := entries[i].(map[string]any)
		if entry["type"] != "user" {
			continue
		}
		id, _ := entry["id"].(string)
		if userEntryID == "" || id == userEntryID {
			return i
		}
	}
	return -1
}

func applyRuntimeTurnToSessionLocked(session map[string]any, turn *agentRuntimeTurn) error {
	if turn == nil {
		return nil
	}
	entries, _ := session["entries"].([]any)
	anchor := findRuntimeUserAnchor(session, turn.UserEntryID)
	if anchor < 0 {
		return fmt.Errorf("agent runtime user entry not found")
	}
	if turn.Mode == "regenerate" && turn.UserContent != "" {
		entry, _ := entries[anchor].(map[string]any)
		entry["content"] = turn.UserContent
		if turn.UserReferences != nil {
			if len(*turn.UserReferences) > 0 {
				entry["references"] = *turn.UserReferences
			} else {
				delete(entry, "references")
			}
		}
		if turn.UserEditorContext != nil {
			entry["editorContext"] = turn.UserEditorContext
		} else {
			delete(entry, "editorContext")
		}
	}

	// The runtime is authoritative for the current turn's assistant content; the frontend only contributes
	// UI entries like thinking/confirm/question.
	// Replacing frontend placeholders one by one with the authoritative assistant entries preserves the
	// relative position of UI entries as much as possible; when there is no placeholder, append to the end instead.
	authoritative := make([]any, 0, len(turn.Delta)+1)
	for i, message := range turn.Delta {
		if message.Role != "assistant" {
			continue
		}
		entry := map[string]any{
			"id":        fmt.Sprintf("runtime_%s_%d", turn.TurnID, i),
			"type":      "assistant",
			"timestamp": turn.UpdatedAt,
		}
		if message.Content != "" {
			entry["content"] = message.Content
		}
		if len(message.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				result := call.Result
				if result == "" {
					if call.State == "pending" {
						result = toolNotExecutedResult
					} else {
						result = toolUnknownResult
					}
				}
				calls = append(calls, map[string]any{
					"name":      call.Name,
					"arguments": call.Arguments,
					"result":    result,
					"state":     call.State,
				})
			}
			entry["toolCalls"] = calls
		}
		authoritative = append(authoritative, entry)
	}
	if turn.DraftContent != "" {
		authoritative = append(authoritative, map[string]any{
			"id":        fmt.Sprintf("runtime_draft_%s", turn.TurnID),
			"type":      "assistant",
			"content":   turn.DraftContent,
			"timestamp": turn.UpdatedAt,
		})
	}

	// Before starting, regenerate has already truncated the old answer back to the target user entry, so every
	// UI entry after that user entry belongs to the current turn.
	merged := append([]any(nil), entries[:anchor+1]...)
	authoritativeIndex := 0
	for _, raw := range entries[anchor+1:] {
		entry, _ := raw.(map[string]any)
		typeName, _ := entry["type"].(string)
		switch typeName {
		case "assistant":
			if authoritativeIndex < len(authoritative) {
				merged = append(merged, authoritative[authoritativeIndex])
				authoritativeIndex++
			}
		case "thinking", "confirm", "question", "snapshot", "rollback":
			merged = append(merged, raw)
		}
	}
	merged = append(merged, authoritative[authoritativeIndex:]...)

	existingSnapshots := map[string]bool{}
	for _, raw := range merged {
		entry, _ := raw.(map[string]any)
		if snapshotID, _ := entry["snapshotID"].(string); snapshotID != "" {
			existingSnapshots[snapshotID] = true
		}
	}
	for i, snapshotID := range turn.SnapshotIDs {
		if existingSnapshots[snapshotID] {
			continue
		}
		merged = append(merged, map[string]any{
			"id":         fmt.Sprintf("runtime_snapshot_%s_%d", turn.TurnID, i),
			"type":       "snapshot",
			"snapshotID": snapshotID,
		})
	}
	session["entries"] = merged
	if turn.PromptTokens > 0 || turn.CompletionTokens > 0 || turn.LastPromptTokens > 0 {
		session["promptTokens"] = turn.PromptTokens
		session["completionTokens"] = turn.CompletionTokens
		session["contextTokens"] = turn.LastPromptTokens
		session["contextCachedTokens"] = turn.CachedTokens
		session["contextLimit"] = turn.ContextLimit
		if len(turn.TokenBreakdown) > 0 {
			session["contextTokenBreakdown"] = turn.TokenBreakdown
		}
	}
	return nil
}

// mergeRuntimeIntoSessionLocked only overlays the uncommitted turn onto the API return value, it never rewrites session.json directly.
func mergeRuntimeIntoSessionLocked(sessionID string, session map[string]any) error {
	runtime, err := loadRuntimeLocked(sessionID)
	if err != nil {
		return err
	}
	if runtime.ActiveTurn == nil {
		return nil
	}
	turn := runtime.ActiveTurn
	if committed, _ := session["lastCommittedTurnID"].(string); committed == turn.TurnID {
		return nil
	}

	if err := applyRuntimeTurnToSessionLocked(session, turn); err != nil {
		return err
	}
	session["recoveryTurnID"] = turn.TurnID
	session["recoveryState"] = turn.State
	session["recoveryRevision"] = runtime.Revision
	return nil
}
