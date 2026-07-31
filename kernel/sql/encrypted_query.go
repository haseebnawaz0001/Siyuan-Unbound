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

package sql

import (
	"bytes"
	"database/sql"
	"math"
	"regexp"
	"strings"

	"github.com/siyuan-note/logging"
)

// This file provides box-scoped read queries for encrypted notebooks. Each function takes a boxID and routes to
// either the encrypted db (once open) or the global db.
// Callers (the model layer) switch to these InBox variants in an encrypted-notebook context; global functionality
// continues to use the original functions.

// GetBlockInBox looks up a block by id in the given box's db. An empty boxID queries the global db.
func GetBlockInBox(id, boxID string) (ret *Block) {
	ret = getBlockCacheInBox(id, boxID)
	if nil != ret {
		return
	}
	row := queryRowForBox(boxID, "SELECT * FROM blocks WHERE id = ?", id)
	if row == nil {
		return
	}
	ret = scanBlockRow(row)
	if nil != ret {
		putBlockCache(ret)
	}
	return
}

// GetBlocksInBox batch-looks up blocks by ids in the given box's db.
func GetBlocksInBox(ids []string, boxID string) (ret []*Block) {
	if 1 > len(ids) {
		return
	}

	var notHitIDs []string
	cached := map[string]*Block{}
	for _, id := range ids {
		if block := getBlockCacheInBox(id, boxID); nil != block {
			cached[id] = block
		} else {
			notHitIDs = append(notHitIDs, id)
		}
	}

	if 1 > len(notHitIDs) {
		for _, id := range ids {
			ret = append(ret, cached[id])
		}
		return
	}

	sqlStmt := "SELECT * FROM blocks WHERE id IN (" + strings.Repeat("?,", len(notHitIDs)-1) + "?)"
	args := make([]any, len(notHitIDs))
	for i, id := range notHitIDs {
		args[i] = id
	}
	rows, err := queryForBox(boxID, sqlStmt, args...)
	if err != nil {
		logging.LogErrorf("sql query [%s] failed: %s", sqlStmt, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		if block := scanBlockRows(rows); nil != block {
			cached[block.ID] = block
		}
	}
	for _, id := range ids {
		ret = append(ret, cached[id])
	}
	return
}

// GetRefTextInBox looks up the ref text by defBlockID in the given box's db.
func GetRefTextInBox(defBlockID, boxID string) (ret string) {
	row := queryRowForBox(boxID, "SELECT content FROM blocks WHERE id = ?", defBlockID)
	if row == nil {
		return
	}
	if err := row.Scan(&ret); err != nil {
		if err != sql.ErrNoRows {
			logging.LogErrorf("sql query failed: %s", err)
		}
		ret = ""
	}
	return
}

// QueryRefsByDefIDInBox looks up the ref list by defBlockID in the given box's db.
// When containChildren is true, it recursively queries the defining block and all its child blocks, consistent
// with QueryRefsByDefID.
func QueryRefsByDefIDInBox(defBlockID string, containChildren bool, boxID string) (ret []*Ref) {
	var sqlStmt string
	var args []any
	if containChildren {
		blockIDs := queryBlockChildrenIDsForBox(defBlockID, boxID)
		sqlStmt = "SELECT * FROM refs WHERE def_block_id IN (" + strings.Repeat("?,", len(blockIDs)-1) + "?)"
		args = make([]any, len(blockIDs))
		for i, id := range blockIDs {
			args[i] = id
		}
	} else {
		sqlStmt = "SELECT * FROM refs WHERE def_block_id = ?"
		args = []any{defBlockID}
	}
	rows, err := queryForBox(boxID, sqlStmt, args...)
	if err != nil {
		logging.LogErrorf("sql query [%s] failed: %s", sqlStmt, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var ref Ref
		if err = rows.Scan(&ref.ID, &ref.DefBlockID, &ref.DefBlockParentID, &ref.DefBlockRootID, &ref.DefBlockPath, &ref.BlockID, &ref.RootID, &ref.Box, &ref.Path, &ref.Content, &ref.Markdown, &ref.Type); err != nil {
			logging.LogErrorf("query scan field failed: %s", err)
			return
		}
		ret = append(ret, &ref)
	}
	return
}

// QueryRootChildrenRefCountInBox queries the ref count of each block under a root document by defRootID, in the given box's db.
func QueryRootChildrenRefCountInBox(defRootID, boxID string) (ret map[string]int) {
	ret = map[string]int{}
	sqlStmt := "SELECT def_block_id, COUNT(*) AS ref_cnt FROM refs WHERE def_block_root_id = ? GROUP BY def_block_id"
	rows, err := queryForBox(boxID, sqlStmt, defRootID)
	if err != nil {
		logging.LogErrorf("sql query [%s] failed: %s", sqlStmt, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var count int
		if err = rows.Scan(&id, &count); err != nil {
			logging.LogErrorf("query scan field failed: %s", err)
			return
		}
		ret[id] = count
	}
	return
}

// SelectBlocksRawStmtInBox runs a raw SQL query for blocks in the given box's db.
func SelectBlocksRawStmtInBox(stmt string, page, limit int, boxID string) (ret []*Block) {
	queryFn := func(stmt string, args ...any) (*sql.Rows, error) {
		return queryForBox(boxID, stmt, args...)
	}
	return selectBlocksRawStmtWithQuery(stmt, page, limit, queryFn)
}

// QueryRefCountInBox looks up ref counts by defBlockIDs in the given box's db.
func QueryRefCountInBox(defIDs []string, boxID string) (ret map[string]int) {
	ret = map[string]int{}
	if 1 > len(defIDs) {
		return
	}
	ids := "('" + strings.Join(defIDs, "','") + "')"
	rows, err := queryForBox(boxID, "SELECT def_block_id, COUNT(*) AS ref_cnt FROM refs WHERE def_block_id IN "+ids+" GROUP BY def_block_id")
	if err != nil {
		logging.LogErrorf("sql query failed: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var cnt int
		if err = rows.Scan(&id, &cnt); err != nil {
			logging.LogErrorf("query scan field failed: %s", err)
			return
		}
		ret[id] = cnt
	}
	return
}

// QueryNoLimitInBox runs an unlimited raw query in the given box's db (returning map rows).
func QueryNoLimitInBox(stmt, boxID string) (ret []map[string]any, err error) {
	return queryRawStmtForBox(boxID, stmt, math.MaxInt)
}

// QueryNoLimitArgsInBox behaves like QueryNoLimitInBox but supports a parameterized query.
func QueryNoLimitArgsInBox(stmt, boxID string, args ...any) (ret []map[string]any, err error) {
	return queryRawStmtArgsForBox(boxID, stmt, args, math.MaxInt)
}

// SelectBlocksRawStmtArgsInBox runs a parameterized raw SQL query for blocks in the given box's db.
// Corresponds to SelectBlocksRawStmtArgs, bypassing sqlparser's rewriting of "?" placeholders.
func SelectBlocksRawStmtArgsInBox(stmt string, args []any, limit int, boxID string) (ret []*Block) {
	rows, err := queryForBox(boxID, stmt, args...)
	if err != nil {
		if strings.Contains(err.Error(), "syntax error") {
			return
		}
		logging.LogWarnf("sql query [%s] failed: %s", stmt, err)
		return
	}
	defer rows.Close()

	noLimit := !containsLimitClause(stmt)
	var count, errCount int
	for rows.Next() {
		count++
		if block := scanBlockRows(rows); nil != block {
			ret = append(ret, block)
		} else {
			logging.LogWarnf("raw sql query [%s] failed", stmt)
			errCount++
		}

		if (noLimit && limit < count) || 0 < errCount {
			break
		}
	}
	return
}

// SelectBlocksRegexInBox runs a regex-matching query for blocks in the given box's db (the no-placeholder-arguments variant).
func SelectBlocksRegexInBox(stmt string, exp *regexp.Regexp, name, alias, memo, ial bool, page, pageSize int, boxID string) (ret []*Block) {
	rows, err := queryForBox(boxID, stmt)
	if err != nil {
		logging.LogErrorf("sql query [%s] failed: %s", stmt, err)
		return
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count <= (page-1)*pageSize {
			continue
		}

		var block Block
		if err := rows.Scan(&block.ID, &block.ParentID, &block.RootID, &block.Hash, &block.Box, &block.Path, &block.HPath, &block.Name, &block.Alias, &block.Memo, &block.Tag, &block.Content, &block.FContent, &block.Markdown, &block.Length, &block.Type, &block.SubType, &block.IAL, &block.Sort, &block.Created, &block.Updated); err != nil {
			logging.LogErrorf("query scan field failed: %s\n%s", err, logging.ShortStack())
			return
		}

		if matchRegexBlock(&block, exp, name, alias, memo, ial) {
			ret = append(ret, &block)
			if len(ret) >= pageSize {
				break
			}
		}
	}
	return
}

// SelectBlocksRegexArgsInBox behaves like SelectBlocksRegexInBox but executes with bound arguments.
func SelectBlocksRegexArgsInBox(stmt string, exp *regexp.Regexp, name, alias, memo, ial bool, page, pageSize int, boxID string, args ...any) (ret []*Block) {
	rows, err := queryForBox(boxID, stmt, args...)
	if err != nil {
		logging.LogErrorf("sql query [%s] failed: %s", stmt, err)
		return
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count <= (page-1)*pageSize {
			continue
		}

		var block Block
		if err := rows.Scan(&block.ID, &block.ParentID, &block.RootID, &block.Hash, &block.Box, &block.Path, &block.HPath, &block.Name, &block.Alias, &block.Memo, &block.Tag, &block.Content, &block.FContent, &block.Markdown, &block.Length, &block.Type, &block.SubType, &block.IAL, &block.Sort, &block.Created, &block.Updated); err != nil {
			logging.LogErrorf("query scan field failed: %s\n%s", err, logging.ShortStack())
			return
		}

		if matchRegexBlock(&block, exp, name, alias, memo, ial) {
			ret = append(ret, &block)
			if len(ret) >= pageSize {
				break
			}
		}
	}
	return
}

// matchRegexBlock regex-matches each field of block and highlights matches in place, returning true on a match.
func matchRegexBlock(block *Block, exp *regexp.Regexp, name, alias, memo, ial bool) bool {
	hitContent := exp.MatchString(block.Content)
	hitName := name && exp.MatchString(block.Name)
	hitAlias := alias && exp.MatchString(block.Alias)
	hitMemo := memo && exp.MatchString(block.Memo)
	hitIAL := ial && exp.MatchString(block.IAL)
	if hitContent || hitName || hitAlias || hitMemo || hitIAL {
		if hitContent {
			block.Content = exp.ReplaceAllString(block.Content, "__@mark__${0}__mark@__")
		}
		if hitName {
			block.Name = exp.ReplaceAllString(block.Name, "__@mark__${0}__mark@__")
		}
		if hitAlias {
			block.Alias = exp.ReplaceAllString(block.Alias, "__@mark__${0}__mark@__")
		}
		if hitMemo {
			block.Memo = exp.ReplaceAllString(block.Memo, "__@mark__${0}__mark@__")
		}
		if hitIAL {
			block.IAL = exp.ReplaceAllString(block.IAL, "__@mark__${0}__mark@__")
		}
		return true
	}
	return false
}

// QueryBlockNamesByRootIDInBox looks up block names by rootID in the given box's db.
func QueryBlockNamesByRootIDInBox(rootID, boxID string) (ret []string) {
	sqlStmt := "SELECT DISTINCT name FROM blocks WHERE root_id = ? AND name != ''"
	rows, err := queryForBox(boxID, sqlStmt, rootID)
	if err != nil {
		logging.LogErrorf("sql query [%s] failed: %s", sqlStmt, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		rows.Scan(&name)
		ret = append(ret, name)
	}
	return
}

// QueryBlockAliasesInBox looks up block aliases by rootID in the given box's db (comma-split and deduplicated).
func QueryBlockAliasesInBox(rootID, boxID string) (ret []string) {
	sqlStmt := "SELECT alias FROM blocks WHERE root_id = ? AND alias != ''"
	rows, err := queryForBox(boxID, sqlStmt, rootID)
	if err != nil {
		logging.LogErrorf("sql query [%s] failed: %s", sqlStmt, err)
		return
	}
	defer rows.Close()
	var aliasesRows []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		aliasesRows = append(aliasesRows, name)
	}

	for _, aliasStr := range aliasesRows {
		aliases := strings.SplitSeq(aliasStr, ",")
		for alias := range aliases {
			var exist bool
			for _, retAlias := range ret {
				if retAlias == alias {
					exist = true
				}
			}
			if !exist {
				ret = append(ret, alias)
			}
		}
	}
	return
}

// QueryRefsByDefIDRefIDInBox looks up a ref by defBlockID+refBlockID in the given box's db.
func QueryRefsByDefIDRefIDInBox(defBlockID, refBlockID, boxID string) (ret []*Ref) {
	stmt := "SELECT * FROM refs WHERE def_block_id = ? AND block_id = ?"
	rows, err := queryForBox(boxID, stmt, defBlockID, refBlockID)
	if err != nil {
		logging.LogErrorf("sql query failed: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		ref := scanRefRows(rows)
		ret = append(ret, ref)
	}
	return
}

// QueryRefsRecentInBox routes by boxID to query recent refs, used for block-ref search within an encrypted notebook.
func QueryRefsRecentInBox(onlyDoc bool, typeFilter string, ignoreLines []string, boxID string) (ret []*Ref) {
	stmt := "SELECT r.* FROM refs AS r, blocks AS b WHERE b.id = r.def_block_id AND b.type IN " + typeFilter
	if onlyDoc {
		stmt = "SELECT r.* FROM refs AS r, blocks AS b WHERE b.id = r.def_block_id AND b.type = 'd'"
	}
	if 0 < len(ignoreLines) {
		buf := bytes.Buffer{}
		for _, line := range ignoreLines {
			buf.WriteString(" AND ")
			buf.WriteString(line)
		}
		stmt += buf.String()
	}
	stmt += " GROUP BY r.def_block_id ORDER BY r.id DESC LIMIT 32"
	rows, err := queryForBox(boxID, stmt)
	if err != nil {
		logging.LogErrorf("sql query failed: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		ref := scanRefRows(rows)
		ret = append(ret, ref)
	}
	return
}

// QueryChildRefDefIDsByRootDefIDInBox looks up child ref definitions by rootDefID, routed by boxID.
func QueryChildRefDefIDsByRootDefIDInBox(rootDefID, boxID string) (ret map[string][]string) {
	ret = map[string][]string{}
	rows, err := queryForBox(boxID, "SELECT block_id, def_block_id FROM refs WHERE def_block_root_id = ?", rootDefID)
	if err != nil {
		logging.LogErrorf("sql query failed: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var defID, refID string
		if err = rows.Scan(&defID, &refID); err != nil {
			logging.LogErrorf("query scan field failed: %s", err)
			return
		}
		if nil == ret[defID] {
			ret[defID] = []string{refID}
		} else {
			ret[defID] = append(ret[defID], refID)
		}
	}
	return
}

// QueryRefIDsByDefIDInBox looks up the ref ID list by defID, routed by boxID.
func QueryRefIDsByDefIDInBox(defID string, containChildren bool, boxID string) (refIDs []string) {
	refIDs = []string{}
	var rows *sql.Rows
	var err error
	if containChildren {
		rows, err = queryForBox(boxID, "SELECT DISTINCT block_id FROM refs WHERE def_block_root_id = ?", defID)
	} else {
		rows, err = queryForBox(boxID, "SELECT DISTINCT block_id FROM refs WHERE def_block_id = ?", defID)
	}
	if err != nil {
		logging.LogErrorf("sql query failed: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			logging.LogErrorf("query scan field failed: %s", err)
			return
		}
		refIDs = append(refIDs, id)
	}
	return
}

// SelectBlocksRawStmtNoParseInBox behaves like SelectBlocksRawStmtNoParse but is routed by boxID.
func SelectBlocksRawStmtNoParseInBox(stmt string, limit int, boxID string) (ret []*Block) {
	rows, err := queryForBox(boxID, stmt)
	if err != nil {
		if strings.Contains(err.Error(), "syntax error") {
			return
		}
		return
	}
	defer rows.Close()

	noLimit := !containsLimitClause(stmt)
	var count, errCount int
	for rows.Next() {
		count++
		if block := scanBlockRows(rows); nil != block {
			ret = append(ret, block)
		} else {
			logging.LogWarnf("raw sql query [%s] failed", stmt)
			errCount++
		}

		if (noLimit && limit < count) || 0 < errCount {
			break
		}
	}
	return
}

// GetChildBlocksInBox looks up all child blocks by parentID in the given box's db.
func GetChildBlocksInBox(parentID, condition string, limit int, boxID string) (ret []*Block) {
	blockIDs := queryBlockChildrenIDsForBox(parentID, boxID)
	var params []string
	for _, id := range blockIDs {
		params = append(params, "\""+id+"\"")
	}

	ret = []*Block{}
	sqlStmt := "SELECT * FROM blocks AS ref WHERE ref.id IN (" + strings.Join(params, ",") + ")"
	if "" != condition {
		sqlStmt += " AND " + condition
	}
	sqlStmt += " LIMIT " + itoa(limit)
	rows, err := queryForBox(boxID, sqlStmt)
	if err != nil {
		logging.LogErrorf("sql query [%s] failed: %s", sqlStmt, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		if block := scanBlockRows(rows); nil != block {
			ret = append(ret, block)
		}
	}
	return
}

// queryBlockChildrenIDsForBox recursively collects parentID and all its child block ids, routed by boxID.
func queryBlockChildrenIDsForBox(id, boxID string) (ret []string) {
	ret = append(ret, id)
	childIDs := queryBlockIDByParentIDForBox(id, boxID)
	for _, childID := range childIDs {
		ret = append(ret, queryBlockChildrenIDsForBox(childID, boxID)...)
	}
	return
}

// queryBlockIDByParentIDForBox looks up direct child block ids by parentID, routed by boxID.
func queryBlockIDByParentIDForBox(parentID, boxID string) (ret []string) {
	sqlStmt := "SELECT id FROM blocks WHERE parent_id = ?"
	rows, err := queryForBox(boxID, sqlStmt, parentID)
	if err != nil {
		logging.LogErrorf("sql query [%s] failed: %s", sqlStmt, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ret = append(ret, id)
	}
	return
}

// itoa is a shorthand alias for strconv.Itoa, avoiding a duplicate import.
func itoa(i int) string {
	return intToStr(i)
}

// intToStr converts an int to a string (avoiding a dependency on importing strconv in this file).
func intToStr(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// queryRawStmtForBox behaves like queryRawStmt but routes to the encrypted db or the global db by boxID.
func queryRawStmtForBox(boxID, stmt string, limit int) (ret []map[string]any, err error) {
	rows, err := queryForBox(boxID, stmt)
	if err != nil {
		if strings.Contains(err.Error(), "syntax error") {
			return
		}
		return
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil || nil == cols {
		return
	}

	noLimit := !containsLimitClause(stmt)
	var count int
	for rows.Next() {
		columns := make([]any, len(cols))
		columnPointers := make([]any, len(cols))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}

		if err = rows.Scan(columnPointers...); err != nil {
			return
		}

		m := make(map[string]any)
		for i, colName := range cols {
			val := columnPointers[i].(*any)
			m[colName] = *val
		}

		ret = append(ret, m)
		count++
		if noLimit && limit < count {
			break
		}
	}
	return
}

// queryRawStmtArgsForBox behaves like queryRawStmtArgs but routes to the encrypted db or the global db by boxID.
func queryRawStmtArgsForBox(boxID, stmt string, args []any, limit int) (ret []map[string]any, err error) {
	rows, err := queryForBox(boxID, stmt, args...)
	if err != nil {
		return
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil || nil == cols {
		return
	}

	noLimit := !containsLimitClause(stmt)
	var count int
	for rows.Next() {
		columns := make([]any, len(cols))
		columnPointers := make([]any, len(cols))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}

		if err = rows.Scan(columnPointers...); err != nil {
			return
		}

		m := make(map[string]any)
		for i, colName := range cols {
			val := columnPointers[i].(*any)
			m[colName] = *val
		}

		ret = append(ret, m)
		count++
		if noLimit && limit < count {
			break
		}
	}
	return
}
