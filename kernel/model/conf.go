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
	"bytes"
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/88250/gulu"
	"github.com/88250/lute"
	"github.com/88250/lute/ast"
	"github.com/siyuan-note/eventbus"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/conf"
	"github.com/siyuan-note/siyuan/kernel/sql"
	"github.com/siyuan-note/siyuan/kernel/task"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
	"golang.org/x/mod/semver"
)

var Conf *AppConf

// AppConf maintains the application metadata, persisted to ~/.siyuan/conf.json.
type AppConf struct {
	LogLevel       string               `json:"logLevel"`       // log level: off, trace, debug, info, warn, error, fatal
	Appearance     *conf.Appearance     `json:"appearance"`     // appearance settings
	Langs          []*conf.Lang         `json:"langs"`          // list of available UI languages
	Lang           string               `json:"lang"`           // the selected UI language, mirrors Appearance.Lang
	FileTree       *conf.FileTree       `json:"fileTree"`       // file tree panel
	Tag            *conf.Tag            `json:"tag"`            // tag panel
	Editor         *conf.Editor         `json:"editor"`         // editor configuration
	Export         *conf.Export         `json:"export"`         // export configuration
	Graph          *conf.Graph          `json:"graph"`          // graph view configuration
	UILayout       *conf.UILayout       `json:"uiLayout"`       // UI layout; do not use directly, use GetUILayout() and SetUILayout() instead
	UserData       string               `json:"userData"`       // community user info, stores User in encrypted form
	User           *conf.User           `json:"-"`              // in-memory community user struct, not persisted; do not use directly, use GetUser() and SetUser() instead
	Account        *conf.Account        `json:"account"`        // account configuration
	ReadOnly       bool                 `json:"readonly"`       // whether running in read-only mode
	ServerAddrs    []string             `json:"serverAddrs"`    // list of local server addresses
	AccessAuthCode string               `json:"accessAuthCode"` // lock screen password
	System         *conf.System         `json:"system"`         // system configuration
	Keymap         *conf.Keymap         `json:"keymap"`         // keymap configuration
	Sync           *conf.Sync           `json:"sync"`           // sync configuration
	Search         *conf.Search         `json:"search"`         // search configuration
	Flashcard      *conf.Flashcard      `json:"flashcard"`      // flashcard configuration
	AI             *conf.AI             `json:"ai"`             // AI configuration
	Secrets        *conf.Secrets        `json:"secrets"`        // global secrets store
	Variables      *conf.Variables      `json:"variables"`      // global variables store
	Bazaar         *conf.Bazaar         `json:"bazaar"`         // bazaar (marketplace) configuration
	Stat           *conf.Stat           `json:"stat"`           // statistics
	Api            *conf.API            `json:"api"`            // API
	Repo           *conf.Repo           `json:"repo"`           // data repo
	NotebookCrypto *conf.NotebookCrypto `json:"notebookCrypto"` // encrypted notebook key management
	Publish        *conf.Publish        `json:"publish"`        // publish service
	Onboarding     *conf.Onboarding     `json:"onboarding"`     // first-run onboarding
	ShowChangelog  bool                 `json:"showChangelog"`  // whether to show the version changelog
	CloudRegion    int                  `json:"cloudRegion"`    // cloud region: 0 = mainland China, 1 = North America
	Snippet        *conf.Snpt           `json:"snippet"`        // code snippets
	DataIndexState int                  `json:"dataIndexState"` // data index state: 0 = indexed, 1 = not indexed
	CookieKey      string               `json:"cookieKey"`      // key used to encrypt the cookie

	MCPOAuth string `json:"mcpOAuth"` // encrypted MCP OAuth credentials

	m        *sync.RWMutex // lock guarding the config data
	userLock *sync.RWMutex // separate lock for user data, avoiding contention with config save operations
}

func NewAppConf() *AppConf {
	return &AppConf{
		LogLevel: "debug",
		// English-first build: new installs use the North America cloud. This runs before conf.json is unmarshalled
		// over it, so an existing workspace keeps whichever region the user already saved.
		CloudRegion: 1,
		m:           &sync.RWMutex{},
		userLock:    &sync.RWMutex{},
	}
}

func (conf *AppConf) GetMCPOAuth() string {
	conf.m.RLock()
	defer conf.m.RUnlock()
	return conf.MCPOAuth
}

func (conf *AppConf) SetMCPOAuth(value string) {
	conf.m.Lock()
	conf.MCPOAuth = value
	conf.m.Unlock()
	conf.Save()
}

func (conf *AppConf) SetAI(ai *conf.AI) {
	conf.m.Lock()
	conf.AI = ai
	conf.m.Unlock()
	conf.Save()
}

func (conf *AppConf) GetUILayout() *conf.UILayout {
	conf.m.Lock()
	defer conf.m.Unlock()
	return conf.UILayout
}

func (conf *AppConf) SetUILayout(uiLayout *conf.UILayout) {
	conf.m.Lock()
	defer conf.m.Unlock()
	conf.UILayout = uiLayout
}

func (conf *AppConf) GetUser() *conf.User {
	conf.userLock.RLock()
	defer conf.userLock.RUnlock()
	return conf.User
}

func (conf *AppConf) SetUser(user *conf.User) {
	conf.userLock.Lock()
	defer conf.userLock.Unlock()
	conf.User = user
}

func InitConf() {
	initLang()

	Conf = NewAppConf()
	clearEncryptedExportTempOnBoot()
	confPath := filepath.Join(util.ConfDir, "conf.json")
	if gulu.File.IsExist(confPath) {
		if data, err := os.ReadFile(confPath); err != nil {
			logging.LogErrorf("load conf [%s] failed: %s", confPath, err)
		} else {
			// If parsing fails, keep whatever fields were successfully written; unexported fields (m, userLock) and
			// untouched exported fields retain their NewAppConf() defaults.
			if err = gulu.JSON.UnmarshalJSON(data, Conf); err != nil {
				logging.LogWarnf("parse conf failed, parsed fields retained: %s", err)
			} else {
				logging.LogInfof("loaded conf [%s]", confPath)
			}

			// Detect and complete any interrupted password-change migration on boot
			recoverMasterPasswordMigration()

			if conf.NeedsAIMigration(data) {
				Conf.AI = conf.MigrateAI(data)
				Conf.Save()
				logging.LogInfof("migrated AI config [%s]", confPath)
			}

			// After a restart, the DEK of an encrypted notebook is lost (it only ever lived in memory), so it must
			// be unlocked again. Force-mark every encrypted notebook as closed so the boot-time indexer doesn't
			// try to read still-encrypted .sy files it can't decrypt.
			// Use IsEncryptedBox for the check consistently (including backup fallback).
			changed := false
			for _, box := range Conf.GetBoxes() {
				if IsEncryptedBox(box.ID) && !box.Closed {
					boxConf := box.GetConf()
					boxConf.Closed = true
					if err := box.SaveConf(boxConf); err != nil {
						logging.LogErrorf("close encrypted notebook on boot [%s] failed: %s", box.ID, err)
					}
					changed = true
				}
			}
			if changed {
				logging.LogInfof("closed encrypted notebooks on boot (DEK not in memory)")
			}
		}
	}

	if "" != util.Lang {
		initialized := false
		if util.IsMobileContainer() {
			// On mobile, defer to the previously set appearance language
			if "" != Conf.Lang && util.Lang != Conf.Lang {
				util.Lang = Conf.Lang
				logging.LogInfof("use the last specified language [%s]", util.Lang)
				initialized = true
			}
		}

		if !initialized {
			Conf.Lang = util.Lang
			logging.LogInfof("initialized the specified language [%s]", util.Lang)
		}
	} else if "" == Conf.Lang {
		// English-first build: the UI language is never inferred from the OS locale, so a fresh install opens in
		// English everywhere. Users can still pick any bundled language in Settings and the choice is persisted in
		// conf.json; --lang and SIYUAN_LANG continue to override this (handled by the branch above).
		util.Lang = "en"
		logging.LogInfof("initialized the default language [%s]", util.Lang)
		Conf.Lang = util.Lang
	} else {
		// conf.json already has an appearance language saved
		util.Lang = Conf.Lang
	}

	// Migrate legacy underscore-style language codes to their BCP 47 equivalents (zh_CN -> zh-CN, etc.)
	if migrated := util.LangToBCP47(Conf.Lang); migrated != Conf.Lang {
		logging.LogInfof("migrate legacy lang [%s] → [%s]", Conf.Lang, migrated)
		Conf.Lang = migrated
		util.Lang = migrated
	}

	Conf.Langs = loadLangs()
	if nil == Conf.Appearance {
		Conf.Appearance = conf.NewAppearance()
	}
	var langOK bool
	for _, l := range Conf.Langs {
		if Conf.Lang == l.Name {
			langOK = true
			break
		}
	}
	if !langOK {
		Conf.Lang = "en"
		util.Lang = Conf.Lang
	}
	Conf.Appearance.Lang = Conf.Lang

	// Legacy underscore-named i18n files (zh_CN.json, etc.) have been renamed to BCP 47 (zh-CN.json, etc.);
	// clean up any leftover old-named files under ConfDir/appearance/langs/ so they don't linger as dead files.
	if langsDir := filepath.Join(util.AppearancePath, "langs"); gulu.File.IsDir(langsDir) {
		if entries, err := os.ReadDir(langsDir); err == nil {
			for _, entry := range entries {
				name := entry.Name()
				if entry.IsDir() || !strings.HasSuffix(name, ".json") {
					continue
				}
				stem := strings.TrimSuffix(name, ".json")
				if _, ok := util.LangLegacyToBCP47[stem]; !ok {
					continue
				}
				os.RemoveAll(filepath.Join(langsDir, name))
			}
		}
	}
	if "ant" == Conf.Appearance.Icon || "material" == Conf.Appearance.Icon {
		// v3.7.0 removed the ant/material icon packs; if the user had previously selected either one, switch to
		// the litheness icon pack after upgrading to avoid broken icon rendering https://github.com/siyuan-note/siyuan/issues/7976
		Conf.Appearance.Icon = "litheness"
	}
	os.RemoveAll(filepath.Join(util.IconsPath, "ant"))
	os.RemoveAll(filepath.Join(util.IconsPath, "material"))
	if nil == Conf.UILayout {
		Conf.UILayout = &conf.UILayout{}
	}
	if nil == Conf.Keymap {
		Conf.Keymap = &conf.Keymap{}
	}
	if "" == Conf.Appearance.CodeBlockThemeDark {
		Conf.Appearance.CodeBlockThemeDark = "dracula"
	}
	if "" == Conf.Appearance.CodeBlockThemeLight {
		Conf.Appearance.CodeBlockThemeLight = "github"
	}
	if nil == Conf.Appearance.StatusBar {
		Conf.Appearance.StatusBar = &util.StatusBar{}
	}
	util.StatusBarCfg = Conf.Appearance.StatusBar
	if nil == Conf.Appearance.Notifications {
		Conf.Appearance.Notifications = util.NewNotifications()
	}
	util.NotificationsCfg = Conf.Appearance.Notifications
	if nil == Conf.FileTree {
		Conf.FileTree = conf.NewFileTree()
	}
	if 1 > Conf.FileTree.MaxListCount {
		Conf.FileTree.MaxListCount = 512
	}
	if 1 > Conf.FileTree.MaxOpenTabCount {
		Conf.FileTree.MaxOpenTabCount = 8
	}
	if 32 < Conf.FileTree.MaxOpenTabCount {
		Conf.FileTree.MaxOpenTabCount = 32
	}
	Conf.FileTree.DocCreateSavePath = util.TrimSpaceInPath(Conf.FileTree.DocCreateSavePath)
	Conf.FileTree.RefCreateSavePath = util.TrimSpaceInPath(Conf.FileTree.RefCreateSavePath)
	Conf.FileTree.ShorthandSavePath = util.TrimSpaceInPath(Conf.FileTree.ShorthandSavePath)
	util.UseSingleLineSave = Conf.FileTree.UseSingleLineSave
	if 2 > Conf.FileTree.LargeFileWarningSize {
		Conf.FileTree.LargeFileWarningSize = 8
	}
	util.LargeFileWarningSize = Conf.FileTree.LargeFileWarningSize
	if nil == Conf.FileTree.CreateDocAtTop { // versions before v3.4.0 lacked this field; default it to true (create new docs at the top) to keep users' existing habit
		Conf.FileTree.CreateDocAtTop = func() *bool { b := true; return &b }()
	}
	if nil == Conf.FileTree.BoxDocEnabled {
		// Existing workspaces default to having the top-level notebook document disabled; new workspaces use the
		// default from NewFileTree.
		Conf.FileTree.BoxDocEnabled = func() *bool { b := false; return &b }()
	}

	if conf.MinFileTreeRecentDocsListCount > Conf.FileTree.RecentDocsMaxListCount {
		Conf.FileTree.RecentDocsMaxListCount = conf.MinFileTreeRecentDocsListCount
	}
	if conf.MaxFileTreeRecentDocsListCount < Conf.FileTree.RecentDocsMaxListCount {
		Conf.FileTree.RecentDocsMaxListCount = conf.MaxFileTreeRecentDocsListCount
	}

	util.CurrentCloudRegion = Conf.CloudRegion

	if nil == Conf.Tag {
		Conf.Tag = conf.NewTag()
	}

	defaultEditor := conf.NewEditor()
	if nil == Conf.Editor {
		Conf.Editor = defaultEditor
	}

	// Defaults for newly added fields; pointer types are used to distinguish a missing field (nil) from the user
	// explicitly setting it to 0 (non-nil)
	if nil == Conf.Editor.BacklinkSort {
		Conf.Editor.BacklinkSort = defaultEditor.BacklinkSort
	}
	if nil == Conf.Editor.BackmentionSort {
		Conf.Editor.BackmentionSort = defaultEditor.BackmentionSort
	}
	if 1 > len(Conf.Editor.Emoji) {
		Conf.Editor.Emoji = []string{}
	}
	for i, emoji := range Conf.Editor.Emoji {
		if strings.Contains(emoji, ".") {
			// XSS through emoji name https://github.com/siyuan-note/siyuan/issues/15034
			emoji = util.FilterUploadEmojiFileName(emoji)
			Conf.Editor.Emoji[i] = emoji
		}
	}
	if 9 > Conf.Editor.FontSize || 72 < Conf.Editor.FontSize {
		Conf.Editor.FontSize = 16
	}
	if "" == Conf.Editor.PlantUMLServePath {
		Conf.Editor.PlantUMLServePath = "https://www.plantuml.com/plantuml/svg/~1"
	}
	if 1 > Conf.Editor.BlockRefDynamicAnchorTextMaxLen {
		Conf.Editor.BlockRefDynamicAnchorTextMaxLen = 64
	}
	if 5120 < Conf.Editor.BlockRefDynamicAnchorTextMaxLen {
		Conf.Editor.BlockRefDynamicAnchorTextMaxLen = 5120
	}
	if 1440 < Conf.Editor.GenerateHistoryInterval {
		Conf.Editor.GenerateHistoryInterval = 1440
	}
	if 1 > Conf.Editor.HistoryRetentionDays {
		Conf.Editor.HistoryRetentionDays = 30
	}
	if 3650 < Conf.Editor.HistoryRetentionDays {
		Conf.Editor.HistoryRetentionDays = 3650
	}
	if nil == Conf.Editor.FloatWindowDelay {
		v := 620
		Conf.Editor.FloatWindowDelay = &v
	} else {
		*Conf.Editor.FloatWindowDelay = max(0, min(2000, *Conf.Editor.FloatWindowDelay))
	}
	if conf.MinDynamicLoadBlocks > Conf.Editor.DynamicLoadBlocks {
		Conf.Editor.DynamicLoadBlocks = conf.MinDynamicLoadBlocks
	}
	if 1 > len(Conf.Editor.SpellcheckLanguages) {
		Conf.Editor.SpellcheckLanguages = []string{"en-US"}
	}
	if 0 > Conf.Editor.BacklinkExpandCount {
		Conf.Editor.BacklinkExpandCount = 0
	}
	if -1 > Conf.Editor.BackmentionExpandCount {
		Conf.Editor.BackmentionExpandCount = -1
	}
	if nil == Conf.Editor.Markdown {
		Conf.Editor.Markdown = &util.Markdown{}
	}
	util.MarkdownSettings = Conf.Editor.Markdown

	if nil == Conf.Export {
		Conf.Export = conf.NewExport()
	}
	if 0 == Conf.Export.BlockRefMode || 1 == Conf.Export.BlockRefMode || 5 == Conf.Export.BlockRefMode {
		// Deprecated export options for converting block refs to raw blocks and block quotes https://github.com/siyuan-note/siyuan/issues/3155
		// The anchor-hash mode and footnote mode have been merged https://github.com/siyuan-note/siyuan/issues/13331
		Conf.Export.BlockRefMode = 4 // switch to footnote + anchor hash
	}
	if "" == Conf.Export.PandocBin {
		Conf.Export.PandocBin = util.PandocBinPath
	}

	if nil == Conf.Graph || nil == Conf.Graph.Local || nil == Conf.Graph.Global {
		Conf.Graph = conf.NewGraph()
	}

	isNewWorkspace := nil == Conf.System
	if isNewWorkspace {
		Conf.System = conf.NewSystem()
	} else {
		cmp := semver.Compare("v"+util.Ver, "v"+Conf.System.KernelVersion)
		if 0 < cmp {
			logging.LogInfof("upgraded from version [%s] to [%s]", Conf.System.KernelVersion, util.Ver)
			Conf.ShowChangelog = true
		} else if 0 > cmp {
			logging.LogInfof("downgraded from version [%s] to [%s]", Conf.System.KernelVersion, util.Ver)
		}

		Conf.System.KernelVersion = util.Ver
		Conf.System.IsInsider = util.IsInsider
	}
	if nil == Conf.Onboarding {
		Conf.Onboarding = &conf.Onboarding{State: conf.OnboardingCompleted}
	}
	if boxes, listErr := ListNotebooks(); listErr == nil {
		prepareOnboardingForEmptyWorkspace(Conf.Onboarding, util.ReadOnly, len(boxes))
	}
	if nil == Conf.System.NetworkProxy {
		Conf.System.NetworkProxy = &conf.NetworkProxy{}
	}
	if "" == Conf.System.ID {
		Conf.System.ID = util.GetDeviceID()
	}
	if "" == Conf.System.Name {
		Conf.System.Name = util.GetDeviceName()
	}
	if util.ContainerStd == util.Container {
		// The device name is a random value that stays fixed once first generated, so it is not regenerated on
		// every startup here; otherwise the sync device list would gain a new entry after every restart
		Conf.System.Name = util.GetDeviceName()
	}
	Conf.System.DisabledFeatures = util.DisabledFeatures
	if 1 > len(Conf.System.DisabledFeatures) {
		Conf.System.DisabledFeatures = []string{}
	}

	Conf.System.AppDir = util.WorkingDir
	Conf.System.ConfDir = util.ConfDir
	Conf.System.HomeDir = util.HomeDir
	Conf.System.WorkspaceDir = util.WorkspaceDir
	Conf.System.DataDir = util.DataDir
	Conf.System.Container = util.Container
	Conf.System.IsMicrosoftStore = util.ISMicrosoftStore
	if util.ISMicrosoftStore {
		logging.LogInfof("using Microsoft Store edition")
	}
	Conf.System.OS = runtime.GOOS
	Conf.System.OSPlatform = util.GetOSPlatform()

	docxTemplate := util.RemoveInvalid(Conf.Export.DocxTemplate)
	if "" != docxTemplate {
		params := util.RemoveInvalid(Conf.Export.PandocParams)
		if gulu.File.IsExist(docxTemplate) && !strings.Contains(params, "--reference-doc") && !Conf.System.IsMicrosoftStore {
			if !strings.HasPrefix(docxTemplate, "\"") {
				docxTemplate = "\"" + docxTemplate + "\""
			}
			params += " --reference-doc " + docxTemplate
			Conf.Export.PandocParams = strings.TrimSpace(params)
		}
		Conf.Export.DocxTemplate = ""
		Conf.Save()
	}

	if nil == Conf.Snippet {
		Conf.Snippet = conf.NewSnpt()
	}

	if "" != Conf.UserData {
		Conf.SetUser(loadUserFromConf())
	}
	if nil == Conf.Account {
		Conf.Account = conf.NewAccount()
	}

	if nil == Conf.Sync {
		Conf.Sync = conf.NewSync()
	}
	if 0 == Conf.Sync.Mode {
		Conf.Sync.Mode = 1
	}
	if 30 > Conf.Sync.Interval {
		Conf.Sync.Interval = 30
	}
	if 60*60*12 < Conf.Sync.Interval {
		Conf.Sync.Interval = 60 * 60 * 12
	}
	if nil == Conf.Sync.S3 {
		Conf.Sync.S3 = &conf.S3{PathStyle: true, SkipTlsVerify: true}
	}
	Conf.Sync.S3.Endpoint = util.NormalizeEndpoint(Conf.Sync.S3.Endpoint)
	Conf.Sync.S3.Timeout = util.NormalizeTimeout(Conf.Sync.S3.Timeout)
	Conf.Sync.S3.ConcurrentReqs = util.NormalizeConcurrentReqs(Conf.Sync.S3.ConcurrentReqs, conf.ProviderS3)
	if nil == Conf.Sync.WebDAV {
		Conf.Sync.WebDAV = &conf.WebDAV{SkipTlsVerify: true}
	}
	Conf.Sync.WebDAV.Endpoint = util.NormalizeEndpoint(Conf.Sync.WebDAV.Endpoint)
	Conf.Sync.WebDAV.Timeout = util.NormalizeTimeout(Conf.Sync.WebDAV.Timeout)
	Conf.Sync.WebDAV.ConcurrentReqs = util.NormalizeConcurrentReqs(Conf.Sync.WebDAV.ConcurrentReqs, conf.ProviderWebDAV)
	if nil == Conf.Sync.Local {
		Conf.Sync.Local = &conf.Local{}
	}
	Conf.Sync.Local.Endpoint = util.NormalizeLocalPath(Conf.Sync.Local.Endpoint)
	Conf.Sync.Local.Timeout = util.NormalizeTimeout(Conf.Sync.Local.Timeout)
	Conf.Sync.Local.ConcurrentReqs = util.NormalizeConcurrentReqs(Conf.Sync.Local.ConcurrentReqs, conf.ProviderLocal)

	if util.ContainerDocker == util.Container {
		Conf.Sync.Perception = false
	}

	if nil == Conf.Api {
		Conf.Api = conf.NewAPI()
	}

	if nil == Conf.Bazaar {
		Conf.Bazaar = conf.NewBazaar()
	}

	if nil == Conf.Publish {
		Conf.Publish = conf.NewPublish()
	}
	if nil == Conf.Repo {
		Conf.Repo = conf.NewRepo()
	}
	if timingEnv := os.Getenv("SIYUAN_SYNC_INDEX_TIMING"); "" != timingEnv {
		val, err := strconv.Atoi(timingEnv)
		if err == nil {
			Conf.Repo.SyncIndexTiming = int64(val)
		}
	}
	if 12000 > Conf.Repo.SyncIndexTiming {
		Conf.Repo.SyncIndexTiming = 12 * 1000
	}
	if 1 > Conf.Repo.IndexRetentionDays {
		Conf.Repo.IndexRetentionDays = 180
	}
	if 1 > Conf.Repo.RetentionIndexesDaily {
		Conf.Repo.RetentionIndexesDaily = 2
	}
	if 0 < len(Conf.Repo.Key) {
		logging.LogInfof("repo key [%x]", sha1.Sum(Conf.Repo.Key))
	}

	if nil == Conf.NotebookCrypto {
		Conf.NotebookCrypto = conf.NewNotebookCrypto()
	}

	// Note: we deliberately do NOT backfill the key backup on boot for the case where it's enabled but the backup
	// is missing. A backup generated without a KEK would necessarily have an empty KEKMAC, but the deriveKEK/
	// recovery path requires a valid KEKMAC, so backfilling would leave this machine permanently unable to unlock
	// (self-contradictory).
	// A backup in the current format can only be generated after the master password has been verified (see
	// EnableEncryptedNotebook / tryRestoreNotebookCryptoFromBackupLocked).
	// Enabled=true with a missing/invalid backup is treated as an incomplete configuration; on unlock, deriveKEK
	// returns a recovery hint (Language 315) guiding the user to import a matching backup file and re-verify the
	// master password.

	if nil == Conf.Search {
		Conf.Search = conf.NewSearch()
	}
	if 1 > Conf.Search.Limit {
		Conf.Search.Limit = 64
	}
	if 32 > Conf.Search.Limit {
		Conf.Search.Limit = 32
	}
	if 1 > Conf.Search.BacklinkMentionKeywordsLimit {
		Conf.Search.BacklinkMentionKeywordsLimit = 512
	}
	if nil == Conf.Search.HanSensitive {
		Conf.Search.SetHanSensitive(true)
	}
	sql.SetHanSensitive(Conf.Search.HanSensitiveVal())

	if nil == Conf.Stat {
		Conf.Stat = conf.NewStat()
	}

	if nil == Conf.Flashcard {
		Conf.Flashcard = conf.NewFlashcard()
	}
	if 0 > Conf.Flashcard.NewCardLimit {
		Conf.Flashcard.NewCardLimit = 20
	}
	if 0 > Conf.Flashcard.ReviewCardLimit {
		Conf.Flashcard.ReviewCardLimit = 200
	}
	if 0 >= Conf.Flashcard.RequestRetention || 1 <= Conf.Flashcard.RequestRetention {
		Conf.Flashcard.RequestRetention = conf.NewFlashcard().RequestRetention
	}
	if 0 >= Conf.Flashcard.MaximumInterval || 36500 <= Conf.Flashcard.MaximumInterval {
		Conf.Flashcard.MaximumInterval = conf.NewFlashcard().MaximumInterval
	}
	if "" == Conf.Flashcard.Weights {
		Conf.Flashcard.Weights = conf.NewFlashcard().Weights
	}
	if 19 != len(strings.Split(Conf.Flashcard.Weights, ",")) {
		defaultWeights := conf.DefaultFSRSWeights()
		msg := "fsrs store weights length must be [19]"
		logging.LogWarnf("%s , given [%s], reset to default weights [%s]", msg, Conf.Flashcard.Weights, defaultWeights)
		Conf.Flashcard.Weights = defaultWeights
		go func() {
			util.WaitForUILoaded()
			task.AppendAsyncTaskWithDelay(task.PushMsg, 2*time.Second, util.PushErrMsg, msg, 15000)
		}()
	}
	isInvalidFlashcardWeights := false
	for w := range strings.SplitSeq(Conf.Flashcard.Weights, ",") {
		if _, err := strconv.ParseFloat(strings.TrimSpace(w), 64); err != nil {
			isInvalidFlashcardWeights = true
			break
		}
	}
	if isInvalidFlashcardWeights {
		defaultWeights := conf.DefaultFSRSWeights()
		msg := "fsrs store weights contain invalid number"
		logging.LogWarnf("%s, given [%s], reset to default weights [%s]", msg, Conf.Flashcard.Weights, defaultWeights)
		Conf.Flashcard.Weights = defaultWeights
		go func() {
			util.WaitForUILoaded()
			task.AppendAsyncTaskWithDelay(task.PushMsg, 2*time.Second, util.PushErrMsg, msg, 15000)
		}()
	}

	if nil == Conf.AI {
		Conf.AI = conf.NewAI()
	} else {
		Conf.AI.DecryptAPIKeys()
	}
	Conf.AI.Normalize()

	if nil == Conf.Secrets {
		Conf.Secrets = conf.NewSecrets()
	} else {
		Conf.Secrets.Decrypt()
	}

	if nil == Conf.Variables {
		Conf.Variables = conf.NewVariables()
	}

	for _, p := range Conf.AI.Providers {
		if p == nil || !p.Enabled {
			continue
		}
		for _, m := range p.Models {
			if m == nil || m.Name == "" || !m.Enabled {
				continue
			}
			logging.LogInfof("AI provider enabled\n"+
				"    baseURL=%s\n"+
				"    timeout=%ds\n"+
				"    model=%s\n"+
				"    maxCompletionTokens=%d\n"+
				"    temperature=%.1f\n"+
				"    maxHistoryMessages=%d",
				p.BaseURL,
				p.RequestTimeout,
				m.Name,
				Conf.AI.Editing.MaxCompletionTokens,
				Conf.AI.Editing.Temperature,
				Conf.AI.Editing.MaxHistoryMessages)
		}
	}

	if Conf.AI.Embedding != nil && len(Conf.AI.Embedding.APIKey) > 0 {
		logging.LogInfof("embedding API enabled\n"+
			"    baseURL=%s\n"+
			"    model=%s",
			Conf.AI.Embedding.BaseURL,
			Conf.AI.Embedding.Name)
	}

	Conf.ReadOnly = util.ReadOnly

	if "" != util.AccessAuthCode {
		Conf.AccessAuthCode = util.AccessAuthCode
	}
	Conf.AccessAuthCode = util.RemoveInvalid(Conf.AccessAuthCode)
	Conf.AccessAuthCode = strings.TrimSpace(Conf.AccessAuthCode)

	if 1 == Conf.DataIndexState {
		// Data indexing did not finish normally last time; recoverIndexQueue() will recover it later
		logging.LogInfof("data index state is [%d], will recover through index queue", Conf.DataIndexState)
	}

	Conf.DataIndexState = 0

	if cookieKey := readCookieKey(); "" != cookieKey {
		Conf.CookieKey = cookieKey
	} else {
		if "" == Conf.CookieKey {
			Conf.CookieKey = gulu.Rand.String(16)
		}
		writeCookieKey(Conf.CookieKey)
	}

	Conf.Save()

	// Safe mode: injected by the desktop main process via --safe-mode after recovering from a renderer process crash.
	// safeMode is purely a runtime state, not persisted to conf.json (it's excluded on Save), so it's reassigned
	// from util.SafeMode on every startup.
	Conf.System.SafeMode = util.SafeMode
	if util.SafeMode {
		// Directly override and persist appearance, bazaar, and snippet related configuration, disabling snippets,
		// plugins, custom themes, and icons, to rule out extensions as the cause of another crash.
		// Note: this is a destructive operation that overwrites the user's existing configuration and is not
		// automatically restored afterward.
		Conf.Appearance.ThemeLight = "daylight"
		Conf.Appearance.ThemeDark = "midnight"
		Conf.Appearance.Icon = "litheness"
		Conf.Appearance.ThemeJS = false
		Conf.Bazaar.PetalDisabled = true
		Conf.Snippet.EnabledCSS = false
		Conf.Snippet.EnabledJS = false
		Conf.Save()
		logging.LogInfof("booted in safe mode")
	}

	// When a CLI subcommand explicitly specifies a log level via --log-level (util.CLILogLevel is non-empty), the
	// command-line level takes priority and is no longer overridden by conf.json's system.logLevel, so the
	// command-line argument takes effect early during initialization.
	if "" == util.CLILogLevel {
		logging.SetLogLevel(Conf.LogLevel)
	}

	util.SetNetworkProxy(Conf.System.NetworkProxy.String())

	go util.InitPandoc()
	go util.InitTesseract()
}

func readCookieKey() (cookieKey string) {
	cookieKeyPath := filepath.Join(util.HomeDir, ".config", "siyuan", "cookie.key")
	if !gulu.File.IsExist(cookieKeyPath) {
		return
	}

	data, err := os.ReadFile(cookieKeyPath)
	if err != nil {
		logging.LogErrorf("read cookie key file [%s] failed: %s", cookieKeyPath, err)
		return
	}

	cookieKey = string(bytes.TrimSpace(data))
	return
}

func writeCookieKey(cookieKey string) {
	cookieKeyPath := filepath.Join(util.HomeDir, ".config", "siyuan", "cookie.key")
	if gulu.File.IsExist(cookieKeyPath) {
		return
	}

	if err := os.WriteFile(cookieKeyPath, []byte(cookieKey), 0644); err != nil {
		logging.LogErrorf("save cookie key file [%s] failed: %s", cookieKeyPath, err)
	}
}

func initLang() {
	p := filepath.Join(util.WorkingDir, "appearance", "langs")
	dir, err := os.Open(p)
	if err != nil {
		logging.LogErrorf("open language configuration folder [%s] failed: %s", p, err)
		util.ReportFileSysFatalError(err)
		return
	}
	defer dir.Close()

	langNames, err := dir.Readdirnames(-1)
	if err != nil {
		logging.LogErrorf("list language configuration folder [%s] failed: %s", p, err)
		util.ReportFileSysFatalError(err)
		return
	}

	for _, langName := range langNames {
		jsonPath := filepath.Join(p, langName)
		data, err := os.ReadFile(jsonPath)
		if err != nil {
			logging.LogErrorf("read language configuration [%s] failed: %s", jsonPath, err)
			continue
		}
		data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
		langMap := map[string]any{}
		if err := gulu.JSON.UnmarshalJSON(data, &langMap); err != nil {
			logging.LogErrorf("parse language configuration failed [%s] failed: %s", jsonPath, err)
			continue
		}

		kernelMap := map[int]string{}
		label := langMap["_label"].(string)
		kernelLangs := langMap["_kernel"].(map[string]any)
		for k, v := range kernelLangs {
			num, convErr := strconv.Atoi(k)
			if nil != convErr {
				logging.LogErrorf("parse language configuration [%s] item [%d] failed: %s", p, num, convErr)
				continue
			}
			kernelMap[num] = v.(string)
		}
		kernelMap[-1] = label
		name := langName[:strings.LastIndex(langName, ".")]
		util.Langs[name] = kernelMap

		util.TimeLangs[name] = langMap["_time"].(map[string]any)
		util.TaskActionLangs[name] = langMap["_taskAction"].(map[string]any)
		util.TrayMenuLangs[name] = langMap["_trayMenu"].(map[string]any)
		util.AttrViewLangs[name] = langMap["_attrView"].(map[string]any)
	}
}

func loadLangs() (ret []*conf.Lang) {
	for name, langMap := range util.Langs {
		lang := &conf.Lang{Label: langMap[-1], Name: name}
		ret = append(ret, lang)
	}
	sort.Slice(ret, func(i, j int) bool {
		return ret[i].Name < ret[j].Name
	})
	return
}

var exitLock = sync.Mutex{}

// Close exits the kernel process.
//
// force: whether to exit immediately without running the sync process
//
// setCurrentWorkspace: whether to move the current workspace to the end of the workspace list
//
// execInstallPkg: whether to return the new version's install package
//
//	0: check per the System.DownloadInstallPkg setting and push a prompt by default
//	1: never return the new version's install package
//	2: return the new version's install package path and exit, letting the desktop host run the install
//
// Return value exitCode:
//
//	0: exited normally
//	1: sync failed
//	2: a new install package is available
//
// When force is true (forced exit), execInstallPkg is 0 (the default update check), and a new version's install
// package is already ready, the install package path is returned to the desktop host
// https://github.com/siyuan-note/siyuan/issues/10288
func Close(force, setCurrentWorkspace bool, execInstallPkg int) (exitCode int, installPkgPath string) {
	exitLock.Lock()
	defer exitLock.Unlock()

	logging.LogInfof("exiting kernel [force=%v, setCurrentWorkspace=%v, execInstallPkg=%d]", force, setCurrentWorkspace, execInstallPkg)

	util.PushMsg(Conf.Language(95), 10000*60)
	FlushTxQueue()

	cancelPurge()

	if !force {
		if OnKernelPluginsStop != nil {
			OnKernelPluginsStop()
		}

		if Conf.Sync.Enabled && 3 != Conf.Sync.Mode &&
			((IsSubscriber() && conf.ProviderSiYuan == Conf.Sync.Provider) || conf.ProviderSiYuan != Conf.Sync.Provider) {
			syncData(true, false)
			if 0 != ExitSyncSucc {
				exitCode = 1
				if 1 != execInstallPkg && !skipNewVerInstallPkg() {
					installPkgPath = getNewVerInstallPkgPath()
				}
				return
			}
		}
	}

	// Close the user guide when exiting https://github.com/siyuan-note/siyuan/issues/10322
	closeUserGuide()

	// Improve indexing completeness when exiting https://github.com/siyuan-note/siyuan/issues/12039
	sql.FlushQueue()

	util.IsExiting.Store(true)
	newVerInstallPkgPath := getNewVerInstallPkgPath()
	if !skipNewVerInstallPkg() && "" != newVerInstallPkgPath {
		if 2 == execInstallPkg || (force && 0 == execInstallPkg) { // hand the new version's install package off to the desktop host to run
			installPkgPath = newVerInstallPkgPath
			logging.LogInfof("the new version install pkg is ready for the desktop host [%s]", newVerInstallPkgPath)
		} else if 0 == execInstallPkg { // the new version's install package is already ready
			installPkgPath = newVerInstallPkgPath
			exitCode = 2
			logging.LogInfof("the new version install pkg is ready [%s], waiting for the user's next instruction", newVerInstallPkgPath)
			return
		}
	}

	Conf.Close()
	// Before exiting, close any open encrypted notebooks and push closeBox so the frontend closes the corresponding
	// plaintext document tabs, preventing data exposure after a restart.
	// This goes through Unmount: persist Closed=true + generate history + lock and clear the DEK + broadcast closeBox.
	// The user guide is excluded: calling Unmount on the user guide would trigger RemoveBox (mount.go:208-214).
	// This is pushed before BroadcastByType("exit") (line 933 below), and the subsequent time.Sleep(500ms) gives
	// the frontend time to process the event.
	for _, box := range Conf.GetOpenedBoxes() {
		if IsEncryptedBox(box.ID) && !IsUserGuide(box.ID) {
			Unmount(box.ID)
		}
	}
	sql.CloseDatabase()
	closePushQueue()
	util.SaveAssetsTexts()
	clearWorkspaceTemp("" != installPkgPath)
	clearCorruptedNotebooks()
	clearPortJSON()

	if setCurrentWorkspace {
		// Move the current workspace to the end of the workspace list
		// Open the last workspace by default https://github.com/siyuan-note/siyuan/issues/10570
		workspacePaths, err := util.ReadWorkspacePaths()
		if err != nil {
			logging.LogErrorf("read workspace paths failed: %s", err)
		} else {
			workspacePaths = util.RemoveWorkspacePath(workspacePaths, util.WorkspaceDir)
			workspacePaths = append(workspacePaths, util.WorkspaceDir)
			util.WriteWorkspacePaths(workspacePaths)
		}
	}

	util.BroadcastByType("main", "exit", 0, "", nil)
	util.UnlockWorkspace()

	time.Sleep(500 * time.Millisecond)
	closeSyncWebSocket()

	go func() {
		time.Sleep(500 * time.Millisecond)
		logging.LogInfof("exited kernel")
		if nil != util.WebSocketServer {
			util.WebSocketServer.Close()
		}
		if nil != util.HttpServer {
			util.HttpServer.Close()
		}
		util.HttpServing = false

		if util.IsMobileContainer() {
			return
		}

		os.Exit(logging.ExitCodeOk)
	}()
	return
}

var customEmojis = sync.Map{}

func AddCustomEmoji(emojiName, imgSrc string) {
	customEmojis.Store(emojiName, imgSrc)
}

func ClearCustomEmojis() {
	customEmojis.Clear()
}

func NewLute() (ret *lute.Lute) {
	ret = util.NewLute()
	ret.SetCodeSyntaxHighlightLineNum(Conf.Editor.CodeSyntaxHighlightLineNum)
	ret.SetChineseParagraphBeginningSpace(Conf.Export.ParagraphBeginningSpace)
	ret.SetProtyleMarkNetImg(Conf.Editor.DisplayNetImgMark)
	ret.SetSpellcheck(Conf.Editor.Spellcheck)

	customEmojiMap := map[string]string{}
	customEmojis.Range(func(key, value any) bool {
		customEmojiMap[key.(string)] = value.(string)
		return true
	})
	ret.PutEmojis(customEmojiMap)
	return
}

func enableLuteInlineSyntax(luteEngine *lute.Lute) {
	luteEngine.SetInlineAsterisk(true)
	luteEngine.SetInlineUnderscore(true)
	luteEngine.SetSup(true)
	luteEngine.SetSub(true)
	luteEngine.SetTag(true)
	luteEngine.SetInlineMath(true)
	luteEngine.SetGFMStrikethrough(true)
}

func (conf *AppConf) Save() {
	if util.ReadOnly {
		return
	}

	conf.m.Lock()
	defer conf.m.Unlock()

	plainData, err := gulu.JSON.MarshalJSON(conf)
	if err != nil {
		logging.LogErrorf("marshal conf failed: %s", err)
		return
	}
	snapshot := NewAppConf()
	if err = gulu.JSON.UnmarshalJSON(plainData, snapshot); err != nil {
		logging.LogErrorf("copy conf failed: %s", err)
		return
	}
	if snapshot.AI != nil {
		snapshot.AI.EncryptAPIKeys()
	}
	if snapshot.Secrets != nil {
		snapshot.Secrets.Encrypt()
	}
	// safeMode is purely a runtime state (injected via --safe-mode) and is never persisted to conf.json, to avoid
	// it lingering across restarts.
	if snapshot.System != nil {
		snapshot.System.SafeMode = false
	}

	newData, err := gulu.JSON.MarshalIndentJSON(snapshot, "", "  ")
	if err != nil {
		logging.LogErrorf("marshal conf snapshot failed: %s", err)
		return
	}
	confPath := filepath.Join(util.ConfDir, "conf.json")
	oldData, err := filelock.ReadFile(confPath)
	if err != nil {
		conf.save0(newData)
		return
	}

	if bytes.Equal(newData, oldData) {
		return
	}

	conf.save0(newData)
}

func (conf *AppConf) save0(data []byte) {
	confPath := filepath.Join(util.ConfDir, "conf.json")
	if err := filelock.WriteFile(confPath, data); err != nil {
		logging.LogErrorf("write conf [%s] failed: %s", confPath, err)
		util.ReportFileSysFatalError(err)
		return
	}
}

func (conf *AppConf) Close() {
	conf.Save()
}

func (conf *AppConf) Box(boxID string) *Box {
	for _, box := range conf.GetOpenedBoxes() {
		if box.ID == boxID {
			return box
		}
	}
	return nil
}

func (conf *AppConf) GetBox(boxID string) *Box {
	for _, box := range conf.GetBoxes() {
		if box.ID == boxID {
			return box
		}
	}
	return nil
}

func (conf *AppConf) BoxNames(boxIDs []string) (ret map[string]string) {
	ret = map[string]string{}

	boxes := conf.GetOpenedBoxes()
	for _, boxID := range boxIDs {
		for _, box := range boxes {
			if box.ID == boxID {
				ret[boxID] = box.Name
				break
			}
		}
	}
	return
}

func (conf *AppConf) GetBoxes() (ret []*Box) {
	ret = []*Box{}
	notebooks, err := ListNotebooks()
	if err != nil {
		return
	}

	for _, notebook := range notebooks {
		id := notebook.ID
		name := notebook.Name
		closed := notebook.Closed
		encrypted := IsEncryptedBox(id) // use IsEncryptedBox for the check consistently (including backup fallback)
		box := &Box{ID: id, Name: name, Closed: closed, Encrypted: encrypted}
		ret = append(ret, box)
	}
	return
}

func (conf *AppConf) GetOpenedBoxes() (ret []*Box) {
	ret = []*Box{}
	notebooks, err := ListNotebooks()
	if err != nil {
		return
	}

	for _, notebook := range notebooks {
		if !notebook.Closed {
			ret = append(ret, notebook)
		}
	}
	return
}

func (conf *AppConf) GetClosedBoxes() (ret []*Box) {
	ret = []*Box{}
	notebooks, err := ListNotebooks()
	if err != nil {
		return
	}

	for _, notebook := range notebooks {
		if notebook.Closed {
			ret = append(ret, notebook)
		}
	}
	return
}

func (conf *AppConf) Language(num int) (ret string) {
	ret = conf.language(num)
	ret = strings.ReplaceAll(ret, "${accountServer}", util.GetCloudAccountServer())
	return
}

func (conf *AppConf) language(num int) (ret string) {
	ret = util.Langs[conf.Lang][num]
	if "" != ret {
		return
	}
	ret = util.Langs["en"][num]
	return
}

func InitBoxes() {
	blockCount := treenode.CountBlocks()
	initialized := 0 < blockCount
	for _, box := range Conf.GetOpenedBoxes() {
		if _, err := EnsureBoxDoc(box.ID); nil != err {
			logging.LogErrorf("ensure box document [%s] failed: %s", box.ID, err)
		}
		box.UpdateHistoryGenerated() // initialize the history-generated time to now

		if !initialized {
			indexBox(box.ID)
		}
	}

	logging.LogInfof("tree/block count [%d/%d]", treenode.CountTrees(), blockCount)
}

func IsSubscriber() bool {
	u := Conf.GetUser()
	return nil != u && (-1 == u.UserSiYuanProExpireTime || 0 < u.UserSiYuanProExpireTime) && 0 == u.UserSiYuanSubscriptionStatus
}

func IsPaidUser() bool {
	if IsSubscriber() {
		return true
	}

	u := Conf.GetUser()
	if nil == u {
		return false
	}
	return 1 == u.UserSiYuanOneTimePayStatus
}

const (
	MaskedUserData       = ""
	MaskedAccessAuthCode = "*******"
)

// GetMaskedConf gets a redacted copy of Conf
func GetMaskedConf() (ret *AppConf, err error) {
	// Hold the lock while serializing to avoid a concurrent write (e.g. loadThemes/LoadIcons) mutating a slice
	// mid-encode and causing a panic https://github.com/siyuan-note/siyuan/issues/16978
	Conf.m.Lock()
	data, err := gulu.JSON.MarshalJSON(Conf)
	Conf.m.Unlock()
	if err != nil {
		logging.LogErrorf("marshal conf failed: %s", err)
		return
	}
	ret = &AppConf{}
	if err = gulu.JSON.UnmarshalJSON(data, ret); err != nil {
		logging.LogErrorf("unmarshal conf failed: %s", err)
		return
	}

	ret.UserData = MaskedUserData
	ret.MCPOAuth = ""
	if "" != ret.AccessAuthCode {
		ret.AccessAuthCode = MaskedAccessAuthCode
	}
	return
}

// HideConfSecret hides secret information in the settings
// REF: https://github.com/siyuan-note/siyuan/issues/11364
func HideConfSecret(c *AppConf) {
	c.AI = &conf.AI{}
	c.MCPOAuth = ""
	c.Api = &conf.API{}
	c.Flashcard = &conf.Flashcard{}
	c.ServerAddrs = []string{}
	c.Publish = &conf.Publish{}
	c.Repo = &conf.Repo{}
	c.Sync = &conf.Sync{}
	c.Secrets = &conf.Secrets{}
	c.Variables = &conf.Variables{}
	c.System.AppDir = ""
	c.System.ConfDir = ""
	c.System.DataDir = ""
	c.System.HomeDir = ""
	c.System.Name = ""
	c.System.NetworkProxy = &conf.NetworkProxy{}
}

func clearPortJSON() {
	pid := fmt.Sprintf("%d", os.Getpid())
	portJSON := filepath.Join(util.HomeDir, ".config", "siyuan", "port.json")
	pidPorts := map[string]string{}
	var data []byte
	var err error

	if gulu.File.IsExist(portJSON) {
		data, err = os.ReadFile(portJSON)
		if err != nil {
			logging.LogWarnf("read port.json failed: %s", err)
		} else {
			if err = gulu.JSON.UnmarshalJSON(data, &pidPorts); err != nil {
				logging.LogWarnf("unmarshal port.json failed: %s", err)
			}
		}
	}

	delete(pidPorts, pid)
	if data, err = gulu.JSON.MarshalIndentJSON(pidPorts, "", "  "); err != nil {
		logging.LogWarnf("marshal port.json failed: %s", err)
	} else {
		if err = os.WriteFile(portJSON, data, 0644); err != nil {
			logging.LogWarnf("write port.json failed: %s", err)
		}
	}
}

func clearCorruptedNotebooks() {
	// Expanding the document tree during data sync can cause data loss https://github.com/siyuan-note/siyuan/issues/7129

	dirs, err := os.ReadDir(util.DataDir)
	if err != nil {
		logging.LogErrorf("read dir [%s] failed: %s", util.DataDir, err)
		return
	}
	for _, dir := range dirs {
		if util.IsReservedFilename(dir.Name()) {
			continue
		}

		if !dir.IsDir() {
			continue
		}

		if !ast.IsNodeIDPattern(dir.Name()) {
			continue
		}

		boxDirPath := filepath.Join(util.DataDir, dir.Name())
		boxConfPath := filepath.Join(boxDirPath, ".siyuan", "conf.json")
		if !filelock.IsExist(boxConfPath) {
			logging.LogWarnf("found a corrupted box [%s]", boxDirPath)
			continue
		}
	}
}

func clearWorkspaceTemp(preserveInstallPkgs bool) {
	os.RemoveAll(filepath.Join(util.TempDir, "bazaar"))
	os.RemoveAll(filepath.Join(util.TempDir, "export"))
	os.RemoveAll(filepath.Join(util.TempDir, "import"))
	os.RemoveAll(filepath.Join(util.TempDir, "convert"))
	os.RemoveAll(filepath.Join(util.TempDir, "repo"))
	os.RemoveAll(filepath.Join(util.TempDir, "os"))
	os.RemoveAll(filepath.Join(util.TempDir, "base64"))
	os.RemoveAll(filepath.Join(util.TempDir, "ai"))

	// Automatically delete install packages older than 7 days on exit https://github.com/siyuan-note/siyuan/issues/6128
	install := filepath.Join(util.TempDir, "install")
	if !preserveInstallPkgs && gulu.File.IsDir(install) {
		monthAgo := time.Now().Add(-time.Hour * 24 * 7)
		entries, err := os.ReadDir(install)
		if err != nil {
			logging.LogErrorf("read dir [%s] failed: %s", install, err)
		} else {
			for _, entry := range entries {
				info, _ := entry.Info()
				if nil != info && !info.IsDir() && info.ModTime().Before(monthAgo) {
					installPkgPath := filepath.Join(install, entry.Name())
					if err = os.RemoveAll(installPkgPath); err != nil {
						logging.LogErrorf("remove old install pkg [%s] failed: %s", installPkgPath, err)
					}
				}
			}
		}
	}

	tmps, err := filepath.Glob(filepath.Join(util.TempDir, "*.tmp"))
	if err != nil {
		logging.LogErrorf("glob temp files failed: %s", err)
	}
	for _, tmp := range tmps {
		if err = os.RemoveAll(tmp); err != nil {
			logging.LogErrorf("remove temp file [%s] failed: %s", tmp, err)
		} else {
			logging.LogInfof("removed temp file [%s]", tmp)
		}
	}

	tmps, err = filepath.Glob(filepath.Join(util.DataDir, ".siyuan", "*.tmp"))
	if err != nil {
		logging.LogErrorf("glob temp files failed: %s", err)
	}
	for _, tmp := range tmps {
		if err = os.RemoveAll(tmp); err != nil {
			logging.LogErrorf("remove temp file [%s] failed: %s", tmp, err)
		} else {
			logging.LogInfof("removed temp file [%s]", tmp)
		}
	}

	// Clean up files left over from older versions
	os.RemoveAll(filepath.Join(util.DataDir, "assets", ".siyuan", "assets.json"))
	os.RemoveAll(filepath.Join(util.DataDir, ".siyuan", "history"))
	os.RemoveAll(filepath.Join(util.WorkspaceDir, "backup"))
	os.RemoveAll(filepath.Join(util.WorkspaceDir, "sync"))
	os.RemoveAll(filepath.Join(util.TempDir, "blocktree.msgpack")) // block tree data from versions before v2.7.2
	os.RemoveAll(filepath.Join(util.DataDir, "%"))                 // erroneous history folder generated by v3.0.6
	os.RemoveAll(filepath.Join(util.TempDir, "blocktree"))         // block tree data from versions before v3.1.0

	// Clean up files left over from the v3.7.0-dev development build
	os.RemoveAll(filepath.Join(util.TempDir, "queue.wal"))
	os.RemoveAll(filepath.Join(util.TempDir, "queue.wal.lock"))
	os.RemoveAll(filepath.Join(util.DataDir, "storage", "ai", "agent", "todos"))
	os.RemoveAll(filepath.Join(util.DataDir, "storage", "ai", "agent", "operations", "image"))

	logging.LogInfof("cleared workspace temp")
}

func closeUserGuide() {
	defer logging.Recover()

	dirs, err := os.ReadDir(util.DataDir)
	if err != nil {
		logging.LogErrorf("read dir [%s] failed: %s", util.DataDir, err)
		return
	}

	for _, dir := range dirs {
		if !IsUserGuide(dir.Name()) {
			continue
		}

		boxID := dir.Name()
		boxDirPath := filepath.Join(util.DataDir, boxID)
		boxConf := conf.NewBoxConf()
		boxConfPath := filepath.Join(boxDirPath, ".siyuan", "conf.json")
		if !filelock.IsExist(boxConfPath) {
			logging.LogWarnf("found a corrupted user guide box [%s]", boxDirPath)
			if removeErr := filelock.Remove(boxDirPath); nil != removeErr {
				logging.LogErrorf("remove corrupted user guide box [%s] failed: %s", boxDirPath, removeErr)
			} else {
				logging.LogInfof("removed corrupted user guide box [%s]", boxDirPath)
			}
			continue
		}

		data, readErr := filelock.ReadFile(boxConfPath)
		if nil != readErr {
			logging.LogErrorf("read box conf [%s] failed: %s", boxConfPath, readErr)
			if removeErr := filelock.Remove(boxDirPath); nil != removeErr {
				logging.LogErrorf("remove corrupted user guide box [%s] failed: %s", boxDirPath, removeErr)
			} else {
				logging.LogInfof("removed corrupted user guide box [%s]", boxDirPath)
			}
			continue
		}
		if readErr = gulu.JSON.UnmarshalJSON(data, boxConf); nil != readErr {
			logging.LogErrorf("parse box conf [%s] failed: %s", boxConfPath, readErr)
			if removeErr := filelock.Remove(boxDirPath); nil != removeErr {
				logging.LogErrorf("remove corrupted user guide box [%s] failed: %s", boxDirPath, removeErr)
			} else {
				logging.LogInfof("removed corrupted user guide box [%s]", boxDirPath)
			}
			continue
		}

		if boxConf.Closed {
			continue
		}

		msgId := util.PushMsg(Conf.language(233), 30000)

		unindex(boxID)

		sql.FlushQueue()

		if removeErr := RemoveBox(boxID); nil == removeErr {
			evt := util.NewCmdResult("removeBox", 0, util.PushModeBroadcast)
			evt.Data = map[string]any{
				"box": boxID,
			}
			util.PushEvent(evt)
		} else {
			logging.LogErrorf("close user guide box [%s] failed: %s", boxID, removeErr)
			util.PushClearMsg(msgId)
			continue
		}

		util.PushClearMsg(msgId)
		logging.LogInfof("closed user guide box [%s]", boxID)
	}
}

func init() {
	subscribeConfEvents()
}

func subscribeConfEvents() {
	eventbus.Subscribe(util.EvtConfPandocInitialized, func() {
		logging.LogInfof("pandoc initialized, set pandoc bin to [%s]", util.PandocBinPath)
		Conf.Export.PandocBin = util.PandocBinPath

		params := util.RemoveInvalid(Conf.Export.PandocParams)
		if !strings.Contains(params, "--reference-doc") && "" != util.PandocTemplatePath && !Conf.System.IsMicrosoftStore {
			params += " --reference-doc"
			params += " \"" + util.PandocTemplatePath + "\""
			Conf.Export.PandocParams = strings.TrimSpace(params)
		}

		logging.LogInfof("pandoc params set to [%s]", Conf.Export.PandocParams)
		logging.LogInfof("pandoc resources [%s, %s]", util.PandocTemplatePath, util.PandocColorFilterPath)
		Conf.Save()
	})
}

// NotebookCryptoEnabled returns whether the encrypted notebook feature is enabled (thread-safe).
func NotebookCryptoEnabled() bool {
	Conf.m.RLock()
	defer Conf.m.RUnlock()
	return Conf.NotebookCrypto.Enabled
}
