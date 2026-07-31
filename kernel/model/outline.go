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
	"github.com/88250/gulu"
	"github.com/88250/lute/ast"
	"github.com/88250/lute/html"
	"github.com/88250/lute/parse"
	"github.com/emirpasic/gods/stacks/linkedliststack"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func (tx *Transaction) doMoveOutlineHeading(operation *Operation) (ret *TxErr) {
	headingID := operation.ID
	previousID := operation.PreviousID
	parentID := operation.ParentID

	tree, err := tx.loadTree(headingID)
	if err != nil {
		return &TxErr{code: TxErrCodeBlockNotFound, id: headingID}
	}
	operation.RetData = tree.Root.ID

	if headingID == parentID || headingID == previousID {
		return
	}

	heading := treenode.GetNodeInTree(tree, headingID)
	if nil == heading {
		return &TxErr{code: TxErrCodeBlockNotFound, id: headingID}
	}

	if ast.NodeDocument != heading.Parent.Type {
		// Only top-level headings directly under the document root are supported, not headings inside container blocks
		util.PushMsg(Conf.language(240), 5000)
		return
	}

	headings := []*ast.Node{}
	ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if entering && ast.NodeHeading == n.Type && !n.ParentIs(ast.NodeBlockquote) && !n.ParentIs(ast.NodeCallout) {
			headings = append(headings, n)
		}
		return ast.WalkContinue
	})

	headingChildren := treenode.HeadingChildren(heading)

	if "" != previousID {
		previousHeading := treenode.GetNodeInTree(tree, previousID)
		if nil == previousHeading {
			return &TxErr{code: TxErrCodeBlockNotFound, id: previousID}
		}

		if ast.NodeDocument != previousHeading.Parent.Type {
			// Only top-level headings directly under the document root are supported, not headings inside container blocks
			util.PushMsg(Conf.language(248), 5000)
			return
		}

		for _, h := range headingChildren {
			if h.ID == previousID {
				// Cannot move a heading under its own child heading
				util.PushMsg(Conf.language(241), 5000)
				return
			}
		}

		generateOpTypeHistory(tree, HistoryOpOutline)

		targetNode := previousHeading
		previousHeadingChildren := treenode.HeadingChildren(previousHeading)
		if 0 < len(previousHeadingChildren) {
			targetNode = previousHeadingChildren[len(previousHeadingChildren)-1]
		}

		for _, h := range headingChildren {
			if h.ID == targetNode.ID { // the target node is a child of the current heading
				targetNode = heading.Previous
			}
		}
		if targetNode.ID == heading.ID {
			targetNode = heading.Previous
		}

		diffLevel := heading.HeadingLevel - previousHeading.HeadingLevel
		heading.HeadingLevel = previousHeading.HeadingLevel

		for i := len(headingChildren) - 1; i >= 0; i-- {
			child := headingChildren[i]
			if ast.NodeHeading == child.Type {
				child.HeadingLevel -= diffLevel
				if 6 < child.HeadingLevel {
					child.HeadingLevel = 6
				}
			}
			targetNode.InsertAfter(child)
		}
		targetNode.InsertAfter(heading)
	} else if "" != parentID {
		parentHeading := treenode.GetNodeInTree(tree, parentID)
		if nil == parentHeading {
			return &TxErr{code: TxErrCodeBlockNotFound, id: parentID}
		}

		if ast.NodeDocument != parentHeading.Parent.Type {
			// Only top-level headings directly under the document root are supported, not headings inside container blocks
			util.PushMsg(Conf.language(248), 5000)
			return
		}

		for _, h := range headingChildren {
			if h.ID == parentID {
				// Cannot move a heading under its own child heading
				util.PushMsg(Conf.language(241), 5000)
				return
			}
		}

		generateOpTypeHistory(tree, HistoryOpOutline)

		targetNode := parentHeading
		parentHeadingChildren := treenode.HeadingChildren(parentHeading)
		// Find the first non-heading node below
		var tmp []*ast.Node
		for _, child := range parentHeadingChildren {
			if ast.NodeHeading == child.Type {
				break
			}
			tmp = append(tmp, child)
		}
		parentHeadingChildren = tmp
		if 0 < len(parentHeadingChildren) {
			for _, child := range parentHeadingChildren {
				if child.ID == headingID {
					break
				}
				targetNode = child
			}
		}

		diffLevel := heading.HeadingLevel - parentHeading.HeadingLevel - 1
		heading.HeadingLevel = min(6, parentHeading.HeadingLevel+1)

		for i := len(headingChildren) - 1; i >= 0; i-- {
			child := headingChildren[i]
			if ast.NodeHeading == child.Type {
				child.HeadingLevel -= diffLevel
				if 6 < child.HeadingLevel {
					child.HeadingLevel = 6
				}
			}
			targetNode.InsertAfter(child)
		}
		targetNode.InsertAfter(heading)
	} else {
		generateOpTypeHistory(tree, HistoryOpOutline)

		// Move before the first heading
		var firstHeading *ast.Node
		for n := tree.Root.FirstChild; nil != n; n = n.Next {
			if ast.NodeHeading == n.Type {
				firstHeading = n
				break
			}
		}
		if nil == firstHeading || firstHeading.ID == heading.ID {
			return
		}

		diffLevel := heading.HeadingLevel - firstHeading.HeadingLevel
		heading.HeadingLevel = firstHeading.HeadingLevel

		firstHeading.InsertBefore(heading)
		for i := range headingChildren {
			child := headingChildren[i]
			if ast.NodeHeading == child.Type {
				child.HeadingLevel -= diffLevel
				if 6 < child.HeadingLevel {
					child.HeadingLevel = 6
				}
			}
			firstHeading.InsertBefore(child)
		}
	}

	tx.writeTree(tree)
	return
}

func Outline(rootID string, preview bool) (ret []*Path, err error) {
	return OutlineInBox(rootID, preview, "")
}

// OutlineInBox behaves the same as Outline, but routes the blocktree query by boxID to either the
// encrypted db or the global db.
func OutlineInBox(rootID string, preview bool, boxID string) (ret []*Path, err error) {
	FlushTxQueue()

	ret = []*Path{}
	tree, _ := loadTreeByBlockIDInBox(rootID, boxID)
	if nil == tree {
		return
	}

	if preview && Conf.Export.AddTitle {
		if root, _ := getBlock(tree.ID, tree); nil != root {
			root.IAL["type"] = "doc"
			title := &ast.Node{ID: root.ID, Type: ast.NodeHeading, HeadingLevel: 1}
			for k, v := range root.IAL {
				if "type" == k {
					continue
				}
				title.SetIALAttr(k, v)
			}
			title.InsertAfter(&ast.Node{Type: ast.NodeKramdownBlockIAL, Tokens: parse.IAL2Tokens(title.KramdownIAL)})

			content := html.UnescapeString(root.Content)
			title.AppendChild(&ast.Node{Type: ast.NodeText, Tokens: []byte(content)})
			tree.Root.PrependChild(title)
		}
	}

	ret = outline(tree)

	storage, _ := GetOutlineStorage(rootID)
	if nil == storage || 0 == len(storage) {
		// Expand everything by default
		for _, p := range ret {
			p.Folded = false
			for _, b := range p.Blocks {
				b.Folded = false
				for _, c := range b.Children {
					walkChildren(c, []string{"expandAll"})
				}
			}
		}
	}

	if nil != storage["expandIds"] {
		// Collapse everything first, then expand the corresponding headings according to the expand ID list
		for _, p := range ret {
			p.Folded = true
			for _, b := range p.Blocks {
				b.Folded = true
				for _, c := range b.Children {
					walkChildren(c, []string{"expandNone"})
				}
			}
		}

		expandIDsArg := storage["expandIds"].([]any)
		var expandIDs []string
		for _, id := range expandIDsArg {
			expandIDs = append(expandIDs, id.(string))
		}

		for _, p := range ret {
			p.Folded = !gulu.Str.Contains(p.ID, expandIDs)
			for _, b := range p.Blocks {
				b.Folded = !gulu.Str.Contains(b.ID, expandIDs)
				for _, c := range b.Children {
					walkChildren(c, expandIDs)
				}
			}
		}
	}
	return
}

func walkChildren(b *Block, expandIDs []string) {
	if 1 == len(expandIDs) {
		if "expandAll" == expandIDs[0] {
			b.Folded = false
		} else if "expandNone" == expandIDs[0] {
			b.Folded = true
		} else {
			b.Folded = !gulu.Str.Contains(b.ID, expandIDs)
		}
	} else {
		b.Folded = !gulu.Str.Contains(b.ID, expandIDs)
	}

	for _, c := range b.Children {
		walkChildren(c, expandIDs)
	}
}

func outline(tree *parse.Tree) (ret []*Path) {
	luteEngine := NewLute()
	var headings []*Block
	ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if entering && ast.NodeHeading == n.Type && !n.ParentIs(ast.NodeBlockquote) && !n.ParentIs(ast.NodeCallout) {
			n.Box, n.Path = tree.Box, tree.Path
			block := &Block{
				RootID:  tree.Root.ID,
				Depth:   n.HeadingLevel,
				Box:     n.Box,
				Path:    n.Path,
				ID:      n.ID,
				Content: renderOutline(n, luteEngine),
				Type:    n.Type.String(),
				SubType: treenode.SubTypeAbbr(n),
				Folded:  true,
			}
			headings = append(headings, block)
			return ast.WalkSkipChildren
		}
		return ast.WalkContinue
	})

	if 1 > len(headings) {
		return
	}

	var blocks []*Block
	stack := linkedliststack.New()
	for _, h := range headings {
	L:
		for ; ; stack.Pop() {
			cur, ok := stack.Peek()
			if !ok {
				blocks = append(blocks, h)
				stack.Push(h)
				break L
			}

			tip := cur.(*Block)
			if tip.Depth < h.Depth {
				tip.Children = append(tip.Children, h)
				stack.Push(h)
				break L
			}
			tip.Count = len(tip.Children)
		}
	}

	ret = toFlatTree(blocks, 0, "outline", tree)
	if 0 < len(ret) {
		children := ret[0].Blocks
		ret = nil
		for _, b := range children {
			resetDepth(b, 0)
			ret = append(ret, &Path{
				ID:       b.ID,
				Box:      b.Box,
				Name:     b.Content,
				NodeType: b.Type,
				Type:     "outline",
				SubType:  b.SubType,
				Blocks:   b.Children,
				Depth:    0,
				Count:    b.Count,
				Folded:   true,
			})
		}
	}
	return
}

func resetDepth(b *Block, depth int) {
	b.Depth = depth
	b.Count = len(b.Children)
	for _, c := range b.Children {
		resetDepth(c, depth+1)
	}
}
