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
	"flag"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/88250/go-humanize"
	"github.com/88250/gulu"
	figure "github.com/common-nighthawk/go-figure"
	"github.com/gofrs/flock"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/httpclient"
	"github.com/siyuan-note/logging"
	"golang.org/x/mod/semver"
)

// var Mode = "dev"
var Mode = "prod"

const (
	Ver       = "3.7.3"
	IsInsider = false
)

// IsReleaseVer determines whether ver is a formal release (no alpha, beta, rc, or other pre-release identifiers).
func IsReleaseVer(ver string) bool {
	v := "v" + strings.TrimPrefix(ver, "v")
	return semver.IsValid(v) && semver.Prerelease(v) == ""
}

var (
	RunInContainer             = false // Whether running inside a container
	SiYuanAccessAuthCodeBypass = false // Whether to skip the empty lock-screen password check
	AttachUI                   = false // Whether to bind to the desktop UI process lifecycle (true when launched by Electron, false for manual serve)
)

func initEnvVars() {
	RunInContainer = isRunningInDockerContainer()
	var err error
	if SiYuanAccessAuthCodeBypass, err = strconv.ParseBool(os.Getenv("SIYUAN_ACCESS_AUTH_CODE_BYPASS")); err != nil {
		SiYuanAccessAuthCodeBypass = false
	}
}

var (
	bootProgress = atomic.Int32{} // Boot progress, from 0 to 100
	bootDetails  string           // Boot detail description
	HttpServer   *http.Server     // HTTP server instance
	HttpServing  = false          // Whether HTTP serving is already available

	SafeMode = false // Whether booted in safe mode: disables snippets, plugins, custom themes and icons
)

// If a commandline parameter is empty, fallback to the env var.
//
// "empty" means the parameter is not set or set to an empty string.
// It returns a pointer to string, to be a drop-in replacement for
// the commandline parameter itself.
func coalesceToEnvVar(fromCLI *string, envVarName string) *string {
	if fromCLI == nil || "" == *fromCLI {
		ret := os.Getenv(envVarName)
		return &ret
	}
	return fromCLI
}

func InitWorkspace(workspacePath, wdPath string) {
	initEnvVars()
	initMime()
	initHttpClient()

	if "" != wdPath {
		WorkingDir = wdPath
	}

	Container = ContainerStd
	if RunInContainer {
		Container = ContainerDocker
	}

	initWorkspaceDir(workspacePath)
	initPathDir()

	AppearancePath = filepath.Join(ConfDir, "appearance")
	if "dev" == Mode {
		ThemesPath = filepath.Join(WorkingDir, "appearance", "themes")
		IconsPath = filepath.Join(WorkingDir, "appearance", "icons")
	} else {
		ThemesPath = filepath.Join(AppearancePath, "themes")
		IconsPath = filepath.Join(AppearancePath, "icons")
	}

	LogPath = filepath.Join(TempDir, "siyuan.log")
}

func Boot() {
	IncBootProgress(3, BootL10n(299, "Booting kernel..."))

	// Parse os.Args with the standard library flag package, then go through the unified BootWithFlags.
	workspacePath := flag.String("workspace", "", "dir path of the workspace, default to ~/SiYuan/")
	wdPath := flag.String("wd", WorkingDir, "working directory of SiYuan")
	port := flag.String("port", "0", "port of the HTTP server")
	readOnly := flag.String("readonly", "false", "read-only mode")
	accessAuthCode := flag.String("accessAuthCode", "", "access auth code")
	ssl := flag.Bool("ssl", false, "for https and wss")
	attachUI := flag.Bool("attach-ui", false, "attach kernel lifecycle to desktop UI process (used by Electron)")
	lang := flag.String("lang", "", "ar/de/en/es/fr/he/hi/id/it/ja/ko/nl/pl/pt-BR/ru/sk/th/tr/uk/zh-CN/zh-TW")
	mode := flag.String("mode", "prod", "dev/prod")
	safeMode := flag.Bool("safe-mode", false, "boot in safe mode")
	flag.Parse()

	BootWithFlags(*workspacePath, *wdPath, *port, *readOnly, *accessAuthCode, *lang, *mode, *ssl, *attachUI, *safeMode)
}

// BootWithFlags takes already-parsed boot parameters and finishes the boot sequence: env var fallback, global
// variable assignment, workspace initialization, and locking. Both Boot() (standard library flag parsing) and the
// serve subcommand (cobra parsing) go through this unified entry point.
func BootWithFlags(workspacePath, wdPath, port, readOnly, accessAuthCode, lang, mode string, ssl, attachUI, safeMode bool) {
	SafeMode = safeMode
	// Fallback to env vars if commandline args are not set
	// valid only for CLI args that default to "", as the
	// others have explicit (sane) defaults
	workspacePath = *coalesceToEnvVar(&workspacePath, "SIYUAN_WORKSPACE_PATH")
	accessAuthCode = *coalesceToEnvVar(&accessAuthCode, "SIYUAN_ACCESS_AUTH_CODE")
	lang = *coalesceToEnvVar(&lang, "SIYUAN_LANG")

	if "" != lang {
		Lang = LangToBCP47(lang) // Compatible with legacy underscore values, e.g. zh_CN -> zh-CN
	}
	Mode = mode
	ServerPort = port
	ReadOnly, _ = strconv.ParseBool(readOnly)
	AttachUI = attachUI
	AccessAuthCode = accessAuthCode
	AccessAuthCode = RemoveInvalid(AccessAuthCode)
	AccessAuthCode = strings.TrimSpace(AccessAuthCode)
	Container = ContainerStd
	if RunInContainer {
		Container = ContainerDocker
		if "" == AccessAuthCode { // Still empty?
			interruptBoot := true

			// Set the env `SIYUAN_ACCESS_AUTH_CODE_BYPASS=true` to skip checking empty access auth code https://github.com/siyuan-note/siyuan/issues/9709
			if SiYuanAccessAuthCodeBypass {
				interruptBoot = false
				fmt.Println("bypass access auth code check since the env [SIYUAN_ACCESS_AUTH_CODE_BYPASS] is set to [true]")
			}

			if interruptBoot {
				// The access authorization code command line parameter must be set when deploying via Docker https://github.com/siyuan-note/siyuan/issues/9328
				fmt.Printf("the access authorization code command line parameter (--accessAuthCode) must be set when deploying via Docker\n")
				fmt.Printf("or you can set the SIYUAN_ACCESS_AUTH_CODE env var")
				os.Exit(logging.ExitCodeSecurityRisk)
			}
		}
	}
	if ContainerStd != Container {
		ServerPort = FixedPort
	}

	UserAgent = UserAgent + " " + Container + "/" + runtime.GOOS
	httpclient.SetUserAgent(UserAgent)

	InitWorkspace(workspacePath, wdPath)

	// Must run after InitWorkspace: only then has WorkingDir been corrected to the real working directory (e.g.
	// app\resources) by the --wd flag; otherwise the process CWD would be used and misjudge the Microsoft Store marker file
	msStoreFilePath := filepath.Join(WorkingDir, "ms-store")
	ISMicrosoftStore = gulu.File.IsExist(msStoreFilePath)

	SSL = ssl
	logging.SetLogPath(LogPath)

	// A workspace can only be served by one kernel process
	tryLockWorkspace()

	bootBanner := figure.NewColorFigure("SiYuan", "isometric3", "green", true)
	logging.LogInfo("\n" + bootBanner.String())
	logBootInfo()
}

var bootDetailsLock = sync.Mutex{}

func setBootDetails(details string) {
	bootDetailsLock.Lock()
	bootDetails = "v" + Ver + " " + details
	bootDetailsLock.Unlock()
}

func SetBootDetails(details string) {
	if 100 <= bootProgress.Load() {
		return
	}
	setBootDetails(details)
}

// BootL10n returns the localized string for boot progress text.
//
// It looks up the integer key in the _kernel block of util.Langs using the current UI language (util.Lang, from
// conf.json), then falls back to English, and finally falls back to the caller-supplied English fallback text.
// This way the original text can still be shown during the first boot before InitConf() has finished loading the
// language file, avoiding an empty string.
func BootL10n(num int, fallback string) string {
	if s := Langs[Lang][num]; "" != s {
		return s
	}
	if s := Langs["en"][num]; "" != s {
		return s
	}
	return fallback
}

func IncBootProgress(progress int32, details string) {
	if 100 <= bootProgress.Load() {
		return
	}
	bootProgress.Add(progress)
	setBootDetails(details)
}

func IsBooted() bool {
	return 100 <= bootProgress.Load()
}

func GetBootProgressDetails() (progress int32, details string) {
	progress = bootProgress.Load()
	bootDetailsLock.Lock()
	details = bootDetails
	bootDetailsLock.Unlock()
	return
}

func GetBootProgress() int32 {
	return bootProgress.Load()
}

func SetBooted() {
	// Set progress to 100 before writing details, guaranteeing that once the frontend polling/SSE reads
	// progress>=100 the redirect condition is always satisfied, avoiding the race window where "details is
	// written before progress" produces "Finishing boot... but progress isn't full yet"
	bootProgress.Store(100)
	setBootDetails(BootL10n(300, "Finishing boot..."))
	logging.LogInfof("kernel booted")
}

var (
	HomeDir, _    = gulu.OS.Home()
	WorkingDir, _ = os.Getwd()

	WorkspaceDir       string        // Workspace directory path
	WorkspaceName      string        // Workspace name
	WorkspaceLock      *flock.Flock  // Workspace lock
	ConfDir            string        // Configuration directory path
	DataDir            string        // Data directory path
	RepoDir            string        // Repository directory path
	HistoryDir         string        // Data history directory path
	TempDir            string        // Temp directory path
	QueueDir           string        // Queue directory path
	LogPath            string        // Path to the siyuan.log file under the conf directory
	DBName             = "siyuan.db" // SQLite database file name
	DBPath             string        // SQLite database file path
	HistoryDBPath      string        // SQLite history database file path
	AssetContentDBPath string        // SQLite asset content database file path
	BlockTreeDBPath    string        // Block tree database file path
	AppearancePath     string        // Path to the appearance/ directory under the conf directory
	ThemesPath         string        // Path to the themes/ directory under the appearance directory
	IconsPath          string        // Path to the icons/ directory under the appearance directory
	SnippetsPath       string        // Path to the snippets/ directory under the data directory
	ShortcutsPath      string        // Path to the shortcuts directory under the user home directory, home/.config/siyuan/shortcuts/

	UIProcessIDs = sync.Map{} // UI process IDs
)

func initWorkspaceDir(workspaceArg string) {
	userHomeConfDir := filepath.Join(HomeDir, ".config", "siyuan")
	workspaceConf := filepath.Join(userHomeConfDir, "workspace.json")
	logging.SetLogPath(filepath.Join(userHomeConfDir, "kernel.log"))

	if !gulu.File.IsExist(workspaceConf) {
		if err := os.MkdirAll(userHomeConfDir, 0755); err != nil && !os.IsExist(err) {
			logging.LogErrorf("create user home conf folder [%s] failed: %s", userHomeConfDir, err)
			os.Exit(logging.ExitCodeInitWorkspaceErr)
		}
	}

	defaultWorkspaceDir := filepath.Join(HomeDir, "SiYuan")
	if gulu.OS.IsWindows() {
		// Improve the default workspace path on Windows https://github.com/siyuan-note/siyuan/issues/5622
		if userProfile := os.Getenv("USERPROFILE"); "" != userProfile {
			defaultWorkspaceDir = filepath.Join(userProfile, "SiYuan")
		}
	} else if gulu.OS.IsDarwin() {
		// Change the initial workspace path to ~/Library/Application Support/SiYuan on macOS https://github.com/siyuan-note/siyuan/issues/17095
		defaultWorkspaceDir = filepath.Join(HomeDir, "Library", "Application Support", "SiYuan")
	}

	var workspacePaths []string
	if !gulu.File.IsExist(workspaceConf) {
		WorkspaceDir = defaultWorkspaceDir
	} else {
		workspacePaths, _ = ReadWorkspacePaths()
		if 0 < len(workspacePaths) {
			WorkspaceDir = workspacePaths[len(workspacePaths)-1]
		} else {
			WorkspaceDir = defaultWorkspaceDir
		}
	}

	if "" != workspaceArg {
		WorkspaceDir = workspaceArg
	}

	// Normalize the path separator so WorkspaceDir stays consistent with directories derived via
	// filepath.Join(WorkspaceDir, ...) (HistoryDir/DataDir, etc). Otherwise, when starting on Windows with forward
	// slashes (--workspace="D:/foo"), strings.TrimPrefix(path, util.WorkspaceDir) would fail due to the separator
	// mismatch https://github.com/siyuan-note/siyuan/issues/17862
	WorkspaceDir = filepath.Clean(WorkspaceDir)

	if !gulu.File.IsDir(WorkspaceDir) {
		logging.LogWarnf("use the default workspace [%s] since the specified workspace [%s] is not a dir", defaultWorkspaceDir, WorkspaceDir)
		if err := os.MkdirAll(defaultWorkspaceDir, 0755); err != nil && !os.IsExist(err) {
			logging.LogErrorf("create default workspace folder [%s] failed: %s", defaultWorkspaceDir, err)
			os.Exit(logging.ExitCodeInitWorkspaceErr)
		}
		WorkspaceDir = defaultWorkspaceDir
	}
	workspacePaths = append(workspacePaths, WorkspaceDir)

	if err := WriteWorkspacePaths(workspacePaths); err != nil {
		logging.LogErrorf("write workspace conf [%s] failed: %s", workspaceConf, err)
		os.Exit(logging.ExitCodeInitWorkspaceErr)
	}

	WorkspaceName = filepath.Base(WorkspaceDir)
	ConfDir = filepath.Join(WorkspaceDir, "conf")
	DataDir = filepath.Join(WorkspaceDir, "data")
	RepoDir = filepath.Join(WorkspaceDir, "repo")
	HistoryDir = filepath.Join(WorkspaceDir, "history")
	TempDir = filepath.Join(WorkspaceDir, "temp")
	QueueDir = filepath.Join(TempDir, "queue")
	osTmpDir := filepath.Join(TempDir, "os")
	os.RemoveAll(osTmpDir)
	if err := os.MkdirAll(osTmpDir, 0755); err != nil {
		logging.LogErrorf("create os tmp dir [%s] failed: %s", osTmpDir, err)
		os.Exit(logging.ExitCodeInitWorkspaceErr)
	}
	os.RemoveAll(filepath.Join(TempDir, "repo"))
	// The export directory only holds temp files, cleared on every boot; plugins must not rely on files in it
	// surviving across processes.
	os.RemoveAll(filepath.Join(TempDir, "export"))
	os.Setenv("TMPDIR", osTmpDir)
	os.Setenv("TEMP", osTmpDir)
	os.Setenv("TMP", osTmpDir)
	DBPath = filepath.Join(TempDir, DBName)
	HistoryDBPath = filepath.Join(TempDir, "history.db")
	AssetContentDBPath = filepath.Join(TempDir, "asset_content.db")
	BlockTreeDBPath = filepath.Join(TempDir, "blocktree.db")
	SnippetsPath = filepath.Join(DataDir, "snippets")
	ShortcutsPath = filepath.Join(userHomeConfDir, "shortcuts")
}

func DeduplicateWorkspacePaths(paths []string) []string {
	if !gulu.OS.IsWindows() {
		return gulu.Str.RemoveDuplicatedElem(paths)
	}
	seen := map[string]bool{}
	var result []string
	for _, p := range paths {
		key := strings.ToLower(filepath.Clean(p)) // Normalize before deduping so D:/foo, D:\foo, D:\foo\, etc. are recognized as the same workspace https://github.com/siyuan-note/siyuan/issues/17862
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, p)
	}
	return result
}

func RemoveWorkspacePath(paths []string, target string) []string {
	if !gulu.OS.IsWindows() {
		return gulu.Str.RemoveElem(paths, target)
	}
	targetLower := strings.ToLower(target)
	var result []string
	for _, p := range paths {
		if strings.ToLower(p) == targetLower {
			continue
		}
		result = append(result, p)
	}
	return result
}

func ReadWorkspacePaths() (ret []string, err error) {
	ret = []string{}
	workspaceConf := filepath.Join(HomeDir, ".config", "siyuan", "workspace.json")
	data, err := os.ReadFile(workspaceConf)
	if err != nil {
		msg := fmt.Sprintf("read workspace conf [%s] failed: %s", workspaceConf, err)
		logging.LogError(msg)
		err = errors.New(msg)
		return
	}

	if err = gulu.JSON.UnmarshalJSON(data, &ret); err != nil {
		msg := fmt.Sprintf("unmarshal workspace conf [%s] failed: %s", workspaceConf, err)
		logging.LogError(msg)
		err = errors.New(msg)
		return
	}

	var tmp []string
	workspaceBaseDir := filepath.Dir(HomeDir)
	for _, d := range ret {
		if ContainerIOS == Container && strings.Contains(d, "/Documents/") {
			// The iOS sandbox path can change, so convert it to a relative path and rejoin it with the workspace base path in the current sandbox
			d = d[strings.Index(d, "/Documents/")+len("/Documents/"):]
			d = filepath.Join(workspaceBaseDir, d)
		}

		d = strings.TrimRight(d, " \t\n") // Trim trailing whitespace from the workspace path https://github.com/siyuan-note/siyuan/issues/6353
		d = filepath.Clean(d)             // Normalize the path separator, cleaning up legacy persisted slash differences (e.g. D:/foo vs D:\foo) https://github.com/siyuan-note/siyuan/issues/17862
		if gulu.File.IsDir(d) {
			tmp = append(tmp, d)
		} else {
			logging.LogWarnf("workspace path [%s] is not a dir", d)
		}
	}
	ret = tmp
	ret = DeduplicateWorkspacePaths(ret)
	return
}

func WriteWorkspacePaths(workspacePaths []string) (err error) {
	workspacePaths = DeduplicateWorkspacePaths(workspacePaths)
	workspaceConf := filepath.Join(HomeDir, ".config", "siyuan", "workspace.json")
	data, err := gulu.JSON.MarshalJSON(workspacePaths)
	if err != nil {
		msg := fmt.Sprintf("marshal workspace conf [%s] failed: %s", workspaceConf, err)
		logging.LogError(msg)
		err = errors.New(msg)
		return
	}

	if err = filelock.WriteFile(workspaceConf, data); err != nil {
		msg := fmt.Sprintf("write workspace conf [%s] failed: %s", workspaceConf, err)
		logging.LogError(msg)
		err = errors.New(msg)
		return
	}
	return
}

var (
	ServerURL  *url.URL // Kernel service URL
	ServerPort = "0"    // HTTP/WebSocket port, 0 means use a random port

	ReadOnly       bool
	AccessAuthCode string
	Lang           = ""

	Container        string // docker, android, ios, harmony, std
	ISMicrosoftStore bool   // Whether the desktop client is the Microsoft Store edition
)

const (
	ContainerStd     = "std"     // Desktop client
	ContainerDocker  = "docker"  // Docker container client
	ContainerAndroid = "android" // Android client
	ContainerIOS     = "ios"     // iOS client
	ContainerHarmony = "harmony" // HarmonyOS client

	LocalHost = "127.0.0.1" // Serving address
	FixedPort = "6806"      // Fixed port
)

// IsMobileContainer reports whether the kernel is currently running on an Android, iOS, or HarmonyOS client.
func IsMobileContainer() bool {
	return ContainerAndroid == Container || ContainerIOS == Container || ContainerHarmony == Container
}

func initPathDir() {
	if err := os.MkdirAll(ConfDir, 0755); err != nil && !os.IsExist(err) {
		logging.LogFatalf(logging.ExitCodeInitWorkspaceErr, "create conf folder [%s] failed: %s", ConfDir, err)
	}
	if err := os.MkdirAll(DataDir, 0755); err != nil && !os.IsExist(err) {
		logging.LogFatalf(logging.ExitCodeInitWorkspaceErr, "create data folder [%s] failed: %s", DataDir, err)
	}
	if err := os.MkdirAll(TempDir, 0755); err != nil && !os.IsExist(err) {
		logging.LogFatalf(logging.ExitCodeInitWorkspaceErr, "create temp folder [%s] failed: %s", TempDir, err)
	}

	assets := filepath.Join(DataDir, "assets")
	if err := os.MkdirAll(assets, 0755); err != nil && !os.IsExist(err) {
		logging.LogFatalf(logging.ExitCodeInitWorkspaceErr, "create data assets folder [%s] failed: %s", assets, err)
	}

	templates := filepath.Join(DataDir, "templates")
	if err := os.MkdirAll(templates, 0755); err != nil && !os.IsExist(err) {
		logging.LogFatalf(logging.ExitCodeInitWorkspaceErr, "create data templates folder [%s] failed: %s", templates, err)
	}

	widgets := filepath.Join(DataDir, "widgets")
	if err := os.MkdirAll(widgets, 0755); err != nil && !os.IsExist(err) {
		logging.LogFatalf(logging.ExitCodeInitWorkspaceErr, "create data widgets folder [%s] failed: %s", widgets, err)
	}

	plugins := filepath.Join(DataDir, "plugins")
	if err := os.MkdirAll(plugins, 0755); err != nil && !os.IsExist(err) {
		logging.LogFatalf(logging.ExitCodeInitWorkspaceErr, "create data plugins folder [%s] failed: %s", plugins, err)
	}

	emojis := filepath.Join(DataDir, "emojis")
	if err := os.MkdirAll(emojis, 0755); err != nil && !os.IsExist(err) {
		logging.LogFatalf(logging.ExitCodeInitWorkspaceErr, "create data emojis folder [%s] failed: %s", emojis, err)
	}

	queueDir := filepath.Join(TempDir, "queue")
	if err := os.MkdirAll(queueDir, 0755); err != nil && !os.IsExist(err) {
		logging.LogFatalf(logging.ExitCodeInitWorkspaceErr, "create queue folder [%s] failed: %s", queueDir, err)
	}

	// Support directly access `data/public/*` contents via URL link https://github.com/siyuan-note/siyuan/issues/8593
	public := filepath.Join(DataDir, "public")
	if err := os.MkdirAll(public, 0755); err != nil && !os.IsExist(err) {
		logging.LogFatalf(logging.ExitCodeInitWorkspaceErr, "create data public folder [%s] failed: %s", public, err)
	}
}

func initMime() {
	// UI style rendering issue on some Windows 10 versions
	// https://github.com/siyuan-note/siyuan/issues/247
	// https://github.com/siyuan-note/siyuan/issues/3813
	mime.AddExtensionType(".css", "text/css")
	mime.AddExtensionType(".js", "text/javascript")
	mime.AddExtensionType(".mjs", "text/javascript")
	mime.AddExtensionType(".html", "text/html")
	mime.AddExtensionType(".json", "application/json")
	mime.AddExtensionType(".woff2", "font/woff2")

	// On some systems, downloaded asset files open as zip https://github.com/siyuan-note/siyuan/issues/6347
	mime.AddExtensionType(".doc", "application/msword")
	mime.AddExtensionType(".docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	mime.AddExtensionType(".xls", "application/vnd.ms-excel")
	mime.AddExtensionType(".xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	mime.AddExtensionType(".dwg", "image/x-dwg")
	mime.AddExtensionType(".dxf", "image/x-dxf")
	mime.AddExtensionType(".dwf", "drawing/x-dwf")
	mime.AddExtensionType(".pdf", "application/pdf")

	// SVG images cannot be displayed on some systems https://github.com/siyuan-note/siyuan/issues/9413
	mime.AddExtensionType(".svg", "image/svg+xml")

	// Document data file
	mime.AddExtensionType(".sy", "application/json")

	mime.AddExtensionType(".md", "text/markdown")
	mime.AddExtensionType(".markdown", "text/markdown")

	// Add common image formats
	mime.AddExtensionType(".png", "image/png")
	mime.AddExtensionType(".jpg", "image/jpeg")
	mime.AddExtensionType(".jpeg", "image/jpeg")
	mime.AddExtensionType(".gif", "image/gif")
	mime.AddExtensionType(".bmp", "image/bmp")
	mime.AddExtensionType(".tiff", "image/tiff")
	mime.AddExtensionType(".tif", "image/tiff")
	mime.AddExtensionType(".webp", "image/webp")
	mime.AddExtensionType(".ico", "image/x-icon")
}

func GetDataAssetsAbsPath() (ret string) {
	ret = filepath.Join(DataDir, "assets")
	if IsSymlinkPath(ret) {
		// Follow symlinks https://github.com/siyuan-note/siyuan/issues/5480
		var err error
		ret, err = filepath.EvalSymlinks(ret)
		if err != nil {
			logging.LogErrorf("read assets link failed: %s", err)
		}
	}
	return
}

// EncryptedDBPath returns the path to an encrypted notebook's dedicated SQLCipher db file.
// It is placed in the temp directory alongside siyuan.db, with the file name including the boxID to distinguish
// between multiple encrypted notebooks. The db is a rebuildable index, not the original content.
func EncryptedDBPath(boxID string) string {
	return filepath.Join(TempDir, "siyuan-encrypted-"+boxID+".db")
}

// EncryptedBlockTreeDBPath returns the path to an encrypted notebook's dedicated SQLCipher blocktree db file.
func EncryptedBlockTreeDBPath(boxID string) string {
	return filepath.Join(TempDir, "siyuan-encrypted-"+boxID+"-blocktree.db")
}

func tryLockWorkspace() {
	WorkspaceLock = flock.New(filepath.Join(WorkspaceDir, ".lock"))
	ok, err := WorkspaceLock.TryLock()
	if ok {
		return
	}
	if err != nil {
		logging.LogErrorf("lock workspace [%s] failed: %s", WorkspaceDir, err)
	} else {
		logging.LogErrorf("lock workspace [%s] failed", WorkspaceDir)
	}
	os.Exit(logging.ExitCodeWorkspaceLocked)
}

func IsWorkspaceLocked(workspacePath string) bool {
	if !gulu.File.IsDir(workspacePath) {
		return false
	}

	lockFilePath := filepath.Join(workspacePath, ".lock")
	if !gulu.File.IsExist(lockFilePath) {
		return false
	}

	f := flock.New(lockFilePath)
	defer f.Unlock()
	ok, _ := f.TryLock()
	if ok {
		return false
	}
	return true
}

func UnlockWorkspace() {
	if nil == WorkspaceLock {
		return
	}

	if err := WorkspaceLock.Unlock(); err != nil {
		logging.LogErrorf("unlock workspace [%s] failed: %s", WorkspaceDir, err)
		return
	}

	if err := os.Remove(filepath.Join(WorkspaceDir, ".lock")); err != nil {
		logging.LogErrorf("remove workspace lock failed: %s", err)
		return
	}
}

func LogDatabaseSize(dbPath string) {
	dbFile, err := os.Stat(dbPath)
	if nil != err {
		return
	}

	dbSize := humanize.BytesCustomCeil(uint64(dbFile.Size()), 2)
	logging.LogInfof("database [%s] size [%s]", dbPath, dbSize)
}

func RemoveDatabaseFile(dbPath string) {
	if gulu.File.IsExist(dbPath) {
		err := os.RemoveAll(dbPath)
		if err != nil {
			logging.LogErrorf("remove database file [%s] failed: %s", dbPath, err)
			return
		}
	}

	if gulu.File.IsExist(dbPath + "-shm") {
		err := os.RemoveAll(dbPath + "-shm")
		if err != nil {
			logging.LogErrorf("remove database file [%s] failed: %s", dbPath+"-shm", err)
			return
		}
	}

	if gulu.File.IsExist(dbPath + "-wal") {
		err := os.RemoveAll(dbPath + "-wal")
		if err != nil {
			logging.LogErrorf("remove database file [%s] failed: %s", dbPath+"-wal", err)
			return
		}
	}
}
