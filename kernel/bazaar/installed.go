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

package bazaar

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/88250/go-humanize"
	"github.com/88250/gulu"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/util"
	"golang.org/x/mod/semver"
	"golang.org/x/sync/singleflight"
)

// ReadInstalledPackageDirs reads the directory list of locally installed bazaar packages
func ReadInstalledPackageDirs(basePath string) ([]os.DirEntry, error) {
	if !util.IsPathRegularDirOrSymlinkDir(basePath) {
		return []os.DirEntry{}, nil
	}

	entries, err := os.ReadDir(basePath)
	if err != nil {
		return nil, err
	}

	dirs := make([]os.DirEntry, 0, len(entries))
	for _, e := range entries {
		if util.IsDirRegularOrSymlink(e) {
			dirs = append(dirs, e)
		}
	}
	return dirs, nil
}

// SetInstalledPackageMetadata sets the common metadata of a locally installed bazaar package
func SetInstalledPackageMetadata(pkg *Package, installPath, baseURLPath, pkgType, frontend string, bazaarPackagesMap map[string]*Package) bool {
	// display info
	pkg.IconURL = baseURLPath + "icon.png"
	pkg.PreviewURL = baseURLPath + "preview.png"
	pkg.PreferredName = GetPreferredLocaleString(pkg.DisplayName, pkg.Name)
	pkg.PreferredDesc = GetPreferredLocaleString(pkg.Description, "")
	pkg.PreferredReadme = getInstalledPackageREADME(installPath, baseURLPath, pkg.Readme)
	pkg.PreferredFunding = getPreferredFunding(pkg.Funding)

	// update info
	pkg.Installed = true
	pkg.DisallowInstall = isBelowRequiredAppVersion(pkg)
	if bazaarPkg := bazaarPackagesMap[pkg.Name]; nil != bazaarPkg {
		pkg.RepoURL = bazaarPkg.RepoURL // use the online data for the updated link, to avoid an incorrect link from the local metadata
		pkg.UpdateRequiredMinAppVer = bazaarPkg.MinAppVersion

		if 0 > semver.Compare("v"+pkg.Version, "v"+bazaarPkg.Version) {
			pkg.RepoHash = bazaarPkg.RepoHash
			pkg.Outdated = true
			disallowUpdate := isBelowRequiredAppVersion(bazaarPkg)
			if "plugins" == pkgType {
				disallowUpdate = disallowUpdate || IsIncompatiblePlugin(bazaarPkg, frontend)
			}
			pkg.DisallowUpdate = disallowUpdate
			pkg.Updated = bazaarPkg.Updated
			pkg.HUpdated = bazaarPkg.HUpdated
			pkg.Size = bazaarPkg.Size
			pkg.HSize = bazaarPkg.HSize
			pkg.Stars = bazaarPkg.Stars
			pkg.OpenIssues = bazaarPkg.OpenIssues
			pkg.Downloads = bazaarPkg.Downloads
		}
	} else {
		pkg.RepoURL = pkg.URL
	}

	// install info
	pkg.HInstallDate = getPackageHInstallDate(pkgType, pkg.Name, installPath)
	// TODO change the local install size cache to a 1-minute TTL; only walk the package folder to compute the
	// size when the bazaar package README is opened, and return the result to the frontend asynchronously
	// https://github.com/siyuan-note/siyuan/issues/16983
	// Currently the online stage data is preferred: not time-consuming, but may be inaccurate, e.g. an old
	// local version and the latest cloud version may have a different install size; falling back to the local
	// directory size: time-consuming, but accurate.
	// Need to separate the local install size from the online stage data's install size.
	bazaarMemMu.RLock()
	cachedSize, hit := installSizeCache[pkg.RepoURL]
	bazaarMemMu.RUnlock()
	if hit {
		pkg.InstallSize = cachedSize
	} else {
		size, _ := util.SizeOfDirectory(installPath)
		pkg.InstallSize = size
		bazaarMemMu.Lock()
		installSizeCache[pkg.RepoURL] = size
		bazaarMemMu.Unlock()
	}
	pkg.HInstallSize = humanize.BytesCustomCeil(uint64(pkg.InstallSize), 2)

	return true
}

// Add marketplace package config item `minAppVersion` https://github.com/siyuan-note/siyuan/issues/8330
func isBelowRequiredAppVersion(pkg *Package) bool {
	// If the package doesn't specify minAppVersion, allow install
	if "" == pkg.MinAppVersion {
		return false
	}

	// If the package's required minAppVersion is greater than the current version, disallow install
	if 0 < semver.Compare("v"+pkg.MinAppVersion, "v"+util.Ver) {
		return true
	}
	return false
}

// BazaarInfo is the persisted info of the bazaar
type BazaarInfo struct {
	Packages map[string]map[string]*PackageInfo `json:"packages"`
}

// PackageInfo is the persisted info of a bazaar package
type PackageInfo struct {
	InstallTime int64 `json:"installTime"` // install timestamp (milliseconds)
}

var (
	bazaarInfoCache        *BazaarInfo
	bazaarInfoModTime      time.Time
	bazaarInfoCacheLock    = sync.RWMutex{}
	bazaarInfoSingleFlight singleflight.Group
)

// getBazaarInfo ensures the bazaar's persisted info has been loaded into bazaarInfoCache
func getBazaarInfo() {
	infoPath := filepath.Join(util.DataDir, "storage", "bazaar.json")
	info, err := os.Stat(infoPath)

	bazaarInfoCacheLock.RLock()
	cache := bazaarInfoCache
	modTime := bazaarInfoModTime
	bazaarInfoCacheLock.RUnlock()
	// If the file's modification time hasn't changed, consider the cache valid
	if cache != nil && err == nil && info.ModTime().Equal(modTime) {
		return
	}

	_, _, _ = bazaarInfoSingleFlight.Do("loadBazaarInfo", func() (any, error) {
		// Load from disk when the cache is invalid
		newRet := loadBazaarInfo()
		// Update the cache and the modification time
		bazaarInfoCacheLock.Lock()
		bazaarInfoCache = newRet
		if err == nil {
			bazaarInfoModTime = info.ModTime()
		}
		bazaarInfoCacheLock.Unlock()
		return newRet, nil
	})
}

// loadBazaarInfo loads the bazaar's persisted info from disk
func loadBazaarInfo() (ret *BazaarInfo) {
	// Initialize an empty BazaarInfo, so later usage doesn't need to check for nil
	ret = &BazaarInfo{
		Packages: make(map[string]map[string]*PackageInfo),
	}

	infoDir := filepath.Join(util.DataDir, "storage")
	if err := os.MkdirAll(infoDir, 0755); err != nil {
		logging.LogErrorf("create bazaar info dir [%s] failed: %s", infoDir, err)
		return
	}

	infoPath := filepath.Join(infoDir, "bazaar.json")
	if !filelock.IsExist(infoPath) {
		return
	}

	data, err := filelock.ReadFile(infoPath)
	if err != nil {
		logging.LogErrorf("read bazaar info [%s] failed: %s", infoPath, err)
		return
	}

	if err = gulu.JSON.UnmarshalJSON(data, &ret); err != nil {
		logging.LogErrorf("unmarshal bazaar info [%s] failed: %s", infoPath, err)
		ret = &BazaarInfo{
			Packages: make(map[string]map[string]*PackageInfo),
		}
	}

	return
}

// saveBazaarInfo saves the bazaar's persisted info (the caller must hold the bazaarInfoCacheLock write lock)
func saveBazaarInfo() {
	infoPath := filepath.Join(util.DataDir, "storage", "bazaar.json")

	data, err := gulu.JSON.MarshalIndentJSON(bazaarInfoCache, "", "\t")
	if err != nil {
		logging.LogErrorf("marshal bazaar info [%s] failed: %s", infoPath, err)
		return
	}
	if err = filelock.WriteFile(infoPath, data); err != nil {
		logging.LogErrorf("write bazaar info [%s] failed: %s", infoPath, err)
		return
	}

	if fi, statErr := os.Stat(infoPath); statErr == nil {
		bazaarInfoModTime = fi.ModTime()
	}
}

// setPackageInstallTime sets the install time of a bazaar package
func setPackageInstallTime(pkgType, pkgName string, installTime time.Time) {
	getBazaarInfo()

	bazaarInfoCacheLock.Lock()
	defer bazaarInfoCacheLock.Unlock()

	if bazaarInfoCache == nil {
		return
	}
	if bazaarInfoCache.Packages[pkgType] == nil {
		bazaarInfoCache.Packages[pkgType] = make(map[string]*PackageInfo)
	}
	p := bazaarInfoCache.Packages[pkgType][pkgName]
	if p == nil {
		p = &PackageInfo{}
		bazaarInfoCache.Packages[pkgType][pkgName] = p
	}
	p.InstallTime = installTime.UnixMilli()
	saveBazaarInfo()
}

// getPackageHInstallDate gets the install date of a bazaar package
func getPackageHInstallDate(pkgType, pkgName, installPath string) string {
	getBazaarInfo()
	bazaarInfoCacheLock.RLock()
	var installTime int64
	if bazaarInfoCache != nil && bazaarInfoCache.Packages[pkgType] != nil {
		if p := bazaarInfoCache.Packages[pkgType][pkgName]; p != nil {
			installTime = p.InstallTime
		}
	}
	bazaarInfoCacheLock.RUnlock()

	if installTime > 0 {
		return time.UnixMilli(installTime).Format("2006-01-02")
	}

	// If there's no record in bazaar.json, use the folder's modification time and record it into bazaar.json
	fi, err := os.Stat(installPath)
	if err != nil {
		logging.LogWarnf("stat install package folder [%s] failed: %s", installPath, err)
		return time.Now().Format("2006-01-02")
	}
	setPackageInstallTime(pkgType, pkgName, fi.ModTime())

	return fi.ModTime().Format("2006-01-02")
}

// RemovePackageInfo removes the persisted info of a bazaar package
func RemovePackageInfo(pkgType, pkgName string) {
	getBazaarInfo()

	bazaarInfoCacheLock.Lock()
	defer bazaarInfoCacheLock.Unlock()

	if bazaarInfoCache != nil && bazaarInfoCache.Packages[pkgType] != nil {
		delete(bazaarInfoCache.Packages[pkgType], pkgName)
	}

	saveBazaarInfo()
}
