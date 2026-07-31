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

package conf

import "github.com/siyuan-note/siyuan/kernel/util"

type Appearance struct {
	Mode                int                 `json:"mode"`                // Mode: 0: light, 1: dark
	ModeOS              bool                `json:"modeOS"`              // Whether the mode follows the system
	DarkThemes          []*AppearanceTheme  `json:"darkThemes"`          // List of dark mode appearance themes
	LightThemes         []*AppearanceTheme  `json:"lightThemes"`         // List of light mode appearance themes
	ThemeDark           string              `json:"themeDark"`           // Selected dark mode appearance theme
	ThemeLight          string              `json:"themeLight"`          // Selected light mode appearance theme
	ThemeVer            string              `json:"themeVer"`            // Selected theme version
	Icons               []*AppearanceIcon   `json:"icons"`               // List of icons
	Icon                string              `json:"icon"`                // Selected icon
	IconVer             string              `json:"iconVer"`             // Selected icon version
	CodeBlockThemeLight string              `json:"codeBlockThemeLight"` // Code block theme in light mode
	CodeBlockThemeDark  string              `json:"codeBlockThemeDark"`  // Code block theme in dark mode
	Lang                string              `json:"lang"`                // Selected UI language, same as AppConf.Lang
	ThemeJS             bool                `json:"themeJS"`             // Whether theme JavaScript is enabled
	CloseButtonBehavior int                 `json:"closeButtonBehavior"` // Close button behavior, 0: quit, 1: minimize to tray
	HideToolbar         bool                `json:"hideToolbar"`         // Whether to hide the top toolbar
	HideStatusBar       bool                `json:"hideStatusBar"`       // Whether to hide the bottom status bar
	StatusBar           *util.StatusBar     `json:"statusBar"`           // Bottom status bar configuration
	Notifications       *util.Notifications `json:"notifications"`       // Appearance notification toggle configuration
}

func NewAppearance() *Appearance {
	return &Appearance{
		Mode:                0,
		ModeOS:              true,
		ThemeDark:           "midnight",
		ThemeLight:          "daylight",
		Icon:                "litheness",
		CodeBlockThemeLight: "github",
		CodeBlockThemeDark:  "base16/dracula",
		Lang:                "en",
		CloseButtonBehavior: 0,
		HideToolbar:         true,
		HideStatusBar:       false,
		StatusBar:           &util.StatusBar{},
		Notifications:       util.NewNotifications(),
	}
}

type AppearanceTheme struct {
	Name  string `json:"name"`  // daylight
	Label string `json:"label"` // i18n display name
}

type AppearanceIcon struct {
	Name  string `json:"name"`  // litheness
	Label string `json:"label"` // i18n display name
}
