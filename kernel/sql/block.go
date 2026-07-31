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

package sql

import (
	"bytes"
	"database/sql"
	"fmt"
	"maps"
	"strings"

	"github.com/88250/gulu"
	"github.com/88250/lute/ast"
	"github.com/88250/lute/editor"
	"github.com/88250/lute/html"
	"github.com/88250/lute/parse"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/av"
	"github.com/siyuan-note/siyuan/kernel/cache"
	"github.com/siyuan-note/siyuan/kernel/filesys"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

type Block struct {
	ID       string
	ParentID string
	RootID   string
	Hash     string
	Box      string
	Path     string
	HPath    string
	Name     string
	Alias    string
	Memo     string
	Tag      string
	Content  string
	FContent string
	Markdown string
	Length   int
	Type     string
	SubType  string
	IAL      string
	Sort     int
	Created  string
	Updated  string
}

// blockRowIDByBlockID returns the given block's implicit rowid in the blocks table.
// In external content mode, blocks_fts writes need this as the locator key.
func blockRowIDByBlockID(tx *sql.Tx, id string) (rowID int64, err error) {
	stmt := "SELECT ROWID FROM blocks WHERE id = ?"
	rows, err := tx.Query(stmt, id)
	if err != nil {
		logging.LogErrorf("query block rowid failed: %s", err)
		return
	}
	defer rows.Close()
	if !rows.Next() {
		logging.LogErrorf("query block rowid failed: id=%s not found", id)
		err = fmt.Errorf("block rowid not found: %s", id)
		return
	}
	if err = rows.Scan(&rowID); err != nil {
		logging.LogErrorf("scan block rowid failed: %s", err)
		return
	}
	return
}

// queryBlockRowIDsTx batch-returns the blocks rowid for each id, indexed by id.
// Consistent with deleteBlocksByIDs, it builds the IN list via string interpolation (ids are kernel-generated
// block ids, not user input).
func queryBlockRowIDsTx(tx *sql.Tx, blocks []*Block) (ret map[string]int64, err error) {
	ret = map[string]int64{}
	if 1 > len(blocks) {
		return
	}
	var ids []string
	for _, b := range blocks {
		ids = append(ids, "\""+b.ID+"\"")
	}
	stmt := "SELECT id, ROWID FROM blocks WHERE id IN (" + strings.Join(ids, ",") + ")"
	rows, err := tx.Query(stmt)
	if err != nil {
		logging.LogErrorf("query block rowids failed: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var rowID int64
		if err = rows.Scan(&id, &rowID); err != nil {
			logging.LogErrorf("scan block rowid failed: %s", err)
			return
		}
		ret[id] = rowID
	}
	return
}

// The following partial index-column update paths (updateRootContent, updateBlockContent, indexNode) must write
// blocks_fts before writing blocks, so that when FTS deletes old tokens it can still read the old value from blocks.

func updateRootContent(tx *sql.Tx, content, updated, ialContent, id string) (err error) {
	var rowID int64
	if rowID, err = blockRowIDByBlockID(tx, id); err != nil {
		return
	}
	stmt := "UPDATE blocks_fts SET content = ?, fcontent = ?, ial = ? WHERE rowid = ?"
	if err = execStmtTx(tx, stmt, content, content, ialContent, rowID); err != nil {
		return
	}
	stmt = "UPDATE blocks SET content = ?, fcontent = ?, updated = ?, ial = ? WHERE id = ?"
	if err = execStmtTx(tx, stmt, content, content, updated, ialContent, id); err != nil {
		return
	}
	removeBlockCache(id)
	cache.RemoveBlockIAL(id)
	return
}

func updateBlockContent(tx *sql.Tx, block *Block) (err error) {
	var rowID int64
	if rowID, err = blockRowIDByBlockID(tx, block.ID); err != nil {
		tx.Rollback()
		return
	}
	stmt := "UPDATE blocks_fts SET content = ? WHERE rowid = ?"
	if err = execStmtTx(tx, stmt, block.Content, rowID); err != nil {
		tx.Rollback()
		return
	}
	stmt = "UPDATE blocks SET content = ? WHERE id = ?"
	if err = execStmtTx(tx, stmt, block.Content, block.ID); err != nil {
		tx.Rollback()
		return
	}

	putBlockCache(block)
	return
}

func indexNode(tx *sql.Tx, id, boxID string) (err error) {
	bt := treenode.GetBlockTreeInBox(id, boxID)
	if nil == bt {
		return
	}

	tree, _ := filesys.LoadTree(bt.BoxID, bt.Path, luteEngine)
	if nil == tree {
		return
	}

	node := treenode.GetNodeInTree(tree, id)
	if nil == node {
		return
	}

	content := nodeStaticContent(node, nil, true, indexAssetPath, true, true)
	content = strings.ReplaceAll(content, editor.Zwsp, "")
	var rowID int64
	if rowID, err = blockRowIDByBlockID(tx, id); err != nil {
		tx.Rollback()
		return
	}
	stmt := "UPDATE blocks_fts SET content = ? WHERE rowid = ?"
	if err = execStmtTx(tx, stmt, content, rowID); err != nil {
		tx.Rollback()
		return
	}
	stmt = "UPDATE blocks SET content = ? WHERE id = ?"
	if err = execStmtTx(tx, stmt, content, id); err != nil {
		tx.Rollback()
		return
	}
	return
}

func NodeStaticContent(node *ast.Node, excludeTypes []string, includeTextMarkATitleURL, includeAssetPath, fullAttrView bool) string {
	return nodeStaticContent(node, excludeTypes, includeTextMarkATitleURL, includeAssetPath, fullAttrView, false)
}

func nodeStaticContent(node *ast.Node, excludeTypes []string, includeTextMarkATitleURL, includeAssetPath, fullAttrView, unescapeBlockRef bool) string {
	if nil == node {
		return ""
	}

	if ast.NodeDocument == node.Type {
		return html.EscapeHTMLStr(node.IALAttr("title"))
	}

	if ast.NodeAttributeView == node.Type {
		if fullAttrView {
			return av.GetAttributeViewContent(node.AttributeViewID)
		}

		ret, _ := av.GetAttributeViewName(node.AttributeViewID)
		return html.EscapeHTMLStr(ret)
	}

	buf := bytes.Buffer{}
	buf.Grow(4096)
	lastSpace := false
	ast.Walk(node, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			if ast.NodeTable == n.Type {
				caption := n.IALAttr("caption")
				if "" != caption {
					caption = html.UnescapeHTMLStr(caption)
					if strings.Contains(caption, "caption-side:") && strings.Contains(caption, "bottom") {
						caption = gulu.Str.SubStringBetween(caption, ">", "<")
						buf.WriteByte(' ')
						buf.WriteString(caption)
					}
				}
			}
			return ast.WalkContinue
		}

		if n.IsContainerBlock() {
			if !lastSpace {
				buf.WriteByte(' ')
				lastSpace = true
			}
			if ast.NodeCallout == n.Type {
				buf.WriteString(n.CalloutType + " ")
				if "" != n.CalloutIcon && 0 == n.CalloutIconType {
					buf.WriteString(n.CalloutIcon + " ")
				}
				if "" != n.CalloutTitle {
					if titleTree := parse.Inline("", []byte(n.CalloutTitle), luteEngine.ParseOptions); nil != titleTree && nil != titleTree.Root.FirstChild.FirstChild {
						var inlines []*ast.Node
						for c := titleTree.Root.FirstChild.FirstChild; nil != c; c = c.Next {
							inlines = append(inlines, c)
						}
						for _, inline := range inlines {
							buf.WriteString(inline.Content())
						}
					}
					buf.WriteByte(' ')
				}
			}
			return ast.WalkContinue
		}

		if gulu.Str.Contains(n.Type.String(), excludeTypes) {
			return ast.WalkContinue
		}

		switch n.Type {
		case ast.NodeTable:
			caption := n.IALAttr("caption")
			if "" != caption {
				caption = html.UnescapeHTMLStr(caption)
				if strings.Contains(caption, "caption-side:") && strings.Contains(caption, "bottom") {
					return ast.WalkContinue
				}
				caption = gulu.Str.SubStringBetween(caption, ">", "<")
				buf.WriteString(caption)
				buf.WriteByte(' ')
			}
		case ast.NodeTableCell:
			// Add a space between cells when a table block is written to the database table https://github.com/siyuan-note/siyuan/issues/7654
			if 0 < buf.Len() && ' ' != buf.Bytes()[buf.Len()-1] {
				buf.WriteByte(' ')
			}
		case ast.NodeImage:
			linkDest := n.ChildByType(ast.NodeLinkDest)
			var linkDestStr, ocrText string
			if nil != linkDest {
				linkDestStr = linkDest.TokensStr()
				ocrText = util.GetAssetText(linkDestStr)
			}

			linkText := n.ChildByType(ast.NodeLinkText)
			if nil != linkText {
				buf.Write(linkText.Tokens)
				buf.WriteByte(' ')
			}
			if "" != ocrText {
				buf.WriteString(ocrText)
				buf.WriteByte(' ')
			}
			if nil != linkDest {
				if !bytes.HasPrefix(linkDest.Tokens, []byte("assets/")) || includeAssetPath {
					buf.Write(linkDest.Tokens)
					buf.WriteByte(' ')
				}
			}
			if linkTitle := n.ChildByType(ast.NodeLinkTitle); nil != linkTitle {
				buf.Write(linkTitle.Tokens)
			}
			return ast.WalkSkipChildren
		case ast.NodeLinkText:
			buf.Write(n.Tokens)
			buf.WriteByte(' ')
		case ast.NodeLinkDest:
			buf.Write(n.Tokens)
			buf.WriteByte(' ')
		case ast.NodeLinkTitle:
			buf.Write(n.Tokens)
		case ast.NodeText, ast.NodeCodeBlockCode, ast.NodeMathBlockContent, ast.NodeHTMLBlock:
			tokens := n.Tokens
			if treenode.IsChartCodeBlockCode(n) {
				// A chart block's content is escaped in the database `blocks` table's `content` field https://github.com/siyuan-note/siyuan/issues/6326
				tokens = html.UnescapeHTML(tokens)
			}
			buf.Write(tokens)
		case ast.NodeTextMark:
			for _, excludeType := range excludeTypes {
				if strings.HasPrefix(excludeType, "NodeTextMark-") {
					if n.IsTextMarkType(excludeType[len("NodeTextMark-"):]) {
						return ast.WalkContinue
					}
				}
			}

			if n.IsTextMarkType("tag") {
				buf.WriteByte('#')
			}
			content := n.Content()
			if unescapeBlockRef && treenode.IsBlockRef(n) {
				content = util.UnescapeHTML(content)
			}
			buf.WriteString(content)
			if n.IsTextMarkType("tag") {
				buf.WriteByte('#')
			}
			if n.IsTextMarkType("a") && includeTextMarkATitleURL {
				// The URL and title of a hyperlink element couldn't be found in search https://github.com/siyuan-note/siyuan/issues/7352
				if "" != n.TextMarkATitle {
					buf.WriteString(" " + util.UnescapeHTML(n.TextMarkATitle))
				}

				if !strings.HasPrefix(n.TextMarkAHref, "assets/") || includeAssetPath {
					href := util.UnescapeHTML(n.TextMarkAHref)
					buf.WriteString(" " + util.UnescapeHTML(href))
				}
			}
		case ast.NodeBackslashContent:
			buf.Write(n.Tokens)
		case ast.NodeAudio, ast.NodeVideo:
			buf.WriteString(treenode.GetNodeSrcTokens(n))
			buf.WriteByte(' ')
		}
		lastSpace = false
		return ast.WalkContinue
	})

	// Do not trim here, otherwise leading/trailing spaces can't be searched
	// Improve search and replace for spaces https://github.com/siyuan-note/siyuan/issues/10231
	return buf.String()
}

func BatchGetBlockAttrsWitTrees(ids []string, trees map[string]*parse.Tree) (ret map[string]map[string]string) {
	ret = map[string]map[string]string{}

	hitCache := true
	for _, id := range ids {
		boxID := ""
		if tree := trees[id]; nil != tree {
			boxID = tree.Box
		}
		ial := cache.GetBlockIALWithBoxFallback(id, boxID)
		if nil != ial {
			ret[id] = ial
			continue
		}
		hitCache = false
		break
	}
	if hitCache {
		return
	}

	for _, id := range ids {
		tree := trees[id]
		if nil == tree {
			continue
		}

		ret[id] = getBlockAttrsFromTree(id, tree)
	}
	return
}

func BatchGetBlockAttrs(ids []string) (ret map[string]map[string]string) {
	ret = map[string]map[string]string{}

	hitCache := true
	for _, id := range ids {
		boxID := ""
		if bt := treenode.GetBlockTree(id); nil != bt {
			boxID = bt.BoxID
		}
		ial := cache.GetBlockIALWithBoxFallback(id, boxID)
		if nil != ial {
			ret[id] = ial
			continue
		}
		hitCache = false
		break
	}
	if hitCache {
		return
	}

	trees := filesys.LoadTrees(ids)
	for _, id := range ids {
		tree := trees[id]
		if nil == tree {
			continue
		}

		ret[id] = getBlockAttrsFromTree(id, tree)
	}
	return
}

func GetBlockAttrs(id string) (ret map[string]string) {
	ret = map[string]string{}
	// Some write paths use a box-aware key while others use a bare key, so query with box-aware preferred and
	// bare key as a fallback, to avoid missing an update in either namespace (e.g. after a block is bound to a
	// database, the box-aware key is written, but the old bare key still holds the pre-binding value).
	bt := treenode.GetBlockTree(id)
	boxID := ""
	if nil != bt {
		boxID = bt.BoxID
	}
	if cached := cache.GetBlockIALWithBoxFallback(id, boxID); nil != cached {
		ret = cached
		return
	}

	var tree *parse.Tree
	if nil != bt {
		tree, _ = filesys.LoadTree(bt.BoxID, bt.Path, luteEngine)
	} else {
		tree = loadTreeByBlockID(id)
	}
	if nil == tree {
		return
	}

	ret = getBlockAttrsFromTree(id, tree)
	return
}

func getBlockAttrsFromTree(id string, tree *parse.Tree) (ret map[string]string) {
	ret = map[string]string{}

	ial := cache.GetBlockIALWithBoxFallback(id, tree.Box)
	if nil != ial {
		maps.Copy(ret, ial)
		return
	}

	node := treenode.GetNodeInTree(tree, id)
	if nil == node {
		logging.LogWarnf("block [%s] not found", id)
		return
	}

	for _, kv := range node.KramdownIAL {
		ret[kv[0]] = html.UnescapeAttrVal(kv[1])
	}
	cache.PutBlockIALInBox(id, tree.Box, ret)
	return
}

func loadTreeByBlockID(id string) (ret *parse.Tree) {
	bt := treenode.GetBlockTree(id)
	if nil == bt {
		return
	}

	ret, err := filesys.LoadTree(bt.BoxID, bt.Path, luteEngine)
	if nil != err {
		return
	}
	return
}
