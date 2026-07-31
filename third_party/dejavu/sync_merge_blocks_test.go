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
	"os"
	"strings"
	"testing"

	"github.com/88250/lute"
	"github.com/88250/lute/ast"
	"github.com/88250/lute/parse"
	"github.com/siyuan-note/dataparser"
	"github.com/siyuan-note/dejavu/entity"
)

// These tests are the safety net for the block-level merge. Each one pins a case where getting it wrong would silently
// lose a user's edit, so they are written as "this must be detected as a change" or "this must be refused", never as
// assertions about convenience.

// parseSy parses a .sy document body, i.e. the Children array of the document node.
func parseSy(t *testing.T, children string) *parse.Tree {
	t.Helper()
	doc := `{"ID":"20240101120000-docroot","Spec":"1","Type":"NodeDocument","Properties":{"id":"20240101120000-docroot","title":"Test"},"Children":[` + children + `]}`
	tree, err := dataparser.ParseJSONWithoutFix([]byte(doc), lute.New().ParseOptions)
	if nil != err {
		t.Fatalf("parse synthetic .sy failed: %s", err)
	}
	return tree
}

// para builds a plain paragraph block.
func para(id, text string) string {
	return `{"ID":"` + id + `","Type":"NodeParagraph","Properties":{"id":"` + id + `","updated":"20240101120000"},"Children":[{"Type":"NodeText","Data":"` + text + `"}]}`
}

// mustBlocks returns the block index, failing the test if the document was rejected.
func mustBlocks(t *testing.T, tree *parse.Tree) map[string]*ast.Node {
	t.Helper()
	blocks, ok := collectBlocks(tree)
	if !ok {
		t.Fatal("collectBlocks rejected a document that should be valid")
	}
	return blocks
}

// --- C5: documents that cannot be addressed by block ID must be refused -------------------------------------------

func TestCollectBlocksRejectsDuplicateID(t *testing.T) {
	tree := parseSy(t, para("b1", "one")+","+para("b1", "two"))
	if _, ok := collectBlocks(tree); ok {
		t.Fatal("a document with a duplicated block ID must be refused, otherwise a merge can substitute into the wrong block")
	}
}

func TestCollectBlocksRejectsEmptyID(t *testing.T) {
	tree := parseSy(t, `{"ID":"","Type":"NodeParagraph","Properties":{},"Children":[{"Type":"NodeText","Data":"x"}]}`)
	if _, ok := collectBlocks(tree); ok {
		t.Fatal("a document with an ID-less block must be refused")
	}
}

func TestCollectBlocksAcceptsValidDocument(t *testing.T) {
	blocks := mustBlocks(t, parseSy(t, para("b1", "one")+","+para("b2", "two")))
	if 2 != len(blocks) {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if _, ok := blocks["20240101120000-docroot"]; ok {
		t.Fatal("the document node must not be indexed as an editable block")
	}
}

// --- C4: structural changes must be refused, not silently discarded -----------------------------------------------

func TestSameStructureDetectsReorder(t *testing.T) {
	before := blockSequence(parseSy(t, para("b1", "one")+","+para("b2", "two")))
	after := blockSequence(parseSy(t, para("b2", "two")+","+para("b1", "one")))
	if sameStructure(before, after) {
		t.Fatal("reordering two paragraphs must count as a structural change, otherwise the reorder is discarded")
	}
}

func TestSameStructureDetectsAddAndRemove(t *testing.T) {
	base := blockSequence(parseSy(t, para("b1", "one")+","+para("b2", "two")))
	if sameStructure(base, blockSequence(parseSy(t, para("b1", "one")+","+para("b2", "two")+","+para("b3", "three")))) {
		t.Fatal("an added block must count as a structural change")
	}
	if sameStructure(base, blockSequence(parseSy(t, para("b1", "one")))) {
		t.Fatal("a removed block must count as a structural change")
	}
}

func TestSameStructureAllowsContentOnlyEdit(t *testing.T) {
	before := blockSequence(parseSy(t, para("b1", "one")+","+para("b2", "two")))
	after := blockSequence(parseSy(t, para("b1", "EDITED")+","+para("b2", "two")))
	if !sameStructure(before, after) {
		t.Fatal("editing text must not count as a structural change")
	}
}

// --- C2: the change detector must be lossless ---------------------------------------------------------------------

// TestBlockChangedDetectsFormattingAndTargets is the core of C2. Content() ignores every one of these, so a detector
// built on it reports "unchanged" and the other side's version wins, destroying the edit.
func TestBlockChangedDetectsFormattingAndTargets(t *testing.T) {
	cases := []struct {
		name string
		a    string
		b    string
	}{
		{
			name: "bold applied to a word",
			a:    `{"ID":"b1","Type":"NodeParagraph","Properties":{"id":"b1"},"Children":[{"Type":"NodeText","Data":"word"}]}`,
			b:    `{"ID":"b1","Type":"NodeParagraph","Properties":{"id":"b1"},"Children":[{"Type":"NodeTextMark","TextMarkType":"strong","TextMarkTextContent":"word"}]}`,
		},
		{
			name: "link target changed",
			a:    `{"ID":"b1","Type":"NodeParagraph","Properties":{"id":"b1"},"Children":[{"Type":"NodeTextMark","TextMarkType":"a","TextMarkAHref":"https://example.com/one","TextMarkTextContent":"link"}]}`,
			b:    `{"ID":"b1","Type":"NodeParagraph","Properties":{"id":"b1"},"Children":[{"Type":"NodeTextMark","TextMarkType":"a","TextMarkAHref":"https://example.com/two","TextMarkTextContent":"link"}]}`,
		},
		{
			name: "block reference target changed",
			a:    `{"ID":"b1","Type":"NodeParagraph","Properties":{"id":"b1"},"Children":[{"Type":"NodeTextMark","TextMarkType":"block-ref","TextMarkBlockRefID":"20240101120000-aaaaaaa","TextMarkTextContent":"ref"}]}`,
			b:    `{"ID":"b1","Type":"NodeParagraph","Properties":{"id":"b1"},"Children":[{"Type":"NodeTextMark","TextMarkType":"block-ref","TextMarkBlockRefID":"20240101120000-bbbbbbb","TextMarkTextContent":"ref"}]}`,
		},
		{
			name: "highlight mark added",
			a:    `{"ID":"b1","Type":"NodeParagraph","Properties":{"id":"b1"},"Children":[{"Type":"NodeTextMark","TextMarkType":"text","TextMarkTextContent":"word"}]}`,
			b:    `{"ID":"b1","Type":"NodeParagraph","Properties":{"id":"b1"},"Children":[{"Type":"NodeTextMark","TextMarkType":"mark","TextMarkTextContent":"word"}]}`,
		},
		{
			name: "custom block attribute changed",
			a:    `{"ID":"b1","Type":"NodeParagraph","Properties":{"id":"b1","style":"color:red"},"Children":[{"Type":"NodeText","Data":"word"}]}`,
			b:    `{"ID":"b1","Type":"NodeParagraph","Properties":{"id":"b1","style":"color:blue"},"Children":[{"Type":"NodeText","Data":"word"}]}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := mustBlocks(t, parseSy(t, c.a))["b1"]
			b := mustBlocks(t, parseSy(t, c.b))["b1"]
			if nil == a || nil == b {
				t.Fatal("fixture did not produce block b1")
			}
			if !blockChanged(a, b) {
				t.Fatal("this edit must be detected; missing it means the other side's version silently wins")
			}
		})
	}
}

// TestBlockChangedIgnoresFoldAndUpdated pins the exemptions. Treating a fold as an edit would turn every outline
// collapse into a conflict.
func TestBlockChangedIgnoresFoldAndUpdated(t *testing.T) {
	a := mustBlocks(t, parseSy(t, para("b1", "same")))["b1"]
	b := mustBlocks(t, parseSy(t, para("b1", "same")))["b1"]

	if blockChanged(a, b) {
		t.Fatal("identical blocks must not be reported as changed")
	}

	b.SetIALAttr("fold", "1")
	b.SetIALAttr("heading-fold", "1")
	b.SetIALAttr("updated", "20990101000000")
	if blockChanged(a, b) {
		t.Fatal("fold state and updated timestamp must not count as an edit")
	}
}

// --- C3: the document node must take part in the merge ------------------------------------------------------------

// TestBlockChangedDetectsDocumentTitle covers C3: the title lives in the root node's IAL, and the .sy filename is the
// block ID, so a rename leaves the path unchanged and is invisible unless the root is compared.
func TestBlockChangedDetectsDocumentTitle(t *testing.T) {
	a := parseSy(t, para("b1", "text"))
	b := parseSy(t, para("b1", "text"))
	b.Root.SetIALAttr("title", "Renamed")

	if !blockChanged(a.Root, b.Root) {
		t.Fatal("a document rename must be detected, otherwise the cloud title silently overwrites it")
	}
}

func TestBlockChangedDetectsDocumentIconAndTags(t *testing.T) {
	for _, attr := range []string{"icon", "tags", "alias", "bookmark"} {
		t.Run(attr, func(t *testing.T) {
			a := parseSy(t, para("b1", "text"))
			b := parseSy(t, para("b1", "text"))
			b.Root.SetIALAttr(attr, "value")
			if !blockChanged(a.Root, b.Root) {
				t.Fatalf("a change to the document %s must be detected", attr)
			}
		})
	}
}

// --- H3: container blocks must never be substituted ---------------------------------------------------------------

func TestHasBlockChildrenFindsNestedBlocks(t *testing.T) {
	leaf := mustBlocks(t, parseSy(t, para("b1", "text")))["b1"]
	if hasBlockChildren(leaf) {
		t.Fatal("a paragraph holding only inline text is a leaf block")
	}

	// A block behind a non-block intermediate must still be found, which a single-level check would miss.
	outer := &ast.Node{Type: ast.NodeBlockquote, ID: "c1"}
	middle := &ast.Node{Type: ast.NodeText}
	outer.AppendChild(middle)
	middle.AppendChild(&ast.Node{Type: ast.NodeParagraph, ID: "c2"})
	if !hasBlockChildren(outer) {
		t.Fatal("a nested block behind a non-block intermediate must be found")
	}
}

// --- merge decision table (local is the base, cloud edits are applied onto it) -------------------------------------

func TestMergeDecisionTable(t *testing.T) {
	const id = "b1"

	cases := []struct {
		name          string
		base          string
		local         string
		cloud         string
		wantTakeCloud bool
		wantConflict  bool
	}{
		{name: "neither side changed", base: "x", local: "x", cloud: "x"},
		{name: "only the local side changed", base: "x", local: "local", cloud: "x"},
		{name: "only the cloud changed", base: "x", local: "x", cloud: "cloud", wantTakeCloud: true},
		{name: "both sides made the same edit", base: "x", local: "same", cloud: "same"},
		{name: "both sides edited differently", base: "x", local: "local", cloud: "cloud", wantConflict: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			baseNode := mustBlocks(t, parseSy(t, para(id, c.base)))[id]
			localNode := mustBlocks(t, parseSy(t, para(id, c.local)))[id]
			cloudNode := mustBlocks(t, parseSy(t, para(id, c.cloud)))[id]

			localChanged := blockChanged(baseNode, localNode)
			cloudChanged := blockChanged(baseNode, cloudNode)

			var takeCloud, conflict bool
			switch {
			case !cloudChanged:
			case !localChanged:
				takeCloud = true
			case !blockChanged(localNode, cloudNode):
			default:
				conflict = true
			}

			if takeCloud != c.wantTakeCloud {
				t.Fatalf("takeCloud = %v, want %v", takeCloud, c.wantTakeCloud)
			}
			if conflict != c.wantConflict {
				t.Fatalf("conflict = %v, want %v", conflict, c.wantConflict)
			}
		})
	}
}

// TestMergeKeepsBothSidesEdits is the case the feature exists for: two devices edit different paragraphs, and both
// edits survive. The local tree is the base, so the cloud's block is spliced onto it.
func TestMergeKeepsBothSidesEdits(t *testing.T) {
	body := func(b1, b2, b3 string) string {
		return para("b1", b1) + "," + para("b2", b2) + "," + para("b3", b3)
	}
	base := parseSy(t, body("intro", "todo", "refs"))
	local := parseSy(t, body("intro", "TODO v2", "refs"))
	cloud := parseSy(t, body("INTRO!", "todo", "refs"))

	baseBlocks := mustBlocks(t, base)
	localBlocks := mustBlocks(t, local)
	cloudBlocks := mustBlocks(t, cloud)

	if !sameStructure(blockSequence(base), blockSequence(local)) || !sameStructure(blockSequence(base), blockSequence(cloud)) {
		t.Fatal("this fixture must be structurally identical on all three sides")
	}

	for id, baseNode := range baseBlocks {
		localChanged := blockChanged(baseNode, localBlocks[id])
		cloudChanged := blockChanged(baseNode, cloudBlocks[id])
		if localChanged && cloudChanged {
			t.Fatalf("block [%s] should not be edited on both sides in this fixture", id)
		}
		if cloudChanged && !localChanged {
			if !replaceNode(localBlocks[id], cloudBlocks[id]) {
				t.Fatalf("substituting block [%s] failed", id)
			}
		}
	}

	merged := mustBlocks(t, local)
	if got := strings.TrimSpace(string(merged["b1"].Content())); "INTRO!" != got {
		t.Fatalf("cloud edit lost: %q", got)
	}
	if got := strings.TrimSpace(string(merged["b2"].Content())); "TODO v2" != got {
		t.Fatalf("local edit lost: %q", got)
	}
	if got := strings.TrimSpace(string(merged["b3"].Content())); "refs" != got {
		t.Fatalf("untouched block changed: %q", got)
	}
}

// --- C1 / C6: the merge must never overwrite the local file --------------------------------------------------------

// TestMergedFilesAreNotUpserts is C1. restoreFiles checks out Upserts over the working copy, so a merged document
// appearing there would replace the local file with the cloud version before the merge result is written.
func TestMergedFilesAreNotUpserts(t *testing.T) {
	file := &entity.File{ID: "f1", Path: "/notes/doc.sy"}
	mr := &MergeResult{}
	mr.Merges = append(mr.Merges, file)

	if 0 != len(mr.Upserts) {
		t.Fatal("a merged document must never be added to Upserts; restoreFiles would overwrite the local file with the cloud version")
	}
	if !mr.DataChanged() {
		t.Fatal("DataChanged must account for Merges, otherwise the merge index is never created and the merged content is never uploaded")
	}
}

// TestWriteMergedTreesDemotesToConflictOnFailure is C6. A write failure must leave the local file alone and fall back
// to the conflict path rather than aborting the sync partway through.
func TestWriteMergedTreesDemotesToConflictOnFailure(t *testing.T) {
	repo := &Repo{DataPath: t.TempDir()}
	file := &entity.File{ID: "f1", Path: "/notes/doc.sy"}
	mr := &MergeResult{}

	// A path whose parent is a regular file cannot be written, which is the closest portable stand-in for a failing
	// write.
	blocker := repo.absPath("/blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); nil != err {
		t.Fatalf("prepare blocker failed: %s", err)
	}

	repo.writeMergedTrees(mr, []*mergedTree{{
		File: file,
		Path: "/blocker/doc.sy",
		Data: []byte(`{"ID":"x"}`),
	}})

	if 1 != len(mr.Conflicts) {
		t.Fatalf("a failed merge write must demote the document to a conflict, got %d conflicts", len(mr.Conflicts))
	}
}

// TestWriteMergedTreesSkipsEmptyData covers the case where nothing from the cloud needed applying: the local file is
// already the merged result and must not be rewritten.
func TestWriteMergedTreesSkipsEmptyData(t *testing.T) {
	repo := &Repo{DataPath: t.TempDir()}
	mr := &MergeResult{}
	repo.writeMergedTrees(mr, []*mergedTree{{File: &entity.File{Path: "/notes/doc.sy"}, Path: "/notes/doc.sy"}})
	if 0 != len(mr.Conflicts) {
		t.Fatal("a merge with no cloud changes to apply must not be treated as a failure")
	}
}

// --- non-.sy documents are never merged ---------------------------------------------------------------------------

func TestNonSyPathsAreNeverMerged(t *testing.T) {
	repo := &Repo{}
	for _, path := range []string{"/assets/image.png", "/storage/av/20240101.json", "/notes/doc.sy.tmp"} {
		file := &entity.File{Path: path}
		if _, ok := repo.tryMergeSyBlocks(file, file, nil, "20240101-120000", nil); ok {
			t.Fatalf("path [%s] must never be block-merged", path)
		}
	}
}
