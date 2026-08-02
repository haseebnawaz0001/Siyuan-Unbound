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
	"github.com/siyuan-note/siyuan/kernel/model"
	"github.com/siyuan-note/siyuan/kernel/util"
)

var WebSearchTool = &Tool{
	Name:        "web_search",
	Description: "Web search via Exa, returns text results. Action: query(keywords).",
	InputSchema: ToolSchema{
		Type: "object",
		Properties: map[string]Property{
			"query": {Type: "string", Description: "Search query keywords"},
		},
		Required: []string{"query"},
	},
	Handler: webSearchHandler,
}

func init() {
	register(WebSearchTool)
}

// WebSearchEnabled reports whether the user has opted in to web search. It is off by default because the query is
// sent to Exa, a third party, rather than to the user's own model provider.
func WebSearchEnabled() bool {
	return nil != model.Conf && nil != model.Conf.AI && nil != model.Conf.AI.WebSearch && model.Conf.AI.WebSearch.Enabled
}

func webSearchHandler(args map[string]any) (CallToolResult, error) {
	if !WebSearchEnabled() {
		// GetAllTools already withholds this tool when it is disabled, so a model never sees it. This guards the
		// direct-lookup path a caller could still reach, and says why rather than reporting an unknown tool.
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: "web_search is disabled; enable it in Settings - AI - Web search"}},
			IsError: true,
		}, nil
	}

	query, _ := args["query"].(string)

	exaAPIKey := ""
	if nil != model.Conf && nil != model.Conf.AI && nil != model.Conf.AI.WebSearch {
		exaAPIKey = model.Conf.AI.WebSearch.ExaAPIKey
	}
	result, err := util.WebSearch(query, exaAPIKey)
	if err != nil {
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: "web_search error: " + err.Error()}},
			IsError: true,
		}, nil
	}

	return CallToolResult{
		Content: []ContentItem{{Type: "text", Text: result}},
	}, nil
}
