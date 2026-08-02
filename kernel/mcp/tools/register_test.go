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
	"testing"

	"github.com/siyuan-note/siyuan/kernel/conf"
	"github.com/siyuan-note/siyuan/kernel/model"
)

func TestGetAllToolsSorted(t *testing.T) {
	allTools := GetAllTools()
	for i := 1; i < len(allTools); i++ {
		if allTools[i-1].Name > allTools[i].Name {
			t.Fatalf("tools are not sorted: %q appears before %q", allTools[i-1].Name, allTools[i].Name)
		}
	}
}

// TestWebSearchWithheldUntilEnabled pins the privacy guarantee behind the web search setting: while it is off the
// tool must not appear in the advertised list at all, so no model -- the built-in agent's or an external MCP
// client's -- is ever told it exists. GetTool stays unfiltered so the handler can explain itself.
func TestWebSearchWithheldUntilEnabled(t *testing.T) {
	if nil == model.Conf {
		model.Conf = &model.AppConf{}
	}
	if nil == model.Conf.AI {
		model.Conf.AI = conf.NewAI()
	}
	original := model.Conf.AI.WebSearch
	defer func() { model.Conf.AI.WebSearch = original }()

	listed := func() bool {
		for _, t := range GetAllTools() {
			if "web_search" == t.Name {
				return true
			}
		}
		return false
	}

	model.Conf.AI.WebSearch = &conf.WebSearch{Enabled: false}
	if listed() {
		t.Fatal("web_search must not be advertised while it is disabled")
	}
	if nil == GetTool("web_search") {
		t.Fatal("a disabled tool must still resolve by name so its handler can report why it refused")
	}

	model.Conf.AI.WebSearch = &conf.WebSearch{Enabled: true}
	if !listed() {
		t.Fatal("web_search must be advertised once the user enables it")
	}

	model.Conf.AI.WebSearch = nil
	if listed() {
		t.Fatal("an absent web search config must be treated as disabled")
	}
}
