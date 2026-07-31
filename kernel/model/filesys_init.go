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
	"github.com/siyuan-note/siyuan/kernel/av"
	"github.com/siyuan-note/siyuan/kernel/filesys"
	"github.com/siyuan-note/siyuan/kernel/sql"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

// init injects encryption-related callbacks into the underlying packages so they can query a box's
// DEK / encryption state.
// filesys / av / sql / treenode cannot import model directly (circular dependency), so callback
// injection is used instead.
// Based on this, the routing functions in sql / treenode fail closed: they never fall back to the
// global database when an encrypted notebook is locked.
func init() {
	filesys.DEKProvider = GetDEKIfUnlocked
	filesys.DEKLockAcquire = HoldBoxReadLock
	filesys.DEKLockRelease = ReleaseBoxReadLock
	av.AVDEKProvider = GetDEKIfUnlocked
	av.AVLockAcquire = HoldBoxReadLock
	av.AVLockRelease = ReleaseBoxReadLock
	av.AVEncryptedBoxIDs = treenode.GetOpenedEncryptedBoxIDs
	av.AVIsEncryptedBox = IsEncryptedBox
	av.AVGetBlockBoxID = func(blockID string) string {
		bt := treenode.GetBlockTree(blockID)
		if nil == bt {
			return ""
		}
		return bt.BoxID
	}
	sql.IsEncryptedBoxFn = IsEncryptedBox
	treenode.IsEncryptedBoxFn = IsEncryptedBox
	util.ReloadDocInfoGuard = func(boxID string) bool {
		// Once an encrypted notebook is locked, drop the deferred reloadDocInfo broadcast to prevent
		// plaintext metadata leakage
		if !IsEncryptedBox(boxID) {
			return true
		}
		return IsBoxUnlocked(boxID)
	}
}
