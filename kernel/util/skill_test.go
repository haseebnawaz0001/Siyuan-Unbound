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
	"sort"
	"strings"
	"testing"
)

// touchSkillMd creates a skill directory containing SKILL.md under root.
func touchSkillMd(t *testing.T, parts ...string) {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
	skillMd := filepath.Join(p, "SKILL.md")
	if err := os.WriteFile(skillMd, []byte("---\nname: x\n---\nbody"), 0644); err != nil {
		t.Fatalf("write %s: %v", skillMd, err)
	}
}

// sorted sorts findSkillDirs' return value before comparison, so traversal order doesn't affect assertions.
func sorted(ss []string) []string {
	out := append([]string(nil), ss...)
	sort.Strings(out)
	return out
}

// toSlashSet normalizes path separators to /, to make assertions portable across platforms.
func toSlashSet(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = filepath.ToSlash(s)
	}
	sort.Strings(out)
	return out
}

// TestFindSkillDirsRootSkill verifies that SKILL.md directly at the unzip root is recognized as a single skill
// (relative path ".").
func TestFindSkillDirsRootSkill(t *testing.T) {
	root := t.TempDir()
	touchSkillMd(t, root)
	got := findSkillDirs(root)
	want := []string{"."}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("findSkillDirs(root skill) = %v, want %v", got, want)
	}
}

// TestFindSkillDirsWrappedSingle verifies a single skill wrapped by codeload (<repo-name>/SKILL.md).
// This is the most common scenario for the owner/repo shorthand; before the fix it was missed because the code
// only looked for a skills/ container at the root.
func TestFindSkillDirsWrappedSingle(t *testing.T) {
	root := t.TempDir()
	touchSkillMd(t, root, "WeChatReading")
	got := toSlashSet(findSkillDirs(root))
	want := []string{"WeChatReading"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("findSkillDirs(wrapped single) = %v, want %v", got, want)
	}
}

// TestFindSkillDirsWrappedCollection verifies a collection repo wrapped by codeload
// (<repo>/skills/<name>/SKILL.md).
// Before the fix, this would not be found at all (root only has the repo directory, with skills/ underneath it).
func TestFindSkillDirsWrappedCollection(t *testing.T) {
	root := t.TempDir()
	touchSkillMd(t, root, "myrepo", "skills", "foo")
	touchSkillMd(t, root, "myrepo", "skills", "bar")
	got := toSlashSet(findSkillDirs(root))
	want := []string{"myrepo/skills/bar", "myrepo/skills/foo"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("findSkillDirs(wrapped collection) = %v, want %v", got, want)
	}
}

// TestFindSkillDirsCollectionNoWrap verifies an unwrapped collection repo (skills/<name>/SKILL.md).
func TestFindSkillDirsCollectionNoWrap(t *testing.T) {
	root := t.TempDir()
	touchSkillMd(t, root, "skills", "baz")
	got := toSlashSet(findSkillDirs(root))
	want := []string{"skills/baz"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("findSkillDirs(collection no wrap) = %v, want %v", got, want)
	}
}

// TestFindSkillDirsDoesNotDescendIntoSkillInternals verifies that recursion stops once a skill is recognized, so
// the internal references/ and scripts/ subdirectories of a skill are not mistaken for skills.
func TestFindSkillDirsDoesNotDescendIntoSkillInternals(t *testing.T) {
	root := t.TempDir()
	// skill5 itself has SKILL.md, and also contains references/a.md, scripts/b.py
	skillDir := filepath.Join(root, "skill5", "SKILL.md")
	if err := os.MkdirAll(filepath.Join(root, "skill5", "references"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "skill5", "scripts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillDir, []byte("---\nname: x\n---"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skill5", "references", "a.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skill5", "scripts", "b.py"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	got := toSlashSet(findSkillDirs(root))
	want := []string{"skill5"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("should not descend into skill internals: got %v, want %v", got, want)
	}
}

// TestFindSkillDirsNoneFound verifies an empty slice is returned when there is no SKILL.md.
func TestFindSkillDirsNoneFound(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "some", "dir"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := findSkillDirs(root); len(got) != 0 {
		t.Fatalf("findSkillDirs(no skill) = %v, want empty", got)
	}
}

// TestFindSkillDirsSkipsVCSDirs verifies that metadata directories like .git/.github are skipped, avoiding
// pointless recursion.
func TestFindSkillDirsSkipsVCSDirs(t *testing.T) {
	root := t.TempDir()
	touchSkillMd(t, root, "real-skill")
	// Create a .github directory (no SKILL.md) to confirm recursing into it doesn't error out or misidentify
	if err := os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0755); err != nil {
		t.Fatal(err)
	}
	got := toSlashSet(findSkillDirs(root))
	want := []string{"real-skill"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("findSkillDirs(skip vcs) = %v, want %v", got, want)
	}
}

// urlCase is a table-driven test case for normalizeSkillURL.
type urlCase struct {
	name       string
	in         string
	wantURL    string // Expected downloadURL; empty means don't check the exact value, check isZip/branch instead
	wantIsZip  bool
	wantBranch string
}

// TestNormalizeSkillURL verifies that various input shapes are normalized into the correct download source.
func TestNormalizeSkillURL(t *testing.T) {
	cases := []urlCase{
		// owner/repo shorthand -> codeload main, falls back to master on failure
		{name: "shorthand", in: "Tencent/WeChatReading", wantURL: "https://codeload.github.com/Tencent/WeChatReading/zip/refs/heads/main", wantIsZip: true, wantBranch: "main"},
		// A full npx skills add command -> extract owner/repo
		{name: "npx command", in: "npx skills add Tencent/WeChatReading -g", wantURL: "https://codeload.github.com/Tencent/WeChatReading/zip/refs/heads/main", wantIsZip: true, wantBranch: "main"},
		// skills package name with an @ version
		{name: "npx scoped", in: "npx skills@latest add foo/bar", wantURL: "https://codeload.github.com/foo/bar/zip/refs/heads/main", wantIsZip: true, wantBranch: "main"},
		// Full GitHub repo URL -> codeload main
		{name: "full repo", in: "https://github.com/Tencent/WeChatReading", wantURL: "https://codeload.github.com/Tencent/WeChatReading/zip/refs/heads/main", wantIsZip: true, wantBranch: "main"},
		// tree/<branch> -> codeload with the specified branch
		{name: "tree branch", in: "https://github.com/foo/bar/tree/dev", wantURL: "https://codeload.github.com/foo/bar/zip/refs/heads/dev", wantIsZip: true, wantBranch: "dev"},
		// tree/<branch>/<path> -> still pulls the zip for that branch of the whole repo
		{name: "tree branch path", in: "https://github.com/foo/bar/tree/dev/skills/x", wantURL: "https://codeload.github.com/foo/bar/zip/refs/heads/dev", wantIsZip: true, wantBranch: "dev"},
		// commit/<sha> -> codeload zip/<sha>
		{name: "commit sha", in: "https://github.com/foo/bar/commit/abc123", wantURL: "https://codeload.github.com/foo/bar/zip/abc123", wantIsZip: true},
		// blob/<branch>/<path> -> raw direct link (single file)
		{name: "blob file", in: "https://github.com/foo/bar/blob/main/SKILL.md", wantURL: "https://raw.githubusercontent.com/foo/bar/main/SKILL.md", wantIsZip: false},
		// raw direct link -> unchanged
		{name: "raw direct", in: "https://raw.githubusercontent.com/foo/bar/main/skills/x/SKILL.md", wantURL: "https://raw.githubusercontent.com/foo/bar/main/skills/x/SKILL.md", wantIsZip: false},
		// releases/download/<tag>/<asset>.zip -> unchanged, pre-guessed as zip
		{name: "release zip", in: "https://github.com/foo/bar/releases/download/v1.0/skill.zip", wantURL: "https://github.com/foo/bar/releases/download/v1.0/skill.zip", wantIsZip: true},
		// releases/download but asset is not zip -> unchanged, not pre-guessed as zip (decided by Content-Type)
		{name: "release non-zip", in: "https://github.com/foo/bar/releases/download/v1.0/skill.tar.gz", wantURL: "https://github.com/foo/bar/releases/download/v1.0/skill.tar.gz", wantIsZip: false},
		// Third-party direct link -> unchanged
		{name: "third-party", in: "https://example.com/skill.zip", wantURL: "https://example.com/skill.zip"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := normalizeSkillURL(c.in)
			if err != nil {
				t.Fatalf("normalizeSkillURL(%q) error: %v", c.in, err)
			}
			if c.wantURL != "" && got.downloadURL != c.wantURL {
				t.Errorf("downloadURL = %q, want %q", got.downloadURL, c.wantURL)
			}
			if got.isZip != c.wantIsZip {
				t.Errorf("isZip = %v, want %v", got.isZip, c.wantIsZip)
			}
			if c.wantBranch != "" && got.branch != c.wantBranch {
				t.Errorf("branch = %q, want %q", got.branch, c.wantBranch)
			}
		})
	}
}

// TestNormalizeSkillURLOwnerRepoBoundary verifies that owner/repo shorthand detection doesn't misfire on plain
// text.
// Inputs containing a dot (domain), a colon, or a scheme should never go through the shorthand branch.
func TestNormalizeSkillURLOwnerRepoBoundary(t *testing.T) {
	bad := []string{
		"example.com",     // Not owner/repo
		"a/b/c",           // More than one slash
		"https://foo/bar", // Has a scheme (should go through the URL branch, but is a valid URL so no error)
		"/abs/path",       // Absolute path
	}
	for _, in := range bad {
		// These inputs either error out or go through the URL branch; as long as it doesn't panic or get
		// misidentified as codeload, that's fine
		got, err := normalizeSkillURL(in)
		if err != nil {
			continue // An error is acceptable
		}
		// Should not be misidentified as codeload.github.com (only owner/repo shorthand and github.com URLs would be)
		if in == "example.com" && strings.HasPrefix(got.downloadURL, "https://codeload.github.com/") {
			t.Errorf("input %q should not map to codeload, got %s", in, got.downloadURL)
		}
	}
}

// TestNormalizeSkillURLOwnerRepoValid confirms that a valid owner/repo goes through the codeload branch.
func TestNormalizeSkillURLOwnerRepoValid(t *testing.T) {
	good := []string{"a/b", "Tencent/WeChatReading", "user-1/my.repo.v2"}
	for _, in := range good {
		got, err := normalizeSkillURL(in)
		if err != nil {
			t.Errorf("normalizeSkillURL(%q) unexpected error: %v", in, err)
			continue
		}
		if !got.isZip || got.branch != "main" {
			t.Errorf("normalizeSkillURL(%q) = %+v, want codeload main zip", in, got)
		}
	}
}

// TestParseSkillFrontmatter verifies that frontmatter parsing extracts name/description and correctly strips the
// body.
func TestParseSkillFrontmatter(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		wantName string
		wantDesc string
		wantBody string
	}{
		{
			name:     "standard",
			text:     "---\nname: my-skill\ndescription: does X\n---\nbody line",
			wantName: "my-skill",
			wantDesc: "does X",
			wantBody: "body line",
		},
		{
			name:     "no frontmatter",
			text:     "just body",
			wantName: "",
			wantDesc: "",
			wantBody: "just body",
		},
		{
			name:     "extra fields ignored",
			text:     "---\nname: a\nversion: \"1.0\"\nauthor: me\ndescription: d\n---\nb",
			wantName: "a",
			wantDesc: "d",
			wantBody: "b",
		},
		{
			name:     "crlf line endings",
			text:     "---\r\nname: cr\r\ndescription: crlf\r\n---\r\nbody",
			wantName: "cr",
			wantDesc: "crlf",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fm, body := parseSkillFrontmatter(c.text)
			if fm["name"] != c.wantName {
				t.Errorf("name = %q, want %q", fm["name"], c.wantName)
			}
			if fm["description"] != c.wantDesc {
				t.Errorf("description = %q, want %q", fm["description"], c.wantDesc)
			}
			if c.wantBody != "" && body != c.wantBody {
				t.Errorf("body = %q, want %q", body, c.wantBody)
			}
		})
	}
}

// TestFirstLine verifies the description fallback extracts the first line and truncates it.
func TestFirstLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"single", "single"},
		{"first\nsecond", "first"},
		{"first\r\nsecond", "first"},
		{"   trimmed   ", "trimmed"},
	}
	for _, c := range cases {
		if got := firstLine(c.in); got != c.want {
			t.Errorf("firstLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestFirstLineTruncation verifies that an overly long first line is truncated to 200 characters + "...".
func TestFirstLineTruncation(t *testing.T) {
	long := make([]rune, 300)
	for i := range long {
		long[i] = 'x'
	}
	got := firstLine(string(long))
	if len([]rune(got)) != 203 { // 200 + "..."
		t.Errorf("firstLine(long) len = %d, want 203", len([]rune(got)))
	}
}
