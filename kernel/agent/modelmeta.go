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
	"embed"
	"encoding/json"
	"strings"
	"sync"
)

//go:embed models.json
var modelsJSON embed.FS

// modelMetaEntry corresponds to a single model's value object in models.json; currently only contains contextLength.
type modelMetaEntry struct {
	ContextLength int `json:"contextLength"`
}

var (
	modelContextOnce  sync.Once
	modelContextLimit map[string]int
)

// loadModelContext lazily parses the embedded models.json, building a "model name -> context window" map.
// models.json has a reserved top-level _meta metadata object, which is skipped during parsing (its value
// structure differs from a model entry).
func loadModelContext() map[string]int {
	modelContextOnce.Do(func() {
		raw, err := modelsJSON.ReadFile("models.json")
		if err != nil {
			return
		}
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return
		}
		result := make(map[string]int, len(decoded))
		for name, value := range decoded {
			if name == "_meta" {
				continue
			}
			var entry modelMetaEntry
			if err := json.Unmarshal(value, &entry); err != nil {
				continue
			}
			if entry.ContextLength > 0 {
				// Store the key lowercased so matching is case-insensitive (whether the user enters
				// Baichuan4-Turbo or baichuan4-turbo, both hit).
				result[strings.ToLower(name)] = entry.ContextLength
			}
		}
		modelContextLimit = result
	})
	return modelContextLimit
}

// GetModelContextLimit returns the model's context window size (in tokens). Returns 0 for an unknown model.
// Matching is case-insensitive (both the table keys and the query parameter are lowercased).
// Match order: first an exact lookup by the full model name; then by the "last segment" (with the provider
// prefix stripped), to support users who enter an id with a prefix (such as z-ai/glm-4.6). The table itself
// is keyed by the last segment, so a last-segment match hits directly.
func GetModelContextLimit(model string) int {
	if model == "" {
		return 0
	}
	table := loadModelContext()
	if table == nil {
		return 0
	}
	lower := strings.ToLower(model)
	if limit, ok := table[lower]; ok {
		return limit
	}
	if idx := strings.LastIndexByte(lower, '/'); idx >= 0 {
		if limit, ok := table[lower[idx+1:]]; ok {
			return limit
		}
	}
	return 0
}
