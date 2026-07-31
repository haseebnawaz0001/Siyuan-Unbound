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
	"html"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/88250/gulu"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/util"
)

// LocaleStrings represents a string table keyed by locale, where the key is a locale such as "default", "en_US", "zh_CN", etc
type LocaleStrings map[string]string

type Funding struct {
	OpenCollective string   `json:"openCollective"`
	Patreon        string   `json:"patreon"`
	GitHub         string   `json:"github"`
	Custom         []string `json:"custom"`
}

// Package describes bazaar package metadata and other info passed to the frontend.
//   - Adding a new metadata field for bazaar packages requires updating the bazaar workflow in sync;
//     see https://github.com/siyuan-note/bazaar/commit/aa36d0003139c52d8e767c6e18a635be006323e2
type Package struct {
	Author            string        `json:"author"`
	URL               string        `json:"url"`
	Version           string        `json:"version"`
	MinAppVersion     string        `json:"minAppVersion"`
	DisabledInPublish bool          `json:"disabledInPublish"`
	Kernels           []string      `json:"kernels"`
	Backends          []string      `json:"backends"`
	Frontends         []string      `json:"frontends"`
	DisplayName       LocaleStrings `json:"displayName"`
	Description       LocaleStrings `json:"description"`
	Readme            LocaleStrings `json:"readme"`
	Funding           *Funding      `json:"funding"`
	Keywords          []string      `json:"keywords"`

	PreferredFunding string `json:"preferredFunding"`
	PreferredName    string `json:"preferredName"`
	PreferredDesc    string `json:"preferredDesc"`
	PreferredReadme  string `json:"preferredReadme"`

	Name       string `json:"name"`    // package name, not necessarily the repo name
	RepoURL    string `json:"repoURL"` // in the form https://github.com/owner/repo
	RepoHash   string `json:"repoHash"`
	PreviewURL string `json:"previewURL"`
	IconURL    string `json:"iconURL"`

	Installed               bool   `json:"installed"`
	Outdated                bool   `json:"outdated"`
	Current                 bool   `json:"current"`
	Updated                 string `json:"updated"`
	Stars                   int    `json:"stars"`
	OpenIssues              int    `json:"openIssues"`
	Size                    int64  `json:"size"`
	HSize                   string `json:"hSize"`
	InstallSize             int64  `json:"installSize"`
	HInstallSize            string `json:"hInstallSize"`
	HInstallDate            string `json:"hInstallDate"`
	HUpdated                string `json:"hUpdated"`
	Downloads               int    `json:"downloads"`
	DisallowInstall         bool   `json:"disallowInstall"`
	DisallowUpdate          bool   `json:"disallowUpdate"`
	UpdateRequiredMinAppVer string `json:"updateRequiredMinAppVer,omitempty"` // the minimum app version required by the update target

	// dedicated fields, not serialized when nil
	InstalledIncompatible *bool     `json:"installedIncompatible,omitempty"` // Plugin: whether the locally installed version is incompatible
	BazaarIncompatible    *bool     `json:"bazaarIncompatible,omitempty"`    // Plugin: whether the online bazaar version is incompatible
	Enabled               *bool     `json:"enabled,omitempty"`               // Plugin: whether it's enabled
	Modes                 *[]string `json:"modes,omitempty"`                 // Theme: the list of supported modes
}

type StageRepo struct {
	URL         string `json:"url"` // in the form owner/repo@hash
	Updated     string `json:"updated"`
	Stars       int    `json:"stars"`
	OpenIssues  int    `json:"openIssues"`
	Size        int64  `json:"size"`
	InstallSize int64  `json:"installSize"`

	// Package is identical to the full package embedded in stage/*.json, and can be used directly to build the list
	Package *Package `json:"package"`
}

type StageIndex struct {
	Repos []*StageRepo `json:"repos"`

	reposByURL map[string]*StageRepo // not serialized; lazily built on the first lookup by URL, and expires along with the whole index
	reposOnce  sync.Once
}

// ParsePackageJSON parses a bazaar package JSON file
func ParsePackageJSON(filePath string) (ret *Package, err error) {
	if !filelock.IsExist(filePath) {
		err = os.ErrNotExist
		return
	}
	data, err := filelock.ReadFile(filePath)
	if err != nil {
		logging.LogErrorf("read [%s] failed: %s", filePath, err)
		return
	}
	if err = gulu.JSON.UnmarshalJSON(data, &ret); err != nil {
		logging.LogErrorf("parse [%s] failed: %s", filePath, err)
		return
	}

	ret.URL = strings.TrimSuffix(ret.URL, "/")
	return
}

// unescapePackageDisplayStrings restores display fields that were HTML-escaped in the online stage back to
// their original text, matching the local JSON.
func unescapePackageDisplayStrings(pkg *Package) {
	if pkg == nil {
		return
	}
	pkg.Name = html.UnescapeString(pkg.Name)
	pkg.Author = html.UnescapeString(pkg.Author)
	pkg.Version = html.UnescapeString(pkg.Version)
	for k, v := range pkg.DisplayName {
		pkg.DisplayName[k] = html.UnescapeString(v)
	}
	for k, v := range pkg.Description {
		pkg.Description[k] = html.UnescapeString(v)
	}
	if pkg.Funding != nil {
		pkg.Funding.OpenCollective = html.UnescapeString(pkg.Funding.OpenCollective)
		pkg.Funding.Patreon = html.UnescapeString(pkg.Funding.Patreon)
		pkg.Funding.GitHub = html.UnescapeString(pkg.Funding.GitHub)
		for i, v := range pkg.Funding.Custom {
			pkg.Funding.Custom[i] = html.UnescapeString(v)
		}
	}
	for i, kw := range pkg.Keywords {
		pkg.Keywords[i] = html.UnescapeString(kw)
	}
}

// GetPreferredLocaleString takes the value from LocaleStrings for the current locale; if absent, falls back to
// default, en, en_US (for legacy naming compatibility), then falls back to fallback.
func GetPreferredLocaleString(m LocaleStrings, fallback string) string {
	if len(m) == 0 {
		return fallback
	}
	if v := strings.TrimSpace(m[util.Lang]); "" != v {
		return v
	}
	// compatible with legacy underscore keys in bazaar JSON data (zh_CN, en_US, etc)
	if v := strings.TrimSpace(m[util.LangToLegacy(util.Lang)]); "" != v {
		return v
	}
	if v := strings.TrimSpace(m["default"]); "" != v {
		return v
	}
	if v := strings.TrimSpace(m["en"]); "" != v {
		return v
	}
	if v := strings.TrimSpace(m["en_US"]); "" != v {
		return v
	}
	return fallback
}

// getPreferredFunding gets the package's preferred funding link
func getPreferredFunding(funding *Funding) string {
	if nil == funding {
		return ""
	}
	if v := normalizeFundingURL(funding.OpenCollective, "https://opencollective.com/"); "" != v {
		return v
	}
	if v := normalizeFundingURL(funding.Patreon, "https://www.patreon.com/"); "" != v {
		return v
	}
	if v := normalizeFundingURL(funding.GitHub, "https://github.com/sponsors/"); "" != v {
		return v
	}
	if 0 < len(funding.Custom) {
		v := funding.Custom[0]
		if strings.HasPrefix(v, "https://") || strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "mailto:") {
			return v
		}
		return ""
	}
	return ""
}

func normalizeFundingURL(s, base string) string {
	if "" == s {
		return ""
	}
	if strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://") {
		return s
	}
	return base + s
}

// FilterPackages filters the bazaar package list by keyword
func FilterPackages(packages []*Package, keyword string) []*Package {
	keywords := getSearchKeywords(keyword)
	if 0 == len(keywords) {
		return packages
	}
	ret := []*Package{}
	for _, pkg := range packages {
		if packageContainsKeywords(pkg, keywords) {
			ret = append(ret, pkg)
		}
	}
	return ret
}

func getSearchKeywords(query string) (ret []string) {
	query = strings.TrimSpace(query)
	if "" == query {
		return
	}
	keywords := strings.SplitSeq(query, " ")
	for k := range keywords {
		if "" != k {
			ret = append(ret, strings.ToLower(k))
		}
	}
	return
}

func packageContainsKeywords(pkg *Package, keywords []string) bool {
	if 0 == len(keywords) {
		return true
	}
	if nil == pkg {
		return false
	}
	for _, kw := range keywords {
		if !packageContainsKeyword(pkg, kw) {
			return false
		}
	}
	return true
}

func packageContainsKeyword(pkg *Package, kw string) bool {
	if strings.Contains(strings.ToLower(pkg.Name), kw) || // https://github.com/siyuan-note/siyuan/issues/10515
		strings.Contains(strings.ToLower(pkg.Author), kw) { // https://github.com/siyuan-note/siyuan/issues/11673
		return true
	}
	for _, s := range pkg.DisplayName {
		if strings.Contains(strings.ToLower(s), kw) {
			return true
		}
	}
	for _, s := range pkg.Description {
		if strings.Contains(strings.ToLower(s), kw) {
			return true
		}
	}
	for _, s := range pkg.Keywords {
		if strings.Contains(strings.ToLower(s), kw) {
			return true
		}
	}
	if strings.Contains(strings.ToLower(path.Base(pkg.RepoURL)), kw) { // repo name, not necessarily the package name
		return true
	}
	return false
}
