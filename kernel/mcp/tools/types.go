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

import "context"

const (
	EffectScopeLocal    = "local"
	EffectScopeExternal = "external"
	EffectScopeMixed    = "mixed"
	EffectScopeUnknown  = "unknown"
)

type Tool struct {
	Name         string      `json:"name"`
	Title        string      `json:"title,omitempty"`
	Description  string      `json:"description"`
	InputSchema  ToolSchema  `json:"inputSchema"`
	OutputSchema *ToolSchema `json:"outputSchema,omitempty"`
	// Source marks the tool's origin: "native" (SiYuan built-in), "plugin" (registered by a plugin), "mcp"
	// (external MCP service).
	// Used to split token breakdown statistics by source. An empty value is treated as "native" (for backward
	// compatibility with old callers).
	Source string `json:"source,omitempty"`
	// ReadOnlyHint is true only when an external tool explicitly declares itself read-only; when undeclared,
	// it is treated as a possible write and confirmation is required.
	ReadOnlyHint bool `json:"readOnlyHint,omitempty"`
	// EffectScope describes the scope of impact of a write operation, used to determine whether a local data
	// repository snapshot has rollback value.
	EffectScope string `json:"effectScope,omitempty"`
	// ActionEffects describes, per action, local read/write, data egress, and external cost, for the agent to
	// precisely drive confirmation and snapshot policy.
	ActionEffects map[string]ToolEffects `json:"-"`

	Handler        func(args map[string]any) (CallToolResult, error)                      `json:"-"`
	ContextHandler func(ctx context.Context, args map[string]any) (CallToolResult, error) `json:"-"`
}

type ToolEffects struct {
	LocalRead    bool `json:"localRead,omitempty"`
	LocalWrite   bool `json:"localWrite,omitempty"`
	DataEgress   bool `json:"dataEgress,omitempty"`
	ExternalCost bool `json:"externalCost,omitempty"`
}

func (t *Tool) EffectsFor(action string) (ToolEffects, bool) {
	if t == nil || t.ActionEffects == nil {
		return ToolEffects{}, false
	}
	effects, ok := t.ActionEffects[action]
	return effects, ok
}

type ToolSchema struct {
	Type       string                `json:"type,omitempty"`
	Properties map[string]Property   `json:"properties,omitempty"`
	Required   []string              `json:"required,omitempty"`
	OneOf      []ToolSchema          `json:"oneOf,omitempty"`
	AnyOf      []ToolSchema          `json:"anyOf,omitempty"`
	AllOf      []ToolSchema          `json:"allOf,omitempty"`
	Ref        string                `json:"$ref,omitempty"`
	Defs       map[string]ToolSchema `json:"$defs,omitempty"`
}

type Property struct {
	Type        string              `json:"type,omitempty"`
	Description string              `json:"description,omitempty"`
	Enum        []string            `json:"enum,omitempty"`
	Items       *Property           `json:"items,omitempty"`
	Properties  map[string]Property `json:"properties,omitempty"`
	Required    []string            `json:"required,omitempty"`
	OneOf       []Property          `json:"oneOf,omitempty"`
	AnyOf       []Property          `json:"anyOf,omitempty"`
	AllOf       []Property          `json:"allOf,omitempty"`
	Ref         string              `json:"$ref,omitempty"`
}

type CallToolResult struct {
	Content          []ContentItem `json:"content"`
	IsError          bool          `json:"isError,omitempty"`
	ExecutionUnknown bool          `json:"-"`
}

type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
