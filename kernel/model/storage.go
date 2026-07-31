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
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/88250/gulu"
	"github.com/88250/lute/parse"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/sql"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

var localStorageLock = sync.Mutex{}

func GetLocalStorage() (ret map[string]any) {
	localStorageLock.Lock()
	defer localStorageLock.Unlock()
	return getLocalStorage()
}

func SetLocalStorage(val map[string]any) (err error) {
	localStorageLock.Lock()
	defer localStorageLock.Unlock()
	return setLocalStorage(val)
}

func SetLocalStorageVals(keyVals map[string]any) (setKeyVals map[string]any, err error) {
	localStorageLock.Lock()
	defer localStorageLock.Unlock()

	setKeyVals = make(map[string]any, len(keyVals))
	localStorage := getLocalStorage()
	for k, v := range keyVals {
		if v == nil {
			err = fmt.Errorf("local storage value for key [%s] must not be empty", k)
			return
		}
		localStorage[k] = v
		setKeyVals[k] = v
	}
	err = setLocalStorage(localStorage)
	return
}

func RemoveLocalStorageVals(keys []string) (err error) {
	localStorageLock.Lock()
	defer localStorageLock.Unlock()

	localStorage := getLocalStorage()
	for _, key := range keys {
		delete(localStorage, key)
	}
	return setLocalStorage(localStorage)
}

func getLocalStorage() (ret map[string]any) {
	// When local.json is corrupted, clear the file to avoid being unable to enter the main interface https://github.com/siyuan-note/siyuan/issues/7911
	ret = map[string]any{}
	lsPath := filepath.Join(util.DataDir, "storage/local.json")
	if !filelock.IsExist(lsPath) {
		return
	}

	data, err := filelock.ReadFile(lsPath)
	if err != nil {
		logging.LogErrorf("read storage [local] failed: %s", err)
		return
	}

	if err = gulu.JSON.UnmarshalJSON(data, &ret); err != nil {
		logging.LogErrorf("unmarshal storage [local] failed: %s", err)
		return
	}
	return
}

func setLocalStorage(val map[string]any) (err error) {
	dirPath := filepath.Join(util.DataDir, "storage")
	if err = os.MkdirAll(dirPath, 0755); err != nil {
		logging.LogErrorf("create storage [local] dir failed: %s", err)
		return
	}

	data, err := gulu.JSON.MarshalIndentJSON(val, "", "  ")
	if err != nil {
		logging.LogErrorf("marshal storage [local] failed: %s", err)
		return
	}

	lsPath := filepath.Join(dirPath, "local.json")
	err = filelock.WriteFile(lsPath, data)
	if err != nil {
		logging.LogErrorf("write storage [local] failed: %s", err)
		return
	}
	return
}

type Criterion struct {
	Name         string                 `json:"name"`
	Sort         int                    `json:"sort"`       // 0: by block type (default), 1: by created time asc, 2: by created time desc, 3: by updated time asc, 4: by updated time desc, 5: by content order (only when grouped by document)
	Group        int                    `json:"group"`      // 0: no grouping, 1: group by document
	HasReplace   bool                   `json:"hasReplace"` // whether there is a replacement
	Method       int                    `json:"method"`     // 0: text, 1: query syntax, 2: SQL, 3: regular expression
	HPath        string                 `json:"hPath"`
	IDPath       []string               `json:"idPath"`
	K            string                 `json:"k"`            // search keyword
	R            string                 `json:"r"`            // replacement keyword
	Types        *CriterionTypes        `json:"types"`        // type filter options
	ReplaceTypes *CriterionReplaceTypes `json:"replaceTypes"` // replace type filter options
}

type CriterionTypes struct {
	MathBlock     bool `json:"mathBlock"`
	Table         bool `json:"table"`
	Blockquote    bool `json:"blockquote"`
	SuperBlock    bool `json:"superBlock"`
	Paragraph     bool `json:"paragraph"`
	Document      bool `json:"document"`
	Heading       bool `json:"heading"`
	List          bool `json:"list"`
	ListItem      bool `json:"listItem"`
	CodeBlock     bool `json:"codeBlock"`
	HtmlBlock     bool `json:"htmlBlock"`
	EmbedBlock    bool `json:"embedBlock"`
	DatabaseBlock bool `json:"databaseBlock"`
	AudioBlock    bool `json:"audioBlock"`
	VideoBlock    bool `json:"videoBlock"`
	IFrameBlock   bool `json:"iframeBlock"`
	WidgetBlock   bool `json:"widgetBlock"`
	Callout       bool `json:"callout"`
}

type CriterionReplaceTypes struct {
	Text              bool `json:"text"`
	ImgText           bool `json:"imgText"`
	ImgTitle          bool `json:"imgTitle"`
	ImgSrc            bool `json:"imgSrc"`
	AText             bool `json:"aText"`
	ATitle            bool `json:"aTitle"`
	AHref             bool `json:"aHref"`
	Code              bool `json:"code"`
	Em                bool `json:"em"`
	Strong            bool `json:"strong"`
	InlineMath        bool `json:"inlineMath"`
	InlineMemo        bool `json:"inlineMemo"`
	BlockRef          bool `json:"blockRef"`
	FileAnnotationRef bool `json:"fileAnnotationRef"`
	Kbd               bool `json:"kbd"`
	Mark              bool `json:"mark"`
	S                 bool `json:"s"`
	Sub               bool `json:"sub"`
	Sup               bool `json:"sup"`
	Tag               bool `json:"tag"`
	U                 bool `json:"u"`
	DocTitle          bool `json:"docTitle"`
	CodeBlock         bool `json:"codeBlock"`
	MathBlock         bool `json:"mathBlock"`
	HtmlBlock         bool `json:"htmlBlock"`
}

var criteriaLock = sync.Mutex{}

func GetCriteria() (ret []*Criterion) {
	criteriaLock.Lock()
	defer criteriaLock.Unlock()
	ret, _ = getCriteria()
	return
}

func SetCriterion(criterion *Criterion) (err error) {
	if "" == criterion.Name {
		return errors.New(Conf.Language(142))
	}

	criteriaLock.Lock()
	defer criteriaLock.Unlock()

	criteria, err := getCriteria()
	if err != nil {
		return
	}

	update := false
	for i, c := range criteria {
		if c.Name == criterion.Name {
			criteria[i] = criterion
			update = true
			break
		}
	}
	if !update {
		criteria = append(criteria, criterion)
	}

	err = setCriteria(criteria)
	return
}

func RemoveCriterion(name string) (err error) {
	criteriaLock.Lock()
	defer criteriaLock.Unlock()

	criteria, err := getCriteria()
	if err != nil {
		return
	}

	for i, c := range criteria {
		if c.Name == name {
			criteria = append(criteria[:i], criteria[i+1:]...)
			break
		}
	}

	err = setCriteria(criteria)
	return
}

func getCriteria() (ret []*Criterion, err error) {
	ret = []*Criterion{}
	dataPath := filepath.Join(util.DataDir, "storage/criteria.json")
	if !filelock.IsExist(dataPath) {
		return
	}

	data, err := filelock.ReadFile(dataPath)
	if err != nil {
		logging.LogErrorf("read storage [criteria] failed: %s", err)
		return
	}

	if err = gulu.JSON.UnmarshalJSON(data, &ret); err != nil {
		logging.LogErrorf("unmarshal storage [criteria] failed: %s", err)
		return
	}
	return
}

func setCriteria(criteria []*Criterion) (err error) {
	dirPath := filepath.Join(util.DataDir, "storage")
	if err = os.MkdirAll(dirPath, 0755); err != nil {
		logging.LogErrorf("create storage [criteria] dir failed: %s", err)
		return
	}

	data, err := gulu.JSON.MarshalIndentJSON(criteria, "", "  ")
	if err != nil {
		logging.LogErrorf("marshal storage [criteria] failed: %s", err)
		return
	}

	lsPath := filepath.Join(dirPath, "criteria.json")
	err = filelock.WriteFile(lsPath, data)
	if err != nil {
		logging.LogErrorf("write storage [criteria] failed: %s", err)
		return
	}
	return
}

type RecentDoc struct {
	RootID   string `json:"rootID"`
	Icon     string `json:"icon,omitempty"`
	Title    string `json:"title,omitempty"`
	ViewedAt int64  `json:"viewedAt,omitempty"` // viewed time field
	ClosedAt int64  `json:"closedAt,omitempty"` // closed time field
	OpenAt   int64  `json:"openAt,omitempty"`   // time the document was first loaded from the file tree into a tab
}

var recentDocLock = sync.Mutex{}

func GetRecentDocs(sortBy string) (ret []*RecentDoc, err error) {
	recentDocLock.Lock()
	defer recentDocLock.Unlock()
	return getRecentDocs(sortBy)
}

// UpdateRecentDocOpenTime updates the document's open time (called only the first time it is loaded from the file tree into a tab).
func UpdateRecentDocOpenTime(rootID string) (err error) {
	recentDocLock.Lock()
	defer recentDocLock.Unlock()

	recentDocs, err := loadRecentDocsRaw()
	if err != nil {
		return
	}

	timeNow := time.Now().Unix()
	// Find the document and update its open time and viewed time
	found := false
	for _, doc := range recentDocs {
		if doc.RootID == rootID {
			doc.OpenAt = timeNow
			doc.ViewedAt = timeNow
			doc.ClosedAt = 0
			found = true
			break
		}
	}

	// If the document does not exist, create a new record
	if !found {
		recentDoc := &RecentDoc{
			RootID:   rootID,
			OpenAt:   timeNow,
			ViewedAt: timeNow,
		}
		recentDocs = append([]*RecentDoc{recentDoc}, recentDocs...)
	}

	err = setRecentDocs(recentDocs)
	return
}

// UpdateRecentDocViewTime updates the document's viewed time.
func UpdateRecentDocViewTime(rootID string) (err error) {
	recentDocLock.Lock()
	defer recentDocLock.Unlock()

	recentDocs, err := loadRecentDocsRaw()
	if err != nil {
		return
	}

	timeNow := time.Now().Unix()
	// Find the document and update its viewed time, keeping the original open time
	found := false
	for _, doc := range recentDocs {
		if doc.RootID == rootID {
			// OpenAt is left unchanged, keeping the original open time
			doc.ViewedAt = timeNow
			doc.ClosedAt = 0
			found = true
			break
		}
	}

	// If the document does not exist, create a new record
	if !found {
		recentDoc := &RecentDoc{
			RootID: rootID,
			// OpenAt is not set for a newly created record, since this is a view, not an open
			ViewedAt: timeNow,
		}
		recentDocs = append([]*RecentDoc{recentDoc}, recentDocs...)
	}

	err = setRecentDocs(recentDocs)
	return
}

// UpdateRecentDocCloseTime updates the document's closed time.
func UpdateRecentDocCloseTime(rootID string) (err error) {
	return BatchUpdateRecentDocCloseTime([]string{rootID})
}

// BatchUpdateRecentDocCloseTime batch-updates the closed time for documents.
func BatchUpdateRecentDocCloseTime(rootIDs []string) (err error) {
	if len(rootIDs) == 0 {
		return
	}

	recentDocLock.Lock()
	defer recentDocLock.Unlock()

	recentDocs, err := loadRecentDocsRaw()
	if err != nil {
		return
	}

	rootIDs = gulu.Str.RemoveDuplicatedElem(rootIDs)
	rootIDsMap := make(map[string]bool, len(rootIDs))
	for _, id := range rootIDs {
		rootIDsMap[id] = true
	}

	closeTime := time.Now().Unix()

	// Update existing documents
	updated := false
	for _, doc := range recentDocs {
		if rootIDsMap[doc.RootID] {
			doc.ClosedAt = closeTime
			updated = true
			delete(rootIDsMap, doc.RootID) // mark as processed
		}
	}

	// Create new records for documents that do not exist
	for rootID := range rootIDsMap {
		tree, loadErr := LoadTreeByBlockID(rootID)
		if loadErr != nil {
			continue
		}

		recentDoc := &RecentDoc{
			RootID:   tree.Root.ID,
			ClosedAt: closeTime, // set closed time
		}

		recentDocs = append([]*RecentDoc{recentDoc}, recentDocs...)
		updated = true
	}

	if updated {
		err = setRecentDocs(recentDocs)
	}
	return
}

func loadRecentDocsRaw() (ret []*RecentDoc, err error) {
	dataPath := filepath.Join(util.DataDir, "storage/recent-doc.json")
	if !filelock.IsExist(dataPath) {
		return
	}

	data, err := filelock.ReadFile(dataPath)
	if err != nil {
		logging.LogErrorf("read storage [recent-doc] failed: %s", err)
		return
	}

	if err = gulu.JSON.UnmarshalJSON(data, &ret); err != nil {
		logging.LogErrorf("unmarshal storage [recent-doc] failed: %s", err)
		if err = setRecentDocs([]*RecentDoc{}); err != nil {
			logging.LogErrorf("reset storage [recent-doc] failed: %s", err)
		}
		ret = []*RecentDoc{}
		return
	}
	return
}

func getRecentDocs(sortBy string) (ret []*RecentDoc, err error) {
	ret = []*RecentDoc{} // initialize as an empty slice to ensure the API always returns non-nil
	recentDocs, err := loadRecentDocsRaw()
	if err != nil {
		return
	}

	IDs := make([]string, 0, len(recentDocs))
	for _, doc := range recentDocs {
		IDs = append(IDs, doc.RootID)
	}
	bts := treenode.GetBlockTrees(IDs)
	mergedDocs := make(map[string]*RecentDoc, len(recentDocs))
	rootIDs := make([]string, 0, len(recentDocs))
	changed := false

	for _, doc := range recentDocs {
		bt := bts[doc.RootID]
		if nil == bt {
			changed = true
			continue
		}
		// The document block may have already been converted into a heading block https://github.com/siyuan-note/siyuan/pull/16727#issuecomment-3810081850
		if doc.RootID != bt.RootID {
			changed = true
			doc.RootID = bt.RootID
		}

		if merged, ok := mergedDocs[bt.RootID]; !ok {
			doc.Title = path.Base(bt.HPath) // Recent docs not updated after renaming https://github.com/siyuan-note/siyuan/issues/7827
			mergedDocs[bt.RootID] = doc
			rootIDs = append(rootIDs, bt.RootID)
		} else {
			// Merge duplicate records
			changed = true
			if doc.ViewedAt > merged.ViewedAt {
				merged.ViewedAt = doc.ViewedAt
			}
			if doc.OpenAt > merged.OpenAt {
				merged.OpenAt = doc.OpenAt
			}
			if doc.ClosedAt > merged.ClosedAt {
				merged.ClosedAt = doc.ClosedAt
			}
		}
	}

	attrs := sql.BatchGetBlockAttrs(rootIDs)
	for rootID, doc := range mergedDocs {
		if ial, ok := attrs[rootID]; ok {
			if icon, ok := ial["icon"]; ok && icon != "" {
				doc.Icon = icon
			}
		}
		ret = append(ret, doc)
	}

	if changed {
		if errSet := setRecentDocs(ret); errSet != nil {
			logging.LogErrorf("update storage [recent-doc] failed in getRecentDocs: %s", errSet)
		}
	}
	if !IsBoxDocEnabled() {
		filtered := make([]*RecentDoc, 0, len(ret))
		for _, doc := range ret {
			bt := bts[doc.RootID]
			if nil == bt || !IsBoxDoc(bt.BoxID, bt.RootID) {
				filtered = append(filtered, doc)
			}
		}
		ret = filtered
	}

	// Sort according to the sort parameter
	switch sortBy {
	case "updated": // sort by updated time
		// Query the database for recently modified documents
		boxDocFilter, boxDocArgs := buildRootIDExclusionFilter(hiddenBoxDocRootIDs())
		var sqlBlocks []*sql.Block
		if "" == boxDocFilter {
			sqlBlocks = sql.SelectBlocksRawStmt("SELECT * FROM blocks WHERE type = 'd' ORDER BY updated DESC", 1, Conf.FileTree.RecentDocsMaxListCount)
		} else {
			stmt := "SELECT * FROM blocks WHERE type = 'd'" + boxDocFilter + " ORDER BY updated DESC" +
				fmt.Sprintf(" LIMIT %d", Conf.FileTree.RecentDocsMaxListCount)
			sqlBlocks = sql.SelectBlocksRawStmtArgs(stmt, boxDocArgs, Conf.FileTree.RecentDocsMaxListCount)
		}
		ret = []*RecentDoc{}
		if 1 > len(sqlBlocks) {
			return
		}

		// Get document tree info
		var rootIDs []string
		for _, sqlBlock := range sqlBlocks {
			rootIDs = append(rootIDs, sqlBlock.ID)
		}
		bts := treenode.GetBlockTrees(rootIDs)

		for _, sqlBlock := range sqlBlocks {
			bt := bts[sqlBlock.ID]
			if nil == bt {
				continue
			}

			// Parse IAL to get the icon
			icon := ""
			if sqlBlock.IAL != "" {
				ialStr := strings.TrimPrefix(sqlBlock.IAL, "{:")
				ialStr = strings.TrimSuffix(ialStr, "}")
				ial := parse.Tokens2IAL([]byte(ialStr))
				for _, kv := range ial {
					if kv[0] == "icon" {
						icon = kv[1]
						break
					}
				}
			}
			// Get the document title
			title := path.Base(bt.HPath)
			doc := &RecentDoc{
				RootID: sqlBlock.ID,
				Icon:   icon,
				Title:  title,
			}
			ret = append(ret, doc)
		}
	case "closedAt": // sort by closed time
		filtered := make([]*RecentDoc, 0, len(ret))
		for _, doc := range ret {
			if doc.ClosedAt > 0 {
				filtered = append(filtered, doc)
			}
		}
		ret = filtered
		if 0 < len(ret) {
			sort.Slice(ret, func(i, j int) bool {
				return ret[i].ClosedAt > ret[j].ClosedAt
			})
		}
	case "openAt": // sort by open time
		filtered := make([]*RecentDoc, 0, len(ret))
		for _, doc := range ret {
			if doc.OpenAt > 0 {
				filtered = append(filtered, doc)
			}
		}
		ret = filtered
		if 0 < len(ret) {
			sort.Slice(ret, func(i, j int) bool {
				return ret[i].OpenAt > ret[j].OpenAt
			})
		}
	case "viewedAt": // sort by viewed time
		fallthrough
	default:
		filtered := make([]*RecentDoc, 0, len(ret))
		for _, doc := range ret {
			if doc.ViewedAt > 0 {
				filtered = append(filtered, doc)
			}
		}
		ret = filtered
		if 0 < len(ret) {
			sort.Slice(ret, func(i, j int) bool {
				return ret[i].ViewedAt > ret[j].ViewedAt
			})
		}
	}
	return
}

// normalizeRecentDocs normalizes the recent documents list: deduplicates, clears Title/Icon, and truncates each
// category to the configured maximum count.
func normalizeRecentDocs(recentDocs []*RecentDoc) []*RecentDoc {
	maxCount := Conf.FileTree.RecentDocsMaxListCount

	// Deduplicate
	seen := make(map[string]struct{}, len(recentDocs))
	deduplicated := make([]*RecentDoc, 0, len(recentDocs))
	for _, doc := range recentDocs {
		if _, ok := seen[doc.RootID]; !ok {
			seen[doc.RootID] = struct{}{}
			deduplicated = append(deduplicated, doc)
		}
	}

	if len(deduplicated) <= maxCount {
		return deduplicated
	}

	// Tally the three record categories separately
	var viewedDocs []*RecentDoc
	var openedDocs []*RecentDoc
	var closedDocs []*RecentDoc

	for _, doc := range deduplicated {
		if doc.ViewedAt > 0 {
			viewedDocs = append(viewedDocs, doc)
		}
		if doc.OpenAt > 0 {
			openedDocs = append(openedDocs, doc)
		}
		if doc.ClosedAt > 0 {
			closedDocs = append(closedDocs, doc)
		}
	}

	// Sort each category by time and truncate to the configured maximum count
	if len(viewedDocs) > maxCount {
		sort.Slice(viewedDocs, func(i, j int) bool {
			return viewedDocs[i].ViewedAt > viewedDocs[j].ViewedAt
		})
		viewedDocs = viewedDocs[:maxCount]
	}
	if len(openedDocs) > maxCount {
		sort.Slice(openedDocs, func(i, j int) bool {
			return openedDocs[i].OpenAt > openedDocs[j].OpenAt
		})
		openedDocs = openedDocs[:maxCount]
	}
	if len(closedDocs) > maxCount {
		sort.Slice(closedDocs, func(i, j int) bool {
			return closedDocs[i].ClosedAt > closedDocs[j].ClosedAt
		})
		closedDocs = closedDocs[:maxCount]
	}

	// Merge the three record categories
	docMap := make(map[string]*RecentDoc, maxCount*2)
	for _, doc := range viewedDocs {
		docMap[doc.RootID] = doc
	}
	for _, doc := range openedDocs {
		if _, ok := docMap[doc.RootID]; !ok {
			docMap[doc.RootID] = doc
		}
	}
	for _, doc := range closedDocs {
		if _, ok := docMap[doc.RootID]; !ok {
			docMap[doc.RootID] = doc
		}
	}

	result := make([]*RecentDoc, 0, len(docMap))
	for _, doc := range docMap {
		result = append(result, doc)
	}

	return result
}

func setRecentDocs(recentDocs []*RecentDoc) (err error) {
	recentDocs = normalizeRecentDocs(recentDocs)

	dirPath := filepath.Join(util.DataDir, "storage")
	if err = os.MkdirAll(dirPath, 0755); err != nil {
		logging.LogErrorf("create storage [recent-doc] dir failed: %s", err)
		return
	}

	data, err := gulu.JSON.MarshalIndentJSON(recentDocs, "", "  ")
	if err != nil {
		logging.LogErrorf("marshal storage [recent-doc] failed: %s", err)
		return
	}

	lsPath := filepath.Join(dirPath, "recent-doc.json")
	err = filelock.WriteFile(lsPath, data)
	if err != nil {
		logging.LogErrorf("write storage [recent-doc] failed: %s", err)
		return
	}
	return
}

var refUsedLock = sync.Mutex{}

// refUsedMaxCount limits the maximum number of recent ref-used records, evicting the oldest ones once exceeded to
// keep the file from growing without bound.
const refUsedMaxCount = 512

// TouchRefUsed refreshes the target block's recent-ref-used time when the user actually inserts a reference. This
// timestamp is independent of the refs table rebuild mechanism and is only written during transaction processing
// (real edits), to keep block ref "recently used" sorting stable.
func TouchRefUsed(defBlockIDs []string) {
	if 1 > len(defBlockIDs) {
		return
	}

	refUsedLock.Lock()
	defer refUsedLock.Unlock()

	used := loadRefUsed()
	now := time.Now().Unix()
	for _, defBlockID := range defBlockIDs {
		used[defBlockID] = now
	}
	if refUsedMaxCount < len(used) {
		// When the limit is exceeded, evict the oldest records by timestamp
		type entry struct {
			id string
			ts int64
		}
		entries := make([]entry, 0, len(used))
		for id, ts := range used {
			entries = append(entries, entry{id, ts})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].ts > entries[j].ts
		})
		used = map[string]int64{}
		for i := 0; i < refUsedMaxCount && i < len(entries); i++ {
			used[entries[i].id] = entries[i].ts
		}
	}
	setRefUsed(used)
}

// GetRefUsed returns the target-block-to-recent-ref-used-timestamp map, used for block ref sorting.
func GetRefUsed() (ret map[string]int64) {
	refUsedLock.Lock()
	defer refUsedLock.Unlock()
	ret = loadRefUsed()
	return
}

func loadRefUsed() (ret map[string]int64) {
	ret = map[string]int64{}
	dataPath := filepath.Join(util.DataDir, "storage/ref-used.json")
	if !filelock.IsExist(dataPath) {
		return
	}

	data, err := filelock.ReadFile(dataPath)
	if err != nil {
		logging.LogErrorf("read storage [ref-used] failed: %s", err)
		return
	}

	if err = gulu.JSON.UnmarshalJSON(data, &ret); err != nil {
		logging.LogErrorf("unmarshal storage [ref-used] failed: %s", err)
		ret = map[string]int64{}
		return
	}
	return
}

func setRefUsed(used map[string]int64) (err error) {
	dirPath := filepath.Join(util.DataDir, "storage")
	if err = os.MkdirAll(dirPath, 0755); err != nil {
		logging.LogErrorf("create storage [ref-used] dir failed: %s", err)
		return
	}

	data, err := gulu.JSON.MarshalIndentJSON(used, "", "  ")
	if err != nil {
		logging.LogErrorf("marshal storage [ref-used] failed: %s", err)
		return
	}

	dataPath := filepath.Join(dirPath, "ref-used.json")
	err = filelock.WriteFile(dataPath, data)
	if err != nil {
		logging.LogErrorf("write storage [ref-used] failed: %s", err)
		return
	}
	return
}

type OutlineDoc struct {
	DocID string         `json:"docID"`
	Data  map[string]any `json:"data"`
}

var outlineStorageLock = sync.Mutex{}

func GetOutlineStorage(docID string) (ret map[string]any, err error) {
	outlineStorageLock.Lock()
	defer outlineStorageLock.Unlock()

	ret = map[string]any{}
	outlineDocs, err := getOutlineDocs()
	if err != nil {
		return
	}

	for _, doc := range outlineDocs {
		if doc.DocID == docID {
			ret = doc.Data
			break
		}
	}
	return
}

func SetOutlineStorage(docID string, val map[string]any) (err error) {
	outlineStorageLock.Lock()
	defer outlineStorageLock.Unlock()

	outlineDoc := &OutlineDoc{
		DocID: docID,
		Data:  val,
	}

	outlineDocs, err := getOutlineDocs()
	if err != nil {
		return
	}

	// If the document already exists, remove the old one first
	for i, doc := range outlineDocs {
		if doc.DocID == docID {
			outlineDocs = append(outlineDocs[:i], outlineDocs[i+1:]...)
			break
		}
	}

	// Add the new document info to the front
	outlineDocs = append([]*OutlineDoc{outlineDoc}, outlineDocs...)

	// Limit to 2000 documents
	if 2000 < len(outlineDocs) {
		outlineDocs = outlineDocs[:2000]
	}

	err = setOutlineDocs(outlineDocs)
	return
}

func RemoveOutlineStorage(docID string) (err error) {
	outlineStorageLock.Lock()
	defer outlineStorageLock.Unlock()

	outlineDocs, err := getOutlineDocs()
	if err != nil {
		return
	}

	for i, doc := range outlineDocs {
		if doc.DocID == docID {
			outlineDocs = append(outlineDocs[:i], outlineDocs[i+1:]...)
			break
		}
	}

	err = setOutlineDocs(outlineDocs)
	return
}

func setOutlineDocs(outlineDocs []*OutlineDoc) (err error) {
	dirPath := filepath.Join(util.DataDir, "storage")
	if err = os.MkdirAll(dirPath, 0755); err != nil {
		logging.LogErrorf("create storage [outline] dir failed: %s", err)
		return
	}

	data, err := gulu.JSON.MarshalJSON(outlineDocs)
	if err != nil {
		logging.LogErrorf("marshal storage [outline] failed: %s", err)
		return
	}

	lsPath := filepath.Join(dirPath, "outline.json")
	err = filelock.WriteFile(lsPath, data)
	if err != nil {
		logging.LogErrorf("write storage [outline] failed: %s", err)
		return
	}
	return
}

func getOutlineDocs() (ret []*OutlineDoc, err error) {
	ret = []*OutlineDoc{}
	dataPath := filepath.Join(util.DataDir, "storage/outline.json")
	if !filelock.IsExist(dataPath) {
		return
	}

	data, err := filelock.ReadFile(dataPath)
	if err != nil {
		logging.LogErrorf("read storage [outline] failed: %s", err)
		return
	}

	if err = gulu.JSON.UnmarshalJSON(data, &ret); err != nil {
		logging.LogErrorf("unmarshal storage [outline] failed: %s", err)
		return
	}
	return
}
