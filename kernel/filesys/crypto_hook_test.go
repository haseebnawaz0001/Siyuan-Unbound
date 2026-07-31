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
	"strings"
	"testing"
)

// TestSyObjectBase verifies the extraction and validity of the stable file basename.
// The AAD for .sy is bound to <rootID>.sy, with no involvement from the parent directory, so the extractor
// must correctly strip the directory prefix and validate strictly.
func TestSyObjectBase(t *testing.T) {
	validBase := "20240101120000-1a2b3c4.sy"

	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"bare valid", validBase, validBase, false},
		{"leading slash", "/20240101120000-1a2b3c4.sy", validBase, false},
		{"with parent dir", "/20240101120000-aaa/20240101120000-1a2b3c4.sy", validBase, false},
		{"deep parent dir", "20240101120000-aaa/20240101120000-bbb/20240101120000-1a2b3c4.sy", validBase, false},
		{"windows backslash", "\\20240101120000-aaa\\20240101120000-1a2b3c4.sy", validBase, false},
		{"non-sy extension", "/20240101120000-1a2b3c4.json", "", true},
		{"no extension", "/20240101120000-1a2b3c4", "", true},
		{"bad stem not node id", "/notanid.sy", "", true},
		{"stem too short", "/2024010-abc.sy", "", true},
		{"empty", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := SyObjectBase(c.input)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got %q", c.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for input %q: %v", c.input, err)
			}
			if got != c.want {
				t.Fatalf("SyObjectBase(%q) = %q, want %q", c.input, got, c.want)
			}
		})
	}
}

// TestSyAADIndependentOfParentDir verifies the AAD depends only on the stable file basename, not the parent directory.
// This is the cryptographic precondition for "a move within the same box that keeps the filename unchanged
// can Rename the ciphertext as-is": AAD constructed from the same basename but different parent directories
// must be exactly identical.
func TestSyAADIndependentOfParentDir(t *testing.T) {
	boxID := "20240101120000-boxid01"
	base := "20240101120000-1a2b3c4.sy"

	aadBare, err := SyAAD(boxID, base)
	if err != nil {
		t.Fatal(err)
	}
	aadWithDir, err := SyAAD(boxID, "/20240101120000-parent/"+base)
	if err != nil {
		t.Fatal(err)
	}
	aadDeepDir, err := SyAAD(boxID, "20240101120000-a/20240101120000-b/"+base)
	if err != nil {
		t.Fatal(err)
	}

	if aadBare != aadWithDir || aadBare != aadDeepDir {
		t.Fatalf("AAD must be independent of parent dir: bare=%q dir=%q deep=%q", aadBare, aadWithDir, aadDeepDir)
	}
	// The AAD should not contain a path separator, only the basename
	if strings.Contains(strings.TrimPrefix(aadBare, "siyuan:v1:file:"+boxID+":"), "/") {
		t.Fatalf("AAD must not contain path separator: %q", aadBare)
	}
}

// TestSyAADRejectsInvalidBase verifies that an invalid basename errors out immediately, without producing a usable AAD for encryption.
func TestSyAADRejectsInvalidBase(t *testing.T) {
	if _, err := SyAAD("20240101120000-boxid01", "random.txt"); err == nil {
		t.Fatal("should reject non-.sy base")
	}
	if _, err := SyAAD("20240101120000-boxid01", "notanid.sy"); err == nil {
		t.Fatal("should reject non-node-id stem")
	}
}
