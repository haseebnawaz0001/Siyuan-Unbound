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

// LayoutGallery describes the structure of a gallery layout.
type LayoutGallery struct {
	*BaseLayout

	CoverFrom           CoverFrom       `json:"coverFrom"`                     // Cover source: 0 = none, 1 = content image, 2 = asset field
	CoverFromAssetKeyID string          `json:"coverFromAssetKeyID,omitempty"` // Asset field ID, valid when CoverFrom is 2
	CardAspectRatio     CardAspectRatio `json:"cardAspectRatio"`               // Card aspect ratio
	CardSize            CardSize        `json:"cardSize"`                      // Card size: 0 = small, 1 = medium, 2 = large
	FitImage            bool            `json:"fitImage"`                      // Whether to fit the cover image size
	DisplayFieldName    bool            `json:"displayFieldName"`              // Whether to display the field name

	CardFields []*ViewGalleryCardField `json:"fields"` // Card fields

	// TODO The CardIDs field is deprecated and is planned to be removed after 2026-06-30 https://github.com/siyuan-note/siyuan/issues/15194
	//Deprecated
	CardIDs []string `json:"cardIds"` // Card IDs, used for custom sorting
}

func NewLayoutGallery() *LayoutGallery {
	return &LayoutGallery{
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

type CardAspectRatio int

const (
	CardAspectRatio16_9 CardAspectRatio = iota // 16:9
	CardAspectRatio9_16                        // 9:16
	CardAspectRatio4_3                         // 4:3
	CardAspectRatio3_4                         // 3:4
	CardAspectRatio3_2                         // 3:2
	CardAspectRatio2_3                         // 2:3
	CardAspectRatio1_1                         // 1:1
)

type CardSize int

const (
	CardSizeSmall  CardSize = iota // Small card
	CardSizeMedium                 // Medium card
	CardSizeLarge                  // Large card
)

// CoverFrom describes the enum type of a card's cover source.
type CoverFrom int

const (
	CoverFromNone         CoverFrom = iota // No cover
	CoverFromContentImage                  // Content image
	CoverFromAssetField                    // Asset field
	CoverFromContentBlock                  // Content block
)

// ViewGalleryCardField describes the structure of a card field.
type ViewGalleryCardField struct {
	*BaseField
}

// Gallery describes the structure of a gallery view instance.
type Gallery struct {
	*BaseInstance

	CoverFrom           CoverFrom       `json:"coverFrom"`                     // Cover source
	CoverFromAssetKeyID string          `json:"coverFromAssetKeyID,omitempty"` // Asset field ID, valid when CoverFrom is CoverFromAssetField
	CardAspectRatio     CardAspectRatio `json:"cardAspectRatio"`               // Card aspect ratio
	CardSize            CardSize        `json:"cardSize"`                      // Card size
	FitImage            bool            `json:"fitImage"`                      // Whether to fit the cover image size
	DisplayFieldName    bool            `json:"displayFieldName"`              // Whether to display the field name
	Fields              []*GalleryField `json:"fields"`                        // Card fields
	Cards               []*GalleryCard  `json:"cards"`                         // Cards
	CardCount           int             `json:"cardCount"`                     // Total number of cards
}

// GalleryCard describes the structure of a card instance.
type GalleryCard struct {
	ID     string               `json:"id"`     // Card ID
	Values []*GalleryFieldValue `json:"values"` // Card field values

	CoverURL     string `json:"coverURL"`     // Card cover hyperlink
	CoverContent string `json:"coverContent"` // Card cover text content
}

// GalleryField describes the structure of a card instance field.
type GalleryField struct {
	*BaseInstanceField
}

// GalleryFieldValue describes the structure of a card field instance value.
type GalleryFieldValue struct {
	*BaseValue
}

func (card *GalleryCard) GetID() string {
	return card.ID
}

func (card *GalleryCard) GetBlockValue() (ret *Value) {
	for _, v := range card.Values {
		if KeyTypeBlock == v.ValueType {
			ret = v.Value
			break
		}
	}
	return
}

func (card *GalleryCard) GetValues() (ret []*Value) {
	ret = []*Value{}
	for _, v := range card.Values {
		ret = append(ret, v.Value)
	}
	return
}

func (card *GalleryCard) GetValue(keyID string) (ret *Value) {
	for _, value := range card.Values {
		if nil != value.Value && keyID == value.Value.KeyID {
			ret = value.Value
			break
		}
	}
	return
}

func (gallery *Gallery) GetItems() (ret []Item) {
	ret = []Item{}
	for _, card := range gallery.Cards {
		ret = append(ret, card)
	}
	return
}

func (gallery *Gallery) SetItems(items []Item) {
	gallery.Cards = []*GalleryCard{}
	for _, item := range items {
		gallery.Cards = append(gallery.Cards, item.(*GalleryCard))
	}
}

func (gallery *Gallery) CountItems() int {
	return len(gallery.Cards)
}

func (gallery *Gallery) GetFields() (ret []Field) {
	ret = []Field{}
	for _, field := range gallery.Fields {
		ret = append(ret, field)
	}
	return ret
}

func (gallery *Gallery) GetField(id string) (ret Field, fieldIndex int) {
	for i, field := range gallery.Fields {
		if field.ID == id {
			return field, i
		}
	}
	return nil, -1
}

func (gallery *Gallery) GetValue(itemID, keyID string) (ret *Value) {
	for _, card := range gallery.Cards {
		if card.ID == itemID {
			return card.GetValue(keyID)
		}
	}
	return nil
}

func (gallery *Gallery) GetType() LayoutType {
	return LayoutTypeGallery
}
