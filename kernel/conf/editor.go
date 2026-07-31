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

import "github.com/siyuan-note/siyuan/kernel/util"

type Editor struct {
	AllowSVGScript                  bool           `json:"allowSVGScript"`                  // Allow executing scripts embedded in SVG
	AllowHTMLBLockScript            bool           `json:"allowHTMLBLockScript"`            // Allow executing scripts embedded in HTML content
	FontSize                        int            `json:"fontSize"`                        // Font size
	FontSizeScrollZoom              bool           `json:"fontSizeScrollZoom"`              // Whether font size supports scroll-wheel zooming
	FontFamily                      string         `json:"fontFamily"`                      // Font
	FontWeight                      int            `json:"fontWeight"`                      // Font weight
	FontFamilyDisplay               string         `json:"fontFamilyDisplay"`               // Font name shown in the settings panel (corresponds to FontFamily/FontWeight, optional)
	CodeSyntaxHighlightLineNum      bool           `json:"codeSyntaxHighlightLineNum"`      // Whether code blocks show line numbers
	CodeTabSpaces                   int            `json:"codeTabSpaces"`                   // Number of spaces a Tab converts to in code blocks; 0 means no conversion
	CodeLineWrap                    bool           `json:"codeLineWrap"`                    // Whether code blocks auto-wrap lines
	CodeLigatures                   bool           `json:"codeLigatures"`                   // Whether code blocks use ligatures
	DisplayBookmarkIcon             bool           `json:"displayBookmarkIcon"`             // Whether to show the content block corner badge
	DisplayNetImgMark               bool           `json:"displayNetImgMark"`               // Whether to show the network image corner badge
	DatabaseAttrViewMode            int            `json:"databaseAttrViewMode"`            // Default expand state of database attributes, 0: expanded, 1: collapsed
	GenerateHistoryInterval         int            `json:"generateHistoryInterval"`         // History generation interval, in minutes
	HistoryRetentionDays            int            `json:"historyRetentionDays"`            // History retention days
	Emoji                           []string       `json:"emoji"`                           // Frequently used emoji
	VirtualBlockRef                 bool           `json:"virtualBlockRef"`                 // Whether virtual references are enabled
	VirtualBlockRefExclude          string         `json:"virtualBlockRefExclude"`          // Virtual reference keyword exclude list
	VirtualBlockRefInclude          string         `json:"virtualBlockRefInclude"`          // Virtual reference keyword include list
	BlockRefDynamicAnchorTextMaxLen int            `json:"blockRefDynamicAnchorTextMaxLen"` // Max length of a block ref's dynamic anchor text
	PlantUMLServePath               string         `json:"plantUMLServePath"`               // PlantUML serving address
	FullWidth                       bool           `json:"fullWidth"`                       // Whether to use max width
	KaTexMacros                     string         `json:"katexMacros"`                     // KaTeX macro definitions
	ReadOnly                        bool           `json:"readOnly"`                        // Read-only mode
	EmbedBlockBreadcrumb            bool           `json:"embedBlockBreadcrumb"`            // Whether an embed block shows a breadcrumb
	ListLogicalOutdent              bool           `json:"listLogicalOutdent"`              // Logical reverse outdent for lists
	ListItemDotNumberClickFocus     bool           `json:"listItemDotNumberClickFocus"`     // Focus on clicking a list item's marker
	FloatWindowMode                 int            `json:"floatWindowMode"`                 // Float window trigger mode, 0: hover cursor, 1: hold Ctrl and hover, 2: never trigger the float window
	FloatWindowDelay                *int           `json:"floatWindowDelay"`                // Float window hover trigger delay, in milliseconds, default 620, nil means unset
	DynamicLoadBlocks               int            `json:"dynamicLoadBlocks"`               // Number of dynamically loaded blocks, lower bound 48
	Justify                         bool           `json:"justify"`                         // Whether to justify text
	RTL                             bool           `json:"rtl"`                             // Whether to display right-to-left
	Spellcheck                      bool           `json:"spellcheck"`                      // Whether spellcheck is enabled
	SpellcheckLanguages             []string       `json:"spellcheckLanguages"`             // Spellcheck languages
	OnlySearchForDoc                bool           `json:"onlySearchForDoc"`                // Whether [[ only searches document blocks
	BacklinkExpandCount             int            `json:"backlinkExpandCount"`             // Default expand count for backlinks
	BackmentionExpandCount          int            `json:"backmentionExpandCount"`          // Default expand count for backmentions
	BacklinkContainChildren         bool           `json:"backlinkContainChildren"`         // Whether backlinks include child blocks in the calculation
	BacklinkSort                    *int           `json:"backlinkSort"`                    // Backlink sort mode
	BackmentionSort                 *int           `json:"backmentionSort"`                 // Backmention sort mode
	HeadingEmbedMode                int            `json:"headingEmbedMode"`                // Heading embed block mode, 0: show the heading and blocks below it, 1: show only the heading, 2: show only the blocks below the heading
	PasteURLAutoConvert             bool           `json:"pasteURLAutoConvert"`             // Automatically convert a pasted URL into a link
	Markdown                        *util.Markdown `json:"markdown"`                        // Markdown configuration
}

const (
	MinDynamicLoadBlocks = 48
)

func NewEditor() *Editor {
	return &Editor{
		FontSize:                        16,
		FontSizeScrollZoom:              false,
		CodeSyntaxHighlightLineNum:      false,
		CodeTabSpaces:                   0,
		CodeLineWrap:                    false,
		CodeLigatures:                   false,
		DisplayBookmarkIcon:             true,
		DisplayNetImgMark:               true,
		DatabaseAttrViewMode:            0,
		GenerateHistoryInterval:         10,
		HistoryRetentionDays:            30,
		Emoji:                           []string{},
		VirtualBlockRef:                 false,
		BlockRefDynamicAnchorTextMaxLen: 96,
		PlantUMLServePath:               "https://www.plantuml.com/plantuml/svg/~1",
		FullWidth:                       true,
		KaTexMacros:                     "{}",
		ReadOnly:                        false,
		EmbedBlockBreadcrumb:            false,
		ListLogicalOutdent:              false,
		ListItemDotNumberClickFocus:     true,
		FloatWindowMode:                 0,
		FloatWindowDelay:                func() *int { v := 620; return &v }(),
		DynamicLoadBlocks:               192,
		Justify:                         false,
		RTL:                             false,
		Spellcheck:                      false,
		SpellcheckLanguages:             []string{"en-US"},
		BacklinkExpandCount:             8,
		BackmentionExpandCount:          -1,
		BacklinkContainChildren:         true,
		BacklinkSort:                    func() *int { v := util.SortModeUpdatedDESC; return &v }(),
		BackmentionSort:                 func() *int { v := util.SortModeUpdatedDESC; return &v }(),
		HeadingEmbedMode:                0,
		PasteURLAutoConvert:             false,
		Markdown:                        util.MarkdownSettings,
	}
}
