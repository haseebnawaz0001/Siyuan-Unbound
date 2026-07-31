// DejaVu - Data snapshot and sync.
// Copyright (c) 2022-present, b3log.org
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package dejavu

import (
	"strings"
	"testing"

	"github.com/88250/lute"
	"github.com/88250/lute/ast"
	"github.com/88250/lute/parse"
	"github.com/siyuan-note/dataparser"
	"github.com/siyuan-note/dejavu/entity"
)

// These tests cover the decision table used by the block-level three-way merge. Upstream has no runnable sync test, so
// this is the safety net for the merge: every case that must auto-merge, and every case that must fall back to conflict
// handling rather than risk losing an edit.

// syDoc builds a minimal .sy document containing one paragraph block per entry of blocks, keyed by block ID.
func syDoc(t *testing.T, ids []string, text map[string]string) *parse.Tree {
	t.Helper()

	doc := `{"ID":"20240101120000-docroot","Spec":"1","Type":"NodeDocument","Properties":{"id":"20240101120000-docroot","title":"Test"},"Children":[`
	for i, id := range ids {
		if 0 < i {
			doc += ","
		}
		doc += `{"ID":"` + id + `","Type":"NodeParagraph","Properties":{"id":"` + id + `","updated":"20240101120000"},"Children":[{"Type":"NodeText","Data":"` + text[id] + `"}]}`
	}
	doc += `]}`

	luteEngine := lute.New()
	tree, err := dataparser.ParseJSONWithoutFix([]byte(doc), luteEngine.ParseOptions)
	if nil != err {
		t.Fatalf("parse synthetic .sy failed: %s", err)
	}
	return tree
}

func blockText(t *testing.T, tree *parse.Tree, id string) string {
	t.Helper()
	blocks := collectBlocks(tree)
	node, ok := blocks[id]
	if !ok {
		t.Fatalf("block [%s] missing from tree", id)
	}
	// Content() renders the block and includes its trailing newline, which is noise for these comparisons.
	return strings.TrimSpace(string(node.Content()))
}

// TestCollectBlocksIndexesByID checks the block index used by every other case, including that the document node itself
// is excluded.
func TestCollectBlocksIndexesByID(t *testing.T) {
	tree := syDoc(t, []string{"b1", "b2"}, map[string]string{"b1": "one", "b2": "two"})
	blocks := collectBlocks(tree)
	if 2 != len(blocks) {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	for _, id := range []string{"b1", "b2"} {
		if _, ok := blocks[id]; !ok {
			t.Fatalf("block [%s] not indexed", id)
		}
	}
	if _, ok := blocks["20240101120000-docroot"]; ok {
		t.Fatal("the document node must not be indexed as an editable block")
	}
}

// TestSameBlockIDs covers the structural guard: a merge is only attempted when no block was added, removed or retyped.
func TestSameBlockIDs(t *testing.T) {
	base := collectBlocks(syDoc(t, []string{"b1", "b2"}, map[string]string{"b1": "a", "b2": "b"}))

	same := collectBlocks(syDoc(t, []string{"b1", "b2"}, map[string]string{"b1": "changed", "b2": "b"}))
	if !sameBlockIDs(base, same) {
		t.Fatal("content-only differences must not count as a structural change")
	}

	added := collectBlocks(syDoc(t, []string{"b1", "b2", "b3"}, map[string]string{"b1": "a", "b2": "b", "b3": "c"}))
	if sameBlockIDs(base, added) {
		t.Fatal("an added block must count as a structural change")
	}

	removed := collectBlocks(syDoc(t, []string{"b1"}, map[string]string{"b1": "a"}))
	if sameBlockIDs(base, removed) {
		t.Fatal("a removed block must count as a structural change")
	}
}

// TestBlockChangedIgnoresFoldAndUpdated pins the rule that fold state and the updated timestamp are not user edits,
// matching how ignoreLocalUpsert already treats them. Getting this wrong would turn every outline collapse into a
// conflict.
func TestBlockChangedIgnoresFoldAndUpdated(t *testing.T) {
	a := collectBlocks(syDoc(t, []string{"b1"}, map[string]string{"b1": "same"}))["b1"]
	b := collectBlocks(syDoc(t, []string{"b1"}, map[string]string{"b1": "same"}))["b1"]

	if blockChanged(a, b) {
		t.Fatal("identical blocks must not be reported as changed")
	}

	b.SetIALAttr("fold", "1")
	b.SetIALAttr("updated", "20990101000000")
	if blockChanged(a, b) {
		t.Fatal("fold state and updated timestamp must not count as an edit")
	}

	b.SetIALAttr("style", "color:red")
	if !blockChanged(a, b) {
		t.Fatal("a real attribute change must count as an edit")
	}
}

// TestBlockChangedDetectsContentEdit is the positive case: different text is an edit.
func TestBlockChangedDetectsContentEdit(t *testing.T) {
	a := collectBlocks(syDoc(t, []string{"b1"}, map[string]string{"b1": "before"}))["b1"]
	b := collectBlocks(syDoc(t, []string{"b1"}, map[string]string{"b1": "after"}))["b1"]
	if !blockChanged(a, b) {
		t.Fatal("changed text must be reported as an edit")
	}
}

// TestHasBlockChildren guards the container-block rule. Substituting a container would carry its nested blocks along
// and discard decisions already made for them, so containers are never substituted.
func TestHasBlockChildren(t *testing.T) {
	leaf := collectBlocks(syDoc(t, []string{"b1"}, map[string]string{"b1": "text"}))["b1"]
	if hasBlockChildren(leaf) {
		t.Fatal("a paragraph holding only inline text is a leaf block")
	}

	container := &ast.Node{Type: ast.NodeBlockquote, ID: "c1"}
	container.AppendChild(&ast.Node{Type: ast.NodeParagraph, ID: "c2"})
	if !hasBlockChildren(container) {
		t.Fatal("a node with a block child is a container")
	}
}

// TestReplaceNodeSubstitutesInParent covers the substitution used when the local side wins a block: the replacement
// must take the original's place in the tree, and the original must be unlinked.
func TestReplaceNodeSubstitutesInParent(t *testing.T) {
	cloud := syDoc(t, []string{"b1", "b2"}, map[string]string{"b1": "cloud one", "b2": "shared"})
	local := syDoc(t, []string{"b1", "b2"}, map[string]string{"b1": "local one", "b2": "shared"})

	cloudBlocks, localBlocks := collectBlocks(cloud), collectBlocks(local)
	if !replaceNode(cloudBlocks["b1"], localBlocks["b1"]) {
		t.Fatal("replaceNode should succeed for a block with a parent")
	}

	if got := blockText(t, cloud, "b1"); "local one" != got {
		t.Fatalf("expected the local content to win, got %q", got)
	}
	if got := blockText(t, cloud, "b2"); "shared" != got {
		t.Fatalf("untouched block must be preserved, got %q", got)
	}
	if 2 != len(collectBlocks(cloud)) {
		t.Fatal("substitution must not change the number of blocks")
	}
}

// TestReplaceNodeRejectsOrphan checks the guard against substituting a node that has no parent, which would silently
// drop the replacement instead of inserting it.
func TestReplaceNodeRejectsOrphan(t *testing.T) {
	orphan := &ast.Node{Type: ast.NodeParagraph, ID: "b1"}
	replacement := &ast.Node{Type: ast.NodeParagraph, ID: "b1"}
	if replaceNode(orphan, replacement) {
		t.Fatal("replaceNode must refuse a node with no parent")
	}
	if replaceNode(nil, replacement) {
		t.Fatal("replaceNode must refuse a nil target")
	}
}

// TestMergeDecisionTable walks the per-block decision table that tryMergeSyBlocks applies, using the same helpers it
// uses. Each case names the row of the table it pins.
func TestMergeDecisionTable(t *testing.T) {
	const id = "b1"

	cases := []struct {
		name          string
		base          string
		local         string
		cloud         string
		wantTakeLocal bool
		wantConflict  bool
	}{
		{name: "neither side changed", base: "x", local: "x", cloud: "x"},
		{name: "only the cloud changed", base: "x", local: "x", cloud: "cloud"},
		{name: "only the local side changed", base: "x", local: "local", cloud: "x", wantTakeLocal: true},
		{name: "both sides made the same edit", base: "x", local: "same", cloud: "same"},
		{name: "both sides edited differently", base: "x", local: "local", cloud: "cloud", wantConflict: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			baseNode := collectBlocks(syDoc(t, []string{id}, map[string]string{id: c.base}))[id]
			localNode := collectBlocks(syDoc(t, []string{id}, map[string]string{id: c.local}))[id]
			cloudNode := collectBlocks(syDoc(t, []string{id}, map[string]string{id: c.cloud}))[id]

			localChanged := blockChanged(baseNode, localNode)
			cloudChanged := blockChanged(baseNode, cloudNode)

			var takeLocal, conflict bool
			switch {
			case !localChanged:
			case !cloudChanged:
				takeLocal = true
			case !blockChanged(localNode, cloudNode):
			default:
				conflict = true
			}

			if takeLocal != c.wantTakeLocal {
				t.Fatalf("takeLocal = %v, want %v", takeLocal, c.wantTakeLocal)
			}
			if conflict != c.wantConflict {
				t.Fatalf("conflict = %v, want %v", conflict, c.wantConflict)
			}
		})
	}
}

// TestMergeKeepsBothSidesEdits is the case the whole feature exists for: two devices edit different paragraphs of one
// document, and both edits survive.
func TestMergeKeepsBothSidesEdits(t *testing.T) {
	ids := []string{"b1", "b2", "b3"}
	base := syDoc(t, ids, map[string]string{"b1": "intro", "b2": "todo", "b3": "refs"})
	local := syDoc(t, ids, map[string]string{"b1": "intro", "b2": "TODO v2", "b3": "refs"})
	cloud := syDoc(t, ids, map[string]string{"b1": "INTRO!", "b2": "todo", "b3": "refs"})

	baseBlocks, localBlocks, cloudBlocks := collectBlocks(base), collectBlocks(local), collectBlocks(cloud)
	if !sameBlockIDs(baseBlocks, localBlocks) || !sameBlockIDs(baseBlocks, cloudBlocks) {
		t.Fatal("this fixture must be structurally identical on all three sides")
	}

	for id, baseNode := range baseBlocks {
		localChanged := blockChanged(baseNode, localBlocks[id])
		cloudChanged := blockChanged(baseNode, cloudBlocks[id])
		if localChanged && cloudChanged {
			t.Fatalf("block [%s] should not be edited on both sides in this fixture", id)
		}
		if localChanged && !cloudChanged {
			if !replaceNode(cloudBlocks[id], localBlocks[id]) {
				t.Fatalf("substituting block [%s] failed", id)
			}
		}
	}

	if got := blockText(t, cloud, "b1"); "INTRO!" != got {
		t.Fatalf("cloud edit lost: %q", got)
	}
	if got := blockText(t, cloud, "b2"); "TODO v2" != got {
		t.Fatalf("local edit lost: %q", got)
	}
	if got := blockText(t, cloud, "b3"); "refs" != got {
		t.Fatalf("untouched block changed: %q", got)
	}
}

// TestNonSyPathsAreNeverMerged pins that only SiYuan documents are eligible. Assets and attribute view data have no
// block structure, so merging them could corrupt them.
func TestNonSyPathsAreNeverMerged(t *testing.T) {
	repo := &Repo{}
	for _, path := range []string{"/assets/image.png", "/storage/av/20240101.json", "/notes/doc.sy.tmp"} {
		file := &entity.File{Path: path}
		if _, ok := repo.tryMergeSyBlocks(file, file, nil, "20240101-120000", nil); ok {
			t.Fatalf("path [%s] must never be block-merged", path)
		}
	}
}
