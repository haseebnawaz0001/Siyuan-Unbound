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

package conf

type Export struct {
	ParagraphBeginningSpace bool `json:"paragraphBeginningSpace"` // Whether to indent paragraph beginnings by two spaces, per Chinese typographic convention
	AddTitle                bool `json:"addTitle"`                // Whether to add a title
	// Content block reference export mode
	//   2: anchor text block chain
	//   3: anchor text only
	//   4: convert block ref to footnote + anchor hash
	//  (5: anchor hash https://github.com/siyuan-note/siyuan/issues/10265, already deprecated https://github.com/siyuan-note/siyuan/issues/13331)
	//  (0: use raw text, 1: use Blockquote, both already deprecated https://github.com/siyuan-note/siyuan/issues/3155)
	BlockRefMode          int    `json:"blockRefMode"`
	BlockEmbedMode        int    `json:"blockEmbedMode"`        // Content block embed export mode, 0: use raw text, 1: use Blockquote
	BlockRefTextLeft      string `json:"blockRefTextLeft"`      // Symbol to the left of a content block ref's exported anchor text, empty by default
	BlockRefTextRight     string `json:"blockRefTextRight"`     // Symbol to the right of a content block ref's exported anchor text, empty by default
	TagOpenMarker         string `json:"tagOpenMarker"`         // Tag opening marker, default is #
	TagCloseMarker        string `json:"tagCloseMarker"`        // Tag closing marker, default is #
	FileAnnotationRefMode int    `json:"fileAnnotationRefMode"` // File annotation ref export mode, 0: file name - page number - anchor text, 1: anchor text only
	PandocBin             string `json:"pandocBin"`             // Pandoc executable path
	PandocParams          string `json:"pandocParams"`          // Extra Pandoc parameters
	DocxTemplate          string `json:"docxTemplate"`          // Template file path used for Docx export TODO deprecated, planned for removal after June 30, 2026 https://github.com/siyuan-note/siyuan/issues/16845
	RemoveAssetsID        bool   `json:"removeAssetsID"`        // Whether to strip the ID portion of asset file names on Markdown export https://github.com/siyuan-note/siyuan/issues/16065
	MarkdownYFM           bool   `json:"markdownYFM"`           // Whether to add YAML Front Matter on Markdown export https://github.com/siyuan-note/siyuan/issues/7727
	InlineMemo            bool   `json:"inlineMemo"`            // Whether to export inline memos https://github.com/siyuan-note/siyuan/issues/14605
	IncludeSubDocs        bool   `json:"includeSubDocs"`        // Whether to export subdocuments https://github.com/siyuan-note/siyuan/issues/13635
	IncludeRelatedDocs    bool   `json:"includeRelatedDocs"`    // Whether to export related documents https://github.com/siyuan-note/siyuan/issues/13635
	PDFFooter             string `json:"pdfFooter"`             // Footer content for PDF export
	PDFWatermarkStr       string `json:"pdfWatermarkStr"`       // Watermark text or watermark file path for PDF export
	PDFWatermarkDesc      string `json:"pdfWatermarkDesc"`      // Watermark position, size, style, etc for PDF export
	ImageWatermarkStr     string `json:"imageWatermarkStr"`     // Watermark text or watermark file path for image export
	ImageWatermarkDesc    string `json:"imageWatermarkDesc"`    // Watermark position, size, style, etc for image export
}

func NewExport() *Export {
	return &Export{
		ParagraphBeginningSpace: false,
		AddTitle:                true,
		BlockRefMode:            4,
		BlockEmbedMode:          1,
		BlockRefTextLeft:        "",
		BlockRefTextRight:       "",
		TagOpenMarker:           "#",
		TagCloseMarker:          "#",
		FileAnnotationRefMode:   0,
		PandocBin:               "",
		RemoveAssetsID:          false,
		MarkdownYFM:             false,
		InlineMemo:              false,
		IncludeSubDocs:          true,
		IncludeRelatedDocs:      false,
		PDFFooter:               "%page / %pages",
	}
}
