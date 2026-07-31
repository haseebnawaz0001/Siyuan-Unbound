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
	"encoding/json"
	"sync"

	"github.com/pkoukk/tiktoken-go"
	loader "github.com/pkoukk/tiktoken-go-loader"
	"github.com/sashabaranov/go-openai"
	tools "github.com/siyuan-note/siyuan/kernel/mcp/tools"
)

// tokenCounter uses tiktoken to count BPE tokens for text. The encoder is a singleton to avoid reloading the encoding table.
type tokenCounter struct {
	enc *tiktoken.Tiktoken
}

var (
	tokenCounterOnce sync.Once
	globalCounter    *tokenCounter
	tokenCounterErr  error
)

// getTokenCounter lazily initializes the global singleton counter. modelName is used to pick the encoding
// (GPT-4o -> o200k_base, others -> cl100k_base), falling back to cl100k_base on failure. The first call
// registers an offline BPE loader (an embedded encoding table), to avoid the SiYuan desktop client failing
// to download the encoding table over the network in offline/intranet environments.
func getTokenCounter(modelName string) (*tokenCounter, error) {
	tokenCounterOnce.Do(func() {
		tiktoken.SetBpeLoader(loader.NewOfflineLoader())
		enc, err := tiktoken.EncodingForModel(modelName)
		if err != nil {
			// Model name not recognized, fall back to cl100k_base (covers the GPT-3.5/4 family, the most common encoding).
			enc, err = tiktoken.GetEncoding("cl100k_base")
			if err != nil {
				tokenCounterErr = err
				return
			}
		}
		globalCounter = &tokenCounter{enc: enc}
	})
	return globalCounter, tokenCounterErr
}

// count returns the token count of text. Falls back to a character-based approximation when counter is nil.
func (c *tokenCounter) count(text string) int {
	if c == nil || c.enc == nil {
		return estimateTokensByChars(text)
	}
	return len(c.enc.Encode(text, nil, nil))
}

// estimateTokensByChars is a character-based approximation: Chinese at ~1.5 chars/token, other text at
// ~4 chars/token. Used only as a fallback when tiktoken is unavailable.
func estimateTokensByChars(text string) int {
	if text == "" {
		return 0
	}
	cjk := 0
	other := 0
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF || r >= 0x3040 && r <= 0x30FF || r >= 0xAC00 && r <= 0xD7AF {
			cjk++
		} else {
			other++
		}
	}
	return cjk*2/3 + other/4
}

// toolSource determines the origin (native/plugin/mcp) from the Source field set when the tool was registered.
// Tool names carry no prefix to distinguish them (native tool names are plain strings like block/document), so
// Tool.Source must be looked up. Compatibility fallback: legacy tool names with a plugin__ prefix are also
// recognized as plugin; a tool that can't be found is treated as mcp.
func toolSource(name string) string {
	if t := tools.GetTool(name); t != nil {
		if t.Source != "" {
			return t.Source
		}
	}
	// Fallback: plugin__ prefix (historical compatibility; in theory plugin tools already set Source).
	if len(name) > 8 && name[:8] == "plugin__" {
		return "plugin"
	}
	// Tool not found (possibly an uninstalled tool); classify as mcp (the rarest case).
	return "mcp"
}

// computeTokenBreakdown estimates context token usage across 10 categories.
// messages: the full message list sent to the LLM; tools: the list of function definitions; skillsTokens: the
// token count of just the <available_skills> segment in the system prompt; realPromptTokens: the actual prompt
// tokens returned by OpenAI.
// The returned map contains 10 keys: system/skills/messages/nativeToolsDef/pluginToolsDef/mcpToolsDef/
// nativeTool/pluginTool/mcpTool/other, where other = realPromptTokens - sum of the first 9 categories.
func computeTokenBreakdown(counter *tokenCounter, messages []openai.ChatCompletionMessage, tools []openai.Tool, skillsTokens, realPromptTokens int) map[string]int {
	breakdown := map[string]int{
		"system":         0,
		"skills":         skillsTokens,
		"messages":       0,
		"nativeToolsDef": 0,
		"pluginToolsDef": 0,
		"mcpToolsDef":    0,
		"nativeTool":     0,
		"pluginTool":     0,
		"mcpTool":        0,
		"other":          0,
	}

	// Tally messages by role. The system category accumulates all system messages (including runtime-appended
	// ones like doom-loop warnings), then subtracts skillsTokens (the skills segment forms its own category).
	// Following the OpenAI cookbook formula, add back the chat-format structural overhead: +4 tokens per
	// message (role marker + boundaries), +3 tokens for the whole conversation (priming). This structural
	// overhead is counted into the corresponding category's token count, reducing the "other" residual.
	systemTotal := 0
	// A tool message must be linked back to the preceding assistant's tool_call via ToolCallID to get the tool name.
	// Maintain the idToToolName map (populated when an assistant message carries tool_calls).
	idToToolName := map[string]string{}
	const perMessageOverhead = 4 // structural overhead per message (fixed OpenAI chat-format cost)
	for _, msg := range messages {
		switch msg.Role {
		case openai.ChatMessageRoleSystem:
			systemTotal += counter.count(msg.Content) + perMessageOverhead
		case openai.ChatMessageRoleUser:
			breakdown["messages"] += counter.count(msg.Content) + perMessageOverhead
		case openai.ChatMessageRoleAssistant:
			breakdown["messages"] += counter.count(msg.Content) + perMessageOverhead
			// The reasoning content of an assistant message (deepseek-reasoner, etc) is also counted into messages.
			if msg.ReasoningContent != "" {
				breakdown["messages"] += counter.count(msg.ReasoningContent)
			}
			for _, tc := range msg.ToolCalls {
				name := tc.Function.Name
				idToToolName[tc.ID] = name
				// The tool_call's function name + arguments are counted into the corresponding tool-call category.
				// Each tool_call structure has extra JSON structural overhead for id/type/function (about 7 tokens).
				callTokens := counter.count(name) + counter.count(tc.Function.Arguments) + 7
				switch toolSource(name) {
				case "native":
					breakdown["nativeTool"] += callTokens
				case "plugin":
					breakdown["pluginTool"] += callTokens
				default:
					breakdown["mcpTool"] += callTokens
				}
			}
		case openai.ChatMessageRoleTool:
			// A tool result is classified by its associated tool name.
			name := idToToolName[msg.ToolCallID]
			resultTokens := counter.count(msg.Content) + perMessageOverhead
			switch toolSource(name) {
			case "native":
				breakdown["nativeTool"] += resultTokens
			case "plugin":
				breakdown["pluginTool"] += resultTokens
			default:
				breakdown["mcpTool"] += resultTokens
			}
		}
	}
	breakdown["system"] = max(systemTotal-skillsTokens, 0)

	// Tally the tools definitions (function signatures): count tokens of the serialized JSON of each
	// Function's Name+Description+Parameters.
	// OpenAI has a fixed structural overhead per function definition (about 10 tokens: type/function
	// wrapper + field names), which is added back to reduce the deviation from real billing.
	const perToolDefOverhead = 10
	for _, t := range tools {
		if t.Function == nil {
			continue
		}
		defText := t.Function.Name + " " + t.Function.Description
		if paramsJSON, err := json.Marshal(t.Function.Parameters); err == nil {
			defText += " " + string(paramsJSON)
		}
		defTokens := counter.count(defText) + perToolDefOverhead
		switch toolSource(t.Function.Name) {
		case "native":
			breakdown["nativeToolsDef"] += defTokens
		case "plugin":
			breakdown["pluginToolsDef"] += defTokens
		default:
			breakdown["mcpToolsDef"] += defTokens
		}
	}

	// OpenAI has a fixed priming overhead for the whole conversation (about 3 tokens), counted into messages.
	if len(messages) > 0 {
		breakdown["messages"] += 3
	}

	// Align the sum of the estimates with the real prompt tokens, so the category percentages add up to 100%.
	// Estimate < real: the difference is counted into other (absorbing the underestimation residual).
	// Estimate > real: scale down the first 9 categories proportionally (absorbing the overestimation residual),
	// with the integer rounding residual counted into other (so as not to pollute a category that should be 0).
	// Without normalization, the frontend's category percentages would sum to more than 100% (tiktoken
	// estimation/overhead compensation can overestimate).
	estimated := 0
	for k, v := range breakdown {
		if k == "other" {
			continue
		}
		estimated += v
	}
	if realPromptTokens > estimated {
		breakdown["other"] = realPromptTokens - estimated
	} else if estimated > realPromptTokens && realPromptTokens > 0 {
		scale := float64(realPromptTokens) / float64(estimated)
		allocated := 0
		// Scale the first 9 categories proportionally; a category whose original value is 0 stays 0
		// (so it doesn't become a false positive value due to rounding).
		keys := []string{"system", "skills", "messages",
			"nativeToolsDef", "pluginToolsDef", "mcpToolsDef",
			"nativeTool", "pluginTool", "mcpTool"}
		for _, k := range keys {
			scaled := int(float64(breakdown[k]) * scale)
			breakdown[k] = scaled
			allocated += scaled
		}
		// The integer rounding residual is counted into other (may be positive or negative, clamped >= 0).
		breakdown["other"] = max(realPromptTokens-allocated, 0)
	}
	return breakdown
}
