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
	"slices"
	"strings"
	"testing"
)

func TestAssetContentFieldRegexpUsesArguments(t *testing.T) {
	payload := "x'); DELETE FROM asset_contents_fts_case_insensitive; --"
	clause, args := assetContentFieldRegexp(payload)
	if "(name REGEXP ? OR content REGEXP ?)" != clause {
		t.Fatalf("regex filter clause does not use a placeholder: %q", clause)
	}
	if strings.Contains(clause, payload) {
		t.Fatalf("regex filter clause should not contain user input: %q", clause)
	}
	if 2 != len(args) || payload != args[0] || payload != args[1] {
		t.Fatalf("regex filter arguments are wrong: %v", args)
	}
}

func TestBuildAssetContentTypeFilterUsesArguments(t *testing.T) {
	payload := ".pdf'); DELETE FROM asset_contents_fts_case_insensitive; --"
	clause, args := buildAssetContentTypeFilter(map[string]bool{
		".txt":  true,
		payload: true,
		".md":   false,
	})
	if " AND ext IN (?, ?)" != clause {
		t.Fatalf("asset type filter clause is wrong: %q", clause)
	}
	if strings.Contains(clause, payload) {
		t.Fatalf("asset type filter clause should not contain user input: %q", clause)
	}
	if !slices.Equal(args, []any{payload, ".txt"}) {
		t.Fatalf("asset type filter arguments are wrong: %v", args)
	}
}

func TestBuildAssetContentTypeFilterEmpty(t *testing.T) {
	clause, args := buildAssetContentTypeFilter(nil)
	if "" != clause || 0 != len(args) {
		t.Fatalf("no filter condition should be generated when no asset type is specified: %q %v", clause, args)
	}

	clause, args = buildAssetContentTypeFilter(map[string]bool{".txt": false})
	if " AND 1 = 0" != clause || 0 != len(args) {
		t.Fatalf("when all asset types are disabled, an always-empty-result condition should be returned: %q %v", clause, args)
	}
}

func TestPDFParser(t *testing.T) {
	p := &PdfAssetParser{}
	res := p.Parse("../testdata/parsertest.pdf")
	if res == nil || res.Content == "" {
		t.Fatalf("empty or nil PDF content result")
	}
}
