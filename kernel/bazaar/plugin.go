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
	"runtime"

	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func ParseInstalledPlugin(name, frontend string) (found bool, version, displayName string, incompatible, disabledInPublish, disallowInstall, kernelIncompatible bool) {
	pluginsPath := filepath.Join(util.DataDir, "plugins")
	if !util.IsPathRegularDirOrSymlinkDir(pluginsPath) {
		return
	}

	pluginDirs, err := os.ReadDir(pluginsPath)
	if err != nil {
		logging.LogWarnf("read plugins folder failed: %s", err)
		return
	}

	for _, pluginDir := range pluginDirs {
		if !util.IsDirRegularOrSymlink(pluginDir) {
			continue
		}
		dirName := pluginDir.Name()
		if name != dirName {
			continue
		}

		plugin, parseErr := ParsePackageJSON(filepath.Join(util.DataDir, "plugins", dirName, "plugin.json"))
		if nil != parseErr || nil == plugin {
			return
		}

		found = true
		version = plugin.Version
		displayName = GetPreferredLocaleString(plugin.DisplayName, plugin.Name)
		incompatible = IsIncompatiblePlugin(plugin, frontend)
		disabledInPublish = plugin.DisabledInPublish
		disallowInstall = isBelowRequiredAppVersion(plugin)
		kernelIncompatible = IsIncompatibleKernelPlugin(plugin)
	}
	return
}

// IsIncompatiblePlugin determines whether the plugin is incompatible with the current environment
func IsIncompatiblePlugin(plugin *Package, frontend string) bool {
	backend := GetCurrentBackend()
	if !IsTargetSupported(plugin.Backends, backend) {
		return true
	}

	// When frontend is empty, compatibility is not checked (treated as compatible)
	if "" == frontend {
		return false
	}

	if !IsTargetSupported(plugin.Frontends, frontend) {
		return true
	}

	return false
}

// IsIncompatibleKernelPlugin determines whether the kernel plugin is incompatible with the current environment
func IsIncompatibleKernelPlugin(plugin *Package) bool {
	// When the kernel field is absent from plugin.json, treat it as incompatible (the plugin can still be
	// installed, but the kernel plugin inside it will not be started)
	if len(plugin.Kernels) == 0 {
		return true
	}

	return !IsTargetSupported(plugin.Kernels, GetCurrentBackend())
}

var cachedBackend string

func GetCurrentBackend() string {
	if cachedBackend == "" {
		if util.Container == util.ContainerStd {
			cachedBackend = runtime.GOOS
		} else {
			cachedBackend = util.Container
		}
	}
	return cachedBackend
}

// IsTargetSupported checks whether platforms contains target or "all"
func IsTargetSupported(platforms []string, target string) bool {
	// When the field is missing, skip the check, equivalent to all
	if len(platforms) == 0 {
		return true
	}
	for _, v := range platforms {
		if v == target || v == "all" {
			return true
		}
	}
	return false
}
