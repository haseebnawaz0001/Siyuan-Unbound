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
	"errors"
	"fmt"
	"os"

	mmap "github.com/edsrzf/mmap-go"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/logging"
)

// WriteFileByMmap overwrites filePath in place with data using memory mapping.
//
// Flow: OpenFile(O_RDWR|O_CREATE) -> Truncate to the exact length -> mmap.Map(RDWR) -> copy the data in -> Flush ->
// Unmap, holding filelock's in-process mutex throughout to avoid concurrent write conflicts.
//
// Compared to filelock.WriteFile (temp file + rename + fsync), this path is nearly invisible in process-level I/O
// counters (IO Write Bytes) -- copy is a pure in-memory write that never goes through the I/O subsystem, and only
// Flush registers a tiny amount. On error, the caller falls back to filelock.WriteFile.
func WriteFileByMmap(filePath string, data []byte) (err error) {
	f, err := filelock.OpenFile(filePath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return
	}
	defer filelock.CloseFile(f)

	if err = f.Truncate(int64(len(data))); err != nil {
		msg := fmt.Sprintf("truncate file [%s] failed: %s", filePath, err)
		logging.LogError(msg)
		err = errors.New(msg)
		return
	}

	m, err := mmap.Map(f, mmap.RDWR, 0)
	if err != nil {
		msg := fmt.Sprintf("map file [%s] failed: %s", filePath, err)
		logging.LogError(msg)
		err = errors.New(msg)
		return
	}
	defer m.Unmap()

	copy(m, data)
	if err = m.Flush(); err != nil {
		msg := fmt.Sprintf("flush data [%s] failed: %s", filePath, err)
		logging.LogError(msg)
		err = errors.New(msg)
		return
	}
	return
}
