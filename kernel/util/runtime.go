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
	"crypto/rand"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/88250/gulu"
	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"github.com/jaypipes/ghw"
	"github.com/siyuan-note/httpclient"
	"github.com/siyuan-note/logging"
)

var DisabledFeatures []string

// CLILogLevel is set when a CLI subcommand explicitly specifies the log level via --log-level; based on this,
// the end of model.InitConf skips overriding logging.SetLogLevel, so the command-line argument takes priority
// over system.logLevel in conf.json.
var CLILogLevel string

func DisableFeature(feature string) {
	DisabledFeatures = append(DisabledFeatures, feature)
	DisabledFeatures = gulu.Str.RemoveDuplicatedElem(DisabledFeatures)
}

var (
	UseSingleLineSave    = true // UseSingleLineSave whether to save .sy and database .json files as a single line.
	LargeFileWarningSize = 8    // LargeFileWarningSize large file warning size, in MB
)

func ExceedLargeFileWarningSize(fileSize int) bool {
	return fileSize > LargeFileWarningSize*1024*1024
}

// IsUILoaded whether the UI has already been loaded.
var IsUILoaded = false

func WaitForUILoaded() {
	start := time.Now()
	for !IsUILoaded {
		time.Sleep(200 * time.Millisecond)
		if time.Since(start) > 30*time.Second {
			logging.LogErrorf("wait for ui loaded timeout: %s", logging.ShortStack())
			break
		}
	}
}

func HookUILoaded() {
	for !IsUILoaded {
		if 0 < len(SessionsByType("main")) {
			IsUILoaded = true
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// IsExiting whether the program is currently exiting.
var IsExiting = atomic.Bool{}

// MobileOSVer the mobile client's operating system version.
var MobileOSVer string

// DatabaseVer the database version.
// Format: yyyyMMddHHmm. This value must be updated when the table schema changes; on boot, a version change is
// detected, and if it doesn't match, the old database file is automatically removed and the table schema is
// rebuilt, which also triggers a full index rebuild.
const DatabaseVer = "202607031200"

func logBootInfo() {
	plat := GetOSPlatform()
	logging.LogInfof("kernel is booting:\n"+
		"    * ver [%s]\n"+
		"    * arch [%s]\n"+
		"    * os [%s]\n"+
		"    * pid [%d]\n"+
		"    * runtime mode [%s]\n"+
		"    * working directory [%s]\n"+
		"    * read only [%v]\n"+
		"    * container [%s]\n"+
		"    * database [ver=%s]\n"+
		"    * workspace directory [%s]",
		Ver, runtime.GOARCH, plat, os.Getpid(), Mode, WorkingDir, ReadOnly, Container, DatabaseVer, WorkspaceDir)
	if 0 < len(DisabledFeatures) {
		logging.LogInfof("disabled features [%s]", strings.Join(DisabledFeatures, ", "))
	}

	go func() {
		driveType := getWorkspaceDriveType()
		if "" == driveType {
			return
		}

		if ghw.DriveTypeSSD.String() != driveType {
			logging.LogWarnf("workspace dir [%s] is not in SSD drive, performance may be affected", WorkspaceDir)
			if AttachUI {
				WaitForUILoaded()
				time.Sleep(3 * time.Second)
			}
			if nil == NotificationsCfg || NotificationsCfg.WorkspaceNotSSD {
				PushErrMsg(Langs[Lang][278], 15000)
			}
		}
	}()
}

func getWorkspaceDriveType() string {
	if gulu.OS.IsDarwin() {
		return ghw.DriveTypeSSD.String()
	}

	if IsMobileContainer() {
		return ghw.DriveTypeSSD.String()
	}

	block, err := ghw.Block()
	if err != nil {
		logging.LogWarnf("get block storage info failed: %s", err)
		return ""
	}

	var maxMountPathLen int
	var matchedDriveType string
	parentRelPrefix := ".." + string(filepath.Separator)
	workspacePath := filepath.Clean(WorkspaceDir)

	if gulu.OS.IsWindows() {
		vol := strings.ToLower(filepath.VolumeName(workspacePath))
		for _, disk := range block.Disks {
			for _, partition := range disk.Partitions {
				if strings.EqualFold(strings.TrimSuffix(partition.MountPoint, "\\"), vol) {
					return partition.Disk.DriveType.String()
				}
			}
		}
	} else if gulu.OS.IsLinux() {
		for _, disk := range block.Disks {
			for _, partition := range disk.Partitions {
				if partition.MountPoint == "" {
					continue
				}
				mountPath := filepath.Clean(partition.MountPoint)
				rel, err := filepath.Rel(mountPath, workspacePath)
				if err != nil {
					continue
				}
				if rel == ".." || strings.HasPrefix(rel, parentRelPrefix) {
					continue
				}

				// Pick the mount point with the longest path (e.g. /home/data over /)
				if len(mountPath) >= maxMountPathLen {
					maxMountPathLen = len(mountPath)
					matchedDriveType = partition.Disk.DriveType.String()
				}
			}
		}
	}
	return matchedDriveType
}

func RandomSleep(minMills, maxMills int) {
	r := gulu.Rand.Int(minMills, maxMills)
	time.Sleep(time.Duration(r) * time.Millisecond)
}

func GetDeviceID() string {
	// Use a random identifier instead of a hardware-derived one, to avoid leaking a device fingerprint; this
	// value is persisted in conf.json after it's first generated, so it stays stable for each installation
	return gulu.Rand.String(12)
}

func GetDeviceName() string {
	ret, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return ret
}

func SetNetworkProxy(proxyURL string) {
	if err := os.Setenv("HTTPS_PROXY", proxyURL); err != nil {
		logging.LogErrorf("set env [HTTPS_PROXY] failed: %s", err)
	}
	if err := os.Setenv("HTTP_PROXY", proxyURL); err != nil {
		logging.LogErrorf("set env [HTTP_PROXY] failed: %s", err)
	}

	if "" != proxyURL {
		logging.LogInfof("use network proxy [%s]", proxyURL)
	} else {
		logging.LogInfof("use network proxy [system]")
	}

	httpclient.CloseIdleConnections()
}

const (
	// SQLFlushInterval is the write interval for the database transaction queue.
	SQLFlushInterval = 3000 * time.Millisecond
)

var (
	Langs           = map[string]map[int]string{}
	TimeLangs       = map[string]map[string]any{}
	TaskActionLangs = map[string]map[string]any{}
	TrayMenuLangs   = map[string]map[string]any{}
	AttrViewLangs   = map[string]map[string]any{}
)

var (
	thirdPartySyncCheckTicker = time.NewTicker(time.Minute * 10)
)

func ReportFileSysFatalError(err error) {
	stack := debug.Stack()
	output := string(stack)
	if 5 < strings.Count(output, "\n") {
		lines := strings.Split(output, "\n")
		output = strings.Join(lines[5:], "\n")
	}
	logging.LogErrorf("check file system status failed: %s, %s", err, output)
	os.Exit(logging.ExitCodeFileSysErr)
}

var checkFileSysStatusLock = sync.Mutex{}

func CheckFileSysStatus() {
	if ContainerStd != Container {
		return
	}

	for {
		<-thirdPartySyncCheckTicker.C
		checkFileSysStatus()
	}
}

func checkFileSysStatus() {
	defer logging.Recover()

	if !checkFileSysStatusLock.TryLock() {
		logging.LogWarnf("check file system status is locked, skip")
		return
	}
	defer checkFileSysStatusLock.Unlock()

	const fileSysStatusCheckFile = ".siyuan/filesys_status_check"
	if IsCloudDrivePath(WorkspaceDir) {
		ReportFileSysFatalError(fmt.Errorf("workspace dir [%s] is in third party sync dir", WorkspaceDir))
		return
	}

	dir := filepath.Join(DataDir, fileSysStatusCheckFile)
	if err := os.RemoveAll(dir); err != nil {
		ReportFileSysFatalError(err)
		return
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		ReportFileSysFatalError(err)
		return
	}

	for range 7 {
		tmp := filepath.Join(dir, "check_consistency")
		data := make([]byte, 1024*4)
		_, err := rand.Read(data)
		if err != nil {
			ReportFileSysFatalError(err)
			return
		}

		if err = os.WriteFile(tmp, data, 0644); err != nil {
			ReportFileSysFatalError(err)
			return
		}

		time.Sleep(5 * time.Second)

		for range 32 {
			renamed := tmp + "_renamed"
			if err = os.Rename(tmp, renamed); err != nil {
				ReportFileSysFatalError(err)
				break
			}

			RandomSleep(500, 1000)

			f, err := os.Open(renamed)
			if err != nil {
				ReportFileSysFatalError(err)
				return
			}

			if err = f.Close(); err != nil {
				ReportFileSysFatalError(err)
				return
			}

			if err = os.Rename(renamed, tmp); err != nil {
				ReportFileSysFatalError(err)
				return
			}

			entries, err := os.ReadDir(dir)
			if err != nil {
				ReportFileSysFatalError(err)
				return
			}

			checkFilenames := bytes.Buffer{}
			for _, entry := range entries {
				if !entry.IsDir() && strings.Contains(entry.Name(), "check_") {
					checkFilenames.WriteString(entry.Name())
					checkFilenames.WriteString("\n")
				}
			}
			lines := strings.Split(strings.TrimSpace(checkFilenames.String()), "\n")
			if 1 < len(lines) {
				buf := bytes.Buffer{}
				for _, line := range lines {
					buf.WriteString("  ")
					buf.WriteString(line)
					buf.WriteString("\n")
				}
				output := buf.String()
				ReportFileSysFatalError(fmt.Errorf("dir [%s] has more than 1 file:\n%s", dir, output))
				return
			}
		}

		if err = os.RemoveAll(tmp); err != nil {
			ReportFileSysFatalError(err)
			return
		}
	}
}

func IsCloudDrivePath(workspaceAbsPath string) bool {
	if isICloudPath(workspaceAbsPath) {
		return true
	}

	if isKnownCloudDrivePath(workspaceAbsPath) {
		return true
	}

	if existAvailabilityStatus(workspaceAbsPath) {
		return true
	}

	return false
}

func isKnownCloudDrivePath(workspaceAbsPath string) bool {
	workspaceAbsPathLower := strings.ToLower(workspaceAbsPath)
	return strings.Contains(workspaceAbsPathLower, "onedrive") || strings.Contains(workspaceAbsPathLower, "dropbox") ||
		strings.Contains(workspaceAbsPathLower, "google drive") || strings.Contains(workspaceAbsPathLower, "pcloud") ||
		strings.Contains(workspaceAbsPathLower, "坚果云") ||
		strings.Contains(workspaceAbsPathLower, "天翼云")
}

func isICloudPath(workspaceAbsPath string) (ret bool) {
	if !gulu.OS.IsDarwin() {
		return false
	}

	workspaceAbsPathLower := strings.ToLower(workspaceAbsPath)

	// On macOS, check whether the workspace is placed under an iCloud path https://github.com/siyuan-note/siyuan/issues/7747
	iCloudRoot := filepath.Join(HomeDir, "Library", "Mobile Documents")
	WalkWithSymlinks(iCloudRoot, func(path string, d fs.DirEntry, err error) error {
		if !d.IsDir() {
			return nil
		}

		if strings.HasPrefix(workspaceAbsPathLower, strings.ToLower(path)) {
			ret = true
			logging.LogWarnf("workspace [%s] is in iCloud path [%s]", workspaceAbsPath, path)
			return io.EOF
		}
		return nil
	})
	return
}

func existAvailabilityStatus(workspaceAbsPath string) bool {
	if !gulu.OS.IsWindows() {
		return false
	}

	if !gulu.File.IsExist(workspaceAbsPath) {
		return false
	}

	// Improve detection of third-party sync drives on Windows https://github.com/siyuan-note/siyuan/issues/7777

	defer logging.Recover()

	checkAbsPath := filepath.Join(workspaceAbsPath, "data")
	if !gulu.File.IsExist(checkAbsPath) {
		checkAbsPath = workspaceAbsPath
	}
	if !gulu.File.IsExist(checkAbsPath) {
		logging.LogWarnf("check path [%s] not exist", checkAbsPath)
		return false
	}

	runtime.LockOSThread()
	defer runtime.LockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		logging.LogWarnf("initialize ole failed: %s", err)
		return false
	}
	defer ole.CoUninitialize()
	dir, file := filepath.Split(checkAbsPath)
	unknown, err := oleutil.CreateObject("Shell.Application")
	if err != nil {
		logging.LogWarnf("create shell application failed: %s", err)
		return false
	}
	shell, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		logging.LogWarnf("query shell interface failed: %s", err)
		return false
	}
	defer shell.Release()

	result, err := oleutil.CallMethod(shell, "NameSpace", dir)
	if err != nil {
		logging.LogWarnf("call shell [NameSpace] failed: %s", err)
		return false
	}
	folderObj := result.ToIDispatch()

	result, err = oleutil.CallMethod(folderObj, "ParseName", file)
	if err != nil {
		logging.LogWarnf("call shell [ParseName] failed: %s", err)
		return false
	}
	fileObj := result.ToIDispatch()
	if nil == fileObj {
		logging.LogWarnf("call shell [ParseName] file is nil [%s]", checkAbsPath)
		return false
	}

	result, err = oleutil.CallMethod(folderObj, "GetDetailsOf", fileObj, 303)
	if err != nil {
		logging.LogWarnf("call shell [GetDetailsOf] failed: %s", err)
		return false
	}
	value := result
	if nil == value {
		return false
	}
	status := strings.ToLower(value.ToString())
	if "" == status || "availability status" == status || "可用性状态" == status {
		return false
	}

	if strings.Contains(status, "sync") || strings.Contains(status, "同步") ||
		strings.Contains(status, "available on this device") || strings.Contains(status, "在此设备上可用") ||
		strings.Contains(status, "available when online") || strings.Contains(status, "联机时可用") {
		logging.LogErrorf("[%s] third party sync status [%s]", checkAbsPath, status)
		return true
	}
	return false
}

const (
	EvtConfPandocInitialized = "conf.pandoc.initialized"

	EvtSQLHistoryRebuild      = "sql.history.rebuild"
	EvtSQLAssetContentRebuild = "sql.assetContent.rebuild"
)

var SearchCaseSensitive bool

// SearchHanSensitive whether to distinguish traditional/simplified Chinese; maintained by sql.SetHanSensitive, defaults to true to match past behavior
var SearchHanSensitive = true
