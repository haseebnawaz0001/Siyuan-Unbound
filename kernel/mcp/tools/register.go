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

package tools

import (
	"sort"
	"strings"
	"sync"
)

var registryMu sync.RWMutex
var Registry = map[string]*Tool{}

func GetTool(name string) *Tool {
	return LookupTool(name)
}

func LookupTool(name string) *Tool {
	registryMu.RLock()
	defer registryMu.RUnlock()

	name = strings.TrimSpace(name)

	if t := Registry[name]; t != nil {
		return t
	}

	lower := strings.ToLower(name)
	for k, v := range Registry {
		if strings.ToLower(k) == lower {
			return v
		}
	}

	if prefix := "siyuan_"; strings.HasPrefix(lower, prefix) {
		base := strings.TrimPrefix(lower, prefix)
		for k, v := range Registry {
			if strings.ToLower(k) == base {
				return v
			}
		}
	}

	return nil
}

// GetAllTools returns the tools currently on offer. Tools the user has opted out of are withheld here rather than at
// each call site, so both the agent's tool list and the tools advertised to external MCP clients are filtered by the
// one check; a withheld tool is never described to any model.
func GetAllTools() []*Tool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	result := make([]*Tool, 0, len(Registry))
	for _, t := range Registry {
		if !toolOffered(t) {
			continue
		}
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// toolOffered reports whether a registered tool should be advertised. GetTool stays unfiltered so a direct lookup
// still resolves and the handler can explain itself instead of the caller seeing an unknown tool.
func toolOffered(t *Tool) bool {
	if nil == t {
		return false
	}
	if "web_search" == t.Name {
		return WebSearchEnabled()
	}
	return true
}

func SetTool(name string, t *Tool) {
	registryMu.Lock()
	defer registryMu.Unlock()
	Registry[name] = t
}

func RemoveTool(name string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	delete(Registry, name)
}

func RemoveToolIf(name string, tool *Tool) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if Registry[name] == tool {
		delete(Registry, name)
	}
}

func register(t *Tool) {
	if t.Source == "" {
		t.Source = "native"
	}
	SetTool(t.Name, t)
}
