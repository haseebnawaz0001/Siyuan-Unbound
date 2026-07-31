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
	"bytes"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/88250/gulu"
	"github.com/88250/lute"
	"github.com/88250/lute/ast"
	"github.com/88250/lute/parse"
	"github.com/gabriel-vasile/mimetype"
	"github.com/gin-gonic/gin"
	"github.com/siyuan-note/httpclient"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/model"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func extensionCopy(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(200, ret)

	form, _ := c.MultipartForm()
	dom := form.Value["dom"][0]
	assets := filepath.Join(util.DataDir, "assets")
	boxID := ""
	if notebookVal := form.Value["notebook"]; 0 < len(notebookVal) {
		nb := notebookVal[0]
		if model.IsEncryptedBox(nb) {
			boxID = nb
			assets = filepath.Join(util.DataDir, nb, "assets")
		} else {
			assets = filepath.Join(util.DataDir, nb, "assets")
			if !gulu.File.IsDir(assets) {
				assets = filepath.Join(util.DataDir, "assets")
			}
		}
	}

	if err := os.MkdirAll(assets, 0755); err != nil {
		logging.LogErrorf("create assets folder [%s] failed: %s", assets, err)
		ret.Msg = err.Error()
		return
	}

	clippingSym := false
	symArticleHref := ""
	hasHref := nil != form.Value["href"]
	isPartClip := nil != form.Value["clipType"] && form.Value["clipType"][0] == "part"
	if hasHref && !isPartClip {
		// When clipping a Liandi post, use the Markdown API's response directly
		// https://ld246.com/article/raw/1724850322251
		symArticleHref = form.Value["href"][0]

		var baseURL, originalPrefix string
		if strings.HasPrefix(symArticleHref, "https://ld246.com/article/") {
			baseURL = "https://ld246.com/article/raw/"
			originalPrefix = "https://ld246.com/article/"
		} else if strings.HasPrefix(symArticleHref, "https://liuyun.io/article/") {
			baseURL = "https://liuyun.io/article/raw/"
			originalPrefix = "https://liuyun.io/article/"
		}

		if "" != baseURL {
			articleID := strings.TrimPrefix(symArticleHref, originalPrefix)
			if idx := strings.IndexAny(articleID, "/?#"); -1 != idx {
				articleID = articleID[:idx]
			}

			symArticleHref = baseURL + articleID
			clippingSym = true
		}
	}

	uploaded := map[string]string{}
	for originalName, file := range form.File {
		// A full-page clip from Liandi/Liuyun uses the server's raw Markdown, and the DOM resource URLs uploaded by
		// the extension are inevitably different from the URLs in the raw Markdown, so the uploaded files can't be
		// matched and referenced; in this path, the kernel handles downloading and localizing resources uniformly
		// based on the "download resources" toggle, so files uploaded by the extension are skipped here
		if clippingSym {
			continue
		}

		oName, err := url.PathUnescape(originalName)
		unescaped := oName

		if err != nil {
			if strings.Contains(originalName, "%u") {
				originalName = strings.ReplaceAll(originalName, "%u", "\\u")
				originalName, err = strconv.Unquote("\"" + originalName + "\"")
				if err != nil {
					continue
				}
				oName, err = url.PathUnescape(originalName)
				if err != nil {
					continue
				}
			} else {
				continue
			}
		}
		if strings.Contains(oName, "%") {
			unescaped, _ := url.PathUnescape(oName)
			if "" != unescaped {
				oName = unescaped
			}
		}

		u, _ := url.Parse(oName)
		if nil == u {
			continue
		}
		if "" == u.Path {
			continue
		}
		fName := path.Base(u.Path)

		f, err := file[0].Open()
		if err != nil {
			ret.Code = -1
			ret.Msg = err.Error()
			break
		}

		data, err := io.ReadAll(f)
		if err != nil {
			ret.Code = -1
			ret.Msg = err.Error()
			break
		}

		fName = util.FilterUploadFileName(fName)
		ext := util.Ext(fName)
		if !util.IsCommonExt(ext) || strings.Contains(ext, "!") {
			// Improve the browser clipping extension's conversion of local image extensions https://github.com/siyuan-note/siyuan/issues/7467 https://github.com/siyuan-note/siyuan/issues/15320
			if mtype := mimetype.Detect(data); nil != mtype {
				ext = mtype.Extension()
				fName += ext
			}
		}
		if "" == ext && bytes.HasPrefix(data, []byte("<svg ")) && bytes.HasSuffix(data, []byte("</svg>")) {
			ext = ".svg"
			fName += ext
		}

		// Always write through storeAssetForBox: for an encrypted box this auto-sanitizes the name, encrypts before writing to disk, and appends ?box=
		storedName, storeErr := model.StoreAssetForBox(boxID, assets, fName, data)
		if storeErr != nil {
			ret.Code = -1
			ret.Msg = storeErr.Error()
			break
		}

		assetURL := "assets/" + storedName
		if boxID != "" {
			assetURL += "?box=" + boxID
		}
		uploaded[unescaped] = assetURL
	}

	luteEngine := util.NewLute()
	luteEngine.SetHTMLTag2TextMark(true)
	var md string
	var withMath bool

	if clippingSym {
		resp, err := httpclient.NewCloudRequest30s().Get(symArticleHref)
		if err != nil {
			logging.LogWarnf("get [%s] failed: %s", symArticleHref, err)
		} else {
			bodyData, readErr := io.ReadAll(resp.Body)
			if nil != readErr {
				ret.Code = -1
				ret.Msg = "read response body failed: " + readErr.Error()
				return
			}

			md = string(bodyData)
			luteEngine.SetIndentCodeBlock(true) // Liandi supports indented code blocks, so this needs to be enabled
			tree := parse.Parse("", []byte(md), luteEngine.ParseOptions)
			tree.Box = boxID
			ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
				if ast.NodeInlineMath == n.Type {
					withMath = true
					return ast.WalkStop
				} else if ast.NodeCodeBlock == n.Type {
					if !n.IsFencedCodeBlock {
						// Convert the indented code block into a fenced code block
						n.IsFencedCodeBlock = true
						n.CodeBlockFenceChar = '`'
						n.PrependChild(&ast.Node{Type: ast.NodeCodeBlockFenceInfoMarker})
						n.PrependChild(&ast.Node{Type: ast.NodeCodeBlockFenceOpenMarker, Tokens: []byte("```"), CodeBlockFenceLen: 3})
						n.LastChild.InsertAfter(&ast.Node{Type: ast.NodeCodeBlockFenceCloseMarker, Tokens: []byte("```"), CodeBlockFenceLen: 3})
						code := n.ChildByType(ast.NodeCodeBlockCode)
						if nil != code {
							code.Tokens = bytes.TrimPrefix(code.Tokens, []byte("    "))
							code.Tokens = bytes.ReplaceAll(code.Tokens, []byte("\n    "), []byte("\n"))
							code.Tokens = bytes.TrimPrefix(code.Tokens, []byte("\t"))
							code.Tokens = bytes.ReplaceAll(code.Tokens, []byte("\n\t"), []byte("\n"))
						}
					}
				}
				return ast.WalkContinue
			})

			// For a full-page clip from Liandi/Liuyun, the DOM resource URLs uploaded by the extension don't match
			// the URLs in the server's raw Markdown, so files uploaded by the extension can't be matched; when the
			// user enables "download resources", the kernel downloads the network resources in the raw Markdown
			// directly to local storage
			if assetsOn := len(form.Value["assets"]) > 0 && "true" == form.Value["assets"][0]; assetsOn {
				model.DownloadNetAssets2LocalAssets(tree, false, symArticleHref, assets)
			}

			md, _ = lute.FormatNodeSync(tree.Root, luteEngine.ParseOptions, luteEngine.RenderOptions)
		}
	}

	var tree *parse.Tree
	if "" == md {
		// Use a regex to strip newlines contained inside <iframe>.*</iframe> tags
		regx, _ := regexp.Compile(`(?i)<iframe[^>]*>([\s\S]*?)<\/iframe>`)
		dom = regx.ReplaceAllStringFunc(dom, func(s string) string {
			s = strings.ReplaceAll(s, "\n", "")
			s = strings.ReplaceAll(s, "\r", "")
			return s
		})

		tree, withMath = model.HTML2Tree(dom, luteEngine, boxID)
	} else {
		tree = parse.Parse("", []byte(md), luteEngine.ParseOptions)
	}

	var unlinks []*ast.Node
	ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.WalkContinue
		}

		if ast.NodeText == n.Type {
			// Strip leading whitespace
			if ast.NodeParagraph == n.Parent.Type && n.Parent.FirstChild == n {
				n.Tokens = bytes.TrimLeft(n.Tokens, " \t\n")
			}
		} else if ast.NodeImage == n.Type {
			if dest := n.ChildByType(ast.NodeLinkDest); nil != dest {
				assetPath := uploaded[string(dest.Tokens)]
				if "" == assetPath {
					assetPath = uploaded[string(dest.Tokens)+"?imageView2/2/interlace/1/format/webp"]
				}
				if "" != assetPath {
					dest.Tokens = []byte(assetPath)
				}

				// Check the alt and title format, and convert to plain text if it isn't already https://github.com/siyuan-note/siyuan/issues/14233
				if linkText := n.ChildByType(ast.NodeLinkText); nil != linkText {
					if inlineTree := parse.Inline("", linkText.Tokens, luteEngine.ParseOptions); nil != inlineTree && nil != inlineTree.Root && nil != inlineTree.Root.FirstChild {
						if fc := inlineTree.Root.FirstChild.FirstChild; nil != fc {
							if ast.NodeText != fc.Type {
								linkText.Tokens = []byte(fc.Text())
							}
						}
					}
				}
				if title := n.ChildByType(ast.NodeLinkTitle); nil != title {
					if inlineTree := parse.Inline("", title.Tokens, luteEngine.ParseOptions); nil != inlineTree && nil != inlineTree.Root && nil != inlineTree.Root.FirstChild {
						if fc := inlineTree.Root.FirstChild.FirstChild; nil != fc {
							if ast.NodeText != fc.Type {
								title.Tokens = []byte(fc.Text())
							}
						}
					}
				}
			}
		}
		return ast.WalkContinue
	})
	for _, unlink := range unlinks {
		unlink.Unlink()
	}

	parse.TextMarks2Inlines(tree) // First convert TextMark into Inlines https://github.com/siyuan-note/siyuan/issues/13056
	parse.NestedInlines2FlattedSpansHybrid(tree, false)

	md, _ = lute.FormatNodeSync(tree.Root, luteEngine.ParseOptions, luteEngine.RenderOptions)
	ret.Data = map[string]any{
		"md":       md,
		"withMath": withMath,
	}
	ret.Msg = model.Conf.Language(72)
}
