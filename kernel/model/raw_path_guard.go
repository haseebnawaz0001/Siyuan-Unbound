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
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package model

import (
	"path/filepath"

	"github.com/siyuan-note/siyuan/kernel/util"
)

// EncryptedRawPathBoxID returns the ID of the encrypted notebook that the raw file path belongs to.
// It also checks the resolved path of the longest existing parent directory, to prevent bypassing the
// encryption boundary via symlinks.
func EncryptedRawPathBoxID(absPath string) string {
	if normalized, err := filepath.Abs(absPath); err == nil {
		absPath = normalized
	}
	if boxID := ExtractBoxIDFromAssetsPath(absPath); boxID != "" && IsEncryptedBox(boxID) {
		return boxID
	}
	resolved := util.ResolveLongestExistingParent(absPath)
	if resolved != absPath {
		if boxID := ExtractBoxIDFromAssetsPath(resolved); boxID != "" && IsEncryptedBox(boxID) {
			return boxID
		}
	}
	return ""
}
