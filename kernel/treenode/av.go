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

package treenode

import (
	"github.com/siyuan-note/siyuan/kernel/av"
)

// GetMirrorAttrViewBlockIDs returns all block IDs that reference this AV (only those still present in the block tree).
// The mirror index is obtained via av.GetBlockRels, which already merges the global data with the mirror data
// of all currently open encrypted notebooks (encryption-aware).
func GetMirrorAttrViewBlockIDs(avID string) (ret []string) {
	ret = []string{}
	avBlocks := av.GetBlockRels()
	blockIDs := avBlocks[avID]
	bts := GetBlockTrees(blockIDs)
	for blockID := range bts {
		ret = append(ret, blockID)
	}
	return
}
