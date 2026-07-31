// DejaVu - Data snapshot and sync.
// Copyright (c) 2022-present, b3log.org
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

package dejavu

import (
	"testing"

	"github.com/siyuan-note/dejavu/cloud"
)

func TestSync(t *testing.T) {
	// Upstream disables this with a bare `return`, which leaves the rest of the function unreachable and makes
	// `go vet` fail for the whole module. Skipping instead keeps the intent, keeps the code compiled and type
	// checked, and lets vet pass. The test still needs a cloud server at 127.0.0.1:64388 to actually run.
	t.Skip("requires a local SiYuan cloud server")

	repo, _ := initIndex(t)

	userId := "0"
	token := ""

	repo.cloud = &cloud.SiYuan{BaseCloud: &cloud.BaseCloud{Conf: &cloud.Conf{
		Dir:           "test",
		UserID:        userId,
		AvailableSize: 1024 * 1024 * 1024 * 8,
		Token:         token,
		Server:        "http://127.0.0.1:64388",
	}}}

	mergeResult, trafficStat, err := repo.Sync(nil)
	if nil != err {
		t.Fatalf("sync failed: %s", err)
		return
	}
	_ = mergeResult
	_ = trafficStat
}
