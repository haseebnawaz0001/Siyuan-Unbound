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
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/model"
	"github.com/siyuan-note/siyuan/kernel/sql"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"

	"github.com/spf13/cobra"
)

var (
	workspacePath string
	outputFormat  string
	dryRun        bool
	logLevel      string
)

var rootCmd = &cobra.Command{
	Use:     "SiYuan-Kernel",
	Version: util.Ver,
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		// A one-shot CLI command has no background cron periodically flushing the SQL queue (only server mode
		// has job.StartCron); the process exits right after main returns, and the in-memory SQL index queue is
		// lost along with it (the operation is already persisted to index.queue, but recovery only happens on
		// the next startup's recoverIndexQueue). So flush to the database uniformly here after the command
		// finishes, guaranteeing it's searchable as soon as it's written.
		name := cmd.Name()
		// The serve subcommand has its own long-running exit flow (HandleSignal -> model.Close flushes), so
		// it's not handled here; the workspace subcommand skips database initialization in PersistentPreRunE,
		// so the sql package isn't ready yet and calling this would panic.
		if name == "serve" || (cmd.Parent() != nil && cmd.Parent().Name() == "workspace") {
			return nil
		}
		model.FlushTxQueue()
		sql.FlushQueue()
		return nil
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// The workspace subcommand does not need workspace validation
		if cmd.Parent() != nil && cmd.Parent().Name() == "workspace" {
			return nil
		}

		// The default working directory is the parent of the directory containing the kernel executable (the
		// packaged resources/, where appearance/ and stage/ live), not the directory containing the kernel
		// executable itself (resources/kernel/). resolveWorkingDir() verifies that appearance/langs actually
		// exists, to support various directory layouts including the dev environment.
		if workingDir := resolveWorkingDir(); workingDir != "" {
			util.WorkingDir = workingDir
		}

		langsDir := filepath.Join(util.WorkingDir, "appearance", "langs")
		if _, err := os.Stat(langsDir); os.IsNotExist(err) {
			return fmt.Errorf("appearance files not found at [%s]", langsDir)
		}

		// Set the workspace path
		if workspacePath == "" {
			workspacePath = os.Getenv("SIYUAN_WORKSPACE_PATH")
		}
		if workspacePath == "" {
			workspacePath = filepath.Join(util.HomeDir, "SiYuan")
		}

		if _, err := os.Stat(workspacePath); os.IsNotExist(err) {
			return fmt.Errorf("directory not found: %s", workspacePath)
		}
		if !util.IsWorkspaceDir(workspacePath) {
			return fmt.Errorf("not a valid workspace: %s", workspacePath)
		}

		util.Mode = "prod"
		util.InitWorkspace(workspacePath, util.WorkingDir)

		logging.SetLogPath(filepath.Join(util.TempDir, "siyuan-cli.log"))
		logging.SetLogToStdout(false)

		// A one-shot CLI command defaults to the warn level (siyuan-cli.log only keeps warnings and above), to
		// avoid the noise of the kernel initialization's large volume of Info/Debug logs; the user can
		// explicitly override this via --log-level. The level is recorded into util.CLILogLevel, so that the
		// later model.InitConf no longer overrides it with conf.json.
		// Note the serve subcommand uses its own PersistentPreRunE, is unaffected by this default, and still
		// follows conf.json's system.logLevel.
		effectiveLevel := logLevel
		if "" == effectiveLevel {
			effectiveLevel = "warn"
		}
		logging.SetLogLevel(effectiveLevel)
		util.CLILogLevel = effectiveLevel

		model.InitConf()
		sql.InitDatabase(false)
		sql.InitHistoryDatabase(false)
		sql.InitAssetContentDatabase(false)
		sql.SetCaseSensitive(model.Conf.Search.CaseSensitive)
		sql.SetIndexAssetPath(model.Conf.Search.IndexAssetPath)
		// Let a one-shot CLI command (such as search -m 4) also hit semantic search: StartEmbeddingIndexer is
		// an infinite loop and cannot be used in a process that exits immediately, so here only the flag is set to true
		model.PrepareEmbeddingSearch()
		if err := rejectEncryptedNotebookCLI(cmd, args); err != nil {
			return err
		}
		return nil
	},
}

// rejectEncryptedNotebookCLI rejects CLI operations against an encrypted notebook and its blocks.
// An encrypted notebook can only be unlocked and operated on through the app's dedicated in-app flow, to
// prevent the CLI process from becoming a side-channel entry point to plaintext or ciphertext files.
func rejectEncryptedNotebookCLI(cmd *cobra.Command, args []string) error {
	if cmd == serveCmd {
		return nil
	}
	if (cmd == notebookRandomIconCmd && !cmd.Flags().Changed("id")) || cmd == exportDataCmd {
		boxID, err := firstEncryptedNotebookID()
		if err != nil {
			return err
		}
		if boxID != "" {
			return fmt.Errorf("CLI does not support encrypted notebook [%s]", boxID)
		}
	}

	var encryptedTarget string
	checkID := func(id string) bool {
		if id == "" {
			return false
		}
		if model.IsEncryptedBox(id) {
			encryptedTarget = id
			return true
		}
		if bt := treenode.GetBlockTree(id); bt != nil && model.IsEncryptedBox(bt.BoxID) {
			encryptedTarget = bt.BoxID
			return true
		}
		return false
	}

	for _, flagName := range []string{"notebook", "box", "id", "ids", "parent", "previous", "block"} {
		flag := cmd.Flags().Lookup(flagName)
		if flag == nil {
			continue
		}
		values := []string{flag.Value.String()}
		if flag.Value.Type() == "stringArray" {
			values, _ = cmd.Flags().GetStringArray(flagName)
		}
		for _, value := range values {
			for id := range strings.SplitSeq(value, ",") {
				if checkID(strings.TrimSpace(id)) {
					return fmt.Errorf("CLI does not support encrypted notebook [%s]", encryptedTarget)
				}
			}
		}
	}

	if cmd.Parent() == fileCmd {
		if slices.ContainsFunc(args, isEncryptedNotebookWorkspacePath) {
			return fmt.Errorf("CLI does not support files in encrypted notebooks")
		}
		if pathFlag := cmd.Flags().Lookup("path"); pathFlag != nil && pathFlag.Value.String() != "" && isEncryptedNotebookWorkspacePath(pathFlag.Value.String()) {
			return fmt.Errorf("CLI does not support files in encrypted notebooks")
		}
	}
	if cmd.Parent() == assetCmd {
		if pathFlag := cmd.Flags().Lookup("path"); pathFlag != nil && pathFlag.Value.String() != "" {
			assetPath := pathFlag.Value.String()
			if !filepath.IsAbs(assetPath) {
				assetPath = filepath.Join("data", assetPath)
			}
			if isEncryptedNotebookWorkspacePath(assetPath) {
				return fmt.Errorf("CLI does not support files in encrypted notebooks")
			}
		}
	}
	return nil
}

func firstEncryptedNotebookID() (string, error) {
	boxes, err := model.ListNotebooks()
	if err != nil {
		return "", err
	}
	for _, box := range boxes {
		if model.IsEncryptedBox(box.ID) {
			return box.ID, nil
		}
	}
	return "", nil
}

// isEncryptedNotebookWorkspacePath determines whether a path within the workspace is located under an encrypted notebook directory.
func isEncryptedNotebookWorkspacePath(p string) bool {
	return isEncryptedNotebookWorkspacePathWith(p, util.WorkspaceDir, util.DataDir, model.IsEncryptedBox)
}

func isEncryptedNotebookWorkspacePathWith(p, workspaceDir, dataDir string, isEncryptedBox func(string) bool) bool {
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(workspaceDir, p)
	}
	rel, err := filepath.Rel(dataDir, filepath.Clean(abs))
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return false
	}
	boxID := strings.Split(rel, string(filepath.Separator))[0]
	return isEncryptedBox(boxID)
}

// resolveWorkingDir starts from the kernel executable's path, probes several candidate directories, and
// returns the first one containing appearance/langs as the working directory (resources/ once packaged,
// depending on the directory layout in dev mode); returns an empty string if none is found. Both
// rootCmd.PersistentPreRunE and the serve subcommand's --wd default value go through this function, ensuring
// the two startup paths behave consistently.
func resolveWorkingDir() string {
	if exePath, err := os.Executable(); err == nil {
		if resolved, err2 := filepath.EvalSymlinks(exePath); err2 == nil {
			exePath = resolved
		}
		exeDir := filepath.Dir(exePath)
		candidates := []string{
			filepath.Join(exeDir, ".."),              // resources/kernel/ → resources/ (production)
			filepath.Join(exeDir, "..", "app"),       // kernel/cli/ → kernel/ → app/
			filepath.Join(exeDir, "app"),             // kernel/ → app/
			filepath.Join(exeDir, "..", "..", "app"), // kernel/cli/cmd/... → .../app/
		}
		// Add the macOS app bundle path
		if runtime.GOOS == "darwin" {
			candidates = append(candidates,
				filepath.Join(exeDir, "..", "..", "..", "..", "Resources"),
			)
		}
		for _, d := range candidates {
			langsDir := filepath.Join(d, "appearance", "langs")
			if fi, err := os.Stat(langsDir); err == nil && fi.IsDir() {
				return d
			}
		}
	}
	return ""
}

func init() {
	rootCmd.Use = strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	rootCmd.Short = "SiYuan Kernel v" + util.Ver
	rootCmd.Long = "SiYuan Kernel v" + util.Ver + ". Manage workspace data directly or start the HTTP server."

	rootCmd.PersistentFlags().StringVarP(&workspacePath, "workspace", "w", "", "workspace path")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "format", "f", "table", "output format: table | json")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "dry run mode: validate and print what would happen without making changes")
	rootCmd.PersistentFlags().StringVarP(&logLevel, "log-level", "v", "", "log level: off | trace | debug | info | warn | error | fatal (defaults to conf.json system.logLevel)")
}

func Execute() error {
	return rootCmd.Execute()
}

func HasSubCommand(name string) bool {
	for _, c := range rootCmd.Commands() {
		if c.Name() == name {
			return true
		}
	}
	return false
}
