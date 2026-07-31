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
	"strings"
	"time"

	"github.com/88250/go-humanize"
	"github.com/araddon/dateparse"
	"github.com/siyuan-note/siyuan/kernel/util"
)

// GetBazaarPackages returns the list of online bazaar packages of the given type (the plugins type requires passing the frontend parameter).
func GetBazaarPackages(pkgType string, frontend string) (packages []*Package) {
	result := getStageAndBazaar(pkgType)

	if !result.Online || nil != result.StageErr || nil == result.StageIndex {
		return make([]*Package, 0)
	}

	packages = make([]*Package, 0, len(result.StageIndex.Repos))
	for _, repo := range result.StageIndex.Repos {
		pkg := buildBazaarPackageWithMetadata(repo, result.BazaarStats, pkgType, frontend)
		if nil == pkg {
			continue
		}
		packages = append(packages, pkg)
	}
	return
}

// GetBazaarPackagesMap returns a map of online bazaar packages indexed by package name (the plugins type requires passing the frontend parameter).
func GetBazaarPackagesMap(pkgType, frontend string) map[string]*Package {
	packages := GetBazaarPackages(pkgType, frontend)
	packagesMap := make(map[string]*Package, len(packages))
	for _, pkg := range packages {
		if "" != pkg.Name {
			packagesMap[pkg.Name] = pkg
		}
	}
	return packagesMap
}

// buildBazaarPackageWithMetadata builds a bazaar package with online metadata from a StageRepo.
func buildBazaarPackageWithMetadata(repo *StageRepo, bazaarStats map[string]*bazaarStats, pkgType string, frontend string) *Package {
	if nil == repo || nil == repo.Package {
		return nil
	}

	pkg := *repo.Package
	pkg.URL = strings.TrimSuffix(pkg.URL, "/")
	repoURLHash := strings.Split(repo.URL, "@")
	if 2 != len(repoURLHash) {
		return nil
	}
	pkg.RepoURL = "https://github.com/" + repoURLHash[0]
	pkg.RepoHash = repoURLHash[1]

	// display info
	pkg.IconURL = util.BazaarOSSServer + "/package/" + repo.URL + "/icon.png"
	pkg.PreviewURL = util.BazaarOSSServer + "/package/" + repo.URL + "/preview.png?imageslim"
	pkg.PreferredName = GetPreferredLocaleString(pkg.DisplayName, pkg.Name)
	pkg.PreferredDesc = GetPreferredLocaleString(pkg.Description, "")
	pkg.PreferredFunding = getPreferredFunding(pkg.Funding)

	// update info
	disallowVer := isBelowRequiredAppVersion(&pkg)
	pkg.DisallowInstall = disallowVer
	pkg.DisallowUpdate = disallowVer
	pkg.UpdateRequiredMinAppVer = pkg.MinAppVersion
	if "plugins" == pkgType {
		bazaarIncompatible := IsIncompatiblePlugin(&pkg, frontend)
		pkg.BazaarIncompatible = &bazaarIncompatible
		if bazaarIncompatible {
			pkg.DisallowInstall = true
			pkg.DisallowUpdate = true
		}
	}

	// stats info
	pkg.Updated = repo.Updated
	pkg.HUpdated = formatUpdated(pkg.Updated)
	pkg.Stars = repo.Stars
	pkg.OpenIssues = repo.OpenIssues
	pkg.Size = repo.Size
	pkg.HSize = humanize.BytesCustomCeil(uint64(pkg.Size), 2)
	pkg.InstallSize = repo.InstallSize
	pkg.HInstallSize = humanize.BytesCustomCeil(uint64(pkg.InstallSize), 2)
	if stats := bazaarStats[repoURLHash[0]]; nil != stats { // get the stats for a single package via bazaarStats[owner/repo]
		pkg.Downloads = stats.Downloads
	}
	// TODO separate the local install size from the online stage data's install size, so it's not saved into installSizeCache
	bazaarMemMu.Lock()
	installSizeCache[pkg.RepoURL] = pkg.InstallSize
	bazaarMemMu.Unlock()
	return &pkg
}

// formatUpdated formats the release date string.
func formatUpdated(updated string) (ret string) {
	t, e := dateparse.ParseIn(updated, time.Now().Location())
	if nil == e {
		ret = t.Format("2006-01-02")
	} else {
		if strings.Contains(updated, "T") {
			ret = updated[:strings.Index(updated, "T")]
		} else {
			ret = strings.ReplaceAll(strings.ReplaceAll(updated, "T", ""), "Z", "")
		}
	}
	return
}
