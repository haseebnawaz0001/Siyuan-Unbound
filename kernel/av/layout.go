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

package av

// BaseLayout describes the base structure of a layout.
type BaseLayout struct {
	Spec int    `json:"spec"` // Layout format version
	ID   string `json:"id"`   // Layout ID

	ShowIcon  bool `json:"showIcon"`  // Whether to show field icons
	WrapField bool `json:"wrapField"` // Whether to wrap field content

	// TODO The following three fields are deprecated and are planned to be removed after 2026-06-30 https://github.com/siyuan-note/siyuan/issues/15162

	//Deprecated
	Filters []*ViewFilter `json:"filters,omitempty"` // Filter rules
	//Deprecated
	Sorts []*ViewSort `json:"sorts,omitempty"` // Sort rules
	//Deprecated
	PageSize int `json:"pageSize,omitempty"` // Number of items per page
}

// BaseField describes the base structure of a field.
type BaseField struct {
	ID     string     `json:"id"`             // Field ID
	Wrap   bool       `json:"wrap"`           // Whether to wrap
	Hidden bool       `json:"hidden"`         // Whether hidden
	Desc   string     `json:"desc,omitempty"` // Field description
	Calc   *FieldCalc `json:"calc,omitempty"` // Calculation rule
}

// BaseValue describes the base structure of a field value.
type BaseValue struct {
	ID        string  `json:"id"`        // Field value ID
	Value     *Value  `json:"value"`     // Field value
	ValueType KeyType `json:"valueType"` // Field value type
}

// BaseInstance describes the base structure of an instance.
type BaseInstance struct {
	ID               string        `json:"id"`               // ID
	Icon             string        `json:"icon"`             // Icon
	Name             string        `json:"name"`             // Name
	Desc             string        `json:"desc"`             // Description
	HideAttrViewName bool          `json:"hideAttrViewName"` // Whether to hide the attribute view name
	Filters          []*ViewFilter `json:"filters"`          // Filter rules
	Sorts            []*ViewSort   `json:"sorts"`            // Sort rules
	Group            *ViewGroup    `json:"group"`            // Group rule
	PageSize         int           `json:"pageSize"`         // Number of items per page
	ShowIcon         bool          `json:"showIcon"`         // Whether to show field icons
	WrapField        bool          `json:"wrapField"`        // Whether to wrap field content

	GroupKey    *Key       `json:"groupKey,omitempty"`   // Field to group by
	GroupValue  *Value     `json:"groupValue,omitempty"` // Group value
	Groups      []Viewable `json:"groups,omitempty"`     // List of group instances
	GroupCalc   *GroupCalc `json:"groupCalc,omitempty"`  // Group calculation rule and result
	GroupFolded bool       `json:"groupFolded"`          // Whether the group is folded
	GroupHidden int        `json:"groupHidden"`          // Whether the group is hidden: 0 = shown, 1 = hidden when empty, 2 = manually hidden
}

func NewViewBaseInstance(view *View) *BaseInstance {
	showIcon, wrapField := true, false
	switch view.LayoutType {
	case LayoutTypeTable:
		showIcon = view.Table.ShowIcon
		wrapField = view.Table.WrapField
	case LayoutTypeGallery:
		showIcon = view.Gallery.ShowIcon
		wrapField = view.Gallery.WrapField
	case LayoutTypeKanban:
		showIcon = view.Kanban.ShowIcon
		wrapField = view.Kanban.WrapField
	}
	return &BaseInstance{
		ID:               view.ID,
		Icon:             view.Icon,
		Name:             view.Name,
		Desc:             view.Desc,
		HideAttrViewName: view.HideAttrViewName,
		Filters:          view.Filters,
		Sorts:            view.Sorts,
		Group:            view.Group,
		GroupKey:         view.GroupKey,
		GroupValue:       view.GroupVal,
		GroupCalc:        view.GroupCalc,
		GroupFolded:      view.GroupFolded,
		GroupHidden:      view.GroupHidden,
		PageSize:         view.PageSize,
		ShowIcon:         showIcon,
		WrapField:        wrapField,
	}
}

func (baseInstance *BaseInstance) GetSorts() []*ViewSort {
	return baseInstance.Sorts
}

func (baseInstance *BaseInstance) GetFilters() []*ViewFilter {
	return baseInstance.Filters
}

func (baseInstance *BaseInstance) SetGroups(viewables []Viewable) {
	baseInstance.Groups = viewables
}

func (baseInstance *BaseInstance) SetGroupCalc(group *GroupCalc) {
	baseInstance.GroupCalc = group
}

func (baseInstance *BaseInstance) GetGroupCalc() *GroupCalc {
	return baseInstance.GroupCalc
}

func (baseInstance *BaseInstance) SetGroupFolded(folded bool) {
	baseInstance.GroupFolded = folded
}

func (baseInstance *BaseInstance) GetGroupHidden() int {
	return baseInstance.GroupHidden
}

func (baseInstance *BaseInstance) SetGroupHidden(hidden int) {
	baseInstance.GroupHidden = hidden
}

func (baseInstance *BaseInstance) GetID() string {
	return baseInstance.ID
}

// BaseInstanceField describes the base structure of an instance field.
type BaseInstanceField struct {
	ID     string     `json:"id"`     // ID
	Name   string     `json:"name"`   // Name
	Type   KeyType    `json:"type"`   // Type
	Icon   string     `json:"icon"`   // Icon
	Wrap   bool       `json:"wrap"`   // Whether to wrap
	Hidden bool       `json:"hidden"` // Whether hidden
	Desc   string     `json:"desc"`   // Description
	Calc   *FieldCalc `json:"calc"`   // Calculation rule and result

	// The following are attributes specific to certain field types

	Options      []*SelectOption `json:"options,omitempty"`  // Option list
	NumberFormat NumberFormat    `json:"numberFormat"`       // Number field formatting
	Template     string          `json:"template"`           // Template field content
	Relation     *Relation       `json:"relation,omitempty"` // Relation field
	Rollup       *Rollup         `json:"rollup,omitempty"`   // Rollup field
	Date         *Date           `json:"date,omitempty"`     // Date settings
	Created      *Created        `json:"created,omitempty"`  // Created time settings
	Updated      *Updated        `json:"updated,omitempty"`  // Updated time settings
}

func (baseInstanceField *BaseInstanceField) GetID() string {
	return baseInstanceField.ID
}

func (baseInstanceField *BaseInstanceField) GetCalc() *FieldCalc {
	return baseInstanceField.Calc
}

func (baseInstanceField *BaseInstanceField) SetCalc(calc *FieldCalc) {
	baseInstanceField.Calc = calc
}

func (baseInstanceField *BaseInstanceField) GetType() KeyType {
	return baseInstanceField.Type
}

func (baseInstanceField *BaseInstanceField) GetNumberFormat() NumberFormat {
	return baseInstanceField.NumberFormat
}

// Collection describes the interface of a collection.
// A collection can be a table, a set of cards, etc., containing multiple items.
type Collection interface {

	// GetItems returns all items in the collection.
	GetItems() (ret []Item)

	// SetItems sets the items in the collection.
	SetItems(items []Item)

	// CountItems returns the number of items in the collection.
	CountItems() int

	// GetFields returns all fields of the collection.
	GetFields() []Field

	// GetField returns the field with the specified ID.
	GetField(id string) (ret Field, fieldIndex int)

	// GetValue returns the field value for the specified item ID and key ID.
	GetValue(itemID, keyID string) (ret *Value)

	// GetSorts returns the sort rules of the collection.
	GetSorts() []*ViewSort

	// GetFilters returns the filter rules of the collection.
	GetFilters() []*ViewFilter
}

// Field describes the interface of a field.
type Field interface {

	// GetID returns the ID of the field.
	GetID() string

	// GetType returns the type of the field.
	GetType() KeyType

	// GetCalc returns the calculation rule and result of the field.
	GetCalc() *FieldCalc

	// SetCalc sets the calculation rule and result of the field.
	SetCalc(*FieldCalc)

	// GetNumberFormat returns the formatting settings of a number field.
	GetNumberFormat() NumberFormat
}

// Item describes the interface of an item.
// An item can be a table row, a card, etc.
type Item interface {

	// GetBlockValue returns the value of the primary key.
	GetBlockValue() *Value

	// GetValues returns all field values of the item.
	GetValues() []*Value

	// GetValue returns the field value for the specified key ID.
	GetValue(keyID string) (ret *Value)

	// GetID returns the ID of the item.
	GetID() string
}
