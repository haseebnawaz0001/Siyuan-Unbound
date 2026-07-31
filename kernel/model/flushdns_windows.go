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

//go:build windows

package model

import (
	"os/exec"

	"github.com/88250/gulu"
	"github.com/siyuan-note/logging"
)

// flushDNS flushes the Windows system DNS resolution cache. Used after sync encounters DNS-related
// errors (hostname resolution failure, stale cache) to clear any potentially stale local resolution
// records, so subsequent retries can query the upstream DNS again.
func flushDNS() {
	cmd := exec.Command("ipconfig", "/flushdns")
	gulu.CmdAttr(cmd)
	output, err := cmd.CombinedOutput()
	if nil != err {
		logging.LogErrorf("flush DNS cache failed: %s", err)
		return
	}
	logging.LogInfof("flushed DNS cache: %s", gulu.DecodeCmdOutput(output))
}
