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
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/88250/gulu"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/httpclient"
	"github.com/siyuan-note/logging"
)

type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func SkillsDir() string {
	return filepath.Join(DataDir, "storage", "ai", "agent", "skills")
}

func DiscoverSkills() []SkillInfo {
	dir := SkillsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var skills []SkillInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillDir := e.Name()
		skillMdPath := filepath.Join(dir, skillDir, "SKILL.md")
		b, err := filelock.ReadFile(skillMdPath)
		if err != nil {
			continue
		}
		fm, body := parseSkillFrontmatter(string(b))
		name := fm["name"]
		if name == "" {
			name = skillDir
		}
		desc := fm["description"]
		if desc == "" {
			desc = firstLine(body)
		}
		skills = append(skills, SkillInfo{
			Name:        name,
			Description: desc,
		})
	}
	return skills
}

func LoadSkillContent(name string) string {
	dir := SkillsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillMdPath := filepath.Join(dir, e.Name(), "SKILL.md")
		b, err := filelock.ReadFile(skillMdPath)
		if err != nil {
			continue
		}
		fm, body := parseSkillFrontmatter(string(b))
		skillName := fm["name"]
		if skillName == "" {
			skillName = e.Name()
		}
		if strings.EqualFold(skillName, name) || strings.EqualFold(e.Name(), name) {
			return body
		}
	}
	return ""
}

func validateSkillName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid skill name: %s", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid skill name: %s", name)
	}
	dir := SkillsDir()
	abs := filepath.Join(dir, name)
	if !gulu.File.IsSubPath(dir, abs) {
		return fmt.Errorf("invalid skill name: %s", name)
	}
	return nil
}

func ReadSkill(name string) (string, error) {
	if err := validateSkillName(name); err != nil {
		return "", err
	}
	skillMdPath := filepath.Join(SkillsDir(), name, "SKILL.md")
	b, err := filelock.ReadFile(skillMdPath)
	if err != nil {
		return "", fmt.Errorf("skill not found: %s", name)
	}
	return string(b), nil
}

func SaveSkill(name, content string) error {
	if err := validateSkillName(name); err != nil {
		return err
	}
	dir := SkillsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return err
	}
	skillMdPath := filepath.Join(skillDir, "SKILL.md")
	return filelock.WriteFile(skillMdPath, []byte(content))
}

func RemoveSkill(name string) error {
	if err := validateSkillName(name); err != nil {
		return err
	}
	skillDir := filepath.Join(SkillsDir(), name)
	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		return fmt.Errorf("skill not found: %s", name)
	}
	return os.RemoveAll(skillDir)
}

func RenameSkill(oldName, newName string) error {
	if err := validateSkillName(oldName); err != nil {
		return err
	}
	if err := validateSkillName(newName); err != nil {
		return err
	}
	dir := SkillsDir()
	oldDir := filepath.Join(dir, oldName)
	newDir := filepath.Join(dir, newName)
	if _, err := os.Stat(oldDir); os.IsNotExist(err) {
		return fmt.Errorf("skill not found: %s", oldName)
	}
	if _, err := os.Stat(newDir); err == nil {
		return fmt.Errorf("skill already exists: %s", newName)
	}
	return os.Rename(oldDir, newDir)
}

func parseSkillFrontmatter(text string) (fm map[string]string, body string) {
	fm = map[string]string{}
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "---") {
		return fm, text
	}
	end := strings.Index(text[3:], "\n---")
	if end < 0 {
		return fm, text
	}
	raw := text[3 : 3+end]
	body = strings.TrimSpace(text[3+end+4:])
	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if key == "name" || key == "description" {
			fm[key] = val
		}
	}
	return fm, body
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	idx := strings.IndexAny(text, "\n\r")
	if idx > 0 {
		text = text[:idx]
	}
	runes := []rune(text)
	if len(runes) > 200 {
		text = string(runes[:200]) + "..."
	}
	return text
}

// InstallSkillResult records the list of skills landed by a single installation
type InstallSkillResult struct {
	Names        []string `json:"names"`
	Descriptions []string `json:"descriptions"`
}

// Max size for a skill download body (matches web_fetch's file download limit)
const maxSkillDownloadBytes = 10 * 1024 * 1024

// ownerRepoPattern matches the owner/repo shorthand, e.g. Tencent/WeChatReading
var ownerRepoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`)

// skillsAddPattern extracts owner/repo from a command like "npx skills add owner/repo ..."
var skillsAddPattern = regexp.MustCompile(`(?:^|\s)([A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*)(?:\s|$)`)

// normalizedSkillSource describes a normalized download source
type normalizedSkillSource struct {
	downloadURL string // The actual GET address
	isZip       bool   // Whether to treat it as a zip to unpack (codeload / release zip / Content-Type identified as zip)
	branch      string // codeload branch; empty means no fallback needed; falls back to master if main fails
}

// InstallSkill downloads and installs a skill into SkillsDir() from a GitHub repo or direct link.
// Supported inputs: the owner/repo shorthand, a full "npx skills add owner/repo -g" command,
// a full GitHub repo/subdirectory/commit URL, a raw SKILL.md direct link, or a release zip direct link.
func InstallSkill(rawURL string) (*InstallSkillResult, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("skill source is required")
	}

	src, err := normalizeSkillURL(rawURL)
	if err != nil {
		return nil, err
	}

	data, contentType, err := downloadSkillSource(src)
	if err != nil {
		return nil, err
	}

	// Decide how to handle it based on content type or source
	isZip := src.isZip || strings.HasPrefix(contentType, "application/zip") ||
		strings.HasPrefix(contentType, "application/x-zip-compressed")

	if isZip {
		return installFromZip(data)
	}

	// Text: treat it as a single SKILL.md
	if strings.HasPrefix(contentType, "text/") || strings.HasPrefix(strings.TrimSpace(string(data)), "---") {
		return installFromSingleSkillMD(data)
	}

	return nil, fmt.Errorf("unsupported skill source (content-type: %s); expected a zip archive or a SKILL.md text file", contentType)
}

// normalizeSkillURL normalizes various inputs into a download source
func normalizeSkillURL(raw string) (normalizedSkillSource, error) {
	raw = strings.TrimSpace(raw)

	// 1. A full "npx skills add owner/repo ..." command: extract owner/repo
	if strings.Contains(raw, "skills add") || strings.Contains(raw, "skills@") {
		if m := skillsAddPattern.FindStringSubmatch(raw); len(m) == 2 {
			return codeloadSource(m[1], "main"), nil
		}
	}

	// 2. owner/repo shorthand (no scheme, no dot, single /)
	if !strings.Contains(raw, "://") && !strings.Contains(raw, "//") && ownerRepoPattern.MatchString(raw) {
		return codeloadSource(raw, "main"), nil
	}

	// 3. URL with a scheme
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return normalizedSkillSource{}, fmt.Errorf("unrecognized skill source: %s", raw)
	}

	switch u.Host {
	case "github.com":
		return normalizeGitHubURL(u)
	case "raw.githubusercontent.com":
		// GET a single SKILL.md (or other text file) directly
		return normalizedSkillSource{downloadURL: u.String()}, nil
	default:
		// Other direct links (release zip, self-hosted sites, etc): GET directly, whether it's a zip is decided by Content-Type
		return normalizedSkillSource{downloadURL: u.String()}, nil
	}
}

// codeloadSource builds a codeload zip download source; branch is used for the main->master fallback
func codeloadSource(ownerRepo, branch string) normalizedSkillSource {
	return normalizedSkillSource{
		downloadURL: "https://codeload.github.com/" + ownerRepo + "/zip/refs/heads/" + branch,
		isZip:       true,
		branch:      branch,
	}
}

// normalizeGitHubURL handles the various path shapes of github.com
func normalizeGitHubURL(u *url.URL) (normalizedSkillSource, error) {
	// /owner/repo/tree/<branch> or /owner/repo/tree/<branch>/<path>
	// /owner/repo/commit/<sha>
	// /owner/repo/releases/download/<tag>/<asset>
	// /owner/repo (default branch)
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return normalizedSkillSource{}, fmt.Errorf("invalid github URL: %s", u.String())
	}
	ownerRepo := parts[0] + "/" + parts[1]

	// releases/download/<tag>/<asset>
	if len(parts) >= 6 && parts[2] == "releases" && parts[3] == "download" {
		asset := parts[5]
		// Whether it's a zip is ultimately decided by Content-Type; this only pre-guesses from the asset suffix
		return normalizedSkillSource{downloadURL: u.String(), isZip: strings.HasSuffix(asset, ".zip")}, nil
	}

	// tree/<branch>[/path] or blob/<branch>/...
	if len(parts) >= 4 && (parts[2] == "tree" || parts[2] == "blob") {
		branch := parts[3]
		if parts[2] == "blob" {
			// blob points to a single file, go through raw
			rawPath := strings.Join(parts[4:], "/")
			return normalizedSkillSource{
				downloadURL: "https://raw.githubusercontent.com/" + ownerRepo + "/" + branch + "/" + rawPath,
			}, nil
		}
		return codeloadSource(ownerRepo, branch), nil
	}

	// commit/<sha>
	if len(parts) >= 4 && parts[2] == "commit" {
		sha := parts[3]
		return normalizedSkillSource{
			downloadURL: "https://codeload.github.com/" + ownerRepo + "/zip/" + sha,
			isZip:       true,
		}, nil
	}

	// Plain repo address: defaults to main, falls back to master on failure
	return codeloadSource(ownerRepo, "main"), nil
}

// downloadSkillSource downloads a skill source, returning the bytes and Content-Type
func downloadSkillSource(src normalizedSkillSource) (data []byte, contentType string, err error) {
	u, perr := url.Parse(src.downloadURL)
	if perr != nil || u.Host == "" {
		return nil, "", fmt.Errorf("invalid download URL: %s", src.downloadURL)
	}
	if cerr := CheckHostSSRF(u.Hostname()); cerr != nil {
		return nil, "", cerr
	}

	data, contentType, err = fetchBytes(src.downloadURL)
	if err == nil {
		return data, contentType, nil
	}

	// Fall back to master when codeload's main branch returns 404
	if src.isZip && src.branch == "main" {
		ownerRepo := strings.TrimPrefix(strings.TrimPrefix(src.downloadURL, "https://codeload.github.com/"), "http://codeload.github.com/")
		ownerRepo = strings.TrimSuffix(ownerRepo, "/zip/refs/heads/main")
		fallback := codeloadSource(ownerRepo, "master")
		data, contentType, ferr := fetchBytes(fallback.downloadURL)
		if ferr != nil {
			return nil, "", fmt.Errorf("download failed (tried main and master): %v", err)
		}
		return data, contentType, nil
	}
	return nil, "", err
}

// fetchBytes performs a GET with a size limit
func fetchBytes(rawURL string) (data []byte, contentType string, err error) {
	resp, err := httpclient.NewBrowserRequest().Get(rawURL)
	if err != nil {
		return nil, "", errors.New("download failed: " + err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	contentType = resp.Header.Get("Content-Type")
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSkillDownloadBytes+1))
	if err != nil {
		return nil, "", errors.New("read body failed: " + err.Error())
	}
	if len(body) > maxSkillDownloadBytes {
		return nil, "", errors.New("skill source too large (limit 10MB)")
	}
	return body, contentType, nil
}

// installFromZip unpacks a zip and installs the skill(s) inside it
func installFromZip(data []byte) (*InstallSkillResult, error) {
	tmpRoot := filepath.Join(TempDir, "ai", "skill-install", gulu.Rand.String(7))
	if err := os.MkdirAll(tmpRoot, 0755); err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpRoot)

	zipPath := filepath.Join(tmpRoot, "src.zip")
	if err := os.WriteFile(zipPath, data, 0644); err != nil {
		return nil, err
	}
	unzipDir := filepath.Join(tmpRoot, "unzip")
	if err := os.MkdirAll(unzipDir, 0755); err != nil {
		return nil, err
	}
	// gulu.Zip.Unzip already has built-in zip-slip path traversal protection
	if err := gulu.Zip.Unzip(zipPath, unzipDir); err != nil {
		return nil, errors.New("unzip failed: " + err.Error())
	}

	skillDirs := findSkillDirs(unzipDir)
	if len(skillDirs) == 0 {
		return nil, errors.New("no SKILL.md found in the archive")
	}
	return installSkillDirs(skillDirs, unzipDir)
}

// findSkillDirs finds skill directories containing SKILL.md under the unzip root, returning paths relative to
// root.
// It recurses to tolerate arbitrary wrapper layers (codeload wraps the repo contents in <repo-name>/), but once a
// directory is recognized as a skill (directly contains SKILL.md) it stops recursing further, to avoid mistakenly
// descending into subdirectories inside the skill such as references/scripts. Recognized structures:
//   - SKILL.md directly at root (no wrapper)
//   - <wrap>/SKILL.md (a single skill with one or more wrapper layers)
//   - <wrap>/skills/<name>/SKILL.md (a collection repo, wrap is optional)
func findSkillDirs(root string) []string {
	if gulu.File.IsExist(filepath.Join(root, "SKILL.md")) {
		return []string{"."}
	}
	return findSkillDirsRecursive(root, root)
}

func findSkillDirsRecursive(dir, root string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var result []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Skip dot directories and VCS metadata to avoid pointless recursion
		name := e.Name()
		if name == ".git" || name == ".github" || name == ".idea" || name == "node_modules" {
			continue
		}
		sub := filepath.Join(dir, name)
		if gulu.File.IsExist(filepath.Join(sub, "SKILL.md")) {
			// This directory is a skill; record its relative path and stop recursing
			if rel, rerr := filepath.Rel(root, sub); rerr == nil {
				result = append(result, rel)
			}
		} else {
			// Keep recursing to handle wrapper layers / skills/ containers
			result = append(result, findSkillDirsRecursive(sub, root)...)
		}
	}
	return result
}

// installSkillDirs lands a set of skill directories (relative to root) into SkillsDir()
func installSkillDirs(relDirs []string, root string) (*InstallSkillResult, error) {
	result := &InstallSkillResult{}
	for _, rel := range relDirs {
		srcDir := filepath.Join(root, rel)
		if !gulu.File.IsSubPath(root, srcDir) {
			continue
		}
		skillMdPath := filepath.Join(srcDir, "SKILL.md")
		b, err := filelock.ReadFile(skillMdPath)
		if err != nil {
			logging.LogWarnf("read SKILL.md [%s] failed: %s", skillMdPath, err)
			continue
		}
		fm, body := parseSkillFrontmatter(string(b))
		name := fm["name"]
		if name == "" {
			// frontmatter is missing the name field: at the root-directory case there's no directory name to
			// fall back to (root is a temp directory), so just skip it; the subdirectory case falls back to the
			// directory name
			if rel == "." {
				logging.LogWarnf("skip SKILL.md at archive root without 'name' frontmatter")
				continue
			}
			name = filepath.Base(rel)
		}
		if verr := validateSkillName(name); verr != nil {
			logging.LogWarnf("skip invalid skill name [%s]: %s", name, verr)
			continue
		}

		destDir := filepath.Join(SkillsDir(), name)
		if err := os.MkdirAll(SkillsDir(), 0755); err != nil {
			return nil, err
		}
		// Overwrite install: clear the old directory first
		if gulu.File.IsExist(destDir) {
			os.RemoveAll(destDir)
		}
		if err := filelock.Copy(srcDir, destDir); err != nil {
			return nil, fmt.Errorf("install skill %s failed: %s", name, err)
		}
		result.Names = append(result.Names, name)
		desc := fm["description"]
		if desc == "" {
			desc = firstLine(body)
		}
		result.Descriptions = append(result.Descriptions, desc)
	}
	if len(result.Names) == 0 {
		return nil, errors.New("no valid skill installed")
	}
	return result, nil
}

// installFromSingleSkillMD lands the text content of a single SKILL.md as one skill
func installFromSingleSkillMD(data []byte) (*InstallSkillResult, error) {
	content := string(data)
	fm, body := parseSkillFrontmatter(content)
	name := fm["name"]
	if name == "" {
		return nil, errors.New("SKILL.md frontmatter missing 'name' field")
	}
	if err := validateSkillName(name); err != nil {
		return nil, err
	}
	if err := SaveSkill(name, content); err != nil {
		return nil, err
	}
	return &InstallSkillResult{
		Names:        []string{name},
		Descriptions: []string{firstLine(body)},
	}, nil
}
