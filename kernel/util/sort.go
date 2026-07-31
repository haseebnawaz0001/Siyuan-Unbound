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
	"bytes"
	"io"
	"strings"

	"github.com/facette/natsort"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func NaturalCompare(str1, str2 string) bool {
	str1 = RemoveEmojiInvisible(str1)
	str2 = RemoveEmojiInvisible(str2)
	return natsort.Compare(str1, str2)
}

func EmojiPinYinCompare(str1, str2 string) bool {
	str1_ := strings.TrimSpace(RemoveEmojiInvisible(str1))
	str2_ := strings.TrimSpace(RemoveEmojiInvisible(str2))
	if str1_ == str2_ && 0 == len(str1_) {
		// When both are entirely emoji, sort by the emoji string
		return strings.Compare(str1, str2) < 0
	}
	return PinYinCompare(str1, str2)
}

func PinYinCompare(str1, str2 string) bool {
	str1 = RemoveEmojiInvisible(str1)
	str2 = RemoveEmojiInvisible(str2)

	// Doc tree, backlinks, tags and templates ignores case when sorting alphabetically by name https://github.com/siyuan-note/siyuan/issues/8360
	str1 = strings.ToLower(str1)
	str2 = strings.ToLower(str2)

	a, _ := UTF82GBK(str1)
	b, _ := UTF82GBK(str2)
	bLen := len(b)
	for idx, chr := range a {
		if idx > bLen-1 {
			return false
		}
		if chr != b[idx] {
			return chr < b[idx]
		}
	}
	return true
}

func PinYinCompare4FileTree(str1, str2 string) bool {
	// Doc tree alphabetical sorting does not reuse PinYinCompare but implements it separately
	// Improve doc tree Name Alphabet sorting https://github.com/siyuan-note/siyuan/issues/14773

	str1 = RemoveEmojiInvisible(str1)
	str1 = strings.TrimSuffix(str1, ".sy")
	str2 = RemoveEmojiInvisible(str2)
	str2 = strings.TrimSuffix(str2, ".sy")

	// Doc tree, backlinks, tags and templates ignores case when sorting alphabetically by name https://github.com/siyuan-note/siyuan/issues/8360
	str1 = strings.ToLower(str1)
	str2 = strings.ToLower(str2)

	a, _ := UTF82GBK(str1)
	b, _ := UTF82GBK(str2)

	// When the lengths are equal, compare the byte arrays directly
	if len(a) == len(b) {
		return bytes.Compare(a, b) < 0
	}

	// When the lengths differ, compare the leading equal portion
	if len(a) < len(b) {
		if 0 == bytes.Compare(a, b[:len(a)]) { // When the leading portions are equal, the shorter one comes first
			return true
		}
		return bytes.Compare(a, b[:len(a)]) < 0
	}
	if 0 == bytes.Compare(a[:len(b)], b) {
		return false
	}
	return bytes.Compare(a[:len(b)], b) < 0
}

// UTF82GBK transform UTF8 rune into GBK byte array.
func UTF82GBK(src string) ([]byte, error) {
	GB18030 := simplifiedchinese.All[0]
	return io.ReadAll(transform.NewReader(bytes.NewReader([]byte(src)), GB18030.NewEncoder()))
}

// GBK2UTF8 transform GBK byte array into UTF8 string.
func GBK2UTF8(src []byte) (string, error) {
	GB18030 := simplifiedchinese.All[0]
	bytes, err := io.ReadAll(transform.NewReader(bytes.NewReader(src), GB18030.NewDecoder()))
	return string(bytes), err
}

const (
	SortModeNameASC         = iota // 0: file name alphabetically ascending
	SortModeNameDESC               // 1: file name alphabetically descending
	SortModeUpdatedASC             // 2: file update time ascending
	SortModeUpdatedDESC            // 3: file update time descending
	SortModeAlphanumASC            // 4: file name natural number ascending
	SortModeAlphanumDESC           // 5: file name natural number descending
	SortModeCustom                 // 6: custom sort
	SortModeRefCountASC            // 7: reference count ascending
	SortModeRefCountDESC           // 8: reference count descending
	SortModeCreatedASC             // 9: file creation time ascending
	SortModeCreatedDESC            // 10: file creation time descending
	SortModeSizeASC                // 11: file size ascending
	SortModeSizeDESC               // 12: file size descending
	SortModeSubDocCountASC         // 13: subdocument count ascending
	SortModeSubDocCountDESC        // 14: subdocument count descending
	SortModeFileTree               // 15: use the doc tree's sort rule

	SortModeUnassigned = 256 // 256: no sort rule specified; the sort rule is obtained with the notebook taking priority over the doc tree
)
