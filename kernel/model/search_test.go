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

func TestValidEmbedBlockIDs(t *testing.T) {
	firstID := "20260721120000-block01"
	secondID := "20260721120001-block02"
	thirdID := "20260721120002-block03"
	ids := validEmbedBlockIDs([]string{firstID, "invalid", firstID, secondID, thirdID}, 2)
	if !slices.Equal(ids, []string{firstID, secondID}) {
		t.Fatalf("embed block IDs should preserve order, dedupe, and be limited in count: %v", ids)
	}
}

// TestIsValidSearchBoxPath covers validation of the notebook ID and document path search inputs, preventing SQL
// metacharacters from entering statement concatenation.
// The regression cases reference the SQL injection report against /api/search/fullTextSearchBlock (paths[] poisoning).
func TestIsValidSearchBoxPath(t *testing.T) {
	validBox := "20210808180117-6v0mkxr"

	validCases := []struct {
		name string
		box  string
		path string
	}{
		{"box scope only", validBox, ""},
		{"slash only", validBox, "/"},
		{"specific document", validBox, "/20210808180117-6v0mkxr.sy"},
		{"subtree directory scope", validBox, "/20210808180117-6v0mkxr"},
		{"full path to sub-document", validBox, "/20210808180117-6v0mkxr/20210808180530-a1b2c3d.sy"},
	}
	for _, tc := range validCases {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			if !IsValidSearchBoxPath(tc.box, tc.path) {
				t.Fatalf("expected valid: box=%q path=%q", tc.box, tc.path)
			}
		})
	}

	invalidCases := []struct {
		name string
		box  string
		path string
	}{
		// UNION poisoning payload from the report
		{
			"SQL injection UNION projection",
			validBox,
			"/x%') UNION SELECT id,parent_id FROM blocks WHERE path='/hidden.sy' -- ",
		},
		{"single quote breaks the string", validBox, "/doc'secret.sy"},
		{"leading percent sign", validBox, "/%abc"},
		{"comment marker", validBox, "/doc -- "},
		{"invalid box, short numeric", "123", ""},
		{"invalid box, uppercase", "20210808180117-6V0MKXR", ""},
		{"invalid box, empty", "", "/20210808180117-6v0mkxr.sy"},
		{"path missing leading slash", validBox, "20210808180117-6v0mkxr.sy"},
		{"path segment invalid", validBox, "/notanid.sy"},
		{"path middle segment invalid", validBox, "/20210808180117-6v0mkxr/notanid.sy"},
	}
	for _, tc := range invalidCases {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			if IsValidSearchBoxPath(tc.box, tc.path) {
				t.Fatalf("expected invalid: box=%q path=%q", tc.box, tc.path)
			}
		})
	}
}

// TestBuildBoxesPathFiltersArgCount verifies that the number of "?" placeholders produced by the parameterized
// filter matches the length of args.
func TestBuildBoxesPathFiltersArgCount(t *testing.T) {
	boxes := []string{"20210808180117-6v0mkxr", "20210808180117-a1b2c3d"}
	clause, args := buildBoxesFilter(boxes)
	if countPlaceholder(clause) != len(args) {
		t.Fatalf("box filter placeholder/arg mismatch: %q vs %d args", clause, len(args))
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 box args, got %d", len(args))
	}

	paths := []string{"/20210808180117-6v0mkxr", "/20210808180117-a1b2c3d/20210808180530-e5f6g7h.sy"}
	clause, args = buildPathsFilter(paths)
	if countPlaceholder(clause) != len(args) {
		t.Fatalf("path filter placeholder/arg mismatch: %q vs %d args", clause, len(args))
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 path args, got %d", len(args))
	}
	for i, a := range args {
		s, ok := a.(string)
		if !ok || s != paths[i]+"%" {
			t.Fatalf("path arg %d should be %q%%, got %v", i, paths[i], a)
		}
	}
}

func TestBuildRootIDExclusionFilter(t *testing.T) {
	rootIDs := []string{"20260716120000-abcdefg", "20260716120001-hijklmn"}
	clause, args := buildRootIDExclusionFilter(rootIDs, "b.")
	if " AND b.root_id NOT IN (?, ?)" != clause {
		t.Fatalf("unexpected root ID exclusion filter: %q", clause)
	}
	if countPlaceholder(clause) != len(args) || len(rootIDs) != len(args) {
		t.Fatalf("root ID filter placeholder/arg mismatch: %q vs %d args", clause, len(args))
	}
	for i, arg := range args {
		if rootIDs[i] != arg {
			t.Fatalf("root ID arg %d should be %q, got %v", i, rootIDs[i], arg)
		}
	}

	clause, args = buildRootIDExclusionFilter(nil)
	if "" != clause || 0 != len(args) {
		t.Fatalf("empty root IDs should not generate a filter: %q, %v", clause, args)
	}
}

func TestNormalizeBoxName(t *testing.T) {
	name := "  notebook/name\x00  "
	if normalized := normalizeBoxName(name); "notebookname" != normalized {
		t.Fatalf("unexpected normalized notebook name: %q", normalized)
	}
}

func countPlaceholder(s string) (n int) {
	for i := 0; i < len(s); i++ {
		if s[i] == '?' {
			n++
		}
	}
	return
}

func TestBuildRefUsedOrderBy(t *testing.T) {
	newestID := "20260714120000-newest1"
	olderID := "20260714110000-older01"
	invalidID := "invalid-id' OR 1=1 --"
	orderBy := buildRefUsedOrderBy(map[string]int64{
		olderID:   100,
		newestID:  200,
		invalidID: 300,
	})

	newestPos := strings.Index(orderBy, newestID)
	olderPos := strings.Index(orderBy, olderID)
	if 0 > newestPos || 0 > olderPos || newestPos >= olderPos {
		t.Fatalf("more recently used blocks should be ordered before less recently used ones: %q", orderBy)
	}
	if strings.Contains(orderBy, invalidID) {
		t.Fatalf("order-by statement should not contain the invalid block ID: %q", orderBy)
	}
	if !strings.HasSuffix(orderBy, "END ASC, ") {
		t.Fatalf("order-by statement has the wrong format: %q", orderBy)
	}
}

func TestBuildRefUsedOrderByEmpty(t *testing.T) {
	if orderBy := buildRefUsedOrderBy(nil); "" != orderBy {
		t.Fatalf("an empty record set should not produce an order-by statement: %q", orderBy)
	}
}

func TestBuildOrderByPrioritizesExactDocumentAndHeading(t *testing.T) {
	orderBy := buildOrderBy("数学", 0, 0)
	assertOrderBySequence(t, orderBy,
		"content = '数学' AND type = 'd'",
		"content LIKE '%数学%' AND type = 'd'",
		"content = '数学' AND type = 'h'",
		"content LIKE '%数学%' AND type = 'h'",
		"sort ASC",
	)

	orderBy = buildOrderBy("数学", 0, 7)
	assertOrderBySequence(t, orderBy,
		"content = '数学' AND type = 'd'",
		"content = '数学' AND type = 'h'",
		"rank",
	)

	orderBy = buildOrderBy("数学", 0, 6)
	if strings.Contains(orderBy, "content = '数学'") {
		t.Fatalf("ascending relevance sort should not put exact-match results at the top: %q", orderBy)
	}
}

func TestBuildOrderByEscapesKeyword(t *testing.T) {
	orderBy := buildOrderBy("O'Reilly", 0, 7)
	if !strings.Contains(orderBy, "content = 'O''Reilly'") {
		t.Fatalf("keyword in the order-by statement was not escaped correctly: %q", orderBy)
	}
}

func assertOrderBySequence(t *testing.T, orderBy string, fragments ...string) {
	t.Helper()
	previous := -1
	for _, fragment := range fragments {
		current := strings.Index(orderBy, fragment)
		if 0 > current {
			t.Fatalf("order-by statement is missing %q: %q", fragment, orderBy)
		}
		if current <= previous {
			t.Fatalf("order-by priority sequence is wrong, %q did not appear at the expected position: %q", fragment, orderBy)
		}
		previous = current
	}
}
