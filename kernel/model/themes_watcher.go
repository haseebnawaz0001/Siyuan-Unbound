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

//go:build !darwin

package model

import (
	"os"
	"path/filepath"
	"time"

	"github.com/88250/gulu"
	"github.com/fsnotify/fsnotify"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/util"
)

var themesWatcher *fsnotify.Watcher

func WatchThemes() {
	if util.IsMobileContainer() {
		return
	}

	go watchThemes()
}

func watchThemes() {
	CloseWatchThemes()
	themesDir := util.ThemesPath

	var err error
	themesWatcher, err = fsnotify.NewWatcher()
	if err != nil {
		logging.LogErrorf("add themes watcher for folder [%s] failed: %s", themesDir, err)
		return
	}

	if !gulu.File.IsDir(themesDir) {
		os.MkdirAll(themesDir, 0755)
	}

	if err = themesWatcher.Add(themesDir); err != nil {
		logging.LogErrorf("add themes root watcher for folder [%s] failed: %s", themesDir, err)
		CloseWatchThemes()
		return
	}

	// Add a watch for each subdirectory so we can receive theme.css changes
	addThemesSubdirs(themesWatcher, themesDir)

	go func() {
		defer logging.Recover()

		var (
			timer     *time.Timer
			lastEvent fsnotify.Event
		)
		timer = time.NewTimer(100 * time.Millisecond)
		<-timer.C // timer should be expired at first

		for {
			select {
			case event, ok := <-themesWatcher.Events:
				if !ok {
					return
				}

				// Add a watch when a new directory is created
				if event.Op&fsnotify.Create == fsnotify.Create {
					if isThemesDirectSubdir(event.Name) {
						if addErr := themesWatcher.Add(event.Name); addErr != nil {
							logging.LogWarnf("add themes watcher for new folder [%s] failed: %s", event.Name, addErr)
						}
					}
				}

				lastEvent = event
				timer.Reset(time.Millisecond * 100)
			case err, ok := <-themesWatcher.Errors:
				if !ok {
					return
				}
				logging.LogErrorf("watch themes failed: %s", err)
			case <-timer.C:
				handleThemesEvent(lastEvent)
			}
		}
	}()
}

// addThemesSubdirs adds a watch for each subdirectory under themes
func addThemesSubdirs(w *fsnotify.Watcher, themesDir string) {
	entries, err := os.ReadDir(themesDir)
	if err != nil {
		logging.LogErrorf("read themes folder failed: %s", err)
		return
	}
	for _, e := range entries {
		if !util.IsDirRegularOrSymlink(e) {
			continue
		}
		subdir := filepath.Join(themesDir, e.Name())
		if addErr := w.Add(subdir); addErr != nil {
			logging.LogWarnf("add themes watcher for folder [%s] failed: %s", subdir, addErr)
		}
	}
}

// isThemesDirectSubdir reports whether path is a direct subdirectory of themes
func isThemesDirectSubdir(path string) bool {
	if !gulu.File.IsDir(path) {
		return false
	}
	rel, err := filepath.Rel(util.ThemesPath, path)
	if err != nil {
		return false
	}
	if filepath.Base(path) != rel {
		return false
	}
	entries, err := os.ReadDir(util.ThemesPath)
	if err != nil {
		return false
	}
	name := filepath.Base(path)
	for _, e := range entries {
		if e.Name() == name {
			return util.IsDirRegularOrSymlink(e)
		}
	}
	return false
}

func handleThemesEvent(event fsnotify.Event) {
	if event.Op&fsnotify.Write != fsnotify.Write {
		return
	}
	broadcastRefreshThemeIfCurrent(event.Name)
}

func CloseWatchThemes() {
	if nil != themesWatcher {
		themesWatcher.Close()
		themesWatcher = nil
	}
}
