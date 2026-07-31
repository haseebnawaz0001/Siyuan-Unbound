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
	"bytes"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/88250/gulu"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/logging"
)

var (
	SSL       = false
	UserAgent = "SiYuan/" + Ver

	// invisibleCharsReplacer is used by NormalizeEndpoint: strips zero-width characters that are easily pulled in by copy-paste.
	invisibleCharsReplacer = strings.NewReplacer(
		"\u200b", "", // Zero-width space (ZWSP)
		"\u200c", "", // Zero-width non-joiner (ZWNJ)
		"\u200d", "", // Zero-width joiner (ZWJ)
	)
)

func TrimSpaceInPath(p string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}
	return strings.Join(parts, "/")
}

func GetTreeID(treePath string) string {
	if strings.Contains(treePath, "\\") {
		return strings.TrimSuffix(filepath.Base(treePath), ".sy")
	}
	return strings.TrimSuffix(path.Base(treePath), ".sy")
}

func ShortPathForBootingDisplay(p string) string {
	if 25 > len(p) {
		return p
	}
	p = strings.TrimSuffix(p, ".sy")
	p = path.Base(p)
	return p
}

var LocalIPs []string

func GetServerAddrs() (ret []string) {
	if ContainerAndroid != Container && ContainerHarmony != Container {
		ret = GetPrivateIPv4s()
	} else {
		// net.InterfaceAddrs() doesn't work on Android/HarmonyOS https://github.com/golang/go/issues/40569, so use the localIPs parameter passed in when the kernel was started
		ret = LocalIPs
	}

	ret = append(ret, LocalHost)
	ret = gulu.Str.RemoveDuplicatedElem(ret)

	for i := range ret {
		ret[i] = "http://" + ret[i] + ":" + ServerPort
	}
	return
}

func isRunningInDockerContainer() bool {
	if _, runInContainer := os.LookupEnv("RUN_IN_CONTAINER"); runInContainer {
		return true
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	return false
}

func IsRelativePath(dest string) bool {
	if 1 > len(dest) {
		return true
	}

	if '/' == dest[0] {
		return false
	}

	// Check for specific protocol prefixes
	lowerDest := strings.ToLower(dest)
	if strings.HasPrefix(lowerDest, "mailto:") ||
		strings.HasPrefix(lowerDest, "tel:") ||
		strings.HasPrefix(lowerDest, "sms:") {
		return false
	}
	return !strings.Contains(dest, ":/") && !strings.Contains(dest, ":\\")
}

func TimeFromID(id string) (ret string) {
	if 14 > len(id) {
		logging.LogWarnf("invalid id [%s], stack [\n%s]", id, logging.ShortStack())
		return time.Now().Format("20060102150405")
	}
	ret = id[:14]
	return
}

// NodeIDByTime generates a string matching the block ID format from the given time, using the same algorithm as
// ast.NewNodeID() but with a different time source: used to backfill historical input (e.g. a quick-note temp
// file name timestamp on mobile) into a block ID.
func NodeIDByTime(t time.Time) string {
	return t.Format("20060102150405") + "-" + RandString(7)
}

func GetChildDocDepth(treeAbsPath string) (ret int) {
	dir := strings.TrimSuffix(treeAbsPath, ".sy")
	if !gulu.File.IsDir(dir) {
		return
	}

	baseDepth := strings.Count(filepath.ToSlash(treeAbsPath), "/")
	depth := 1
	filelock.Walk(dir, func(path string, d fs.DirEntry, err error) error {
		p := filepath.ToSlash(path)
		currentDepth := strings.Count(p, "/")
		if depth < currentDepth {
			depth = currentDepth
		}
		return nil
	})
	ret = depth - baseDepth
	return
}

func NormalizeConcurrentReqs(concurrentReqs int, provider int) int {
	switch provider {
	case 0: // SiYuan
		switch {
		case concurrentReqs < 1:
			concurrentReqs = 8
		case concurrentReqs > 16:
			concurrentReqs = 16
		default:
		}
	case 2: // S3
		switch {
		case concurrentReqs < 1:
			concurrentReqs = 8
		case concurrentReqs > 16:
			concurrentReqs = 16
		default:
		}
	case 3: // WebDAV
		switch {
		case concurrentReqs < 1:
			concurrentReqs = 1
		case concurrentReqs > 16:
			concurrentReqs = 16
		default:
		}
	case 4: // Local File System
		switch {
		case concurrentReqs < 1:
			concurrentReqs = 16
		case concurrentReqs > 1024:
			concurrentReqs = 1024
		default:
		}
	}
	return concurrentReqs
}

func NormalizeTimeout(timeout int) int {
	if 7 > timeout {
		if 1 > timeout {
			return 60
		}
		return 7
	}
	if 300 < timeout {
		return 300
	}
	return timeout
}

func NormalizeEndpoint(endpoint string) string {
	endpoint = invisibleCharsReplacer.Replace(endpoint)
	endpoint = strings.TrimSpace(endpoint)
	if "" == endpoint {
		return ""
	}
	endpoint = strings.Replace(endpoint, "http://http(s)://", "https://", 1)
	endpoint = strings.Replace(endpoint, "http(s)://", "https://", 1)
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "http://" + endpoint
	}
	if idx := strings.Index(endpoint, "://"); 0 <= idx {
		head := endpoint[:idx+len("://")]
		tail := endpoint[idx+len("://"):]
		for strings.Contains(tail, "//") {
			tail = strings.ReplaceAll(tail, "//", "/")
		}
		endpoint = head + tail
	}
	endpoint = strings.TrimSpace(endpoint)
	if !strings.HasSuffix(endpoint, "/") {
		endpoint = endpoint + "/"
	}
	return endpoint
}

func NormalizeLocalPath(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if "" == endpoint {
		return ""
	}
	endpoint = filepath.ToSlash(filepath.Clean(endpoint))
	if !strings.HasSuffix(endpoint, "/") {
		endpoint = endpoint + "/"
	}
	return endpoint
}

func FilterMoveDocFromPaths(fromPaths []string, toPath string) (ret []string) {
	tmp := FilterSelfChildDocs(fromPaths)
	for _, fromPath := range tmp {
		fromDir := strings.TrimSuffix(fromPath, ".sy")
		if strings.HasPrefix(toPath, fromDir) {
			continue
		}
		ret = append(ret, fromPath)
	}
	return
}

func FilterSelfChildDocs(paths []string) (ret []string) {
	sort.Slice(paths, func(i, j int) bool { return strings.Count(paths[i], "/") < strings.Count(paths[j], "/") })

	dirs := map[string]string{}
	for _, fromPath := range paths {
		dir := strings.TrimSuffix(fromPath, ".sy")
		existParent := false
		for d := range dirs {
			if strings.HasPrefix(fromPath, d) {
				existParent = true
				break
			}
		}
		if existParent {
			continue
		}
		dirs[dir] = fromPath
		ret = append(ret, fromPath)
	}
	return
}

// FileURLToLocalPath converts a file:// URL into a local file path.
func FileURLToLocalPath(fileURL string) string {
	if len(fileURL) < 7 || strings.ToLower(fileURL[:7]) != "file://" {
		return ""
	}
	p := fileURL[7:]
	if gulu.OS.IsWindows() && strings.Contains(p, ":") {
		// Windows supports file:// followed by multiple slashes https://github.com/siyuan-note/siyuan/issues/11885
		p = strings.TrimLeft(p, "/")
	}
	if strings.Contains(p, "?") {
		// Strip query parameters https://github.com/siyuan-note/siyuan/issues/13600
		p = p[:strings.Index(p, "?")]
	}
	if unescaped, err := url.PathUnescape(p); err == nil && unescaped != p {
		// `Convert network images/assets to local` supports URL-encoded local file names https://github.com/siyuan-note/siyuan/issues/9929
		p = unescaped
	}
	return p
}

func IsAssetLinkDest(dest []byte, includeServePath bool) bool {
	return bytes.HasPrefix(dest, []byte("assets/")) ||
		(includeServePath && (bytes.HasPrefix(dest, []byte("emojis/")) ||
			bytes.HasPrefix(dest, []byte("plugins/")) ||
			bytes.HasPrefix(dest, []byte("public/")) ||
			bytes.HasPrefix(dest, []byte("widgets/"))))
}

var (
	SiYuanAssetsImage = []string{".apng", ".ico", ".cur", ".jpg", ".jpe", ".jpeg", ".jfif", ".pjp", ".pjpeg", ".png", ".gif", ".webp", ".bmp", ".svg", ".avif"}
	SiYuanAssetsAudio = []string{".mp3", ".wav", ".ogg", ".m4a", ".flac"}
	SiYuanAssetsVideo = []string{".mov", ".weba", ".mkv", ".mp4", ".webm"}
)

// IsPossiblyImage makes a fuzzy guess at whether a given file link could be an image.
func IsPossiblyImage(assetPath string) bool {
	ext := strings.ToLower(filepath.Ext(assetPath))
	if "" != ext {
		return gulu.Str.Contains(ext, SiYuanAssetsImage)
	}

	if strings.HasPrefix(assetPath, "https://") || strings.HasPrefix(assetPath, "http://") {
		// A network image link doesn't necessarily have an extension
		return true
	}

	if filePath := FileURLToLocalPath(assetPath); filePath != "" {
		m, ok := GetMimeTypeByPath(filePath)
		if !ok {
			return false
		}
		return gulu.Str.Contains(m.Extension(), SiYuanAssetsImage)
	}

	if IsAssetLinkDest([]byte(assetPath), true) {
		filePath := filepath.Join(DataDir, assetPath)
		m, ok := GetMimeTypeByPath(filePath)
		if !ok {
			return false
		}
		return gulu.Str.Contains(m.Extension(), SiYuanAssetsImage)
	}
	return false
}

func IsDisplayableAsset(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	if "" == ext {
		return false
	}
	if gulu.Str.Contains(ext, SiYuanAssetsImage) {
		return true
	}
	if gulu.Str.Contains(ext, SiYuanAssetsAudio) {
		return true
	}
	if gulu.Str.Contains(ext, SiYuanAssetsVideo) {
		return true
	}
	return false
}

func GetAbsPathInWorkspace(relPath string) (string, error) {
	absPath := filepath.Join(WorkspaceDir, relPath)
	absPath = filepath.Clean(absPath)
	if WorkspaceDir == absPath {
		return absPath, nil
	}

	if gulu.File.IsSubPath(WorkspaceDir, absPath) {
		return absPath, nil
	}
	return "", os.ErrPermission
}

func IsAbsPathInWorkspace(absPath string) bool {
	return gulu.File.IsSubPath(WorkspaceDir, absPath)
}

// IsWorkspaceDir determines whether the given directory is a workspace directory.
func IsWorkspaceDir(dir string) bool {
	conf := filepath.Join(dir, "conf", "conf.json")
	data, err := os.ReadFile(conf)
	if nil != err {
		return false
	}
	return strings.Contains(string(data), "kernelVersion")
}

// IsPartitionRootPath checks if the given path is a partition root path.
func IsPartitionRootPath(path string) bool {
	if path == "" {
		return false
	}

	// Clean the path to remove any trailing slashes
	cleanPath := filepath.Clean(path)

	// Check if the path is the root path based on the operating system
	if runtime.GOOS == "windows" {
		// On Windows, root paths are like "C:\", "D:\", etc.
		return len(cleanPath) == 3 && cleanPath[1] == ':' && cleanPath[2] == '\\'
	}

	// On Unix-like systems, the root path is "/"
	return cleanPath == "/"
}

// IsSensitivePath performs unified sensitivity detection on the given path.
//
// To prevent bypassing the blacklist via symlinks, paths outside the workspace are additionally checked again
// after resolving symlinks: this is the attack surface for interfaces like globalCopyFiles that accept absolute
// paths outside the workspace. Paths inside the workspace do not resolve symlinks, for two reasons: first,
// in-workspace files (e.g. symlinks under assets pointing to external directories) may legitimately point outside
// the workspace, and running the system directory prefix check after resolving them would cause false positives;
// second, to avoid the extra stat overhead on the high-QPS serving hot path.
// When resolution fails (e.g. the path doesn't exist), it falls back to checking only the original path.
func IsSensitivePath(p string) bool {
	if p == "" {
		return false
	}
	if isSensitivePath(p) {
		return true
	}
	// Only resolve symlinks for paths outside the workspace, to prevent using a symlink to bypass the blacklist and point at a sensitive target.
	if gulu.File.IsSubPath(WorkspaceDir, p) {
		return false
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err == nil && resolved != p {
		if isSensitivePath(resolved) {
			return true
		}
	}
	return false
}

// isSensitivePath performs the actual sensitivity blacklist matching, without resolving symlinks.
func isSensitivePath(p string) bool {
	toCheckPathLower := filepath.Clean(strings.ToLower(p))
	toCheckNameLower := filepath.Base(toCheckPathLower)

	// The system directory prefix check only applies to paths outside the workspace.
	// In-workspace paths passed in by callers (e.g. assets, export) have already been validated with
	// IsSubPath(WorkspaceDir); the workspace can never be located under system-sensitive directories like /etc or
	// /var/log. Meanwhile, legitimate data paths on sandboxed platforms like iOS happen to start with /var
	// (/var/mobile/Containers/Data/Application/...), so running the system directory prefix check on in-workspace
	// paths would wrongly flag normal assets/export files on iOS as sensitive, causing the server to return 403.
	if !gulu.File.IsSubPath(WorkspaceDir, p) {
		// Sensitive directory prefixes (UNIX style)
		prefixes := []string{
			"/.",
			"/etc",
			"/root",
			"/var",
			"/proc",
			"/sys",
			"/run",
			"/bin",
			"/boot",
			"/dev",
			"/lib",
			"/srv",
			"/tmp",
			"/usr",
			"/opt",
			"/sbin",
		}
		for _, pre := range prefixes {
			if strings.HasPrefix(toCheckPathLower, pre) {
				return true
			}
		}

		// Common sensitive directories on Windows (case-insensitive comparison)
		winPrefixes := []string{
			`c:\windows\system32`,
			`c:\windows\system`,
		}
		for _, wp := range winPrefixes {
			if strings.HasPrefix(toCheckPathLower, strings.ToLower(wp)) {
				return true
			}
		}

		// Windows Start Menu paths (case-insensitive comparison)
		startMenuPrefixes := []string{
			strings.ToLower(filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu")),
			strings.ToLower(filepath.Join(os.Getenv("ProgramData"), "Microsoft", "Windows", "Start Menu")),
		}
		for _, sp := range startMenuPrefixes {
			if strings.HasPrefix(toCheckPathLower, sp) {
				return true
			}
		}
	}

	// The workspace/conf directory (case-insensitive comparison)
	workspaceConfPrefix := strings.ToLower(filepath.Join(WorkspaceDir, "conf"))
	if strings.HasPrefix(toCheckPathLower, workspaceConfPrefix) {
		return true
	}

	// Only allow exporting the workspace/temp/export directory, not the workspace/temp directory (case-insensitive comparison)
	workspaceTempExportPrefix := strings.ToLower(filepath.Join(WorkspaceDir, "temp", "export"))
	workspaceTempPrefix := strings.ToLower(filepath.Join(WorkspaceDir, "temp"))
	if strings.HasPrefix(toCheckPathLower, workspaceTempPrefix) && !strings.HasPrefix(toCheckPathLower, workspaceTempExportPrefix) {
		return true
	}

	// Sensitive directories and credential files under the user home directory (case-insensitive comparison).
	// Covers common credential dotfiles, to prevent leaking credentials from the kernel user's home directory by
	// copying them into the workspace via interfaces like globalCopyFiles that accept absolute paths outside the
	// workspace: Git push tokens, HTTP/API credentials, Postgres passwords, K8s/Docker/container registry config,
	// GPG private keyrings, cloud provider CLI credentials, package manager tokens, etc.
	homePrefixes := []string{
		strings.ToLower(filepath.Join(HomeDir, ".ssh")),
		strings.ToLower(filepath.Join(HomeDir, ".config")),
		strings.ToLower(filepath.Join(HomeDir, ".bashrc")),
		strings.ToLower(filepath.Join(HomeDir, ".zshrc")),
		strings.ToLower(filepath.Join(HomeDir, ".profile")),
		strings.ToLower(filepath.Join(HomeDir, ".git-credentials")),
		strings.ToLower(filepath.Join(HomeDir, ".netrc")),
		strings.ToLower(filepath.Join(HomeDir, ".pgpass")),
		strings.ToLower(filepath.Join(HomeDir, ".kube")),
		strings.ToLower(filepath.Join(HomeDir, ".docker")),
		strings.ToLower(filepath.Join(HomeDir, ".gnupg")),
		strings.ToLower(filepath.Join(HomeDir, ".aws")),
		strings.ToLower(filepath.Join(HomeDir, ".azure")),
		strings.ToLower(filepath.Join(HomeDir, ".npmrc")),
		strings.ToLower(filepath.Join(HomeDir, ".pypirc")),
	}
	for _, hp := range homePrefixes {
		if strings.HasPrefix(toCheckPathLower, hp) {
			return true
		}
	}

	// Specific file name prefixes (case-insensitive comparison)
	namePrefixes := []string{
		strings.ToLower("credentials"),
		strings.ToLower("id_"),
	}
	for _, np := range namePrefixes {
		if strings.HasPrefix(toCheckNameLower, np) {
			return true
		}
	}
	return false
}

// ResolveLongestExistingParent resolves the symlink of the longest existing portion of absPath, then rejoins the
// remaining path.
// For example, absPath = /workspace/data/link/newdir/file, where /workspace/data/link is a symlink pointing to
// /workspace/data/<encBoxID>/ and newdir/file does not yet exist:
// it returns /workspace/data/<encBoxID>/newdir/file.
func ResolveLongestExistingParent(absPath string) string {
	cleaned := filepath.Clean(absPath)
	dir := cleaned
	for {
		if _, err := os.Lstat(dir); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cleaned
		}
		dir = parent
	}
	if dir == cleaned {
		if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
			return resolved
		}
		return cleaned
	}
	if dir == "/" || dir == "." {
		return cleaned
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return cleaned
	}
	remaining := strings.TrimPrefix(cleaned, dir)
	return resolvedDir + remaining
}
