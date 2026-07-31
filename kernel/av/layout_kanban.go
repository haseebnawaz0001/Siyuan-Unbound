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

import (
	"github.com/88250/lute/ast"
)

// LayoutKanban describes the structure of a kanban view.
type LayoutKanban struct {
	*BaseLayout

	CoverFrom           CoverFrom       `json:"coverFrom"`                     // Cover source: 0 = none, 1 = content image, 2 = asset field
	CoverFromAssetKeyID string          `json:"coverFromAssetKeyID,omitempty"` // Asset field ID, valid when CoverFrom is 2
	CardAspectRatio     CardAspectRatio `json:"cardAspectRatio"`               // Card aspect ratio
	CardSize            CardSize        `json:"cardSize"`                      // Card size: 0 = small, 1 = medium, 2 = large
	FitImage            bool            `json:"fitImage"`                      // Whether to fit the cover image size
	DisplayFieldName    bool            `json:"displayFieldName"`              // Whether to display the field name

	FillColBackgroundColor bool `json:"fillColBackgroundColor"` // Whether to fill the column background color

	Fields []*ViewKanbanField `json:"fields"` // Fields
}

func NewLayoutKanban() *LayoutKanban {
	return &LayoutKanban{
		BaseLayout: &BaseLayout{
			Spec:     0,
			ID:       ast.NewNodeID(),
			ShowIcon: true,
		},
		CoverFrom:       CoverFromContentImage,
		CardAspectRatio: CardAspectRatio16_9,
		CardSize:        CardSizeMedium,
	}
}

// ViewKanbanField describes the structure of a kanban field.
type ViewKanbanField struct {
	*BaseField
}

// Kanban describes the structure of a kanban view instance.
type Kanban struct {
	*BaseInstance

	CoverFrom              CoverFrom       `json:"coverFrom"`                     // Cover source
	CoverFromAssetKeyID    string          `json:"coverFromAssetKeyID,omitempty"` // Asset field ID, valid when CoverFrom is CoverFromAssetField
	CardAspectRatio        CardAspectRatio `json:"cardAspectRatio"`               // Card aspect ratio
	CardSize               CardSize        `json:"cardSize"`                      // Card size
	FitImage               bool            `json:"fitImage"`                      // Whether to fit the cover image size
	DisplayFieldName       bool            `json:"displayFieldName"`              // Whether to display the field name
	FillColBackgroundColor bool            `json:"fillColBackgroundColor"`        // Whether to fill the column background color
	Fields                 []*KanbanField  `json:"fields"`                        // Card fields
	Cards                  []*KanbanCard   `json:"cards"`                         // Cards
	CardCount              int             `json:"cardCount"`                     // Total number of cards
}

// KanbanCard describes the structure of a kanban instance card.
type KanbanCard struct {
	ID     string              `json:"id"`     // Card ID
	Values []*KanbanFieldValue `json:"values"` // Card field values

	CoverURL     string `json:"coverURL"`     // Card cover hyperlink
	CoverContent string `json:"coverContent"` // Card cover text content
}

// KanbanField describes the structure of a kanban instance field.
type KanbanField struct {
	*BaseInstanceField
}

// KanbanFieldValue describes the structure of a card field instance value.
type KanbanFieldValue struct {
	*BaseValue
}

func (card *KanbanCard) GetID() string {
	return card.ID
}

func (card *KanbanCard) GetBlockValue() (ret *Value) {
	for _, v := range card.Values {
		if KeyTypeBlock == v.ValueType {
			ret = v.Value
			break
		}
	}
	return
}

func (card *KanbanCard) GetValues() (ret []*Value) {
	ret = []*Value{}
	for _, v := range card.Values {
		ret = append(ret, v.Value)
	}
	return
}

func (card *KanbanCard) GetValue(keyID string) (ret *Value) {
	for _, value := range card.Values {
		if nil != value.Value && keyID == value.Value.KeyID {
			ret = value.Value
			break
		}
	}
	return
}

func (kanban *Kanban) GetItems() (ret []Item) {
	ret = []Item{}
	for _, card := range kanban.Cards {
		ret = append(ret, card)
	}
	return
}

func (kanban *Kanban) SetItems(items []Item) {
	kanban.Cards = []*KanbanCard{}
	for _, item := range items {
		kanban.Cards = append(kanban.Cards, item.(*KanbanCard))
	}
}

func (kanban *Kanban) CountItems() int {
	return len(kanban.Cards)
}

func (kanban *Kanban) GetFields() (ret []Field) {
	ret = []Field{}
	for _, field := range kanban.Fields {
		ret = append(ret, field)
	}
	return ret
}

func (kanban *Kanban) GetField(id string) (ret Field, fieldIndex int) {
	for i, field := range kanban.Fields {
		if field.ID == id {
			return field, i
		}
	}
	return nil, -1
}

func (kanban *Kanban) GetValue(itemID, keyID string) (ret *Value) {
	for _, card := range kanban.Cards {
		if card.ID == itemID {
			return card.GetValue(keyID)
		}
	}
	return nil
}

func (kanban *Kanban) GetType() LayoutType {
	return LayoutTypeKanban
}
