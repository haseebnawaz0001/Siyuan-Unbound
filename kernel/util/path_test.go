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
	"os"
	"path/filepath"
	"testing"
)

// TestIsSensitivePathCredentialDotfiles covers home directory credential dotfiles that were missing from the GHSA
// report, ensuring they are rejected at interfaces like globalCopyFiles that accept absolute paths outside the
// workspace.
func TestIsSensitivePathCredentialDotfiles(t *testing.T) {
	tmpHome := t.TempDir()
	tmpWorkspace := t.TempDir()
	origHome, origWorkspace := HomeDir, WorkspaceDir
	HomeDir, WorkspaceDir = tmpHome, tmpWorkspace
	t.Cleanup(func() { HomeDir, WorkspaceDir = origHome, origWorkspace })

	// The attack surface named in the report, plus common cloud/package manager credentials.
	cases := []struct {
		name string
		rel  string // Path relative to HomeDir
	}{
		{"git-credentials", ".git-credentials"},
		{"netrc", ".netrc"},
		{"pgpass", ".pgpass"},
		{"kube config", filepath.Join(".kube", "config")},
		{"docker config", filepath.Join(".docker", "config.json")},
		{"gnupg private keyring", filepath.Join(".gnupg", "private-keys-v1.d", "key.key")},
		{"aws credentials", filepath.Join(".aws", "credentials")},
		{"azure token", filepath.Join(".azure", "accessTokens.json")},
		{"npmrc", ".npmrc"},
		{"pypirc", ".pypirc"},
		// Existing blacklist entries should not regress.
		{"ssh id_rsa", filepath.Join(".ssh", "id_rsa")},
		{"bashrc", ".bashrc"},
		{"config dir", filepath.Join(".config", "some-app")},
	}
	for _, c := range cases {
		abs := filepath.Join(tmpHome, c.rel)
		if got := IsSensitivePath(abs); !got {
			t.Errorf("IsSensitivePath(%q) = false, want true [%s]", abs, c.name)
		}
	}
}

// TestIsSensitivePathSymlinkBypass verifies that attempts to bypass the blacklist via a symlink are blocked:
// place a symlink pointing to ~/.ssh/id_rsa in a non-sensitive directory; once resolved, it should be judged sensitive.
func TestIsSensitivePathSymlinkBypass(t *testing.T) {
	tmpHome := t.TempDir()
	tmpWorkspace := t.TempDir()
	origHome, origWorkspace := HomeDir, WorkspaceDir
	HomeDir, WorkspaceDir = tmpHome, tmpWorkspace
	t.Cleanup(func() { HomeDir, WorkspaceDir = origHome, origWorkspace })

	target := filepath.Join(tmpHome, ".ssh", "id_rsa")
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("PRIVATE"), 0600); err != nil {
		t.Fatalf("write target: %v", err)
	}

	// Place the symlink in a location that looks harmless (a temp directory outside the home directory).
	innocentDir := t.TempDir()
	link := filepath.Join(innocentDir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported on this platform: %v", err)
	}

	// link's own path doesn't hit the blacklist, but once resolved it points into .ssh, so it should be rejected.
	if got := IsSensitivePath(link); !got {
		t.Errorf("IsSensitivePath(symlink -> .ssh) = false, want true")
	}
}

// TestIsSensitivePathWorkspaceFilesNotBlocked ensures legitimate files inside the workspace are not misjudged.
func TestIsSensitivePathWorkspaceFilesNotBlocked(t *testing.T) {
	tmpWorkspace := t.TempDir()
	origWorkspace := WorkspaceDir
	WorkspaceDir = tmpWorkspace
	t.Cleanup(func() { WorkspaceDir = origWorkspace })

	cases := []string{
		filepath.Join(tmpWorkspace, "data", "assets", "image.png"),
		filepath.Join(tmpWorkspace, "data", "note.md"),
		filepath.Join(tmpWorkspace, "temp", "export", "output.pdf"),
	}
	for _, p := range cases {
		if got := IsSensitivePath(p); got {
			t.Errorf("IsSensitivePath(%q) = true, want false (workspace file)", p)
		}
	}
}
