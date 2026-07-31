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
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/88250/gulu"
	"github.com/88250/lute/ast"
	"github.com/siyuan-note/dejavu/entity"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/av"
	"github.com/siyuan-note/siyuan/kernel/sql"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

type AttributeViewRenderTarget struct {
	Status   string `json:"status"`
	ItemID   string `json:"itemID"`
	GroupID  string `json:"groupID,omitempty"`
	Index    int    `json:"index"`
	Offset   int    `json:"offset"`
	PageSize int    `json:"pageSize"`
}

func RenderAttributeView(blockID, avID, viewID, query string, page, pageSize int, groupPaging map[string]any, createIfNotExist, ignoreRows bool) (viewable av.Viewable, attrView *av.AttributeView, err error) {
	viewable, attrView, _, err = RenderAttributeViewWithTarget(blockID, avID, viewID, query, page, pageSize, groupPaging, createIfNotExist, ignoreRows, "", "")
	return
}

func RenderAttributeViewWithTarget(blockID, avID, viewID, query string, page, pageSize int, groupPaging map[string]any, createIfNotExist, ignoreRows bool, targetItemID, targetGroupID string) (viewable av.Viewable, attrView *av.AttributeView, target *AttributeViewRenderTarget, err error) {
	if !ast.IsNodeIDPattern(avID) {
		err = ErrInvalidID
		return
	}

	waitForSyncingStorages()

	// The AV definition for an encrypted notebook is stored under a notebook-level path; look up boxID via blockID
	avBoxID := ""
	if "" != blockID {
		bt := treenode.GetBlockTree(blockID)
		if nil == bt {
			for _, encBoxID := range treenode.GetOpenedEncryptedBoxIDs() {
				if encBT := treenode.GetBlockTreeInBox(blockID, encBoxID); nil != encBT {
					bt = encBT
					break
				}
			}
		}
		if nil != bt && IsEncryptedBox(bt.BoxID) {
			avBoxID = bt.BoxID
		}
	}

	// Fall back to looking up the AV definition path (global for regular boxes, notebook-level for encrypted notebooks).
	// When the box is already known, look it up directly via InBox to avoid the global pending map being overwritten concurrently
	var existPath string
	if avBoxID != "" {
		existPath, _ = av.FindAttributeViewPathInBox(avID, avBoxID)
	} else {
		existPath, _ = av.FindAttributeViewPath(avID)
	}
	if "" == existPath {
		if avBoxID != "" {
			existPath = filepath.Join(util.DataDir, avBoxID, "storage", "av", avID+".json")
		} else {
			// If the fallback finds nothing, check the global path (first-creation scenario)
			existPath = av.GetAttributeViewDataPath(avID)
		}
	}
	if !filelock.IsExist(existPath) {
		if !createIfNotExist {
			err = av.ErrAttributeViewNotFound
			return
		}

		// First creation for an encrypted notebook: set pending only to route SaveAttributeView's path, then clear it right after creation
		if avBoxID != "" {
			av.SetAVBoxID(avID, avBoxID)
			defer av.SetAVBoxID(avID, "") // Clear immediately after creation to avoid contaminating subsequent routing
		}
		attrView = av.NewAttributeView(avID)
		if err = av.SaveAttributeView(attrView); err != nil {
			logging.LogErrorf("save attribute view [%s] failed: %s", avID, err)
			return
		}
		if blockID != "" {
			av.UpsertBlockRel(avID, blockID)
		}
	}

	// When the box is already known, parse directly via InBox instead of relying on the global pending state
	if avBoxID != "" {
		attrView, err = av.ParseAttributeViewInBox(avID, avBoxID)
	} else {
		attrView, err = av.ParseAttributeView(avID)
	}
	if err != nil {
		logging.LogErrorf("parse attribute view [%s] failed: %s", avID, err)
		return
	}
	if targetItemID != "" {
		target = &AttributeViewRenderTarget{Status: "itemNotFound", ItemID: targetItemID}
		if nil != attrView.GetBlockValue(targetItemID) {
			target.Status = "filtered"
		}
		if viewID != "" {
			if requestedView := attrView.GetView(viewID); nil == requestedView {
				target.Status = "viewNotFound"
				viewID = ""
			}
		}
	}

	// Diagnostic: data volume after parsing the AV
	blockKV := attrView.GetBlockKeyValues()
	if nil != blockKV {
	} else {
	}

	viewable, err = renderAttributeView(attrView, blockID, viewID, query, page, pageSize, groupPaging, ignoreRows, target, targetGroupID)
	return
}

const (
	groupValueDefault                                        = "_@default@_"    // Default group value (the group used when the value is empty)
	groupValueNotInRange                                     = "_@notInRange@_" // Group value for items out of range (only possible for numeric-type grouping)
	groupValueLast30Days, groupValueLast7Days                = "_@last30Days@_", "_@last7Days@_"
	groupValueYesterday, groupValueToday, groupValueTomorrow = "_@yesterday@_", "_@today@_", "_@tomorrow@_"
	groupValueNext7Days, groupValueNext30Days                = "_@next7Days@_", "_@next30Days@_"
)

func renderAttributeView(attrView *av.AttributeView, nodeID, viewID, query string, page, pageSize int, groupPaging map[string]any, ignoreRows bool, target *AttributeViewRenderTarget, targetGroupID string) (viewable av.Viewable, err error) {
	// Get the view to render
	view, err := getRenderAttributeViewView(attrView, viewID, nodeID, nil == target)
	if nil != err {
		return
	}

	// Do some data compatibility and correction handling
	checkAttrView(attrView, view)
	upgradeAttributeViewSpec(attrView)

	// Render the view
	viewable = sql.RenderView(attrView, view, query, ignoreRows)
	renderTargetItemID := targetItemID(target)
	if view.IsGroupView() || view.LayoutType == av.LayoutTypeKanban {
		renderTargetItemID = ""
	}
	var targetIndex, targetOffset int
	targetIndex, targetOffset, err = renderViewableInstance(viewable, view, attrView, page, pageSize, ignoreRows, renderTargetItemID)
	if nil != err {
		return
	}
	if nil != target && target.Status != "viewNotFound" && targetIndex >= 0 && !view.IsGroupView() && view.LayoutType != av.LayoutTypeKanban {
		setAttributeViewRenderTarget(target, "", targetIndex, targetOffset, view.PageSize)
	}

	// Render the group view. When ignoreRows, render metadata for panel use if groups already exist, or skip if not
	// (generating groups requires row data)
	if !ignoreRows || len(view.Groups) > 0 {
		err = renderAttributeViewGroups(viewable, attrView, view, query, page, pageSize, groupPaging, ignoreRows, target, targetGroupID)
	}
	return
}

func renderAttributeViewGroups(viewable av.Viewable, attrView *av.AttributeView, view *av.View, query string, page, pageSize int, groupPaging map[string]any, ignoreRows bool, target *AttributeViewRenderTarget, targetGroupID string) (err error) {
	groupKey := view.GetGroupKey(attrView)
	if nil == groupKey {
		if view.LayoutType == av.LayoutTypeKanban {
			preferredGroupKey := getKanbanPreferredGroupKey(attrView)
			group := &av.ViewGroup{Field: preferredGroupKey.ID}
			setAttributeViewGroup(attrView, view, group)
			if err = av.SaveAttributeView(attrView); err != nil {
				logging.LogErrorf("save attribute view [%s] failed: %s", attrView.ID, err)
				return
			}
			groupKey = view.GetGroupKey(attrView)
			if nil == groupKey {
				return
			}
		} else {
			return
		}
	}

	// The current date can change, so groups need to be regenerated when grouping by date.
	// Skip regeneration when ignoreRows (it requires row data) and keep using the saved groups.
	if !ignoreRows && isGroupByDate(view) {
		createdDate := time.UnixMilli(view.GroupCreated).Format("2006-01-02")
		if time.Now().Format("2006-01-02") != createdDate {
			genAttrViewGroups(view, attrView) // Only regenerate groups for a single view to improve performance
			if err = av.SaveAttributeView(attrView); err != nil {
				logging.LogErrorf("save attribute view [%s] failed: %s", attrView.ID, err)
				return
			}
		}
	}

	// Groups need to be regenerated when grouping by template.
	// Skip regeneration when ignoreRows (it requires row data) and keep using the saved groups.
	if !ignoreRows && isGroupByTemplate(attrView, view) {
		genAttrViewGroups(view, attrView) // Only regenerate groups for a single view to improve performance
		if err = av.SaveAttributeView(attrView); err != nil {
			logging.LogErrorf("save attribute view [%s] failed: %s", attrView.ID, err)
			return
		}
	}

	// Render the group view. When ignoreRows, render metadata for panel use if groups already exist, or return if not
	// (generating groups requires row data)
	if nil == view.Groups {
		if ignoreRows {
			return
		}
		genAttrViewGroups(view, attrView)
		if err = av.SaveAttributeView(attrView); err != nil {
			logging.LogErrorf("save attribute view [%s] failed: %s", attrView.ID, err)
			return
		}
	}

	for _, groupView := range view.Groups {
		groupView.Name = groupView.GetGroupValue()
		switch groupView.Name {
		case groupValueDefault:
			groupView.Name = fmt.Sprintf(Conf.language(264), groupKey.Name)
		case groupValueNotInRange:
			groupView.Name = Conf.language(265)
		case groupValueLast30Days:
			groupView.Name = fmt.Sprintf(Conf.language(259), 30)
		case groupValueLast7Days:
			groupView.Name = fmt.Sprintf(Conf.language(259), 7)
		case groupValueYesterday:
			groupView.Name = Conf.language(260)
		case groupValueToday:
			groupView.Name = Conf.language(261)
		case groupValueTomorrow:
			groupView.Name = Conf.language(262)
		case groupValueNext7Days:
			groupView.Name = fmt.Sprintf(Conf.language(263), 7)
		case groupValueNext30Days:
			groupView.Name = fmt.Sprintf(Conf.language(263), 30)
		}
	}

	sortGroupViews(attrView, view)
	targetGroupID = resolveAttributeViewTargetGroupID(view, target, targetGroupID)

	var groups []av.Viewable
	for _, groupView := range view.Groups {
		groupViewable := sql.RenderGroupView(attrView, view, groupView, query)

		groupPage, groupPageSize := page, pageSize
		if nil != groupPaging {
			if paging := groupPaging[groupView.ID]; nil != paging {
				pagingMap := paging.(map[string]any)
				if nil != pagingMap["page"] {
					groupPage = int(pagingMap["page"].(float64))
				}
				if nil != pagingMap["pageSize"] {
					groupPageSize = int(pagingMap["pageSize"].(float64))
				}
			}
		}

		groupTargetItemID := ""
		if nil != target && target.Status != "viewNotFound" {
			if (targetGroupID != "" && groupView.ID == targetGroupID) || (targetGroupID == "" && target.Status != "visible") {
				groupTargetItemID = target.ItemID
			}
		}
		targetIndex, targetOffset, renderErr := renderViewableInstance(groupViewable, view, attrView, groupPage, groupPageSize, ignoreRows, groupTargetItemID)
		err = renderErr
		if nil != err {
			return
		}
		if !ignoreRows {
			hideEmptyGroupViews(view, groupViewable)
		}
		if nil != target && target.Status != "viewNotFound" && targetIndex >= 0 {
			if groupViewable.GetGroupHidden() == 0 {
				if target.Status != "visible" || groupView.ID == targetGroupID {
					setAttributeViewRenderTarget(target, groupView.ID, targetIndex, targetOffset, view.PageSize)
				}
			} else if target.Status != "visible" {
				target.Status = "groupHidden"
				target.GroupID = groupView.ID
			}
		}

		groups = append(groups, groupViewable)

		// Clear the group fields on the group view to reduce redundancy (field info can be obtained from the overall view object)
		switch groupView.LayoutType {
		case av.LayoutTypeTable:
			groupView.Table.Columns = nil
		case av.LayoutTypeGallery:
			groupView.Gallery.CardFields = nil
		case av.LayoutTypeKanban:
			groupView.Kanban.Fields = nil
		}
	}
	viewable.SetGroups(groups)

	// Clear the items on the overall view to reduce redundancy
	viewable.(av.Collection).SetItems(nil)
	return
}

func hideEmptyGroupViews(view *av.View, viewable av.Viewable) {
	if !view.IsGroupView() {
		return
	}

	groupHidden := viewable.GetGroupHidden()
	if !view.Group.HideEmpty {
		if 2 != groupHidden {
			viewable.SetGroupHidden(0)
		}
		return
	}

	itemCount := viewable.(av.Collection).CountItems()
	if 1 == groupHidden && 0 < itemCount {
		viewable.SetGroupHidden(0)
	}
}

func sortGroupViews(attrView *av.AttributeView, view *av.View) {
	if av.GroupOrderMan == view.Group.Order {
		sort.Slice(view.Groups, func(i, j int) bool { return view.Groups[i].GroupSort < view.Groups[j].GroupSort })
		return
	}

	if av.GroupMethodDateRelative == view.Group.Method {
		var relativeDateGroups []*av.View
		var last30Days, last7Days, yesterday, today, tomorrow, next7Days, next30Days, defaultGroup *av.View
		for _, groupView := range view.Groups {
			_, err := time.Parse("2006-01", groupView.GetGroupValue())
			if nil == err { // If it parses successfully, it's a "more than 30 days ago/from now" group form
				relativeDateGroups = append(relativeDateGroups, groupView)
			} else { // Otherwise it's a relative-date group form
				switch groupView.GetGroupValue() {
				case groupValueLast30Days:
					last30Days = groupView
				case groupValueLast7Days:
					last7Days = groupView
				case groupValueYesterday:
					yesterday = groupView
				case groupValueToday:
					today = groupView
				case groupValueTomorrow:
					tomorrow = groupView
				case groupValueNext7Days:
					next7Days = groupView
				case groupValueNext30Days:
					next30Days = groupView
				case groupValueDefault:
					defaultGroup = groupView
				}
			}
		}

		sort.SliceStable(relativeDateGroups, func(i, j int) bool {
			return relativeDateGroups[i].GetGroupValue() < relativeDateGroups[j].GetGroupValue()
		})

		var lastNext30Days []*av.View
		if nil != next30Days {
			lastNext30Days = append(lastNext30Days, next30Days)
		}
		if nil != next7Days {
			lastNext30Days = append(lastNext30Days, next7Days)
		}
		if nil != tomorrow {
			lastNext30Days = append(lastNext30Days, tomorrow)
		}
		if nil != today {
			lastNext30Days = append(lastNext30Days, today)
		}
		if nil != yesterday {
			lastNext30Days = append(lastNext30Days, yesterday)
		}

		if nil != last7Days {
			lastNext30Days = append(lastNext30Days, last7Days)
		}
		if nil != last30Days {
			lastNext30Days = append(lastNext30Days, last30Days)
		}

		startIdx := -1
		todayStart := util.GetTodayStart()
		thisMonth := todayStart.Format("2006-01")
		for i, monthGroup := range relativeDateGroups {
			if monthGroup.GetGroupValue() < thisMonth {
				startIdx = i + 1
			}
		}
		if -1 == startIdx {
			startIdx = 0
		}
		for _, g := range lastNext30Days {
			relativeDateGroups = util.InsertElem(relativeDateGroups, startIdx, g)
		}

		if av.GroupOrderDesc == view.Group.Order {
			slices.Reverse(relativeDateGroups)
		}

		if nil != defaultGroup {
			relativeDateGroups = append(relativeDateGroups, defaultGroup)
		}

		view.Groups = relativeDateGroups
		return
	}

	if av.GroupOrderAsc == view.Group.Order || av.GroupOrderDesc == view.Group.Order {
		defaultGroup := view.GetGroupByGroupValue(groupValueDefault)
		if nil != defaultGroup {
			view.RemoveGroupByID(defaultGroup.ID)
		}

		sort.SliceStable(view.Groups, func(i, j int) bool {
			iVal, jVal := view.Groups[i].GetGroupValue(), view.Groups[j].GetGroupValue()
			if av.GroupOrderAsc == view.Group.Order {
				return util.NaturalCompare(iVal, jVal)
			}
			return util.NaturalCompare(jVal, iVal)
		})

		if nil != defaultGroup {
			view.Groups = append(view.Groups, defaultGroup)
		}
		return
	}

	if av.GroupOrderSelectOption == view.Group.Order {
		groupKey := view.GetGroupKey(attrView)
		if nil == groupKey {
			return
		}

		if av.KeyTypeSelect != groupKey.Type && av.KeyTypeMSelect != groupKey.Type {
			return
		}

		sortGroupsBySelectOption(view, groupKey)
		return
	}
}

func sortGroupsBySelectOption(view *av.View, groupKey *av.Key) {
	optionSort := map[string]int{}
	for i, op := range groupKey.Options {
		optionSort[op.Name] = i
	}

	defaultGroup := view.GetGroupByGroupValue(groupValueDefault)
	if nil != defaultGroup {
		view.RemoveGroupByID(defaultGroup.ID)
	}

	sort.Slice(view.Groups, func(i, j int) bool {
		vSort := optionSort[view.Groups[i].GetGroupValue()]
		oSort := optionSort[view.Groups[j].GetGroupValue()]
		return vSort < oSort
	})

	if nil != defaultGroup {
		view.Groups = append(view.Groups, defaultGroup)
	}
}

func isGroupByDate(view *av.View) bool {
	if !view.IsGroupView() {
		return false
	}
	return av.GroupMethodDateDay == view.Group.Method || av.GroupMethodDateWeek == view.Group.Method || av.GroupMethodDateMonth == view.Group.Method || av.GroupMethodDateYear == view.Group.Method || av.GroupMethodDateRelative == view.Group.Method
}

func isGroupByTemplate(attrView *av.AttributeView, view *av.View) bool {
	if !view.IsGroupView() {
		return false
	}

	groupKey := view.GetGroupKey(attrView)
	if nil == groupKey {
		return false
	}
	return av.KeyTypeTemplate == groupKey.Type
}

func renderViewableInstance(viewable av.Viewable, view *av.View, attrView *av.AttributeView, page, pageSize int, ignoreRows bool, targetItemID string) (targetIndex, targetOffset int, err error) {
	targetIndex = -1
	if nil == viewable {
		err = av.ErrViewNotFound
		logging.LogErrorf("render attribute view [%s] failed", attrView.ID)
		return
	}

	// When ignoreRows, rows are already empty, so skip filter/sort/calc and pagination (the menu doesn't need row data)
	if ignoreRows {
		return
	}

	cachedAttrViews := map[string]*av.AttributeView{}
	rollupFurtherCollections := sql.GetFurtherCollections(attrView, cachedAttrViews)
	av.Filter(viewable, attrView, rollupFurtherCollections, cachedAttrViews)
	av.Sort(viewable, attrView)
	av.Calc(viewable, attrView)

	// Pagination
	switch viewable.GetType() {
	case av.LayoutTypeTable:
		table := viewable.(*av.Table)
		targetIndex = findAttributeViewTargetIndex(targetItemID, len(table.Rows), func(index int) string { return table.Rows[index].ID })
		table.RowCount = len(table.Rows)
		table.PageSize = view.PageSize
		if 1 > pageSize {
			pageSize = table.PageSize
		}
		start, end := getAttributeViewRenderRange(page, pageSize, targetIndex, table.PageSize, len(table.Rows))
		if targetIndex >= 0 {
			targetOffset = start
		}
		table.Rows = table.Rows[start:end]
	case av.LayoutTypeGallery:
		gallery := viewable.(*av.Gallery)
		targetIndex = findAttributeViewTargetIndex(targetItemID, len(gallery.Cards), func(index int) string { return gallery.Cards[index].ID })
		gallery.CardCount = len(gallery.Cards)
		gallery.PageSize = view.PageSize
		if 1 > pageSize {
			pageSize = gallery.PageSize
		}
		start, end := getAttributeViewRenderRange(page, pageSize, targetIndex, gallery.PageSize, len(gallery.Cards))
		if targetIndex >= 0 {
			targetOffset = start
		}
		gallery.Cards = gallery.Cards[start:end]
	case av.LayoutTypeKanban:
		kanban := viewable.(*av.Kanban)
		targetIndex = findAttributeViewTargetIndex(targetItemID, len(kanban.Cards), func(index int) string { return kanban.Cards[index].ID })
		kanban.CardCount = len(kanban.Cards)
		kanban.PageSize = view.PageSize
		if 1 > pageSize {
			pageSize = kanban.PageSize
		}
		start, end := getAttributeViewRenderRange(page, pageSize, targetIndex, kanban.PageSize, len(kanban.Cards))
		if targetIndex >= 0 {
			targetOffset = start
		}
		kanban.Cards = kanban.Cards[start:end]
	}
	return
}

func targetItemID(target *AttributeViewRenderTarget) string {
	if nil == target || target.Status == "viewNotFound" {
		return ""
	}
	return target.ItemID
}

func findAttributeViewTargetIndex(targetItemID string, length int, getID func(index int) string) int {
	if targetItemID == "" {
		return -1
	}
	for i := 0; i < length; i++ {
		if getID(i) == targetItemID {
			return i
		}
	}
	return -1
}

func getAttributeViewRenderRange(page, pageSize, targetIndex, defaultPageSize, length int) (start, end int) {
	if 1 > defaultPageSize {
		defaultPageSize = av.ViewDefaultPageSize
	}
	if 1 > pageSize {
		pageSize = defaultPageSize
	}
	if targetIndex < 0 {
		start = min(length, max(0, (page-1)*pageSize))
		end = min(length, start+pageSize)
		return
	}

	windowSize := min(length, max(defaultPageSize, av.ViewDefaultPageSize*4))
	start = max(0, targetIndex-windowSize/2)
	end = min(length, start+windowSize)
	start = max(0, end-windowSize)
	return
}

func resolveAttributeViewTargetGroupID(view *av.View, target *AttributeViewRenderTarget, targetGroupID string) string {
	if targetGroupID == "" || nil == target {
		return targetGroupID
	}
	targetGroup := view.GetGroupByID(targetGroupID)
	if nil == targetGroup || !gulu.Str.Contains(target.ItemID, targetGroup.GroupItemIDs) {
		return ""
	}
	return targetGroupID
}

func setAttributeViewRenderTarget(target *AttributeViewRenderTarget, groupID string, index, offset, pageSize int) {
	target.Status = "visible"
	target.GroupID = groupID
	target.Index = index
	target.Offset = offset
	target.PageSize = pageSize
}

func getRenderAttributeViewView(attrView *av.AttributeView, viewID, nodeID string, persistView bool) (ret *av.View, err error) {
	if 1 > len(attrView.Views) {
		view, _, _ := av.NewTableViewWithBlockKey(ast.NewNodeID())
		attrView.Views = append(attrView.Views, view)
		attrView.ViewID = view.ID
		if err = av.SaveAttributeView(attrView); err != nil {
			logging.LogErrorf("save attribute view [%s] failed: %s", attrView.ID, err)
			return
		}
	}

	if "" == viewID && "" != nodeID {
		node, _, _ := getNodeByBlockID(nil, nodeID)
		if nil != node {
			viewID = node.IALAttr(av.NodeAttrView)
		}
	}

	if "" != viewID {
		ret, _ = attrView.GetCurrentView(viewID)
		if persistView && nil != ret && ret.ID != attrView.ViewID {
			attrView.ViewID = ret.ID
			if err = av.SaveAttributeView(attrView); err != nil {
				logging.LogErrorf("save attribute view [%s] failed: %s", attrView.ID, err)
				return
			}
		}
	} else {
		ret = attrView.GetView(attrView.ViewID)
	}

	if nil == ret {
		ret = attrView.Views[0]
	}
	return
}

// avBoxIDFromRepoPath looks up the boxID from a snapshot file path.
// A global path /storage/av/<avID>.json returns an empty string; an encrypted notebook path
// /<boxID>/storage/av/<avID>.json returns boxID.
func avBoxIDFromRepoPath(repoPath string) string {
	parts := strings.Split(repoPath, "/")
	// Global path: ["", "storage", "av", "xxx.json"] -> parts[1]=="storage"
	// Encrypted box: ["", "<boxID>", "storage", "av", "xxx.json"] -> parts[1]=="<boxID>"
	if len(parts) >= 4 && parts[2] == "storage" {
		return parts[1]
	}
	return ""
}

func RenderRepoSnapshotAttributeView(indexID, avID string) (viewable av.Viewable, attrView *av.AttributeView, err error) {
	if !ast.IsNodeIDPattern(avID) {
		err = ErrInvalidID
		return
	}

	repo, err := newRepository()
	if err != nil {
		return
	}

	index, err := repo.GetIndex(indexID)
	if err != nil {
		return
	}

	files, err := repo.GetFiles(index)
	if err != nil {
		return
	}
	var avFile *entity.File
	for _, f := range files {
		// Match global /storage/av/<avID>.json or encrypted notebook /<boxID>/storage/av/<avID>.json
		if strings.HasSuffix(f.Path, "/storage/av/"+avID+".json") {
			avFile = f
			break
		}
	}

	if nil == avFile {
		attrView = av.NewAttributeView(avID)
		err = av.ErrAttributeViewNotFound
		return
	}

	data, readErr := repo.OpenFile(avFile)
	if nil != readErr {
		logging.LogErrorf("read attribute view [%s] failed: %s", avID, readErr)
		err = readErr
		return
	}

	// An encrypted notebook's AV is ciphertext in the snapshot; look up boxID from the path and decrypt it
	if histBoxID := avBoxIDFromRepoPath(avFile.Path); histBoxID != "" && IsEncryptedBox(histBoxID) {
		dec, decErr := av.DecryptAVData(histBoxID, avID, data)
		if decErr != nil {
			logging.LogErrorf("decrypt snapshot attribute view [%s] failed: %s", avID, decErr)
			err = decErr
			return
		}
		data = dec
	}

	attrView = av.NewAttributeView(avID)
	if err = gulu.JSON.UnmarshalJSON(data, attrView); err != nil {
		logging.LogErrorf("unmarshal attribute view [%s] failed: %s", avID, err)
		return
	}

	viewable, err = renderAttributeView(attrView, "", "", "", 1, -1, nil, false, nil, "")
	return
}

func RenderHistoryAttributeView(blockID, avID, viewID, query string, page, pageSize int, groupPaging map[string]any, created string) (viewable av.Viewable, attrView *av.AttributeView, err error) {
	if !ast.IsNodeIDPattern(avID) {
		err = ErrInvalidID
		return
	}

	createdUnix, parseErr := strconv.ParseInt(created, 10, 64)
	if nil != parseErr {
		logging.LogErrorf("parse created [%s] failed: %s", created, parseErr)
		err = fmt.Errorf("parse created [%s] failed: %w", created, parseErr)
		return
	}

	dirPrefix := time.Unix(createdUnix, 0).Format("2006-01-02-150405")
	globPath := filepath.Join(util.HistoryDir, dirPrefix+"*")
	matches, err := filepath.Glob(globPath)
	if err != nil {
		logging.LogErrorf("glob [%s] failed: %s", globPath, err)
		return
	}
	if 1 > len(matches) {
		err = av.ErrAttributeViewNotFound
		return
	}

	historyDir := matches[0]
	avJSONPath := filepath.Join(historyDir, "storage", "av", avID+".json")
	if !gulu.File.IsExist(avJSONPath) {
		// An encrypted notebook's AV definition may be under a boxID subdirectory of the history directory
		entries, _ := os.ReadDir(historyDir)
		for _, entry := range entries {
			if entry.IsDir() && ast.IsNodeIDPattern(entry.Name()) {
				candidate := filepath.Join(historyDir, entry.Name(), "storage", "av", avID+".json")
				if gulu.File.IsExist(candidate) {
					avJSONPath = candidate
					break
				}
			}
		}
	}
	if !gulu.File.IsExist(avJSONPath) {
		logging.LogWarnf("attribute view [%s] not found in history data [%s], use current data instead", avID, historyDir)
		// An encrypted notebook's AV definition lives in a notebook-level directory
		_, boxID := av.FindAttributeViewPath(avID)
		if boxID != "" {
			avJSONPath = filepath.Join(util.DataDir, boxID, "storage", "av", avID+".json")
		} else {
			avJSONPath = filepath.Join(util.DataDir, "storage", "av", avID+".json")
		}
	}
	if !gulu.File.IsExist(avJSONPath) {
		logging.LogWarnf("attribute view [%s] not found in current data", avID)
		attrView = av.NewAttributeView(avID)
		err = av.ErrAttributeViewNotFound
		return
	}

	data, readErr := os.ReadFile(avJSONPath)
	if nil != readErr {
		logging.LogErrorf("read attribute view [%s] failed: %s", avID, readErr)
		err = readErr
		return
	}

	// An encrypted notebook's historical AV definition is ciphertext and must be decrypted before it can be parsed.
	// Extract boxID from the path; if that fails, iterate over all opened encrypted notebooks and try decrypting.
	avAbsSlash := filepath.ToSlash(avJSONPath)
	var histBoxID string
	if idx := strings.Index(avAbsSlash, "/storage/av/"); idx > 0 {
		prefix := avAbsSlash[:idx]
		segs := strings.Split(prefix, "/")
		for i := len(segs) - 1; i >= 0; i-- {
			if ast.IsNodeIDPattern(segs[i]) {
				histBoxID = segs[i]
				break
			}
		}
	}
	if histBoxID != "" && IsEncryptedBox(histBoxID) {
		data, err = av.DecryptAVData(histBoxID, avID, data)
		if err != nil {
			logging.LogErrorf("decrypt history AV [%s] failed: %s", avID, err)
			return
		}
	} else {
		// No boxID could be extracted from the path (e.g. an old path in the history directory without a boxID prefix);
		// try iterating over the opened encrypted notebooks to decrypt
		for _, encBoxID := range treenode.GetOpenedEncryptedBoxIDs() {
			if dec, decErr := av.DecryptAVData(encBoxID, avID, data); decErr == nil {
				data = dec
				break
			}
		}
	}

	attrView = av.NewAttributeView(avID)
	if err = gulu.JSON.UnmarshalJSON(data, attrView); err != nil {
		logging.LogErrorf("unmarshal attribute view [%s] failed: %s", avID, err)
		return
	}

	viewable, err = renderAttributeView(attrView, blockID, viewID, query, page, pageSize, groupPaging, false, nil, "")
	return
}
