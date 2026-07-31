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

package filesys

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/88250/gulu"
	"github.com/88250/lute"
	"github.com/88250/lute/ast"
	"github.com/88250/lute/html"
	"github.com/88250/lute/parse"
	"github.com/88250/lute/render"
	jsoniter "github.com/json-iterator/go"
	"github.com/panjf2000/ants/v2"
	"github.com/siyuan-note/dataparser"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/cache"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func LoadTrees(ids []string) (ret map[string]*parse.Tree) {
	ret = map[string]*parse.Tree{}
	if 1 > len(ids) {
		return ret
	}

	bts := treenode.GetBlockTrees(ids)

	// For ids not found in the global blocktree, search among already-opened encrypted notebooks
	foundSet := map[string]bool{}
	for id := range bts {
		foundSet[id] = true
	}
	var missing []string
	for _, id := range ids {
		if !foundSet[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		for _, encBoxID := range treenode.GetOpenedEncryptedBoxIDs() {
			if len(missing) == 0 {
				break
			}
			encBTs := treenode.GetBlockTreesInBox(missing, encBoxID)
			maps.Copy(bts, encBTs)
			var stillMissing []string
			for _, id := range missing {
				if _, found := encBTs[id]; !found {
					stillMissing = append(stillMissing, id)
				}
			}
			missing = stillMissing
		}
	}

	luteEngine := util.NewLute()
	var boxIDs []string
	var paths []string
	blockIDs := map[string][]string{}
	for _, bt := range bts {
		boxIDs = append(boxIDs, bt.BoxID)
		paths = append(paths, bt.Path)
		if _, ok := blockIDs[bt.RootID]; !ok {
			blockIDs[bt.RootID] = []string{}
		}
		blockIDs[bt.RootID] = append(blockIDs[bt.RootID], bt.ID)
	}

	trees, errs := batchLoadTrees(boxIDs, paths, luteEngine)
	for i := range trees {
		tree := trees[i]
		err := errs[i]
		if err != nil || tree == nil {
			logging.LogErrorf("load tree failed: %s", err)
			continue
		}

		bIDs := blockIDs[tree.Root.ID]
		for _, bID := range bIDs {
			ret[bID] = tree
		}
	}
	return
}

func batchLoadTrees(boxIDs, paths []string, luteEngine *lute.Lute) (ret []*parse.Tree, errs []error) {
	waitGroup := sync.WaitGroup{}
	lock := sync.Mutex{}
	poolSize := min(runtime.NumCPU(), 8)

	p, _ := ants.NewPoolWithFunc(poolSize, func(arg any) {
		defer waitGroup.Done()

		i := arg.(int)
		boxID := boxIDs[i]
		path := paths[i]
		tree, err := LoadTree(boxID, path, luteEngine)
		lock.Lock()
		ret = append(ret, tree)
		errs = append(errs, err)
		lock.Unlock()
	})
	loaded := map[string]bool{}
	for i := range paths {
		if loaded[boxIDs[i]+paths[i]] {
			continue
		}

		loaded[boxIDs[i]+paths[i]] = true

		waitGroup.Add(1)
		p.Invoke(i)
	}
	waitGroup.Wait()
	p.Release()
	return
}

// ValidateBoxRelativePath validates whether a box-relative path is safe.
// Rejects .. and absolute paths, ensuring the final path lies within <DataDir>/<boxID>.
// A path is allowed to start with / (e.g. /20230101/xxx.sy); it is automatically normalized and the leading
// slash is stripped.
// The root path ("/" or "") is valid and returns an empty string.
func ValidateBoxRelativePath(boxID, p string) (string, error) {
	p = filepath.ToSlash(p)
	// Keep the original path for the IsSubPath check
	origP := p
	// Normalize: strip the leading /
	p = strings.TrimPrefix(p, "/")
	// The root path is allowed through directly (the box root directory itself is a valid path)
	if p == "" {
		return p, nil
	}
	if strings.HasPrefix(p, "..") || strings.Contains(p, "/../") || strings.HasSuffix(p, "/..") || p == ".." || p == "." {
		return "", fmt.Errorf("path [%s] must not contain '..'", origP)
	}
	resolved := filepath.Join(util.DataDir, boxID, origP)
	boxRoot := filepath.Join(util.DataDir, boxID)
	if !gulu.File.IsSubPath(boxRoot, resolved) {
		return "", fmt.Errorf("path [%s] escapes box directory", origP)
	}
	return p, nil
}

func LoadTreeWithFix(boxID, p string, luteEngine *lute.Lute) (ret *parse.Tree, needFix bool, err error) {
	if _, err = ValidateBoxRelativePath(boxID, p); err != nil {
		logging.LogErrorf("invalid tree path [%s] for box [%s]: %s", p, boxID, err)
		return
	}
	rootID := util.GetTreeID(p)
	if raw, ok := cache.GetTreeDataInBox(rootID, boxID); ok {
		ret, err = LoadTreeByData(raw, boxID, p, luteEngine)
		return
	}

	filePath := filepath.Join(util.DataDir, boxID, p)
	data, err := filelock.ReadFile(filePath)
	if nil != err {
		logging.LogErrorf("load tree [%s] failed: %s", p, err)
		return
	}

	// An encrypted notebook's .sy is ciphertext; after reading from disk, decrypt it to plaintext for later
	// parsing; a non-encrypted notebook is returned as-is
	if data, err = decryptData(boxID, p, data); nil != err {
		logging.LogErrorf("decrypt tree [%s] failed: %s", p, err)
		return
	}

	data, needFix, err = fixTreeJSONData(boxID, p, data, luteEngine)
	if nil != err {
		return
	}

	ret, err = LoadTreeByData(data, boxID, p, luteEngine)
	if nil == err {
		cache.SetTreeDataInBox(rootID, boxID, data)
	}
	return
}

func LoadTree(boxID, p string, luteEngine *lute.Lute) (ret *parse.Tree, err error) {
	ret, _, err = LoadTreeWithFix(boxID, p, luteEngine)
	return
}

func LoadTreeByData(data []byte, boxID, p string, luteEngine *lute.Lute) (ret *parse.Tree, err error) {
	ret, err = parseJSON2Tree(boxID, p, data, luteEngine)
	if nil != err {
		logging.LogErrorf("parse tree [%s] failed: %s", p, err)
		return
	}
	ret.Path = p
	ret.Root.Path = p

	hPath := "/" + strings.TrimPrefix(filepath.ToSlash(p), "/")
	parts := strings.Split(hPath, "/")
	if len(parts) < 2 {
		logging.LogErrorf("parse tree [%s] failed: invalid path", p)
		err = errors.New("invalid path")
		return
	}

	parts = parts[1 : len(parts)-1] // strip the leading slash element and the trailing element for itself
	if 1 > len(parts) {
		ret.HPath = "/" + ret.Root.IALAttr("title")
		ret.Hash = treenode.NodeHash(ret.Root, ret, luteEngine)
		return
	}

	// Construct the HPath
	hPathBuilder := bytes.Buffer{}
	hPathBuilder.WriteString("/")
	for i := range parts {
		var parentAbsPath string
		if 0 < i {
			parentAbsPath = strings.Join(parts[:i+1], "/")
		} else {
			parentAbsPath = parts[0]
		}
		parentAbsPath += ".sy"
		parentPath := parentAbsPath
		parentAbsPath = filepath.Join(util.DataDir, boxID, parentAbsPath)

		parentDocIAL := DocIAL(parentAbsPath)
		if 1 > len(parentDocIAL) {
			// Automatically fill in a missing parent document for a child document https://github.com/siyuan-note/siyuan/issues/7376
			parentTree := treenode.NewTree(boxID, parentPath, hPathBuilder.String()+"Untitled", "Untitled")
			if _, writeErr := WriteTree(parentTree); nil != writeErr {
				logging.LogErrorf("rebuild parent tree [%s] failed: %s", parentAbsPath, writeErr)
			} else {
				logging.LogInfof("rebuilt parent tree [%s]", parentAbsPath)
				treenode.UpsertBlockTree(parentTree)
			}
			hPathBuilder.WriteString("Untitled/")
			continue
		}

		title := parentDocIAL["title"]
		if "" == title {
			title = "Untitled"
		}
		hPathBuilder.WriteString(title)
		hPathBuilder.WriteString("/")
	}
	hPathBuilder.WriteString(ret.Root.IALAttr("title"))
	ret.HPath = hPathBuilder.String()
	ret.Hash = treenode.NodeHash(ret.Root, ret, luteEngine)
	return
}

func DocIAL(absPath string) (ret map[string]string) {
	// An encrypted notebook's .sy is ciphertext, which the streaming jsoniter parser cannot handle, so it must
	// be read and decrypted as a whole first.
	// Infer boxID: the path is of the form <DataDir>/<boxID>/...; a non-encrypted notebook uses the original streaming logic.
	boxID := docIALBoxID(absPath)
	if boxID != "" && DEKProvider != nil {
		if dek, err := DEKProvider(boxID); err == nil && dek != nil {
			// An already-unlocked encrypted box: read the whole ciphertext -> decrypt -> streaming parse
			// Note: filelock.ReadFile already locks internally, so do not Lock/Unlock again outside it (would deadlock)
			raw, readErr := filelock.ReadFile(absPath)
			if readErr != nil {
				logging.LogErrorf("read file [%s] failed: %s", absPath, readErr)
				return nil
			}
			relPath := filepath.ToSlash(strings.TrimPrefix(absPath, filepath.Join(util.DataDir, boxID)+string(os.PathSeparator)))
			plain, decErr := decryptData(boxID, relPath, raw)
			if decErr != nil {
				// Decryption failed (possibly a corrupted file or a key mismatch): return an empty map instead
				// of nil, to avoid LoadTreeByData's parent-document completion logic mistaking nil for a
				// "missing document" and creating a document out of thin air
				logging.LogErrorf("decrypt doc [%s] for IAL failed: %s", absPath, decErr)
				return map[string]string{}
			}
			iter := jsoniter.Parse(jsoniter.ConfigCompatibleWithStandardLibrary, bytes.NewReader(plain), 512)
			for field := iter.ReadObject(); field != ""; field = iter.ReadObject() {
				if field == "Properties" {
					iter.ReadVal(&ret)
					break
				} else {
					iter.Skip()
				}
			}
			for k, v := range ret {
				ret[k] = html.UnescapeAttrVal(v)
			}
			return
		}
	}

	filelock.Lock(absPath)
	file, err := os.Open(absPath)
	if err != nil {
		logging.LogErrorf("open file [%s] failed: %s", absPath, err)
		filelock.Unlock(absPath)
		return nil
	}

	iter := jsoniter.Parse(jsoniter.ConfigCompatibleWithStandardLibrary, file, 512)
	for field := iter.ReadObject(); field != ""; field = iter.ReadObject() {
		if field == "Properties" {
			iter.ReadVal(&ret)
			break
		} else {
			iter.Skip()
		}
	}
	file.Close()
	filelock.Unlock(absPath)

	for k, v := range ret {
		ret[k] = html.UnescapeAttrVal(v)
	}
	return
}

func TreeSize(tree *parse.Tree) (size uint64) {
	luteEngine := util.NewLute() // not concerned with the user's custom parse/render options
	renderer := render.NewJSONRenderer(tree, luteEngine.RenderOptions, luteEngine.ParseOptions)
	return uint64(len(renderer.Render()))
}

func WriteTree(tree *parse.Tree) (size uint64, err error) {
	data, filePath, err := prepareWriteTree(tree)
	if err != nil {
		return
	}

	// An encrypted notebook's on-disk content uses the ciphertext encData; the cache and comparisons still use the plaintext data (the cache stores plaintext)
	encData, encErr := encryptData(tree.Box, tree.Path, data)
	if encErr != nil {
		err = encErr
		return
	}

	// Skip writing to disk when the cache matches the data to be written; when there's a cache miss, read from
	// disk and compare instead, to avoid a redundant write when there's no change
	if cachedData, ok := cache.GetTreeDataInBox(tree.ID, tree.Box); ok {
		if len(cachedData) == len(data) && bytes.Equal(cachedData, data) {
			return
		}
	} else {
		// Compare against disk: an encrypted notebook's on-disk data is ciphertext, so it must be decrypted to plaintext before comparing with data
		if diskData, readErr := filelock.ReadFile(filePath); nil == readErr {
			decDisk, decErr := decryptData(tree.Box, tree.Path, diskData)
			if decErr == nil && len(decDisk) == len(data) && bytes.Equal(decDisk, data) {
				cache.SetTreeDataInBox(tree.ID, tree.Box, data)
				return
			}
		}
	}

	if err = util.WriteFileByMmap(filePath, encData); nil != err {
		if err = writeTreeByWriteFile(filePath, encData); nil != err {
			return
		}
	}

	if util.ExceedLargeFileWarningSize(len(data)) {
		msg := fmt.Sprintf(util.Langs[util.Lang][268], tree.Root.IALAttr("title")+" "+filepath.Base(filePath), util.LargeFileWarningSize)
		util.PushErrMsg(msg, 7000)
	}

	cache.SetTreeDataInBox(tree.ID, tree.Box, data)
	afterWriteTree(tree)
	size = uint64(len(data))
	return
}

func writeTreeByWriteFile(filePath string, data []byte) (err error) {
	if err = filelock.WriteFile(filePath, data); err != nil {
		msg := fmt.Sprintf("write data [%s] failed: %s", filePath, err)
		logging.LogErrorf("%s", msg)
		err = errors.New(msg)
		return
	}
	return
}

func prepareWriteTree(tree *parse.Tree) (data []byte, filePath string, err error) {
	luteEngine := util.NewLute() // not concerned with the user's custom parse/render options

	if nil == tree.Root.FirstChild {
		newP := treenode.NewParagraph("")
		tree.Root.AppendChild(newP)
		tree.Root.SetIALAttr("updated", util.TimeFromID(newP.ID))
		treenode.UpsertBlockTree(tree)
	}

	treenode.UpgradeSpec(tree)

	if _, err = ValidateBoxRelativePath(tree.Box, tree.Path); err != nil {
		return
	}

	filePath = filepath.Join(util.DataDir, tree.Box, tree.Path)
	tree.Root.SetIALAttr("type", "doc")
	renderer := render.NewJSONRenderer(tree, luteEngine.RenderOptions, luteEngine.ParseOptions)
	data = renderer.Render()
	data, _ = removeUnescapedUnicodeNull(data)
	if !util.UseSingleLineSave {
		buf := bytes.Buffer{}
		buf.Grow(1024 * 1024 * 2)
		if err = json.Indent(&buf, data, "", "\t"); err != nil {
			logging.LogErrorf("json indent failed: %s", err)
			return
		}
		data = buf.Bytes()
	}

	if err = os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return
	}
	return
}

// removeUnescapedUnicodeNull removes only the unescaped `\u0000` literal sequences.
// Detection method: when a match is found for `\u0000`,
// count the run of consecutive `\` characters before it; if even, treat it as unescaped and remove it.
func removeUnescapedUnicodeNull(data []byte) (ret []byte, needFix bool) {
	patLen := 6 // len(`\u0000`)
	n := len(data)
	if n < patLen {
		return data, false
	}
	if !bytes.Contains(data, []byte(`\u0000`)) {
		return data, false
	}

	dst := make([]byte, 0, n)
	i := 0
	for i < n {
		from := i
		j := bytes.IndexByte(data[i:], '\\')
		if j < 0 {
			dst = append(dst, data[from:]...)
			break
		}
		i += j
		dst = append(dst, data[from:i]...)

		// Quick check whether it could match `\u0000`
		if i+patLen <= n &&
			data[i+1] == 'u' &&
			data[i+2] == '0' &&
			data[i+3] == '0' &&
			data[i+4] == '0' &&
			data[i+5] == '0' {
			// Count the run of consecutive backslashes before the current `\`
			backslashes := 0
			for k := i - 1; k >= 0 && data[k] == '\\'; k-- {
				backslashes++
			}
			// If even, the current `\` is unescaped; skip the whole `\u0000`
			if backslashes%2 == 0 {
				i += patLen
				continue
			}
		}
		// Otherwise keep the current byte
		dst = append(dst, data[i])
		i++
	}
	return dst, len(dst) != n
}

func afterWriteTree(tree *parse.Tree) {
	docIAL := parse.IAL2Map(tree.Root.KramdownIAL)
	cache.PutDocIALInBox(tree.Path, tree.Box, docIAL)
}

// fixTreeJSONData corrects tree JSON data.
func fixTreeJSONData(boxID, p string, jsonData []byte, luteEngine *lute.Lute) (data []byte, needFix bool, err error) {
	jsonData, needFix = removeUnescapedUnicodeNull(jsonData)
	ret, parseNeedFix, err := dataparser.ParseJSON(jsonData, luteEngine.ParseOptions)
	if parseNeedFix {
		needFix = true
	}
	if err != nil {
		logging.LogErrorf("parse json [%s] to tree failed: %s", boxID+p, err)
		err = fmt.Errorf("parse json [%s] to tree failed: %w", boxID+p, err)
		return
	}

	ret.Box = boxID
	ret.Path = p

	if err = treenode.CheckSpec(ret); errors.Is(err, treenode.ErrSpecTooNew) {
		return
	}

	if treenode.UpgradeSpec(ret) {
		needFix = true
	}

	// An issue introduced in v3.5.1 https://github.com/siyuan-note/siyuan/pull/16657 where attribute values were not escaped
	// Fixed in v3.5.2 https://github.com/siyuan-note/siyuan/issues/16686, with correction logic added https://github.com/siyuan-note/siyuan/pull/16712
	// https://github.com/siyuan-note/siyuan/security/advisories/GHSA-ff66-236v-p4fg XSS vulnerability: "title": "&amp;\" onmouseenter=\"require('child_process').exec('calc')"
	if escapeAttributeValues(ret) {
		needFix = true
	}

	if pathID := util.GetTreeID(p); pathID != ret.Root.ID {
		if encryptedBox(boxID) {
			// Encrypted .sy: the basename ID (pathID) must match the decrypted root block ID. A mismatch
			// means the ciphertext was replaced, the filename was tampered with, or AAD authentication was
			// bypassed, and must not be silently corrected -- fail closed, consistent with the encrypted
			// notebook threat model.
			err = fmt.Errorf("encrypted .sy [%s]: base id [%s] != root id [%s]", p, pathID, ret.Root.ID)
			logging.LogErrorf("%s", err)
			return
		}
		needFix = true
		logging.LogInfof("reset tree id from [%s] to [%s]", ret.Root.ID, pathID)
		ret.Root.ID = pathID
		ret.ID = pathID
		ret.Root.SetIALAttr("id", ret.ID)
	}

	if !needFix {
		return jsonData, false, nil
	}

	renderer := render.NewJSONRenderer(ret, luteEngine.RenderOptions, luteEngine.ParseOptions)
	data = renderer.Render()

	if !util.UseSingleLineSave {
		buf := bytes.Buffer{}
		buf.Grow(1024 * 1024 * 2)
		if err = json.Indent(&buf, data, "", "\t"); err != nil {
			return
		}
		data = buf.Bytes()
	}

	filePath := filepath.Join(util.DataDir, ret.Box, ret.Path)
	if err = os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return
	}
	// The corrected data is plaintext; an encrypted notebook must encrypt it before writing to disk
	encData, encErr := encryptData(ret.Box, ret.Path, data)
	if encErr != nil {
		err = encErr
		return
	}
	if err = filelock.WriteFile(filePath, encData); err != nil {
		logging.LogErrorf("write data [%s] failed: %s", filePath, err)
	}
	return
}

func parseJSON2Tree(boxID, p string, jsonData []byte, luteEngine *lute.Lute) (ret *parse.Tree, err error) {
	ret, _, err = dataparser.ParseJSON(jsonData, luteEngine.ParseOptions)
	if err != nil {
		logging.LogErrorf("parse json [%s] to tree failed: %s", boxID+p, err)
		err = fmt.Errorf("parse json [%s] to tree failed: %w", boxID+p, err)
		return
	}

	ret.Box = boxID
	ret.Path = p

	if err = treenode.CheckSpec(ret); errors.Is(err, treenode.ErrSpecTooNew) {
		return
	}
	return
}

// escapeAttributeValues escapes attribute values
func escapeAttributeValues(tree *parse.Tree) (hasEscaped bool) {
	if nil == tree || nil == tree.Root {
		return false
	}

	ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering || !n.IsBlock() || "" == n.ID || 0 == len(n.KramdownIAL) {
			return ast.WalkContinue
		}

		if escaped := escapeNodeAttributeValues(n); escaped {
			hasEscaped = true
		}
		return ast.WalkContinue
	})
	return hasEscaped
}

// escapeNodeAttributeValues escapes a node's attribute values
func escapeNodeAttributeValues(node *ast.Node) (escaped bool) {
	if nil == node || 0 == len(node.KramdownIAL) {
		return false
	}

	for _, kv := range node.KramdownIAL {
		// If decoding then re-encoding produces a change, it means the value was not properly escaped or
		// contains malicious concatenation, and needs correction
		canonical := html.EscapeAttrVal(html.UnescapeAttrVal(kv[1]))
		if canonical != kv[1] {
			kv[1] = canonical
			escaped = true
		}
	}
	return
}
