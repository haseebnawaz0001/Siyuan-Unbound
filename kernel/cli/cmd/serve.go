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

package cmd

import (
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/cache"
	"github.com/siyuan-note/siyuan/kernel/job"
	"github.com/siyuan-note/siyuan/kernel/model"
	"github.com/siyuan-note/siyuan/kernel/plugin"
	"github.com/siyuan-note/siyuan/kernel/server"
	"github.com/siyuan-note/siyuan/kernel/sql"
	"github.com/siyuan-note/siyuan/kernel/util"

	"github.com/spf13/cobra"
)

// Flag values specific to the serve subcommand. --workspace reuses rootCmd's persistent flag and is not declared again here.
var (
	serveWdPath         string
	servePort           string
	serveReadOnly       string
	serveAccessAuthCode string
	serveLang           string
	serveMode           string
	serveSSL            bool
	serveAttachUI       bool
	serveSafeMode       bool
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start kernel HTTP server",
	Long:  "Start kernel HTTP server. All serving-related options below are passed to the kernel boot.",
	// These flags are parsed by cobra (see init); serve -h lists all of them directly.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// serve bypasses root's init, but --log-level must be applied before BootWithFlags (which includes
		// startup logs such as logBootInfo), otherwise the level specified on the command line would be
		// discarded; it's also recorded into util.CLILogLevel, so the later model.InitConf no longer overrides
		// it with conf.json.
		if "" != logLevel {
			logging.SetLogLevel(logLevel)
			util.CLILogLevel = logLevel
		}
		return nil // bypass root's init — BootWithFlags() handles it
	},
	Run: func(cmd *cobra.Command, args []string) {
		// --workspace prefers serve's own value (rootCmd's persistent flag); falling back to the environment
		// variable and the default value is handled internally by util.BootWithFlags (matching the original
		// Boot() behavior).
		ws := workspacePath

		util.BootWithFlags(ws, serveWdPath, servePort, serveReadOnly, serveAccessAuthCode, serveLang, serveMode, serveSSL, serveAttachUI, serveSafeMode)

		model.InitJwtKey()
		model.InitConf()
		go server.Serve(false, model.Conf.CookieKey)
		model.InitAppearance()
		sql.InitDatabase(false)
		sql.InitHistoryDatabase(false)
		sql.InitAssetContentDatabase(false)
		sql.SetCaseSensitive(model.Conf.Search.CaseSensitive)
		sql.SetIndexAssetPath(model.Conf.Search.IndexAssetPath)

		model.BootSyncData()
		model.InitBoxes()
		model.LoadFlashcards()
		util.LoadAssetsTexts()

		util.SetBooted()
		util.PushClearAllMsg()

		job.StartCron()

		go model.AutoGenerateFileHistory()
		go cache.LoadAssets()
		go util.CheckFileSysStatus()
		go plugin.InitManager()
		go model.StartEmbeddingIndexer()

		model.WatchAssets()
		model.WatchEmojis()
		model.WatchThemes()
		model.HandleSignal()
	},
}

func init() {
	// The --wd default value is the parent of the directory containing the kernel executable (the packaged
	// resources/, where appearance/ and stage/ live), using the same resolveWorkingDir() as
	// rootCmd.PersistentPreRunE, ensuring the two startup paths behave consistently.
	serveCmd.Flags().StringVar(&serveWdPath, "wd", resolveWorkingDir(), "working directory of SiYuan")
	serveCmd.Flags().StringVar(&servePort, "port", "0", "port of the HTTP server")
	serveCmd.Flags().StringVar(&serveReadOnly, "readonly", "false", "read-only mode")
	serveCmd.Flags().StringVar(&serveAccessAuthCode, "accessAuthCode", "", "access auth code")
	serveCmd.Flags().StringVar(&serveLang, "lang", "", "ar/de/en/es/fr/he/hi/id/it/ja/ko/nl/pl/pt-BR/ru/sk/th/tr/uk/zh-CN/zh-TW")
	serveCmd.Flags().StringVar(&serveMode, "mode", "prod", "dev/prod")
	serveCmd.Flags().BoolVar(&serveSSL, "ssl", false, "for https and wss")
	serveCmd.Flags().BoolVar(&serveAttachUI, "attach-ui", false, "attach kernel lifecycle to desktop UI process (used by Electron)")
	serveCmd.Flags().BoolVar(&serveSafeMode, "safe-mode", false, "boot in safe mode")

	rootCmd.AddCommand(serveCmd)
}
