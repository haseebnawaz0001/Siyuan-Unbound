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

// Package av contains the implementation related to attribute views (databases).
package av

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/88250/gulu"
	"github.com/88250/lute/ast"
	"github.com/goccy/go-json"
	jsoniter "github.com/json-iterator/go"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/cache"
	"github.com/siyuan-note/siyuan/kernel/util"
)

// AttributeView describes the structure of an attribute view.
type AttributeView struct {
	Spec              int                `json:"spec"`                        // Format version
	ID                string             `json:"id"`                          // Attribute view ID
	Name              string             `json:"name"`                        // Attribute view name
	KeyValues         []*KeyValues       `json:"keyValues"`                   // Attribute view key-value pairs
	KeyIDs            []string           `json:"keyIDs"`                      // Attribute view key IDs, used for sorting
	ViewID            string             `json:"viewID"`                      // Current view ID
	Views             []*View            `json:"views"`                       // Views
	NewItemTemplates  []*NewItemTemplate `json:"newItemTemplates,omitempty"`  // Templates for new items
	DefaultTemplateID string             `json:"defaultTemplateID,omitempty"` // Default template ID for new items

	RenderedViewables map[string]Viewable `json:"-"` // Views that have already been rendered
}

// NewItemTargetType describes the target type created by a new item template.
type NewItemTargetType string

const (
	NewItemTargetDetached NewItemTargetType = "detached"
	NewItemTargetDocument NewItemTargetType = "document"
)

// NewItemSaveLocation describes the save location after a document-type template overrides the global new
// document location.
// nil means it inherits the global configuration; non-nil with an empty BoxID means the notebook of the current
// database instance is used.
type NewItemSaveLocation struct {
	BoxID        string `json:"boxID,omitempty"`
	PathTemplate string `json:"pathTemplate"`
}

// NewItemFieldValueMode describes how a new item template's field default value is filled in.
type NewItemFieldValueMode string

const (
	NewItemFieldValueStatic      NewItemFieldValueMode = "static"
	NewItemFieldValueCurrentTime NewItemFieldValueMode = "currentTime"
)

// NewItemFieldValue describes a single field's default value within a new item template.
type NewItemFieldValue struct {
	Mode  NewItemFieldValueMode `json:"mode"`
	Value *Value                `json:"value,omitempty"`
}

// NewItemTemplate describes a template used when creating a new item in the database.
type NewItemTemplate struct {
	ID                  string                        `json:"id"`
	Name                string                        `json:"name"`
	Icon                string                        `json:"icon,omitempty"`
	TargetType          NewItemTargetType             `json:"targetType"`
	PrimaryKeyTemplate  string                        `json:"primaryKeyTemplate,omitempty"`
	FieldValues         map[string]*NewItemFieldValue `json:"fieldValues,omitempty"`
	SaveLocation        *NewItemSaveLocation          `json:"saveLocation,omitempty"`
	ContentTemplatePath string                        `json:"contentTemplatePath,omitempty"`
}

// NewItemTemplatesConfig describes one complete update to the new item templates configuration.
type NewItemTemplatesConfig struct {
	Templates         []*NewItemTemplate `json:"templates"`
	DefaultTemplateID string             `json:"defaultTemplateID,omitempty"`
}

// KeyValues describes the structure of an attribute view's key-value list.
type KeyValues struct {
	Key    *Key     `json:"key"`              // Attribute view key
	Values []*Value `json:"values,omitempty"` // Attribute view value list
}

func (kValues *KeyValues) GetValue(blockID string) (ret *Value) {
	for _, v := range kValues.Values {
		if v.BlockID == blockID {
			ret = v
			return
		}
	}
	return
}

func (kValues *KeyValues) GetBlockValue() (ret *Value) {
	for _, v := range kValues.Values {
		if KeyTypeBlock == v.Type {
			ret = v
			return
		}
	}
	return
}

func GetValue(keyValues []*KeyValues, keyID, itemID string) (ret *Value) {
	for _, kv := range keyValues {
		if kv.Key.ID == keyID {
			for _, v := range kv.Values {
				if v.BlockID == itemID {
					ret = v
					return
				}
			}
		}
	}
	return
}

// KeyType describes the type of an attribute view field.
type KeyType string

const (
	KeyTypeBlock      KeyType = "block"      // Primary key
	KeyTypeText       KeyType = "text"       // Text
	KeyTypeNumber     KeyType = "number"     // Number
	KeyTypeDate       KeyType = "date"       // Date
	KeyTypeSelect     KeyType = "select"     // Single select
	KeyTypeMSelect    KeyType = "mSelect"    // Multi-select
	KeyTypeURL        KeyType = "url"        // URL
	KeyTypeEmail      KeyType = "email"      // Email
	KeyTypePhone      KeyType = "phone"      // Phone
	KeyTypeMAsset     KeyType = "mAsset"     // Asset
	KeyTypeTemplate   KeyType = "template"   // Template
	KeyTypeCreated    KeyType = "created"    // Created time
	KeyTypeUpdated    KeyType = "updated"    // Updated time
	KeyTypeCheckbox   KeyType = "checkbox"   // Checkbox
	KeyTypeRelation   KeyType = "relation"   // Relation
	KeyTypeRollup     KeyType = "rollup"     // Rollup
	KeyTypeLineNumber KeyType = "lineNumber" // Line number
)

// Key describes the base structure of an attribute view field.
type Key struct {
	ID   string  `json:"id"`   // Field ID
	Name string  `json:"name"` // Field name
	Type KeyType `json:"type"` // Field type
	Icon string  `json:"icon"` // Field icon
	Desc string  `json:"desc"` // Field description

	// The following are properties specific to certain column types

	// Single select/multi-select
	Options []*SelectOption `json:"options,omitempty"` // Option list

	// Number
	NumberFormat NumberFormat `json:"numberFormat"` // Column number formatting

	// Template
	Template string `json:"template"` // Template content

	// Relation
	Relation *Relation `json:"relation,omitempty"` // Relation info

	// Rollup
	Rollup *Rollup `json:"rollup,omitempty"` // Rollup info

	// Date
	Date *Date `json:"date,omitempty"` // Date settings

	// Created time
	Created *Created `json:"created,omitempty"` // Created time settings

	// Updated time
	Updated *Updated `json:"updated,omitempty"` // Updated time settings
}

func NewKey(id, name, icon string, keyType KeyType) *Key {
	return &Key{
		ID:   id,
		Name: name,
		Type: keyType,
		Icon: icon,
	}
}

func (k *Key) GetOption(name string) (ret *SelectOption) {
	for _, option := range k.Options {
		if option.Name == name {
			ret = option
			return
		}
	}
	return
}

type Created struct {
	IncludeTime bool `json:"includeTime"` // Whether to fill in the specific time Add `Include time` switch to database creation time field and update time field https://github.com/siyuan-note/siyuan/issues/12091
}

type Updated struct {
	IncludeTime bool `json:"includeTime"` // Whether to fill in the specific time Add `Include time` switch to database creation time field and update time field https://github.com/siyuan-note/siyuan/issues/12091
}

type Date struct {
	AutoFillNow      bool `json:"autoFillNow"`      // Whether to automatically fill in the current time The database date field supports filling the current time by default https://github.com/siyuan-note/siyuan/issues/10823
	FillSpecificTime bool `json:"fillSpecificTime"` // Whether to fill in a specific time Add `Default fill specific time` switch to database date field https://github.com/siyuan-note/siyuan/issues/12089
}

type Rollup struct {
	RelationKeyID string      `json:"relationKeyID"` // Relation field ID
	KeyID         string      `json:"keyID"`         // Target field ID
	Calc          *RollupCalc `json:"calc"`          // Calculation method
}

type RollupCalc struct {
	Operator CalcOperator `json:"operator"`
	Result   *Value       `json:"result"`
}

type Relation struct {
	AvID      string `json:"avID"`      // ID of the related attribute view
	IsTwoWay  bool   `json:"isTwoWay"`  // Whether the relation is two-way
	BackKeyID string `json:"backKeyID"` // ID of the back-link relation column for a two-way relation
}

type SelectOption struct {
	Name  string `json:"name"`  // Option name
	Color string `json:"color"` // Option color
	Desc  string `json:"desc"`  // Option description
}

// View describes the structure of a view.
type View struct {
	ID               string         `json:"id"`                // View ID
	Icon             string         `json:"icon"`              // View icon
	Name             string         `json:"name"`              // View name
	HideAttrViewName bool           `json:"hideAttrViewName"`  // Whether to hide the attribute view name
	Desc             string         `json:"desc"`              // View description
	Filters          []*ViewFilter  `json:"filters,omitempty"` // Filter rules
	Sorts            []*ViewSort    `json:"sorts,omitempty"`   // Sort rules
	PageSize         int            `json:"pageSize"`          // Number of items per page
	LayoutType       LayoutType     `json:"type"`              // Current layout type
	Table            *LayoutTable   `json:"table,omitempty"`   // Table layout
	Gallery          *LayoutGallery `json:"gallery,omitempty"` // Gallery layout
	Kanban           *LayoutKanban  `json:"kanban,omitempty"`  // Kanban layout
	ItemIDs          []string       `json:"itemIds,omitempty"` // Item ID list, used to maintain all items

	Group        *ViewGroup `json:"group,omitempty"`     // Group rule
	GroupCreated int64      `json:"groupCreated"`        // Group creation timestamp
	Groups       []*View    `json:"groups,omitempty"`    // Group view list
	GroupItemIDs []string   `json:"groupItemIds"`        // Group item ID list, used to maintain all items within the group
	GroupCalc    *GroupCalc `json:"groupCalc,omitempty"` // Group calculation rule
	GroupKey     *Key       `json:"groupKey,omitempty"`  // Field to group by
	GroupVal     *Value     `json:"groupVal,omitempty"`  // Group value
	GroupFolded  bool       `json:"groupFolded"`         // Whether the group is folded
	GroupHidden  int        `json:"groupHidden"`         // Whether the group is hidden: 0 shown, 1 hidden when empty, 2 manually hidden
	GroupSort    int        `json:"groupSort"`           // Group sort value, used for manual sorting
}

// ViewData is used to serialize view data to the frontend.
type ViewData struct {
	ID               string     `json:"id"`
	Icon             string     `json:"icon"`
	Name             string     `json:"name"`
	Desc             string     `json:"desc"`
	HideAttrViewName bool       `json:"hideAttrViewName"`
	Type             LayoutType `json:"type"`
	PageSize         int        `json:"pageSize"`
}

func (view *View) IsGroupView() bool {
	return nil != view.Group && "" != view.Group.Field
}

// GetGroupValue gets the group value of a group view.
func (view *View) GetGroupValue() string {
	if nil == view.GroupVal {
		return ""
	}
	return view.GroupVal.String(false)
}

// GetGroupByID gets the group view with the specified group ID.
func (view *View) GetGroupByID(groupID string) *View {
	if nil == view.Groups {
		return nil
	}
	for _, group := range view.Groups {
		if group.ID == groupID {
			return group
		}
	}
	return nil
}

// GetGroupByGroupValue gets the group view with the specified group value.
func (view *View) GetGroupByGroupValue(groupVal string) *View {
	if nil == view.Groups {
		return nil
	}
	for _, group := range view.Groups {
		if group.GetGroupValue() == groupVal {
			return group
		}
	}
	return nil
}

// RemoveGroupByID removes the group view with the specified ID from the group view list.
func (view *View) RemoveGroupByID(groupID string) {
	if nil == view.Groups {
		return
	}
	for i, group := range view.Groups {
		if group.ID == groupID {
			view.Groups = append(view.Groups[:i], view.Groups[i+1:]...)
			return
		}
	}
}

// GetGroupKey gets the group field of a group view.
func (view *View) GetGroupKey(attrView *AttributeView) (ret *Key) {
	if !view.IsGroupView() {
		return
	}

	for _, kv := range attrView.KeyValues {
		if kv.Key.ID == view.Group.Field {
			ret = kv.Key
			return
		}
	}
	return
}

// GroupCalc describes the structure of the group calculation rule and its result.
type GroupCalc struct {
	Field     string     `json:"field"` // Field ID
	FieldCalc *FieldCalc `json:"calc"`  // Calculation rule and result
}

// LayoutType describes the view layout type.
type LayoutType string

const (
	LayoutTypeTable   LayoutType = "table"   // Attribute view type - table
	LayoutTypeGallery LayoutType = "gallery" // Attribute view type - gallery
	LayoutTypeKanban  LayoutType = "kanban"  // Attribute view type - kanban
)

const (
	ViewDefaultPageSize = 50 // Default page size for views
)

func NewTableView() *View {
	return &View{
		ID:         ast.NewNodeID(),
		Name:       GetAttributeViewI18n("table"),
		Filters:    []*ViewFilter{{Combination: FilterCombinationAnd}},
		Sorts:      []*ViewSort{},
		PageSize:   ViewDefaultPageSize,
		LayoutType: LayoutTypeTable,
		Table:      NewLayoutTable(),
	}
}

func NewTableViewWithBlockKey(blockKeyID string) (view *View, blockKey, selectKey *Key) {
	name := GetAttributeViewI18n("table")
	view = &View{
		ID:         ast.NewNodeID(),
		Name:       name,
		Filters:    []*ViewFilter{{Combination: FilterCombinationAnd}},
		Sorts:      []*ViewSort{},
		LayoutType: LayoutTypeTable,
		Table:      NewLayoutTable(),
		PageSize:   ViewDefaultPageSize,
	}
	blockKey = NewKey(blockKeyID, GetAttributeViewI18n("key"), "", KeyTypeBlock)
	view.Table.Columns = []*ViewTableColumn{{BaseField: &BaseField{ID: blockKeyID}}}

	selectKey = NewKey(ast.NewNodeID(), GetAttributeViewI18n("select"), "", KeyTypeSelect)
	view.Table.Columns = append(view.Table.Columns, &ViewTableColumn{BaseField: &BaseField{ID: selectKey.ID}})
	return
}

func NewGalleryView() (ret *View) {
	return &View{
		ID:         ast.NewNodeID(),
		Name:       GetAttributeViewI18n("gallery"),
		Filters:    []*ViewFilter{{Combination: FilterCombinationAnd}},
		Sorts:      []*ViewSort{},
		PageSize:   ViewDefaultPageSize,
		LayoutType: LayoutTypeGallery,
		Gallery:    NewLayoutGallery(),
	}
}

func NewKanbanView() (ret *View) {
	return &View{
		ID:         ast.NewNodeID(),
		Name:       GetAttributeViewI18n("kanban"),
		Filters:    []*ViewFilter{{Combination: FilterCombinationAnd}},
		Sorts:      []*ViewSort{},
		PageSize:   ViewDefaultPageSize,
		LayoutType: LayoutTypeKanban,
		Kanban:     NewLayoutKanban(),
	}
}

// Viewable describes the interface of a view.
type Viewable interface {

	// GetType gets the layout type of the view.
	GetType() LayoutType

	// GetID gets the ID of the view.
	GetID() string

	// SetGroups sets the group list of the view.
	SetGroups(viewables []Viewable)

	// SetGroupCalc sets the group calculation rule and result of the view.
	SetGroupCalc(group *GroupCalc)

	// GetGroupCalc gets the group calculation rule and result of the view.
	GetGroupCalc() *GroupCalc

	// SetGroupFolded sets whether the group is folded.
	SetGroupFolded(folded bool)

	// GetGroupHidden gets whether the group is hidden.
	// hidden: 0 shown, 1 hidden when empty, 2 manually hidden
	GetGroupHidden() int

	// SetGroupHidden sets whether the group is hidden.
	// hidden: 0 shown, 1 hidden when empty, 2 manually hidden
	SetGroupHidden(hidden int)
}

func NewAttributeView(id string) (ret *AttributeView) {
	view, blockKey, selectKey := NewTableViewWithBlockKey(ast.NewNodeID())
	ret = &AttributeView{
		Spec:              CurrentSpec,
		ID:                id,
		KeyValues:         []*KeyValues{{Key: blockKey}, {Key: selectKey}},
		ViewID:            view.ID,
		Views:             []*View{view},
		RenderedViewables: map[string]Viewable{},
	}
	return
}

func GetAttributeViewName(avID string) (ret string, err error) {
	// Look up the real path of the AV definition via fallback (global for normal boxes, notebook-level for
	// encrypted notebooks)
	avJSONPath, boxID := FindAttributeViewPath(avID)
	if avJSONPath == "" {
		avJSONPath = GetAttributeViewDataPath(avID)
		boxID = ""
	}
	if !filelock.IsExist(avJSONPath) {
		return
	}

	return getAttributeViewNameByPathInBox(avJSONPath, boxID)
}

func getAttributeViewNameByPathInBox(avJSONPath, boxID string) (ret string, err error) {
	data, err := filelock.ReadFile(avJSONPath)
	if err != nil {
		logging.LogErrorf("read attribute view [%s] failed: %s", avJSONPath, err)
		return
	}
	if boxID != "" {
		avID := strings.TrimSuffix(filepath.Base(avJSONPath), filepath.Ext(avJSONPath))
		plain, decErr := decryptAVData(boxID, avID, data)
		if decErr != nil {
			logging.LogErrorf("decrypt attribute view [%s] failed: %s", avJSONPath, decErr)
			return "", decErr
		}
		data = plain
	}

	val := jsoniter.Get(data, "name")
	if nil == val || val.ValueType() == jsoniter.InvalidValue {
		return
	}
	ret = val.ToString()
	return
}

// GetAttributeViewNameByPath reads the AV name from the given path (unencrypted, compatibility entry point for
// normal boxes).
func GetAttributeViewNameByPath(avJSONPath string) (ret string, err error) {
	return getAttributeViewNameByPathInBox(avJSONPath, "")
}

// GetAttributeViewNameInBox gets the database name within the specified notebook.
func GetAttributeViewNameInBox(avID, boxID string) (ret string, err error) {
	avJSONPath, _ := FindAttributeViewPathInBox(avID, boxID)
	if avJSONPath == "" {
		return
	}
	return getAttributeViewNameByPathInBox(avJSONPath, boxID)
}

func GetAttributeViewContent(avID string) (content string) {
	if "" == avID {
		return
	}

	attrView, err := ParseAttributeView(avID)
	if err != nil {
		logging.LogErrorf("parse attribute view [%s] failed: %s", avID, err)
		return
	}
	return getAttributeViewContent0(attrView)
}

func GetAttributeViewContentByPath(avJSONPath string) (content string) {
	attrView, err := ParseAttributeViewByPath(avJSONPath)
	if err != nil {
		logging.LogErrorf("parse attribute view [%s] failed: %s", avJSONPath, err)
		return
	}
	return getAttributeViewContent0(attrView)
}

func getAttributeViewContent0(attrView *AttributeView) (content string) {
	buf := bytes.Buffer{}
	buf.WriteString(attrView.Name)
	buf.WriteByte(' ')
	for _, v := range attrView.Views {
		buf.WriteString(v.Name)
		buf.WriteByte(' ')
	}

	for _, keyValues := range attrView.KeyValues {
		buf.WriteString(keyValues.Key.Name)
		buf.WriteByte(' ')
		for _, value := range keyValues.Values {
			if nil != value {
				buf.WriteString(value.String(true))
				buf.WriteByte(' ')
			}
		}
	}

	content = strings.TrimSpace(buf.String())
	return
}

func IsAttributeViewExist(avID string) bool {
	// Look up via fallback (global for normal boxes, notebook-level for encrypted notebooks)
	avJSONPath, _ := FindAttributeViewPath(avID)
	if avJSONPath == "" {
		avJSONPath = GetAttributeViewDataPath(avID)
	}
	return filelock.IsExist(avJSONPath)
}

func ParseAttributeView(avID string) (ret *AttributeView, err error) {
	if !ast.IsNodeIDPattern(avID) {
		err = ErrInvalidAttributeViewID
		return
	}

	// AV definitions in encrypted notebooks are stored at a notebook-level path; look it up automatically via
	// fallback and decrypt it
	avJSONPath, boxID := FindAttributeViewPath(avID)
	if avJSONPath == "" {
		// File does not exist, possibly a first-time creation; return the global path (handled by the caller)
		avJSONPath = GetAttributeViewDataPath(avID)
		return parseAttributeViewByPathInBox(avJSONPath, "")
	}
	if boxID != "" {
		SetAVBoxID(avID, boxID)
	}
	return parseAttributeViewByPathInBox(avJSONPath, boxID)
}

func ParseAttributeViewInBox(avID, boxID string) (ret *AttributeView, err error) {
	if !ast.IsNodeIDPattern(avID) {
		err = ErrInvalidAttributeViewID
		return
	}
	if boxID != "" && !ast.IsNodeIDPattern(boxID) {
		err = ErrInvalidBoxID
		return
	}

	avJSONPath, avBoxID := FindAttributeViewPathInBox(avID, boxID)
	if avJSONPath == "" {
		avJSONPath = attributeViewDataPathByBox(avID, boxID)
		avBoxID = boxID
	} else {
		// Only set the mapping when the file actually exists within this box, to avoid a wrong boxID polluting
		// subsequent routing
		if boxID != "" {
			SetAVBoxID(avID, boxID)
		}
	}
	return parseAttributeViewByPathInBox(avJSONPath, avBoxID)
}

func ParseAttributeViewByPath(avJSONPath string) (ret *AttributeView, err error) {
	return parseAttributeViewByPathInBox(avJSONPath, avBoxIDFromPath(avJSONPath))
}

func parseAttributeViewByPathInBox(avJSONPath, boxID string) (ret *AttributeView, err error) {
	if !filelock.IsExist(avJSONPath) {
		err = ErrViewNotFound
		return
	}

	avID := filepath.Base(avJSONPath)
	avID = strings.TrimSuffix(avID, filepath.Ext(avID))

	var data []byte
	if cached, ok := cache.GetAVDataInBox(avID, boxID); ok {
		data = cached
	} else {
		var readErr error
		data, readErr = filelock.ReadFile(avJSONPath)
		if nil != readErr {
			logging.LogErrorf("read attribute view [%s] failed: %s", avID, readErr)
			return
		}
		// AV definitions in encrypted notebooks are stored as ciphertext; look up the boxID from the path and
		// decrypt it
		if boxID != "" {
			data, readErr = decryptAVData(boxID, avID, data)
			if readErr != nil {
				logging.LogErrorf("decrypt attribute view [%s] failed: %s", avID, readErr)
				return
			}
		} else if util.IsCiphertext(data) {
			// Ciphertext was read from a global path where the boxID/DEK cannot be obtained (e.g. legacy data):
			// it cannot be decrypted, so return empty content instead of erroring out on JSON parsing.
			// This can happen when an encrypted notebook's AV ends up at the global location due to a path
			// migration (sync, import, legacy layout).
			return
		}
		cache.SetAVDataInBox(avID, boxID, data)
	}

	ret = &AttributeView{RenderedViewables: map[string]Viewable{}}
	if err = json.Unmarshal(data, ret); err != nil {
		if strings.Contains(err.Error(), ".relation.contents of type av.Value") {
			mapAv := map[string]any{}
			if err = json.Unmarshal(data, &mapAv); err != nil {
				logging.LogErrorf("unmarshal attribute view [%s] failed: %s", avID, err)
				return
			}

			// v3.0.3 compatibility with older versions: convert relation.contents[""] to null
			keyValues := mapAv["keyValues"]
			keyValuesMap := keyValues.([]any)
			for _, kv := range keyValuesMap {
				kvMap := kv.(map[string]any)
				if values := kvMap["values"]; nil != values {
					valuesMap := values.([]any)
					for _, v := range valuesMap {
						if vMap := v.(map[string]any); nil != vMap["relation"] {
							vMap["relation"].(map[string]any)["contents"] = nil
						}
					}
				}
			}

			views := mapAv["views"]
			viewsMap := views.([]any)
			for _, view := range viewsMap {
				if table := view.(map[string]any)["table"]; nil != table {
					tableMap := table.(map[string]any)
					if filters := tableMap["filters"]; nil != filters {
						filtersMap := filters.([]any)
						for _, f := range filtersMap {
							if fMap := f.(map[string]any); nil != fMap["value"] {
								if valueMap := fMap["value"].(map[string]any); nil != valueMap["relation"] {
									valueMap["relation"].(map[string]any)["contents"] = nil
								}
							}
						}
					}
				}
			}

			data, err = json.Marshal(mapAv)
			if err != nil {
				logging.LogErrorf("marshal attribute view [%s] failed: %s", avID, err)
				return
			}

			if err = json.Unmarshal(data, ret); err != nil {
				logging.LogErrorf("unmarshal attribute view [%s] failed: %s", avID, err)
				return
			}
		} else {
			logging.LogErrorf("unmarshal attribute view [%s] failed: %s", avID, err)
			return
		}
	}
	if nil == err {
		err = CheckSpec(ret)
	}
	return
}

func SaveAttributeView(av *AttributeView) (err error) {
	if !ast.IsNodeIDPattern(av.ID) {
		err = ErrInvalidAttributeViewID
		logging.LogErrorf("save attribute view failed: %s", err)
		return
	}

	// Perform some data compatibility and correction processing
	UpgradeSpec(av)

	// Deduplicate values
	blockValues := av.GetBlockKeyValues()
	if nil != blockValues {
		blockIDs := map[string]bool{}
		var duplicatedValueIDs []string
		for _, blockValue := range blockValues.Values {
			if !blockIDs[blockValue.BlockID] {
				blockIDs[blockValue.BlockID] = true
			} else {
				duplicatedValueIDs = append(duplicatedValueIDs, blockValue.ID)
			}
		}
		var tmp []*Value
		for _, blockValue := range blockValues.Values {
			if !gulu.Str.Contains(blockValue.ID, duplicatedValueIDs) {
				tmp = append(tmp, blockValue)
			}
		}
		blockValues.Values = tmp
	}

	// Deduplicate view values
	for _, view := range av.Views {
		// Deduplicate the item custom sort order
		view.ItemIDs = gulu.Str.RemoveDuplicatedElem(view.ItemIDs)

		// Page size
		if 1 > view.PageSize {
			view.PageSize = ViewDefaultPageSize
		}
	}

	// Clean up rendered auto-fill values
	for _, kv := range av.KeyValues {
		for i := len(kv.Values) - 1; i >= 0; i-- {
			if kv.Values[i].IsRenderAutoFill {
				kv.Values = append(kv.Values[:i], kv.Values[i+1:]...)
			}
		}
	}

	var data []byte
	if util.UseSingleLineSave {
		data, err = gulu.JSON.MarshalJSON(av)
	} else {
		data, err = gulu.JSON.MarshalIndentJSON(av, "", "\t")
	}
	if err != nil {
		logging.LogErrorf("marshal attribute view [%s] failed: %s", av.ID, err)
		return
	}

	// Skip writing to disk when the cache already matches the data to be written; on a cache miss, read the disk
	// data and compare, to avoid redundant writes when nothing changed
	// Look up the actual path of the AV definition via fallback (global for normal boxes, notebook-level for
	// encrypted notebooks)
	avJSONPath, avBoxID := FindAttributeViewPath(av.ID)
	if avJSONPath == "" {
		// File does not exist (first-time creation); use the global path, with an empty boxID (normal box)
		// For encrypted notebooks, the first-time creation path is preset by the handler layer via SetAVBoxID
		avJSONPath = GetAttributeViewDataPath(av.ID)
	}
	if cachedData, ok := cache.GetAVDataInBox(av.ID, avBoxID); ok {
		if len(cachedData) == len(data) && bytes.Equal(cachedData, data) {
			return
		}
	} else {
		if diskData, readErr := filelock.ReadFile(avJSONPath); nil == readErr {
			// The on-disk data for encrypted notebooks is ciphertext; decrypt it before comparing
			if avBoxID != "" {
				diskData, _ = decryptAVData(avBoxID, av.ID, diskData)
			}
			if len(diskData) == len(data) && bytes.Equal(diskData, data) {
				cache.SetAVDataInBox(av.ID, avBoxID, data)
				return
			}
		}
	}

	// Data for encrypted notebooks must be encrypted before being written to disk
	writeData := data
	if avBoxID != "" {
		writeData, err = encryptAVData(avBoxID, av.ID, data)
		if err != nil {
			logging.LogErrorf("encrypt attribute view [%s] failed: %s", av.ID, err)
			return
		}
	}
	// Make sure the directory exists (the notebook-level AV directory for an encrypted notebook may not exist yet)
	if err = os.MkdirAll(filepath.Dir(avJSONPath), 0755); nil != err {
		logging.LogErrorf("create attribute view dir [%s] failed: %s", filepath.Dir(avJSONPath), err)
		return
	}
	if err = util.WriteFileByMmap(avJSONPath, writeData); nil != err {
		if err = filelock.WriteFile(avJSONPath, writeData); nil != err {
			logging.LogErrorf("save attribute view [%s] failed: %s", av.ID, err)
			return
		}
	}

	cache.SetAVDataInBox(av.ID, avBoxID, data)

	if util.ExceedLargeFileWarningSize(len(data)) {
		msg := fmt.Sprintf(util.Langs[util.Lang][268], av.Name+" "+filepath.Base(avJSONPath), util.LargeFileWarningSize)
		util.PushErrMsg(msg, 7000)
	}
	return
}

func (av *AttributeView) GetView(viewID string) (ret *View) {
	for _, v := range av.Views {
		if v.ID == viewID {
			ret = v
			return
		}
	}
	return
}

func (av *AttributeView) GetCurrentView(viewID string) (ret *View, err error) {
	if "" != viewID {
		ret = av.GetView(viewID)
		if nil != ret {
			return
		}
	}

	for _, v := range av.Views {
		if v.ID == av.ViewID {
			ret = v
			return
		}
	}

	if 1 > len(av.Views) {
		err = ErrViewNotFound
		return
	}
	ret = av.Views[0]
	return
}

func (av *AttributeView) ExistBoundBlock(nodeID string) bool {
	for _, blockVal := range av.GetBlockKeyValues().Values {
		if blockVal.Block.ID == nodeID {
			return true
		}
	}
	return false
}

func (av *AttributeView) GetBlockValueByBoundID(nodeID string) *Value {
	for _, kv := range av.KeyValues {
		if KeyTypeBlock == kv.Key.Type {
			for _, v := range kv.Values {
				if v.Block.ID == nodeID {
					return v
				}
			}
		}
	}
	return nil
}

func (av *AttributeView) GetValue(keyID, itemID string) (ret *Value) {
	for _, kv := range av.KeyValues {
		if kv.Key.ID == keyID {
			for _, v := range kv.Values {
				if v.BlockID == itemID {
					ret = v
					return
				}
			}
		}
	}
	return
}

func (av *AttributeView) GetKey(keyID string) (ret *Key, err error) {
	for _, kv := range av.KeyValues {
		if kv.Key.ID == keyID {
			ret = kv.Key
			return
		}
	}
	err = ErrKeyNotFound
	return
}

func (av *AttributeView) GetBlockKeyValues() (ret *KeyValues) {
	for _, kv := range av.KeyValues {
		if KeyTypeBlock == kv.Key.Type {
			ret = kv
			return
		}
	}
	return
}

func (av *AttributeView) GetBlockValue(itemID string) (ret *Value) {
	for _, kv := range av.KeyValues {
		if KeyTypeBlock == kv.Key.Type && 0 < len(kv.Values) {
			for _, v := range kv.Values {
				if v.BlockID == itemID {
					ret = v
					return
				}
			}
		}
	}
	return
}

func (av *AttributeView) GetKeyValues(keyID string) (ret *KeyValues, err error) {
	for _, kv := range av.KeyValues {
		if kv.Key.ID == keyID {
			ret = kv
			return
		}
	}
	err = ErrKeyNotFound
	return
}

func (av *AttributeView) GetBlockKey() (ret *Key) {
	for _, kv := range av.KeyValues {
		if KeyTypeBlock == kv.Key.Type {
			ret = kv.Key
			return
		}
	}
	return
}

func (av *AttributeView) Clone() (ret *AttributeView) {
	ret = &AttributeView{}
	data, err := gulu.JSON.MarshalJSON(av)
	if err != nil {
		logging.LogErrorf("marshal attribute view [%s] failed: %s", av.ID, err)
		return nil
	}
	if err = gulu.JSON.UnmarshalJSON(data, ret); err != nil {
		logging.LogErrorf("unmarshal attribute view [%s] failed: %s", av.ID, err)
		return nil
	}

	ret.ID = ast.NewNodeID()
	templateIDMap := map[string]string{}
	for _, itemTemplate := range ret.NewItemTemplates {
		if nil == itemTemplate {
			continue
		}
		oldID := itemTemplate.ID
		itemTemplate.ID = ast.NewNodeID()
		templateIDMap[oldID] = itemTemplate.ID
	}
	ret.DefaultTemplateID = templateIDMap[ret.DefaultTemplateID]
	if 1 > len(ret.Views) {
		logging.LogErrorf("attribute view [%s] has no views", av.ID)
		return nil
	}

	var oldKeyIDs []string
	keyIDMap := map[string]string{}
	keyTypeMap := map[string]KeyType{}
	for _, kv := range ret.KeyValues {
		newID := ast.NewNodeID()
		keyIDMap[kv.Key.ID] = newID
		keyTypeMap[kv.Key.ID] = kv.Key.Type
		oldKeyIDs = append(oldKeyIDs, kv.Key.ID)
		kv.Key.ID = newID
		kv.Values = []*Value{}

		if KeyTypeRelation == kv.Key.Type {
			// Disconnect the relation
			kv.Key.Relation.IsTwoWay = false
			kv.Key.Relation.AvID = ""
			kv.Key.Relation.BackKeyID = ""
		}
	}

	for _, itemTemplate := range ret.NewItemTemplates {
		if nil == itemTemplate {
			continue
		}
		fieldValues := map[string]*NewItemFieldValue{}
		for oldKeyID, fieldValue := range itemTemplate.FieldValues {
			newKeyID, ok := keyIDMap[oldKeyID]
			if !ok || KeyTypeRelation == keyTypeMap[oldKeyID] {
				continue
			}
			fieldValues[newKeyID] = fieldValue
		}
		if 0 == len(fieldValues) {
			itemTemplate.FieldValues = nil
		} else {
			itemTemplate.FieldValues = fieldValues
		}
	}

	oldKeyIDs = gulu.Str.RemoveDuplicatedElem(oldKeyIDs)
	sorts := map[string]int{}
	for i, k := range ret.KeyIDs {
		sorts[k] = i
	}
	sort.Slice(oldKeyIDs, func(i, j int) bool {
		return sorts[oldKeyIDs[i]] < sorts[oldKeyIDs[j]]
	})

	for _, view := range ret.Views {
		view.ID = ast.NewNodeID()

		remapFilterColumns(view.Filters, keyIDMap)
		for _, s := range view.Sorts {
			s.Column = keyIDMap[s.Column]
		}

		if nil != view.Group {
			view.Group.Field = keyIDMap[view.Group.Field]
		}

		switch view.LayoutType {
		case LayoutTypeTable:
			view.Table.ID = ast.NewNodeID()
			for _, column := range view.Table.Columns {
				column.ID = keyIDMap[column.ID]
			}
		case LayoutTypeGallery:
			view.Gallery.ID = ast.NewNodeID()
			for _, cardField := range view.Gallery.CardFields {
				cardField.ID = keyIDMap[cardField.ID]
			}
		case LayoutTypeKanban:
			view.Kanban.ID = ast.NewNodeID()
			for _, field := range view.Kanban.Fields {
				field.ID = keyIDMap[field.ID]
			}
		}
		view.ItemIDs = []string{}
	}
	ret.ViewID = ret.Views[0].ID

	ret.KeyIDs = nil
	for _, oldKeyID := range oldKeyIDs {
		newKeyID := keyIDMap[oldKeyID]
		ret.KeyIDs = append(ret.KeyIDs, newKeyID)
	}
	return
}

func GetAttributeViewDataPath(avID string) (ret string) {
	if !ast.IsNodeIDPattern(avID) {
		return
	}

	av := filepath.Join(util.DataDir, "storage", "av")
	ret = filepath.Join(av, avID+".json")
	if !gulu.File.IsDir(av) {
		if err := os.MkdirAll(av, 0755); err != nil {
			logging.LogErrorf("create attribute view dir failed: %s", err)
			return
		}
	}
	return
}

func GetAttributeViewI18n(key string) string {
	return util.AttrViewLangs[util.Lang][key].(string)
}

var (
	ErrAttributeViewNotFound  = errors.New("attribute view not found")
	ErrInvalidAttributeViewID = errors.New("invalid attribute view id")
	ErrInvalidBoxID           = errors.New("invalid box id")
	ErrViewNotFound           = errors.New("view not found")
	ErrKeyNotFound            = errors.New("key not found")
	ErrWrongLayoutType        = errors.New("wrong layout type")
	ErrInvalidColumnAlign     = errors.New("invalid column align")
	ErrSpecTooNew             = errors.New("attribute view spec is too new")
	ErrFilterTooDeep          = errors.New("filter nesting depth exceeds the maximum allowed")
)

const (
	NodeAttrNameAvs        = "custom-avs"          // Marks the attribute view(s) a block belongs to, comma-separated av ids
	NodeAttrView           = "custom-sy-av-view"   // Marks the attribute view's view id a block belongs to Database block support specified view https://github.com/siyuan-note/siyuan/issues/10443
	NodeAttrViewStaticText = "custom-sy-av-s-text" // Marks the attribute view's static text a block belongs to Database-bound block primary key supports setting static anchor text https://github.com/siyuan-note/siyuan/issues/10049

	NodeAttrViewNames = "av-names" // Temporarily marks the attribute view name(s) a block belongs to, space-separated
)
