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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/88250/lute"
	"github.com/88250/lute/ast"
	"github.com/88250/lute/parse"
	"github.com/88250/lute/render"
	"github.com/88250/lute/util"
	"github.com/siyuan-note/dejavu/entity"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/logging"
)

// mergedTree is a document that a three-way block merge resolved without a true conflict.
type mergedTree struct {
	File *entity.File
	Path string
	Data []byte
}

// blockRef captures a block's position in a document so two documents can be compared structurally.
type blockRef struct {
	ID       string
	Type     ast.NodeType
	ParentID string
}

// tryMergeSyBlocks attempts a three-way, block-level merge of a .sy document that changed both locally and in the
// cloud.
//
// SiYuan documents are block trees in which every block carries a stable ID, and a sync already knows all three
// versions it needs: the last synced index is the common ancestor, and the two sides are the local and cloud upserts.
// That makes it possible to keep both sides' edits when they touched different blocks.
//
// The local tree is the base. Blocks only the cloud changed are spliced onto it, and the local version of everything
// else is kept as-is. That direction matters: upstream conflict handling never overwrites the working copy, so a merge
// must not either. If this function misses a change it can only fail to apply a cloud edit, which the next sync will
// offer again -- it can never discard a local edit.
//
// The merge is deliberately narrow. It runs only when all three versions have exactly the same blocks in exactly the
// same order under the same parents, it never substitutes a block that contains other blocks, and any block edited on
// both sides makes the whole document a conflict. Returning ok == false always means "treat this as a conflict",
// never "silently drop something".
func (repo *Repo) tryMergeSyBlocks(localUpsert, cloudUpsert *entity.File, latestSyncFiles []*entity.File, now string, context map[string]interface{}) (ret *mergedTree, ok bool) {
	if !strings.HasSuffix(cloudUpsert.Path, ".sy") {
		// Only SiYuan documents have a block structure to merge. Assets and attribute view data are opaque here.
		return nil, false
	}

	baseFile := repo.getFile(latestSyncFiles, localUpsert)
	if nil == baseFile {
		// Without a common ancestor there is nothing to merge against, so this is a genuine conflict.
		return nil, false
	}

	luteEngine := lute.New()
	temp := repo.mergeTempDir(now)

	baseTree, err := repo.checkoutTree(baseFile, temp, luteEngine, context)
	if nil != err {
		return nil, false
	}
	localTree, err := repo.checkoutTree(localUpsert, temp, luteEngine, context)
	if nil != err {
		return nil, false
	}
	cloudTree, err := repo.checkoutTree(cloudUpsert, temp, luteEngine, context)
	if nil != err {
		return nil, false
	}

	baseBlocks, baseOK := collectBlocks(baseTree)
	localBlocks, localOK := collectBlocks(localTree)
	cloudBlocks, cloudOK := collectBlocks(cloudTree)
	if !baseOK || !localOK || !cloudOK {
		// A document with missing or duplicated block IDs cannot be addressed reliably by ID, and substituting into
		// the wrong block would corrupt content silently.
		logging.LogWarnf("skip block merge of [%s]: document has missing or duplicated block IDs", cloudUpsert.Path)
		return nil, false
	}

	// The three versions must agree on structure. Comparing sequence and parentage, not just the set of IDs, is what
	// catches a block being moved or reordered on one side.
	if !sameStructure(blockSequence(baseTree), blockSequence(localTree)) ||
		!sameStructure(blockSequence(baseTree), blockSequence(cloudTree)) {
		return nil, false
	}

	// The document node carries the title, icon, tags, alias and bookmark in its IAL, so it has to take part in the
	// merge even though it is not an editable block.
	localRootChanged := blockChanged(baseTree.Root, localTree.Root)
	cloudRootChanged := blockChanged(baseTree.Root, cloudTree.Root)
	if localRootChanged && cloudRootChanged && blockChanged(localTree.Root, cloudTree.Root) {
		return nil, false
	}

	// Decide each block, collecting the ones where the cloud edit has to be carried onto the local tree.
	takeCloud := map[string]*ast.Node{}
	for id, baseNode := range baseBlocks {
		localNode, cloudNode := localBlocks[id], cloudBlocks[id]
		localChanged := blockChanged(baseNode, localNode)
		cloudChanged := blockChanged(baseNode, cloudNode)

		switch {
		case !cloudChanged:
			// Only the local side changed it, or neither did. The local tree is the base, so nothing to do.
		case !localChanged:
			// Only the cloud changed it, so the cloud content has to be carried over.
			takeCloud[id] = cloudNode
		case !blockChanged(localNode, cloudNode):
			// Both sides made the same edit, so the versions already agree.
		default:
			// The same block was edited differently on both sides. This is the one case a merge cannot decide.
			return nil, false
		}
	}

	if 0 == len(takeCloud) && !cloudRootChanged {
		// Nothing from the cloud needs applying, so the untouched local file is already correct.
		return &mergedTree{File: cloudUpsert, Path: cloudUpsert.Path}, true
	}

	for id, cloudNode := range takeCloud {
		localNode := localBlocks[id]
		if nil == localNode || hasBlockChildren(localNode) || hasBlockChildren(cloudNode) {
			// Substituting a container block would take its nested blocks with it and discard decisions already made
			// for them, so bail out rather than risk losing content.
			return nil, false
		}
		if !replaceNode(localNode, cloudNode) {
			return nil, false
		}
	}

	if cloudRootChanged && !localRootChanged {
		// The document was renamed or re-tagged in the cloud only, so carry that onto the local tree.
		localTree.Root.KramdownIAL = cloudTree.Root.KramdownIAL
	}

	renderer := render.NewJSONRenderer(localTree, luteEngine.RenderOptions, luteEngine.ParseOptions)
	data := renderer.Render()
	if 1 > len(data) {
		logging.LogWarnf("render merged tree [%s] produced no data", cloudUpsert.Path)
		return nil, false
	}

	logging.LogInfof("sync merged [%d] cloud block(s) into [%s] without conflict", len(takeCloud), cloudUpsert.Path)
	return &mergedTree{File: cloudUpsert, Path: cloudUpsert.Path, Data: data}, true
}

// writeMergedTrees writes block-merge results over the working copy.
//
// The merged documents are not in mergeResult.Upserts, so restoreFiles left the local version in place and that is
// what is being replaced here. A write failure therefore leaves the local file intact; the document is demoted to a
// conflict so the user still gets the usual conflict document, and the sync continues rather than aborting.
func (repo *Repo) writeMergedTrees(mergeResult *MergeResult, merged []*mergedTree) {
	for _, tree := range merged {
		if nil == tree || 1 > len(tree.Data) {
			// Nothing from the cloud needed applying, so the local file is already the merged result.
			continue
		}
		absPath := repo.absPath(tree.Path)
		if err := filelock.WriteFile(absPath, tree.Data); nil != err {
			logging.LogErrorf("write merged tree [%s] failed, falling back to conflict: %s", absPath, err)
			mergeResult.Conflicts = append(mergeResult.Conflicts, tree.File)
			continue
		}
		logging.LogInfof("wrote merged tree [%s]", tree.Path)
	}
}

// mergeTempDir returns the scratch directory used to check out the three versions being merged.
func (repo *Repo) mergeTempDir(now string) string {
	return filepath.Join(repo.TempPath, "repo", "sync", "merges", now)
}

// removeMergeTempDir discards the scratch checkouts once a sync has finished with them.
func (repo *Repo) removeMergeTempDir(now string) {
	if err := os.RemoveAll(repo.mergeTempDir(now)); nil != err {
		logging.LogWarnf("remove merge temp dir failed: %s", err)
	}
}

// collectBlocks indexes a tree's blocks by their stable ID. The document node is handled separately because it holds
// document attributes rather than editable content.
//
// It reports ok == false when a block has no ID or an ID repeats. ParseJSONWithoutFix does not repair either condition,
// and both make a block impossible to address unambiguously.
func collectBlocks(tree *parse.Tree) (ret map[string]*ast.Node, ok bool) {
	ret = map[string]*ast.Node{}
	if nil == tree || nil == tree.Root {
		return ret, false
	}

	ok = true
	ast.Walk(tree.Root, func(node *ast.Node, entering bool) ast.WalkStatus {
		if !entering || !node.IsBlock() || ast.NodeDocument == node.Type {
			return ast.WalkContinue
		}
		if "" == node.ID {
			ok = false
			return ast.WalkStop
		}
		if _, duplicate := ret[node.ID]; duplicate {
			ok = false
			return ast.WalkStop
		}
		ret[node.ID] = node
		return ast.WalkContinue
	})
	return ret, ok
}

// blockSequence returns the tree's blocks in document order together with their parent, which is what makes a moved or
// reordered block visible.
func blockSequence(tree *parse.Tree) (ret []blockRef) {
	if nil == tree || nil == tree.Root {
		return
	}
	ast.Walk(tree.Root, func(node *ast.Node, entering bool) ast.WalkStatus {
		if !entering || !node.IsBlock() || ast.NodeDocument == node.Type {
			return ast.WalkContinue
		}
		parentID := ""
		if nil != node.Parent {
			parentID = node.Parent.ID
		}
		ret = append(ret, blockRef{ID: node.ID, Type: node.Type, ParentID: parentID})
		return ast.WalkContinue
	})
	return
}

// sameStructure reports whether two documents hold the same blocks, in the same order, under the same parents.
func sameStructure(a, b []blockRef) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// blockChanged reports whether a block differs in a way a user would consider an edit.
//
// The comparison is made on the same JSON serialisation used to write the document, so it sees everything the file
// format records: text marks such as bold and highlight, link and image targets, block reference targets, code block
// languages and the raw markup of HTML, iframe, widget and embed blocks. Fold state and the updated timestamp are
// excluded, matching how ignoreLocalUpsert already treats them.
func blockChanged(a, b *ast.Node) bool {
	if nil == a || nil == b {
		return true
	}
	return blockFingerprint(a) != blockFingerprint(b)
}

// blockFingerprint serialises a node and its descendants exactly as the document would be written, minus the
// attributes that do not represent an edit.
func blockFingerprint(node *ast.Node) string {
	var sb strings.Builder
	ast.Walk(node, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.WalkContinue
		}

		// Mirror JSONRenderer.renderNode: it marshals these derived fields rather than the raw ones, so building the
		// fingerprint the same way keeps it faithful to what actually lands in the file.
		originalData, originalTypeStr, originalProperties := n.Data, n.TypeStr, n.Properties
		n.Data, n.TypeStr = util.BytesToStr(n.Tokens), n.Type.String()
		n.Properties = parse.IAL2Map(n.KramdownIAL)
		delete(n.Properties, "fold")
		delete(n.Properties, "heading-fold")
		delete(n.Properties, "updated")
		delete(n.Properties, "refcount")
		delete(n.Properties, "av-names")

		data, err := json.Marshal(n)
		n.Data, n.TypeStr, n.Properties = originalData, originalTypeStr, originalProperties
		if nil != err {
			// Fall back to a value that cannot compare equal, so an unserialisable node is treated as changed and the
			// document becomes a conflict instead of being merged on incomplete information.
			sb.WriteString("\x00marshal-failed\x00")
			return ast.WalkStop
		}
		sb.Write(data)
		return ast.WalkContinue
	})
	return sb.String()
}

// hasBlockChildren reports whether a node contains nested blocks anywhere beneath it, including behind non-block
// intermediates.
func hasBlockChildren(node *ast.Node) bool {
	if nil == node {
		return false
	}
	found := false
	ast.Walk(node, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering || n == node {
			return ast.WalkContinue
		}
		if n.IsBlock() {
			found = true
			return ast.WalkStop
		}
		return ast.WalkContinue
	})
	return found
}

// replaceNode swaps old for replacement in old's parent, reporting whether the swap was possible.
func replaceNode(old, replacement *ast.Node) bool {
	if nil == old || nil == replacement || nil == old.Parent {
		return false
	}
	old.InsertBefore(replacement)
	old.Unlink()
	return true
}
