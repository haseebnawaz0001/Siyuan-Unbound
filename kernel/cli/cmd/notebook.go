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
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/88250/gulu"

	"github.com/siyuan-note/siyuan/kernel/model"
	"github.com/siyuan-note/siyuan/kernel/sql"
	"github.com/siyuan-note/siyuan/kernel/util"

	"github.com/spf13/cobra"
)

var notebookCmd = &cobra.Command{
	Use:   "notebook",
	Short: "Manage notebooks",
}

var notebookListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all notebooks",
	RunE: func(cmd *cobra.Command, args []string) error {
		boxes, err := model.ListNotebooks()
		if err != nil {
			return err
		}
		switch outputFormat {
		case "json":
			data, _ := json.MarshalIndent(boxes, "", "  ")
			fmt.Println(string(data))
		default:
			printNotebookTable(boxes)
		}
		return nil
	},
}

var notebookCreateCmd = &cobra.Command{
	Use:   "create --name <name>",
	Short: "Create a notebook",
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		if name == "" {
			return fmt.Errorf("--name is required")
		}

		if dryRun {
			fmt.Printf("[dry-run] Would create notebook \"%s\"\n", name)
			return nil
		}

		id, err := model.CreateBox(name)
		if err != nil {
			return err
		}
		model.AppendPushReloadFiletreeEntry()
		fmt.Println(id)
		return nil
	},
}

var notebookRemoveCmd = &cobra.Command{
	Use:   "remove --id <id>",
	Short: "Remove a notebook",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if id == "" {
			return fmt.Errorf("--id is required")
		}

		if dryRun {
			fmt.Printf("[dry-run] Would remove notebook %s\n", id)
			return nil
		}

		if err := model.RemoveBox(id); err != nil {
			return err
		}
		model.AppendPushReloadFiletreeEntry()
		fmt.Println(id)
		return nil
	},
}

var notebookRenameCmd = &cobra.Command{
	Use:   "rename --id <id> --name <name>",
	Short: "Rename a notebook",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		name, _ := cmd.Flags().GetString("name")
		if id == "" {
			return fmt.Errorf("--id is required")
		}
		if name == "" {
			return fmt.Errorf("--name is required")
		}

		if dryRun {
			fmt.Printf("[dry-run] Would rename notebook %s to \"%s\"\n", id, name)
			return nil
		}

		if err := model.RenameBox(id, name); err != nil {
			return err
		}
		model.AppendPushReloadFiletreeEntry()
		fmt.Println(id)
		return nil
	},
}

var notebookOpenCmd = &cobra.Command{
	Use:   "open --id <id>",
	Short: "Open a notebook",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if id == "" {
			return fmt.Errorf("--id is required")
		}

		if dryRun {
			fmt.Printf("[dry-run] Would open notebook %s\n", id)
			return nil
		}

		existed, err := model.Mount(id)
		if err != nil {
			return err
		}
		if box := model.Conf.Box(id); nil != box {
			evt := util.NewCmdResult("mount", 0, util.PushModeBroadcast)
			evt.Data = map[string]any{
				"box":     box,
				"existed": existed,
			}
			util.PushEvent(evt)
		}
		time.Sleep(1 * time.Second)
		sql.FlushQueue()
		fmt.Println(id)
		return nil
	},
}

var notebookCloseCmd = &cobra.Command{
	Use:   "close --id <id>",
	Short: "Close a notebook",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if id == "" {
			return fmt.Errorf("--id is required")
		}

		if dryRun {
			fmt.Printf("[dry-run] Would close notebook %s\n", id)
			return nil
		}

		model.Unmount(id)
		time.Sleep(1 * time.Second)
		sql.FlushQueue()
		fmt.Println(id)
		return nil
	},
}

func printNotebookTable(boxes []*model.Box) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tCLOSED\tSORT")
	for _, b := range boxes {
		fmt.Fprintf(w, "%s\t%s\t%v\t%d\n", b.ID, b.Name, b.Closed, b.Sort)
	}
	w.Flush()
}

// notebookSetIconCmd sets a notebook's icon.
// The icon value format is: an emoji hex code point (e.g. "1f4ca"), an emoji character, a custom image path, or a dynamic icon URL.
var notebookSetIconCmd = &cobra.Command{
	Use:   "set-icon --id <id> --icon <icon>",
	Short: "Set a notebook icon",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		icon, _ := cmd.Flags().GetString("icon")
		if id == "" {
			return fmt.Errorf("--id is required")
		}
		if icon == "" {
			return fmt.Errorf("--icon is required")
		}

		// Verify the notebook exists, to avoid silently writing an icon for a non-existent id.
		exists := false
		notebooks, err := model.ListNotebooks()
		if err != nil {
			return err
		}
		for _, nb := range notebooks {
			if nb.ID == id {
				exists = true
				break
			}
		}
		if !exists {
			return fmt.Errorf("notebook not found: %s", id)
		}

		if dryRun {
			fmt.Printf("[dry-run] Would set notebook %s icon to %s\n", id, icon)
			return nil
		}

		// SetBoxIcon internally applies XSS filtering to custom image names.
		model.SetBoxIcon(id, icon)
		util.PushReloadFiletree()

		switch outputFormat {
		case "json":
			data, _ := json.MarshalIndent(map[string]string{"id": id, "icon": icon}, "", "  ")
			fmt.Println(string(data))
		default:
			fmt.Printf("%s\t%s\n", id, icon)
		}
		return nil
	},
}

// notebookRandomIconCmd randomly assigns a built-in emoji icon to a notebook.
// Passing --id changes only that notebook; without it, each notebook gets a random icon.
var notebookRandomIconCmd = &cobra.Command{
	Use:   "random-icon [--id <id>]",
	Short: "Randomly set notebook icon(s) from built-in emojis",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")

		notebooks, err := model.ListNotebooks()
		if err != nil {
			return err
		}

		// Target scope: passing id changes only that notebook; without it, each notebook gets a random icon.
		targets := notebooks
		if id != "" {
			var found *model.Box
			for _, nb := range notebooks {
				if nb.ID == id {
					found = nb
					break
				}
			}
			if found == nil {
				return fmt.Errorf("notebook not found: %s", id)
			}
			targets = []*model.Box{found}
		}
		if len(targets) == 0 {
			return fmt.Errorf("no notebooks to update")
		}

		// Compute the proposed new icons up front, to make dry-run preview and rollback-on-failure easier.
		type change struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			OldIcon string `json:"oldIcon"`
			NewIcon string `json:"newIcon"`
		}
		changes := make([]change, 0, len(targets))
		for _, nb := range targets {
			changes = append(changes, change{ID: nb.ID, Name: nb.Name, OldIcon: nb.Icon, NewIcon: randomEmoji()})
		}

		if dryRun {
			for _, c := range changes {
				fmt.Printf("[dry-run] Would set notebook %s (%s) icon %s -> %s\n", c.ID, c.Name, c.OldIcon, c.NewIcon)
			}
			return nil
		}

		for _, c := range changes {
			model.SetBoxIcon(c.ID, c.NewIcon)
		}
		util.PushReloadFiletree()

		switch outputFormat {
		case "json":
			data, _ := json.MarshalIndent(changes, "", "  ")
			fmt.Println(string(data))
		default:
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tOLD\tNEW")
			for _, c := range changes {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.ID, c.Name, c.OldIcon, c.NewIcon)
			}
			w.Flush()
		}
		return nil
	},
}

// builtinEmojiUnicodes caches all unicode code points of the built-in emoji (sourced from
// appearance/emojis/conf.json).
// This matches the frontend getRandomEmoji() data source, but deliberately excludes user-customized images, to
// keep the random-icon style consistent and avoid accidentally using the user's important images.
var (
	builtinEmojiUnicodes     []string
	builtinEmojiUnicodesOnce sync.Once
)

// loadBuiltinEmojiUnicodes lazily loads the list of built-in emoji unicode code points.
// On failure it falls back to a fixed code point, so the caller always gets a usable value.
func loadBuiltinEmojiUnicodes() {
	builtinEmojiUnicodesOnce.Do(func() {
		confPath := filepath.Join(util.AppearancePath, "emojis", "conf.json")
		data, err := os.ReadFile(confPath)
		if err != nil {
			builtinEmojiUnicodes = []string{"1f4d6"}
			return
		}

		var conf []map[string]any
		if err = gulu.JSON.UnmarshalJSON(data, &conf); err != nil {
			builtinEmojiUnicodes = []string{"1f4d6"}
			return
		}

		for _, category := range conf {
			items, ok := category["items"].([]any)
			if !ok {
				continue
			}
			for _, item := range items {
				e, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if u, ok := e["unicode"].(string); ok && u != "" {
					builtinEmojiUnicodes = append(builtinEmojiUnicodes, u)
				}
			}
		}

		if len(builtinEmojiUnicodes) == 0 {
			builtinEmojiUnicodes = []string{"1f4d6"}
		}
	})
}

// randomEmoji returns a random built-in emoji unicode code point (e.g. "1f4d6").
func randomEmoji() string {
	loadBuiltinEmojiUnicodes()
	return builtinEmojiUnicodes[rand.Intn(len(builtinEmojiUnicodes))]
}

func init() {
	notebookCreateCmd.Flags().String("name", "", "notebook name")
	notebookRemoveCmd.Flags().String("id", "", "notebook ID")
	notebookRenameCmd.Flags().String("id", "", "notebook ID")
	notebookRenameCmd.Flags().String("name", "", "new notebook name")
	notebookOpenCmd.Flags().String("id", "", "notebook ID")
	notebookCloseCmd.Flags().String("id", "", "notebook ID")
	notebookSetIconCmd.Flags().String("id", "", "notebook ID")
	notebookSetIconCmd.Flags().String("icon", "", "notebook icon: emoji hex codepoint like \"1f4ca\", emoji character like \"📊\", custom image path like \"1/b3log.png\", or dynamic icon URL like \"api/icon/getDynamicIcon?type=8&color=%23d23f31&content=SiYuan&id=xxx\"")
	notebookRandomIconCmd.Flags().String("id", "", "notebook ID (optional; omit to update all notebooks)")

	rootCmd.AddCommand(notebookCmd)
	notebookCmd.AddCommand(notebookListCmd)
	notebookCmd.AddCommand(notebookCreateCmd)
	notebookCmd.AddCommand(notebookRemoveCmd)
	notebookCmd.AddCommand(notebookRenameCmd)
	notebookCmd.AddCommand(notebookOpenCmd)
	notebookCmd.AddCommand(notebookCloseCmd)
	notebookCmd.AddCommand(notebookSetIconCmd)
	notebookCmd.AddCommand(notebookRandomIconCmd)
}
