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

package util

import (
	"html"
	"strings"

	"github.com/88250/lute"
	"github.com/PuerkitoBio/goquery"
	"github.com/siyuan-note/logging"
)

// MarkdownSettings holds the runtime Markdown configuration.
var MarkdownSettings = &Markdown{
	InlineAsterisk:      true,
	InlineUnderscore:    true,
	InlineSup:           true,
	InlineSub:           true,
	InlineTag:           true,
	InlineMath:          true,
	InlineStrikethrough: true,
	InlineMark:          true,
}

type Markdown struct {
	InlineAsterisk      bool `json:"inlineAsterisk"`      // Whether to enable inline * syntax
	InlineUnderscore    bool `json:"inlineUnderscore"`    // Whether to enable inline _ syntax
	InlineSup           bool `json:"inlineSup"`           // Whether to enable inline superscript
	InlineSub           bool `json:"inlineSub"`           // Whether to enable inline subscript
	InlineTag           bool `json:"inlineTag"`           // Whether to enable inline tags
	InlineMath          bool `json:"inlineMath"`          // Whether to enable inline math formulas
	InlineStrikethrough bool `json:"inlineStrikethrough"` // Whether to enable inline strikethrough
	InlineMark          bool `json:"inlineMark"`          // Whether to enable inline mark
}

func NewLute() (ret *lute.Lute) {
	ret = lute.New()
	ret.SetTextMark(true)
	ret.SetProtyleWYSIWYG(true)
	ret.SetBlockRef(true)
	ret.SetFileAnnotationRef(true)
	ret.SetKramdownIAL(true)
	ret.SetTag(true)
	ret.SetSuperBlock(true)
	ret.SetImgPathAllowSpace(true)
	ret.SetGitConflict(true)
	ret.SetInlineAsterisk(MarkdownSettings.InlineAsterisk)
	ret.SetInlineUnderscore(MarkdownSettings.InlineUnderscore)
	ret.SetSup(MarkdownSettings.InlineSup)
	ret.SetSub(MarkdownSettings.InlineSub)
	ret.SetTag(MarkdownSettings.InlineTag)
	ret.SetInlineMath(MarkdownSettings.InlineMath)
	ret.SetGFMStrikethrough(MarkdownSettings.InlineStrikethrough)
	ret.SetMark(MarkdownSettings.InlineMark)
	ret.SetInlineMathAllowDigitAfterOpenMarker(true)
	ret.SetGFMStrikethrough1(false)
	ret.SetFootnotes(false)
	ret.SetToC(false)
	ret.SetIndentCodeBlock(false)
	ret.SetParagraphBeginningSpace(true)
	ret.SetAutoSpace(false)
	ret.SetHeadingID(false)
	ret.SetSetext(false)
	ret.SetYamlFrontMatter(false)
	ret.SetLinkRef(false)
	ret.SetCodeSyntaxHighlight(false)
	ret.SetSanitize(true)
	ret.SetUnorderedListMarker("-")
	ret.SetCallout(true)
	ret.SetDataTask(true)
	ret.SetArbitraryTaskListItemMarker(true)
	ret.SetExportNormalizeTaskListMarker(false) // Only set this to true for the Markdown export scenario
	ret.SetEnsureListItemParagraph(true)        // Add an empty paragraph before creating a sublist under an empty list item
	return
}

func NewStdLute() (ret *lute.Lute) {
	ret = lute.New()
	ret.SetFootnotes(false)
	ret.SetToC(false)
	ret.SetIndentCodeBlock(true) // Support indented code block syntax when importing Markdown https://github.com/siyuan-note/siyuan/issues/14429
	ret.SetAutoSpace(false)
	ret.SetHeadingID(false)
	ret.SetSetext(false)
	ret.SetYamlFrontMatter(false)
	ret.SetLinkRef(false)
	ret.SetGFMAutoLink(false) // Do not automatically convert hyperlinks when importing Markdown https://github.com/siyuan-note/siyuan/issues/7682
	ret.SetImgPathAllowSpace(true)
	ret.SetInlineMathAllowDigitAfterOpenMarker(true) // Formula parsing supports $ followed by numbers when importing Markdown https://github.com/siyuan-note/siyuan/issues/8362

	// Follow editor Markdown syntax settings when importing Markdown https://github.com/siyuan-note/siyuan/issues/14731
	ret.SetInlineAsterisk(MarkdownSettings.InlineAsterisk)
	ret.SetInlineUnderscore(MarkdownSettings.InlineUnderscore)
	ret.SetSup(MarkdownSettings.InlineSup)
	ret.SetSub(MarkdownSettings.InlineSub)
	ret.SetTag(MarkdownSettings.InlineTag)
	ret.SetInlineMath(MarkdownSettings.InlineMath)
	ret.SetGFMStrikethrough(MarkdownSettings.InlineStrikethrough)
	ret.SetGFMStrikethrough1(false)
	return
}

func ConvertIframeToLink(htmlStr string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		logging.LogErrorf("parse HTML for iframe conversion failed: %s", err)
		return htmlStr
	}

	doc.Find("iframe").Each(func(i int, s *goquery.Selection) {
		if src, exists := s.Attr("src"); exists && strings.TrimSpace(src) != "" {
			escapedSrc := html.EscapeString(src)
			s.AfterHtml(`<a href="` + escapedSrc + `" target="_blank">` + escapedSrc + `</a>`)
		}
		s.Remove()
	})

	ret, _ := doc.Find("body").Html()
	return ret
}

func LinkTarget(htmlStr, linkBase string) (ret string) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		logging.LogErrorf("parse HTML failed: %s", err)
		return
	}

	doc.Find("a").Each(func(i int, selection *goquery.Selection) {
		if href, ok := selection.Attr("href"); ok {
			if IsRelativePath(href) {
				selection.SetAttr("href", linkBase+href)
			}

			// The hyperlink in the marketplace package README fails to jump to the browser to open https://github.com/siyuan-note/siyuan/issues/8452
			selection.SetAttr("target", "_blank")
		}
	})

	ret, _ = doc.Find("body").Html()
	return
}
