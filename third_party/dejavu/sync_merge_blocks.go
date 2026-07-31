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
	"path/filepath"
	"strings"

	"github.com/88250/lute"
	"github.com/88250/lute/ast"
	"github.com/88250/lute/parse"
	"github.com/88250/lute/render"
	"github.com/siyuan-note/dejavu/entity"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/logging"
)

// mergedTree is a document that a three-way block merge resolved without a true conflict. The merged content is written
// over the working copy after the cloud version has been checked out, so the merge index picks it up and uploads it.
type mergedTree struct {
	Path string
	Data []byte
}

// tryMergeSyBlocks attempts a three-way, block-level merge of a .sy document that changed both locally and in the
// cloud.
//
// SiYuan documents are block trees in which every block carries a stable ID, and a sync already knows all three
// versions it needs: the last synced index is the common ancestor, and the two sides are the local and cloud upserts.
// That makes it possible to keep both sides' edits when they touched different blocks, instead of letting the cloud
// copy overwrite the whole document and pushing the local edit into history.
//
// The merge is deliberately conservative. It only runs when all three versions contain exactly the same set of block
// IDs, so no block was added, removed or moved on either side. Any structural divergence, any change to a block that
// has block children, and any block edited differently on both sides falls back to the caller's existing conflict
// handling. Returning ok == false always means "treat this as a conflict", never "silently drop something".
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

	baseBlocks := collectBlocks(baseTree)
	localBlocks := collectBlocks(localTree)
	cloudBlocks := collectBlocks(cloudTree)

	if !sameBlockIDs(baseBlocks, localBlocks) || !sameBlockIDs(baseBlocks, cloudBlocks) {
		// A block was added, removed or moved. Rewriting the tree structure risks producing an invalid document, so
		// leave it to conflict handling.
		return nil, false
	}

	// Decide each block, and collect the ones where the local edit should win over the cloud skeleton.
	takeLocal := map[string]*ast.Node{}
	for id, baseNode := range baseBlocks {
		localNode, cloudNode := localBlocks[id], cloudBlocks[id]
		localChanged := blockChanged(baseNode, localNode)
		cloudChanged := blockChanged(baseNode, cloudNode)

		switch {
		case !localChanged:
			// Only the cloud changed it, or neither did. The cloud tree is the skeleton, so nothing to do.
		case !cloudChanged:
			// Only the local side changed it, so the local content has to be carried over.
			takeLocal[id] = localNode
		case !blockChanged(localNode, cloudNode):
			// Both sides made the same edit, so the versions already agree.
		default:
			// The same block was edited differently on both sides. This is the one case a merge cannot decide.
			return nil, false
		}
	}

	if 0 == len(takeLocal) {
		// Every difference came from the cloud, so the plain cloud checkout is already the correct merge.
		return &mergedTree{Path: cloudUpsert.Path, Data: nil}, true
	}

	for id, localNode := range takeLocal {
		cloudNode := cloudBlocks[id]
		if nil == cloudNode || hasBlockChildren(cloudNode) {
			// Substituting a container block would take its nested blocks with it and discard decisions already made
			// for them, so bail out rather than risk losing content.
			return nil, false
		}
		if !replaceNode(cloudNode, localNode) {
			return nil, false
		}
	}

	renderer := render.NewJSONRenderer(cloudTree, luteEngine.RenderOptions, luteEngine.ParseOptions)
	data := renderer.Render()
	if 1 > len(data) {
		logging.LogWarnf("render merged tree [%s] produced no data", cloudUpsert.Path)
		return nil, false
	}

	logging.LogInfof("sync merged [%d] block(s) of [%s] without conflict", len(takeLocal), cloudUpsert.Path)
	return &mergedTree{Path: cloudUpsert.Path, Data: data}, true
}

// writeMergedTrees writes block-merge results over the working copy. It runs after the cloud versions have been checked
// out and before the merge index is built, so the combined content is what gets indexed and uploaded.
func (repo *Repo) writeMergedTrees(merged []*mergedTree) (err error) {
	for _, tree := range merged {
		if nil == tree || 1 > len(tree.Data) {
			continue
		}
		absPath := repo.absPath(tree.Path)
		if err = filelock.WriteFile(absPath, tree.Data); nil != err {
			logging.LogErrorf("write merged tree [%s] failed: %s", absPath, err)
			return
		}
		logging.LogInfof("wrote merged tree [%s]", tree.Path)
	}
	return
}

// mergeTempDir returns the scratch directory used to check out the three versions being merged.
func (repo *Repo) mergeTempDir(now string) string {
	return filepath.Join(repo.TempPath, "repo", "sync", "merges", now)
}

// collectBlocks indexes a tree's blocks by their stable ID. The document node itself is skipped because it carries the
// document's own attributes rather than editable content.
func collectBlocks(tree *parse.Tree) (ret map[string]*ast.Node) {
	ret = map[string]*ast.Node{}
	if nil == tree || nil == tree.Root {
		return
	}
	ast.Walk(tree.Root, func(node *ast.Node, entering bool) ast.WalkStatus {
		if !entering || !node.IsBlock() || ast.NodeDocument == node.Type {
			return ast.WalkContinue
		}
		ret[node.ID] = node
		return ast.WalkContinue
	})
	return
}

// sameBlockIDs reports whether two block sets contain exactly the same IDs with the same node types.
func sameBlockIDs(a, b map[string]*ast.Node) bool {
	if len(a) != len(b) {
		return false
	}
	for id, an := range a {
		bn, ok := b[id]
		if !ok || an.Type != bn.Type {
			return false
		}
	}
	return true
}

// blockChanged reports whether a block differs in a way a user would consider an edit. Fold state and the updated
// timestamp are ignored, matching how ignoreLocalUpsert already treats them.
func blockChanged(a, b *ast.Node) bool {
	if nil == a || nil == b {
		return true
	}
	return !onlyChangeFoldIAL(a, b)
}

// hasBlockChildren reports whether a node contains nested blocks, i.e. whether it is a container rather than a leaf.
func hasBlockChildren(node *ast.Node) bool {
	for child := node.FirstChild; nil != child; child = child.Next {
		if child.IsBlock() {
			return true
		}
	}
	return false
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
