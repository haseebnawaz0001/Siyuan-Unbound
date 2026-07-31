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

package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/88250/gulu"
	"github.com/emirpasic/gods/sets/hashset"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/bazaar"
	"github.com/siyuan-note/siyuan/kernel/util"
	"golang.org/x/mod/semver"
	"golang.org/x/sync/singleflight"
)

// installedPackageInfo describes a local bazaar package's package data and directory name
type installedPackageInfo struct {
	Pkg     *bazaar.Package
	DirName string
}

func getPackageInstallPath(pkgType, packageName string) (string, string, error) {
	switch pkgType {
	case "plugins":
		return filepath.Join(util.DataDir, "plugins", packageName), "plugin.json", nil
	case "themes":
		return filepath.Join(util.ThemesPath, packageName), "theme.json", nil
	case "icons":
		return filepath.Join(util.IconsPath, packageName), "icon.json", nil
	case "templates":
		return filepath.Join(util.DataDir, "templates", packageName), "template.json", nil
	case "widgets":
		return filepath.Join(util.DataDir, "widgets", packageName), "widget.json", nil
	default:
		logging.LogErrorf("invalid package type: %s", pkgType)
		return "", "", errors.New("invalid package type")
	}
}

// installMeta records the before/after install state, for use by post-install processing
type installMeta struct {
	update bool
}

// batchInstallItem is the result for a single package during a same-type batch install
type batchInstallItem struct {
	name string
	meta installMeta
}

// updatePackages updates a group of bazaar packages; for a same-type batch update, post-install processing runs only once
func updatePackages(packages []*bazaar.Package, pkgType string, successCount *int, planned int) {
	items := make([]batchInstallItem, 0, len(packages))
	for _, pkg := range packages {
		meta, err := installBazaarPackage(pkgType, pkg.RepoURL, pkg.RepoHash, pkg.Name)
		if err != nil {
			logging.LogErrorf("update %s [%s] failed: %s", pkgType, pkg.Name, err)
			util.PushErrMsg(fmt.Sprintf(Conf.language(238), pkg.Name), 5000)
			continue
		}
		items = append(items, batchInstallItem{name: pkg.Name, meta: meta})
		*successCount++
		util.PushEndlessProgress(fmt.Sprintf(Conf.language(236), *successCount, planned, pkg.Name))
	}
	finishInstall(pkgType, items, 0)
}

// filterUpdatableBazaarPackages filters out bazaar packages that are allowed to be updated
func filterUpdatableBazaarPackages(packages []*bazaar.Package) []*bazaar.Package {
	updatable := make([]*bazaar.Package, 0, len(packages))
	for _, pkg := range packages {
		if !pkg.DisallowUpdate {
			updatable = append(updatable, pkg)
		}
	}
	return updatable
}

// BatchUpdatePackages updates all bazaar packages
func BatchUpdatePackages(frontend string) {
	plugins, widgets, icons, themes, templates := GetUpdatedPackages(frontend)
	plugins = filterUpdatableBazaarPackages(plugins)
	widgets = filterUpdatableBazaarPackages(widgets)
	icons = filterUpdatableBazaarPackages(icons)
	themes = filterUpdatableBazaarPackages(themes)
	templates = filterUpdatableBazaarPackages(templates)

	planned := len(plugins) + len(widgets) + len(icons) + len(themes) + len(templates)
	if 1 > planned {
		return
	}

	defer util.PushClearProgress()
	successCount := 0
	updatePackages(plugins, "plugins", &successCount, planned)
	updatePackages(themes, "themes", &successCount, planned)
	updatePackages(icons, "icons", &successCount, planned)
	updatePackages(templates, "templates", &successCount, planned)
	updatePackages(widgets, "widgets", &successCount, planned)

	if 0 < successCount {
		util.PushMsg(fmt.Sprintf(Conf.language(237), successCount), 5000)
	}
}

// GetUpdatedPackages gets the update list for bazaar packages of all types
//
//   - frontend is used only for plugin environment compatibility checks
func GetUpdatedPackages(frontend string) (plugins, widgets, icons, themes, templates []*bazaar.Package) {
	wg := &sync.WaitGroup{}

	wg.Go(func() {
		plugins = getUpdatedPackages("plugins", frontend)
	})
	wg.Go(func() {
		themes = getUpdatedPackages("themes", "")
	})
	wg.Go(func() {
		icons = getUpdatedPackages("icons", "")
	})
	wg.Go(func() {
		templates = getUpdatedPackages("templates", "")
	})
	wg.Go(func() {
		widgets = getUpdatedPackages("widgets", "")
	})

	wg.Wait()
	return
}

// getUpdatedPackages gets the update list for a single type of bazaar package
func getUpdatedPackages(pkgType, frontend string) (updatedPackages []*bazaar.Package) {
	installedPackages := GetInstalledPackages(pkgType, frontend, "")
	updatedPackages = []*bazaar.Package{} // Ensure an empty slice is returned rather than nil
	for _, pkg := range installedPackages {
		if !pkg.Outdated {
			continue
		}
		updatedPackages = append(updatedPackages, pkg)
	}
	return
}

// GetInstalledPackageInfos gets local bazaar package info, and returns the path-related fields for the caller to reuse
func GetInstalledPackageInfos(pkgType string) (installedPackageInfos []installedPackageInfo, basePath, baseURLPathPrefix string, err error) {
	var jsonFileName string
	switch pkgType {
	case "plugins":
		basePath, jsonFileName, baseURLPathPrefix = filepath.Join(util.DataDir, "plugins"), "plugin.json", "/plugins/"
	case "themes":
		basePath, jsonFileName, baseURLPathPrefix = util.ThemesPath, "theme.json", "/appearance/themes/"
	case "icons":
		basePath, jsonFileName, baseURLPathPrefix = util.IconsPath, "icon.json", "/appearance/icons/"
	case "templates":
		basePath, jsonFileName, baseURLPathPrefix = filepath.Join(util.DataDir, "templates"), "template.json", "/templates/"
	case "widgets":
		basePath, jsonFileName, baseURLPathPrefix = filepath.Join(util.DataDir, "widgets"), "widget.json", "/widgets/"
	default:
		logging.LogErrorf("invalid package type: %s", pkgType)
		err = errors.New("invalid package type")
		return
	}

	dirs, err := bazaar.ReadInstalledPackageDirs(basePath)
	if err != nil {
		logging.LogWarnf("read %s folder failed: %s", pkgType, err)
		return
	}
	if len(dirs) == 0 {
		return
	}

	// Filter out built-in packages
	switch pkgType {
	case "themes":
		filtered := make([]os.DirEntry, 0, len(dirs))
		for _, d := range dirs {
			if isBuiltInTheme(d.Name()) {
				continue
			}
			filtered = append(filtered, d)
		}
		dirs = filtered
	case "icons":
		filtered := make([]os.DirEntry, 0, len(dirs))
		for _, d := range dirs {
			if isBuiltInIcon(d.Name()) {
				continue
			}
			filtered = append(filtered, d)
		}
		dirs = filtered
	}

	for _, dir := range dirs {
		dirName := dir.Name()
		pkg, parseErr := bazaar.ParsePackageJSON(filepath.Join(basePath, dirName, jsonFileName))
		if nil != parseErr || nil == pkg {
			continue
		}
		installedPackageInfos = append(installedPackageInfos, installedPackageInfo{Pkg: pkg, DirName: dirName})
	}
	return
}

var getInstalledPackagesFlight singleflight.Group

// GetInstalledPackages gets the list of local bazaar packages
func GetInstalledPackages(pkgType, frontend, keyword string) (installedPackages []*bazaar.Package) {
	key := "getInstalledPackages:" + pkgType + ":" + frontend + ":" + keyword
	v, err, _ := getInstalledPackagesFlight.Do(key, func() (any, error) {
		return getInstalledPackages0(pkgType, frontend, keyword), nil
	})
	if err != nil {
		return []*bazaar.Package{}
	}
	return v.([]*bazaar.Package)
}

func getInstalledPackages0(pkgType, frontend, keyword string) (installedPackages []*bazaar.Package) {
	installedPackages = []*bazaar.Package{}

	installedInfos, basePath, baseURLPathPrefix, err := GetInstalledPackageInfos(pkgType)
	if err != nil {
		return
	}
	// Return immediately if there are no local bazaar packages of this type, to avoid requesting cloud data
	if len(installedInfos) == 0 {
		return
	}

	bazaarPackagesMap := bazaar.GetBazaarPackagesMap(pkgType, frontend)

	for _, info := range installedInfos {
		pkg := info.Pkg
		installPath := filepath.Join(basePath, info.DirName)
		baseURLPath := baseURLPathPrefix + info.DirName + "/"
		// Set common metadata for the local bazaar package
		if !bazaar.SetInstalledPackageMetadata(pkg, installPath, baseURLPath, pkgType, frontend, bazaarPackagesMap) {
			continue
		}
		installedPackages = append(installedPackages, pkg)
	}

	installedPackages = bazaar.FilterPackages(installedPackages, keyword)

	// Set additional metadata for the local bazaar package
	var petals []*Petal
	if pkgType == "plugins" {
		petals = getPetals()
	}
	for _, pkg := range installedPackages {
		switch pkgType {
		case "plugins":
			installedIncompatible := bazaar.IsIncompatiblePlugin(pkg, frontend)
			pkg.InstalledIncompatible = &installedIncompatible
			var bazaarIncompatible bool
			if onlinePkg := bazaarPackagesMap[pkg.Name]; nil != onlinePkg {
				bazaarIncompatible = bazaar.IsIncompatiblePlugin(onlinePkg, frontend)
			}
			pkg.BazaarIncompatible = &bazaarIncompatible
			petal := getPetalByName(pkg.Name, petals)
			if nil != petal {
				enabled := petal.Enabled
				pkg.Enabled = &enabled
			}
		case "themes":
			pkg.Current = pkg.Name == Conf.Appearance.ThemeDark || pkg.Name == Conf.Appearance.ThemeLight
		case "icons":
			pkg.Current = pkg.Name == Conf.Appearance.Icon
		}
	}
	return
}

// GetBazaarPackages gets the list of online bazaar packages
func GetBazaarPackages(pkgType, frontend, keyword string) (bazaarPackages []*bazaar.Package) {
	bazaarPackages = bazaar.GetBazaarPackages(pkgType, frontend)
	bazaarPackages = bazaar.FilterPackages(bazaarPackages, keyword)
	installedInfos, _, _, err := GetInstalledPackageInfos(pkgType)
	if err != nil {
		return
	}
	installedMap := make(map[string]*bazaar.Package, len(installedInfos))
	for _, info := range installedInfos {
		installedMap[info.Pkg.Name] = info.Pkg
	}
	for _, pkg := range bazaarPackages {
		installedPkg, ok := installedMap[pkg.Name]
		if !ok {
			continue
		}
		pkg.Installed = true
		pkg.Outdated = 0 > semver.Compare("v"+installedPkg.Version, "v"+pkg.Version)
		switch pkgType {
		case "themes":
			pkg.Current = pkg.Name == Conf.Appearance.ThemeDark || pkg.Name == Conf.Appearance.ThemeLight
		case "icons":
			pkg.Current = pkg.Name == Conf.Appearance.Icon
		}
	}
	return
}

func GetBazaarPackageREADME(ctx context.Context, repoURL, repoHash, pkgType string) (ret string) {
	ret = bazaar.GetBazaarPackageREADME(ctx, repoURL, repoHash, pkgType)
	return
}

// installBazaarPackage downloads and installs a bazaar package
func installBazaarPackage(pkgType, repoURL, repoHash, packageName string) (meta installMeta, err error) {
	installPath, jsonFileName, err := getPackageInstallPath(pkgType, packageName)
	if err != nil {
		return
	}

	installedPkg, parseErr := bazaar.ParsePackageJSON(filepath.Join(installPath, jsonFileName))
	meta.update = parseErr == nil && installedPkg != nil && installedPkg.Name == packageName

	err = bazaar.InstallPackage(repoURL, repoHash, installPath, pkgType, packageName)
	if err != nil {
		err = fmt.Errorf(Conf.Language(46), packageName, err)
	}
	return
}

// finishInstall handles post-install processing for a bazaar package (refreshing appearance, pushing plugin reloads,
// etc); for a same-type batch update, this runs only once.
//
//   - themeMode: 0 for light / 1 for dark, only written to appearance when a theme is newly installed
//     (meta.update is false); not used for batch overwrite updates
func finishInstall(pkgType string, items []batchInstallItem, themeMode int) {
	if 1 > len(items) {
		return
	}

	switch pkgType {
	case "plugins":
		reloadPluginSet := hashset.New()
		for _, item := range items {
			if !item.meta.update {
				continue
			}
			petal := GetPetalByName(item.name)
			if nil != petal && petal.Enabled {
				_, err := SetPetalEnabled(petal.Name, petal.Enabled) // Reload plugin content
				if err != nil {
					logging.LogErrorf("reload plugin [%s] after update failed: %s", item.name, err)
					util.PushErrMsg(err.Error(), 5000)
					continue
				}
				reloadPluginSet.Add(item.name)
			}
		}
		if 0 < reloadPluginSet.Size() {
			PushReloadPlugin(nil, nil, reloadPluginSet, nil, "")
		}
	case "themes":
		for _, item := range items {
			if !item.meta.update {
				// Auto-switch only when the theme is newly installed https://github.com/siyuan-note/siyuan/issues/4966
				if 0 == themeMode {
					Conf.Appearance.ThemeLight = item.name
				} else {
					Conf.Appearance.ThemeDark = item.name
				}
				Conf.Appearance.Mode = themeMode
				Conf.Appearance.ThemeJS = gulu.File.IsExist(filepath.Join(util.ThemesPath, item.name, "theme.js"))
				Conf.Save()
			}
		}
		InitAppearance()
		WatchThemes()
		util.BroadcastByType("main", "setAppearance", 0, "", Conf.Appearance)
	case "icons":
		for _, item := range items {
			if !item.meta.update {
				// Auto-switch only when the icon set is newly installed
				Conf.Appearance.Icon = item.name
				Conf.Save()
			}
		}
		InitAppearance()
		util.BroadcastByType("main", "setAppearance", 0, "", Conf.Appearance)
	}
}

// InstallBazaarPackage installs a bazaar package; themeMode only takes effect when pkgType is "themes"
func InstallBazaarPackage(pkgType, repoURL, repoHash, packageName string, themeMode int) error {
	meta, err := installBazaarPackage(pkgType, repoURL, repoHash, packageName)
	if err != nil {
		return err
	}
	finishInstall(pkgType, []batchInstallItem{{name: packageName, meta: meta}}, themeMode)
	return nil
}

func UninstallPackage(pkgType, packageName string) error {
	installPath, _, err := getPackageInstallPath(pkgType, packageName)
	if err != nil {
		return err
	}

	err = bazaar.UninstallPackage(installPath)
	if err != nil {
		return fmt.Errorf(Conf.Language(47), err.Error())
	}

	// Remove the bazaar package's persisted info
	bazaar.RemovePackageInfo(pkgType, packageName)

	switch pkgType {
	case "plugins":
		petals := getPetals()
		var tmp []*Petal
		for i, petal := range petals {
			if petal.Name != packageName {
				tmp = append(tmp, petals[i])
			}
		}
		petals = tmp
		savePetals(petals)

		uninstallPluginSet := hashset.New(packageName)
		PushReloadPlugin(uninstallPluginSet, nil, nil, nil, "")
	case "themes":
		InitAppearance()
		WatchThemes()
		util.BroadcastByType("main", "setAppearance", 0, "", Conf.Appearance)
	case "icons":
		InitAppearance()
		util.BroadcastByType("main", "setAppearance", 0, "", Conf.Appearance)
	}

	return nil
}

// isBuiltInTheme determines whether a package/directory name refers to a built-in theme
func isBuiltInTheme(name string) bool {
	return "daylight" == name || "midnight" == name
}

// isBuiltInIcon determines whether a package/directory name refers to a built-in icon set
func isBuiltInIcon(name string) bool {
	return "litheness" == name
}
