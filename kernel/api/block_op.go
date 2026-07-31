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

package api

import (
	"errors"
	"net/http"

	"github.com/88250/gulu"
	"github.com/88250/lute"
	"github.com/88250/lute/ast"
	"github.com/88250/lute/parse"
	"github.com/gin-gonic/gin"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/filesys"
	"github.com/siyuan-note/siyuan/kernel/model"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func buildUpdatedTaskListItemBlockDOM(id, marker string, luteEngine *lute.Lute) (data string, err error) {
	block, err := model.GetBlock(id, nil)
	if err != nil {
		return "", errors.New("get block failed: " + err.Error())
	}

	if "NodeListItem" != block.Type {
		return "", errors.New("block is not a list item")
	}

	tree, err := filesys.LoadTree(block.Box, block.Path, luteEngine)
	if err != nil {
		return "", errors.New("load tree failed: " + err.Error())
	}

	li := treenode.GetNodeInTree(tree, id)
	if li == nil {
		return "", errors.New("block not found")
	}

	if 3 != li.ListData.Typ {
		return "", errors.New("block is not a task list item")
	}

	if 1 != len(marker) {
		return "", errors.New("task list item marker length should be 1")
	}

	liMarker := marker[0]
	if '[' == liMarker || ']' == liMarker {
		return "", errors.New("task list item marker can not be [ or ]")
	}

	markerNode := li.ChildByType(ast.NodeTaskListItemMarker)
	if nil == markerNode {
		return "", errors.New("task list item marker not found")
	}

	markerNode.TaskListItemMarker = liMarker
	markerNode.TaskListItemChecked = ' ' != markerNode.TaskListItemMarker

	treenode.RefreshUpdated(li)

	return luteEngine.RenderNodeBlockDOM(li), nil
}

func updateTaskListItemMarker(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var id, marker string
	if !util.ParseJsonArgs(arg, ret,
		util.BindJsonArg("id", &id, true, true),
		util.BindJsonArg("marker", &marker, true, false),
	) {
		return
	}
	if util.InvalidIDPattern(id, ret) {
		return
	}

	luteEngine := util.NewLute()
	data, err := buildUpdatedTaskListItemBlockDOM(id, marker, luteEngine)
	if err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}

	transactions := []*model.Transaction{
		{
			DoOperations: []*model.Operation{
				{Action: "update", ID: id, Data: data},
			},
		},
	}

	model.PerformTransactions(&transactions)
	model.FlushTxQueue()

	ret.Data = transactions
	broadcastTransactions(transactions)
}

func batchUpdateTaskListItemMarker(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var itemsArg []any
	if !util.ParseJsonArgs(arg, ret, util.BindJsonArg("items", &itemsArg, true, true)) {
		return
	}

	luteEngine := util.NewLute()
	idToMarker := make(map[string]string, len(itemsArg))
	idsInOrder := make([]string, 0, len(itemsArg))
	for _, itemArg := range itemsArg {
		itemMap, ok := itemArg.(map[string]any)
		if !ok {
			ret.Code = -1
			ret.Msg = "invalid item: each item must be an object"
			return
		}
		var id, marker string
		if !util.ParseJsonArgs(itemMap, ret,
			util.BindJsonArg("id", &id, true, true),
			util.BindJsonArg("marker", &marker, true, false),
		) {
			return
		}
		if util.InvalidIDPattern(id, ret) {
			return
		}
		// For the same id, keep the last marker
		idToMarker[id] = marker
		idsInOrder = append(idsInOrder, id)
	}

	ids := gulu.Str.RemoveDuplicatedElem(idsInOrder)
	ops := make([]*model.Operation, 0, len(ids))
	for _, id := range ids {
		data, err := buildUpdatedTaskListItemBlockDOM(id, idToMarker[id], luteEngine)
		if err != nil {
			ret.Code = -1
			ret.Msg = err.Error()
			return
		}

		ops = append(ops, &model.Operation{Action: "update", ID: id, Data: data})
	}

	tx := &model.Transaction{DoOperations: ops}
	transactions := []*model.Transaction{tx}

	model.PerformTransactions(&transactions)
	model.FlushTxQueue()

	ret.Data = transactions
	broadcastTransactions(transactions)
}

func moveOutlineHeading(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	id := arg["id"].(string)
	if util.InvalidIDPattern(id, ret) {
		return
	}

	var parentID, previousID string
	if nil != arg["parentID"] {
		parentID = arg["parentID"].(string)
		if "" != parentID && util.InvalidIDPattern(parentID, ret) {
			return
		}
	}
	if nil != arg["previousID"] {
		previousID = arg["previousID"].(string)
		if "" != previousID && util.InvalidIDPattern(previousID, ret) {
			return
		}
	}

	transactions := []*model.Transaction{
		{
			DoOperations: []*model.Operation{
				{
					Action:     "moveOutlineHeading",
					ID:         id,
					PreviousID: previousID,
					ParentID:   parentID,
				},
			},
		},
	}

	model.PerformTransactions(&transactions)
	model.FlushTxQueue()

	ret.Data = transactions
	broadcastTransactions(transactions)
}

func appendDailyNoteBlock(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	data := arg["data"].(string)
	dataType := arg["dataType"].(string)
	boxID := arg["notebook"].(string)
	if util.InvalidIDPattern(boxID, ret) {
		return
	}
	if "markdown" == dataType {
		luteEngine := util.NewLute()
		var err error
		data, err = dataBlockDOM(data, luteEngine)
		if err != nil {
			ret.Code = -1
			ret.Msg = "data block DOM failed: " + err.Error()
			return
		}
	}

	p, _, err := model.CreateDailyNote(boxID)
	if err != nil {
		ret.Code = -1
		ret.Msg = "create daily note failed: " + err.Error()
		return
	}

	parentID := util.GetTreeID(p)
	transactions := []*model.Transaction{
		{
			DoOperations: []*model.Operation{
				{
					Action:   "appendInsert",
					Data:     data,
					ParentID: parentID,
				},
			},
		},
	}

	model.PerformTransactions(&transactions)
	model.FlushTxQueue()

	ret.Data = transactions
	broadcastTransactions(transactions)
}

func prependDailyNoteBlock(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	data := arg["data"].(string)
	dataType := arg["dataType"].(string)
	boxID := arg["notebook"].(string)
	if util.InvalidIDPattern(boxID, ret) {
		return
	}
	if "markdown" == dataType {
		luteEngine := util.NewLute()
		var err error
		data, err = dataBlockDOM(data, luteEngine)
		if err != nil {
			ret.Code = -1
			ret.Msg = "data block DOM failed: " + err.Error()
			return
		}
	}

	p, _, err := model.CreateDailyNote(boxID)
	if err != nil {
		ret.Code = -1
		ret.Msg = "create daily note failed: " + err.Error()
		return
	}

	parentID := util.GetTreeID(p)
	transactions := []*model.Transaction{
		{
			DoOperations: []*model.Operation{
				{
					Action:   "prependInsert",
					Data:     data,
					ParentID: parentID,
				},
			},
		},
	}

	model.PerformTransactions(&transactions)
	model.FlushTxQueue()

	ret.Data = transactions
	broadcastTransactions(transactions)
}

func unfoldBlock(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	id := arg["id"].(string)
	if util.InvalidIDPattern(id, ret) {
		return
	}

	bt := treenode.GetBlockTree(id)
	if nil == bt {
		ret.Code = -1
		ret.Msg = "block tree not found [id=" + id + "]"
		return
	}

	if bt.Type == "d" {
		ret.Code = -1
		ret.Msg = "document can not be unfolded"
		return
	}

	var transactions []*model.Transaction
	if "h" == bt.Type {
		transactions = []*model.Transaction{
			{
				DoOperations: []*model.Operation{
					{
						Action: "unfoldHeading",
						ID:     id,
					},
				},
			},
		}
	} else {
		data, _ := gulu.JSON.MarshalJSON(map[string]any{"fold": ""})
		transactions = []*model.Transaction{
			{
				DoOperations: []*model.Operation{
					{
						Action: "setAttrs",
						ID:     id,
						Data:   string(data),
					},
				},
			},
		}
	}

	model.PerformTransactions(&transactions)
	model.FlushTxQueue()

	broadcastTransactions(transactions)
}

func foldBlock(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	id := arg["id"].(string)
	if util.InvalidIDPattern(id, ret) {
		return
	}

	bt := treenode.GetBlockTree(id)
	if nil == bt {
		ret.Code = -1
		ret.Msg = "block tree not found [id=" + id + "]"
		return
	}

	if bt.Type == "d" {
		ret.Code = -1
		ret.Msg = "document can not be folded"
		return
	}

	var transactions []*model.Transaction
	if "h" == bt.Type {
		transactions = []*model.Transaction{
			{
				DoOperations: []*model.Operation{
					{
						Action: "foldHeading",
						ID:     id,
					},
				},
			},
		}
	} else {
		data, _ := gulu.JSON.MarshalJSON(map[string]any{"fold": "1"})
		transactions = []*model.Transaction{
			{
				DoOperations: []*model.Operation{
					{
						Action: "setAttrs",
						ID:     id,
						Data:   string(data),
					},
				},
			},
		}
	}

	model.PerformTransactions(&transactions)
	model.FlushTxQueue()

	broadcastTransactions(transactions)
}

func moveBlock(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	id := arg["id"].(string)
	if util.InvalidIDPattern(id, ret) {
		return
	}

	currentBt := treenode.GetBlockTree(id)
	if nil == currentBt {
		ret.Code = -1
		ret.Msg = "block not found [id=" + id + "]"
		return
	}

	var parentID, previousID string
	if nil != arg["parentID"] {
		parentID = arg["parentID"].(string)
		if "" != parentID && util.InvalidIDPattern(parentID, ret) {
			return
		}
	}
	if nil != arg["previousID"] {
		previousID = arg["previousID"].(string)
		if "" != previousID && util.InvalidIDPattern(previousID, ret) {
			return
		}

		// Check the validity of the API `moveBlock` parameter `previousID` https://github.com/siyuan-note/siyuan/issues/8007
		if bt := treenode.GetBlockTree(previousID); nil == bt || "d" == bt.Type {
			ret.Code = -1
			ret.Msg = "`previousID` can not be the ID of a document"
			return
		}
	}

	var targetBt *treenode.BlockTree
	if "" != previousID {
		targetBt = treenode.GetBlockTree(previousID)
	} else if "" != parentID {
		targetBt = treenode.GetBlockTree(parentID)
	}

	if nil == targetBt {
		ret.Code = -1
		ret.Msg = "target block not found [id=" + parentID + "]"
		return
	}

	// When the target is located solely by parentID (no previousID), the target must be a container block, otherwise the nesting is invalid
	if "" == previousID && "" != parentID {
		if err := treenode.CheckListItemNesting(parentID, id); err != nil {
			ret.Code = -1
			ret.Msg = err.Error()
			return
		}
		if err := treenode.CheckContainerParent(parentID); err != nil {
			ret.Code = -1
			ret.Msg = err.Error()
			return
		}
	}

	transactions := []*model.Transaction{
		{
			DoOperations: []*model.Operation{
				{
					Action:     "move",
					ID:         id,
					PreviousID: previousID,
					ParentID:   parentID,
				},
			},
		},
	}

	model.PerformTransactions(&transactions)
	model.FlushTxQueue()

	model.ReloadProtyle(currentBt.RootID)
	if currentBt.RootID != targetBt.RootID {
		model.ReloadProtyle(targetBt.RootID)
	}
}

func appendBlock(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	data := arg["data"].(string)
	dataType := arg["dataType"].(string)
	parentID := arg["parentID"].(string)
	if util.InvalidIDPattern(parentID, ret) {
		return
	}
	// append locates the target using only parentID, so the target must be a container block, otherwise the nesting is invalid
	if err := treenode.CheckContainerParent(parentID); err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}
	if "markdown" == dataType {
		luteEngine := util.NewLute()
		var err error
		data, err = dataBlockDOM(data, luteEngine)
		if err != nil {
			ret.Code = -1
			ret.Msg = "data block DOM failed: " + err.Error()
			return
		}
	}

	transactions := []*model.Transaction{
		{
			DoOperations: []*model.Operation{
				{
					Action:   "appendInsert",
					Data:     data,
					ParentID: parentID,
				},
			},
		},
	}

	model.PerformTransactions(&transactions)
	model.FlushTxQueue()

	ret.Data = transactions
	broadcastTransactions(transactions)
}

func batchAppendBlock(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	blocksArg := arg["blocks"].([]any)
	var transactions []*model.Transaction
	luteEngine := util.NewLute()
	for _, blockArg := range blocksArg {
		blockMap := blockArg.(map[string]any)
		data := blockMap["data"].(string)
		dataType := blockMap["dataType"].(string)
		parentID := blockMap["parentID"].(string)
		if util.InvalidIDPattern(parentID, ret) {
			return
		}
		// append locates the target using only parentID, so the target must be a container block, otherwise the nesting is invalid
		if err := treenode.CheckContainerParent(parentID); err != nil {
			ret.Code = -1
			ret.Msg = err.Error()
			return
		}
		if "markdown" == dataType {
			var err error
			data, err = dataBlockDOM(data, luteEngine)
			if err != nil {
				ret.Code = -1
				ret.Msg = "data block DOM failed: " + err.Error()
				return
			}
		}

		transactions = append(transactions, &model.Transaction{
			DoOperations: []*model.Operation{
				{
					Action:   "appendInsert",
					Data:     data,
					ParentID: parentID,
				},
			},
		})
	}

	model.PerformTransactions(&transactions)
	model.FlushTxQueue()

	ret.Data = transactions
	broadcastTransactions(transactions)
}

func prependBlock(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	data := arg["data"].(string)
	dataType := arg["dataType"].(string)
	parentID := arg["parentID"].(string)
	if util.InvalidIDPattern(parentID, ret) {
		return
	}
	// prepend locates the target using only parentID, so the target must be a container block, otherwise the nesting is invalid
	if err := treenode.CheckContainerParent(parentID); err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}
	if "markdown" == dataType {
		luteEngine := util.NewLute()
		var err error
		data, err = dataBlockDOM(data, luteEngine)
		if err != nil {
			ret.Code = -1
			ret.Msg = "data block DOM failed: " + err.Error()
			return
		}
	}

	transactions := []*model.Transaction{
		{
			DoOperations: []*model.Operation{
				{
					Action:   "prependInsert",
					Data:     data,
					ParentID: parentID,
				},
			},
		},
	}

	model.PerformTransactions(&transactions)
	model.FlushTxQueue()

	ret.Data = transactions
	broadcastTransactions(transactions)
}

func batchPrependBlock(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	blocksArg := arg["blocks"].([]any)
	var transactions []*model.Transaction
	luteEngine := util.NewLute()
	for _, blockArg := range blocksArg {
		blockMap := blockArg.(map[string]any)
		data := blockMap["data"].(string)
		dataType := blockMap["dataType"].(string)
		parentID := blockMap["parentID"].(string)
		if util.InvalidIDPattern(parentID, ret) {
			return
		}
		// prepend locates the target using only parentID, so the target must be a container block, otherwise the nesting is invalid
		if err := treenode.CheckContainerParent(parentID); err != nil {
			ret.Code = -1
			ret.Msg = err.Error()
			return
		}
		if "markdown" == dataType {
			var err error
			data, err = dataBlockDOM(data, luteEngine)
			if err != nil {
				ret.Code = -1
				ret.Msg = "data block DOM failed: " + err.Error()
				return
			}
		}

		transactions = append(transactions, &model.Transaction{
			DoOperations: []*model.Operation{
				{
					Action:   "prependInsert",
					Data:     data,
					ParentID: parentID,
				},
			},
		})
	}

	model.PerformTransactions(&transactions)
	model.FlushTxQueue()

	ret.Data = transactions
	broadcastTransactions(transactions)
}

func insertBlock(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	data := arg["data"].(string)
	dataType := arg["dataType"].(string)
	var parentID, previousID, nextID string
	if nil != arg["parentID"] {
		parentID = arg["parentID"].(string)
		if "" != parentID && util.InvalidIDPattern(parentID, ret) {
			return
		}
	}
	if nil != arg["previousID"] {
		previousID = arg["previousID"].(string)
		if "" != previousID && util.InvalidIDPattern(previousID, ret) {
			return
		}
	}
	if nil != arg["nextID"] {
		nextID = arg["nextID"].(string)
		if "" != nextID && util.InvalidIDPattern(nextID, ret) {
			return
		}
	}

	// When the target is located solely by parentID (no previousID/nextID), the target must be a container block, otherwise the nesting is invalid
	if "" != parentID && "" == previousID && "" == nextID {
		if err := treenode.CheckContainerParent(parentID); err != nil {
			ret.Code = -1
			ret.Msg = err.Error()
			return
		}
	}

	if "markdown" == dataType {
		luteEngine := util.NewLute()
		var err error
		data, err = dataBlockDOM(data, luteEngine)
		if err != nil {
			ret.Code = -1
			ret.Msg = "data block DOM failed: " + err.Error()
			return
		}
	}

	transactions := []*model.Transaction{
		{
			DoOperations: []*model.Operation{
				{
					Action:     "insert",
					Data:       data,
					ParentID:   parentID,
					PreviousID: previousID,
					NextID:     nextID,
				},
			},
		},
	}

	model.PerformTransactions(&transactions)
	model.FlushTxQueue()

	ret.Data = transactions
	broadcastTransactions(transactions)
}

func updateBlock(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	data := arg["data"].(string)
	dataType := arg["dataType"].(string)
	id := arg["id"].(string)
	if util.InvalidIDPattern(id, ret) {
		return
	}

	luteEngine := util.NewLute()
	if "markdown" == dataType {
		var err error
		data, err = dataBlockDOM(data, luteEngine)
		if err != nil {
			ret.Code = -1
			ret.Msg = "data block DOM failed: " + err.Error()
			return
		}
	}
	tree := luteEngine.BlockDOM2Tree(data)
	if nil == tree || nil == tree.Root || nil == tree.Root.FirstChild {
		ret.Code = -1
		ret.Msg = "parse tree failed"
		return
	}

	block, err := model.GetBlock(id, nil)
	if err != nil {
		ret.Code = -1
		ret.Msg = "get block failed: " + err.Error()
		return
	}

	var transactions []*model.Transaction
	if "NodeDocument" == block.Type {
		oldTree, err := filesys.LoadTree(block.Box, block.Path, luteEngine)
		if err != nil {
			ret.Code = -1
			ret.Msg = "load tree failed: " + err.Error()
			return
		}
		var toRemoves []*ast.Node
		var ops []*model.Operation
		for n := oldTree.Root.FirstChild; nil != n; n = n.Next {
			toRemoves = append(toRemoves, n)
			ops = append(ops, &model.Operation{Action: "delete", ID: n.ID, Data: map[string]any{
				"createEmptyParagraph": false, // After clearing the document, the frontend should not create an empty paragraph
			}})
		}
		for _, n := range toRemoves {
			n.Unlink()
		}
		ops = append(ops, &model.Operation{Action: "appendInsert", Data: data, ParentID: id})
		transactions = append(transactions, &model.Transaction{
			DoOperations: ops,
		})
	} else {
		if "NodeListItem" == block.Type && ast.NodeList == tree.Root.FirstChild.Type {
			// Rendering error when updating a list item via the API `api/block/updateBlock` https://github.com/siyuan-note/siyuan/issues/4658
			tree.Root.AppendChild(tree.Root.FirstChild.FirstChild) // Move the list's first list item to the end of the document; once moved, the list item hangs directly under root, and the renderer works correctly
			tree.Root.FirstChild.Unlink()                          // Remove the list
			if nil != tree.Root.FirstChild && ast.NodeKramdownBlockIAL == tree.Root.FirstChild.Type {
				tree.Root.FirstChild.Unlink() // Also remove the list's IAL
			}
		}

		if nil != tree.Root.FirstChild {
			tree.Root.FirstChild.SetIALAttr("id", id)
		} else {
			logging.LogWarnf("tree root has no child node, append empty paragraph node")
			tree.Root.AppendChild(treenode.NewParagraph(id))
		}

		data = luteEngine.Tree2BlockDOM(tree, luteEngine.RenderOptions, luteEngine.ParseOptions)
		transactions = []*model.Transaction{
			{
				DoOperations: []*model.Operation{
					{
						Action: "update",
						ID:     id,
						Data:   data,
					},
				},
			},
		}
	}

	model.PerformTransactions(&transactions)
	model.FlushTxQueue()

	ret.Data = transactions
	broadcastTransactions(transactions)
}

func batchInsertBlock(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	blocksArg := arg["blocks"].([]any)
	var transactions []*model.Transaction
	luteEngine := util.NewLute()
	for _, blockArg := range blocksArg {
		blockMap := blockArg.(map[string]any)
		data := blockMap["data"].(string)
		dataType := blockMap["dataType"].(string)
		var parentID, previousID, nextID string
		if nil != blockMap["parentID"] {
			parentID = blockMap["parentID"].(string)
			if "" != parentID && util.InvalidIDPattern(parentID, ret) {
				return
			}
		}
		if nil != blockMap["previousID"] {
			previousID = blockMap["previousID"].(string)
			if "" != previousID && util.InvalidIDPattern(previousID, ret) {
				return
			}
		}
		if nil != blockMap["nextID"] {
			nextID = blockMap["nextID"].(string)
			if "" != nextID && util.InvalidIDPattern(nextID, ret) {
				return
			}
		}

		// When the target is located solely by parentID (no previousID/nextID), the target must be a container block, otherwise the nesting is invalid
		if "" != parentID && "" == previousID && "" == nextID {
			if err := treenode.CheckContainerParent(parentID); err != nil {
				ret.Code = -1
				ret.Msg = err.Error()
				return
			}
		}

		if "markdown" == dataType {
			var err error
			data, err = dataBlockDOM(data, luteEngine)
			if err != nil {
				ret.Code = -1
				ret.Msg = "data block DOM failed: " + err.Error()
				return
			}
		}

		transactions = append(transactions, &model.Transaction{
			DoOperations: []*model.Operation{
				{
					Action:     "insert",
					Data:       data,
					ParentID:   parentID,
					PreviousID: previousID,
					NextID:     nextID,
				},
			},
		})
	}

	model.PerformTransactions(&transactions)
	model.FlushTxQueue()

	ret.Data = transactions
	broadcastTransactions(transactions)
}

func batchUpdateBlock(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var blocksArg []any
	if !util.ParseJsonArgs(arg, ret, util.BindJsonArg("blocks", &blocksArg, true, true)) {
		return
	}

	type updateBlockArg struct {
		ID       string
		Data     string
		DataType string
		Block    *model.Block
		Tree     *parse.Tree
	}

	var blocks []*updateBlockArg
	luteEngine := util.NewLute()
	for _, blockArg := range blocksArg {
		blockMap := blockArg.(map[string]any)
		id := blockMap["id"].(string)
		if util.InvalidIDPattern(id, ret) {
			return
		}

		data := blockMap["data"].(string)
		dataType := blockMap["dataType"].(string)
		if "markdown" == dataType {
			var err error
			data, err = dataBlockDOM(data, luteEngine)
			if err != nil {
				ret.Code = -1
				ret.Msg = "data block DOM failed: " + err.Error()
				return
			}
		}
		tree := luteEngine.BlockDOM2Tree(data)
		if nil == tree || nil == tree.Root || nil == tree.Root.FirstChild {
			ret.Code = -1
			ret.Msg = "parse tree failed"
			return
		}

		block, err := model.GetBlock(id, nil)
		if err != nil {
			ret.Code = -1
			ret.Msg = "get block failed: " + err.Error()
			return
		}

		blocks = append(blocks, &updateBlockArg{
			ID:       id,
			Data:     data,
			DataType: dataType,
			Block:    block,
			Tree:     tree,
		})
	}

	var ops []*model.Operation
	tx := &model.Transaction{}
	transactions := []*model.Transaction{tx}
	for _, upBlock := range blocks {
		block := upBlock.Block
		data := upBlock.Data
		tree := upBlock.Tree
		id := upBlock.ID
		if "NodeDocument" == block.Type {
			oldTree, err := filesys.LoadTree(block.Box, block.Path, luteEngine)
			if err != nil {
				ret.Code = -1
				ret.Msg = "load tree failed: " + err.Error()
				return
			}
			var toRemoves []*ast.Node

			for n := oldTree.Root.FirstChild; nil != n; n = n.Next {
				toRemoves = append(toRemoves, n)
				ops = append(ops, &model.Operation{Action: "delete", ID: n.ID, Data: map[string]any{
					"createEmptyParagraph": false, // After clearing the document, the frontend should not create an empty paragraph
				}})
			}
			for _, n := range toRemoves {
				n.Unlink()
			}
			ops = append(ops, &model.Operation{Action: "appendInsert", Data: data, ParentID: id})
		} else {
			if "NodeListItem" == block.Type && ast.NodeList == tree.Root.FirstChild.Type {
				// Rendering error when updating a list item via the API `api/block/updateBlock` https://github.com/siyuan-note/siyuan/issues/4658
				tree.Root.AppendChild(tree.Root.FirstChild.FirstChild) // Move the list's first list item to the end of the document; once moved, the list item hangs directly under root, and the renderer works correctly
				tree.Root.FirstChild.Unlink()                          // Remove the list
				tree.Root.FirstChild.Unlink()                          // Also remove the list's IAL
			}
			tree.Root.FirstChild.SetIALAttr("id", id)

			data = luteEngine.Tree2BlockDOM(tree, luteEngine.RenderOptions, luteEngine.ParseOptions)
			ops = append(ops, &model.Operation{
				Action: "update",
				ID:     id,
				Data:   data,
			})
		}
	}

	tx.DoOperations = ops
	model.PerformTransactions(&transactions)
	model.FlushTxQueue()

	ret.Data = transactions
	broadcastTransactions(transactions)
}

func deleteBlock(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	id := arg["id"].(string)
	if util.InvalidIDPattern(id, ret) {
		return
	}

	transactions := []*model.Transaction{
		{
			DoOperations: []*model.Operation{
				{
					Action: "delete",
					ID:     id,
				},
			},
		},
	}

	model.PerformTransactions(&transactions)

	ret.Data = transactions
	broadcastTransactions(transactions)
}

func broadcastTransactions(transactions []*model.Transaction) {
	evt := util.NewCmdResult("transactions", 0, util.PushModeBroadcast)
	evt.Data = transactions
	util.PushEvent(evt)
}

func dataBlockDOM(data string, luteEngine *lute.Lute) (ret string, err error) {
	luteEngine.SetHTMLTag2TextMark(true) // The API `/api/block/**` can't use `<u>foo</u>` or `<kbd>bar</kbd>` to insert/update inline elements https://github.com/siyuan-note/siyuan/issues/6039

	ret, tree := luteEngine.Md2BlockDOMTree(data, true)
	if "" == ret {
		// Error when using the API to insert an empty string https://github.com/siyuan-note/siyuan/issues/3931
		blankParagraph := treenode.NewParagraph("")
		ret = luteEngine.RenderNodeBlockDOM(blankParagraph)
	}

	invalidID := ""
	ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.WalkContinue
		}

		if "" != n.ID {
			if !ast.IsNodeIDPattern(n.ID) {
				invalidID = n.ID
				return ast.WalkStop
			}
		}
		return ast.WalkContinue
	})

	if "" != invalidID {
		err = errors.New("found invalid ID [" + invalidID + "]")
		ret = ""
		return
	}
	return
}
