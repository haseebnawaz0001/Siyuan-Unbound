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
	"os"
	"strings"
	"text/tabwriter"

	"github.com/siyuan-note/siyuan/kernel/model"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"

	"github.com/88250/lute/ast"
	"github.com/spf13/cobra"
)

// inboxCmd manages the inbox: lists and reads cloud clippings, and batch-converts them into local documents.
// Inbox data is stored in the SiYuan cloud and requires a subscription; the underlying implementation reuses
// the model layer's cloud shorthand read/write functions.
var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Manage the cloud inbox (clipped shorthands)",
}

// inboxListCmd paginates the inbox listing, returning only summaries rather than the full body to control
// output length; the body is fetched on demand via get.
var inboxListCmd = &cobra.Command{
	Use:   "list [--page]",
	Short: "List cloud inbox shorthands",
	RunE: func(cmd *cobra.Command, args []string) error {
		page, _ := cmd.Flags().GetInt("page")
		if page < 1 {
			page = 1
		}

		result, err := model.GetCloudShorthands(page)
		if err != nil {
			return err
		}

		data, _ := result["data"].(map[string]any)
		shorthands, _ := data["shorthands"].([]any)

		switch outputFormat {
		case "json":
			data, _ := json.MarshalIndent(shorthands, "", "  ")
			fmt.Println(string(data))
		default:
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTITLE\tURL\tCREATED")
			for _, item := range shorthands {
				sh, _ := item.(map[string]any)
				if sh == nil {
					continue
				}
				id, _ := sh["oId"].(string)
				title, _ := sh["shorthandTitle"].(string)
				url, _ := sh["shorthandURL"].(string)
				hCreated, _ := sh["hCreated"].(string)
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", id, title, url, hCreated)
			}
			w.Flush()
			fmt.Printf("\n%d item(s) on page %d\n", len(shorthands), page)
		}
		return nil
	},
}

// inboxGetCmd fetches a single inbox item's detail, returning the full markdown body (shorthandMd).
var inboxGetCmd = &cobra.Command{
	Use:   "get --id <id>",
	Short: "Get a cloud inbox shorthand with full markdown",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if id == "" {
			return fmt.Errorf("--id is required")
		}

		sh, err := model.GetCloudShorthand(id)
		if err != nil {
			return err
		}

		switch outputFormat {
		case "json":
			data, _ := json.MarshalIndent(sh, "", "  ")
			fmt.Println(string(data))
		default:
			title, _ := sh["shorthandTitle"].(string)
			url, _ := sh["shorthandURL"].(string)
			hCreated, _ := sh["hCreated"].(string)
			md, _ := sh["shorthandMd"].(string)
			fmt.Println("ID:     ", id)
			fmt.Println("TITLE:  ", title)
			fmt.Println("CREATED:", hCreated)
			if url != "" {
				fmt.Println("URL:    ", url)
			}
			fmt.Println("\nMARKDOWN:")
			fmt.Println(md)
		}
		return nil
	},
}

// inboxConvertCmd converts one or more clippings into local documents: fetch the cloud md -> create the local
// document -> clean up the cloud original on success.
// A failed item is not deleted and does not interrupt processing of the remaining items; per-item results are printed.
var inboxConvertCmd = &cobra.Command{
	Use:   "convert --ids <id1,id2,...> --notebook <id> [--path </h/path>] [--remove-after]",
	Short: "Convert cloud inbox shorthands into local documents",
	RunE: func(cmd *cobra.Command, args []string) error {
		notebook, _ := cmd.Flags().GetString("notebook")
		if notebook == "" {
			return fmt.Errorf("--notebook is required")
		}

		idsRaw, _ := cmd.Flags().GetString("ids")
		ids := parseShorthandIDs(idsRaw)
		if len(ids) == 0 {
			return fmt.Errorf("--ids is required (comma-separated shorthand IDs)")
		}

		hPath, _ := cmd.Flags().GetString("path")
		if hPath == "" {
			hPath = "/"
		}
		removeAfter, _ := cmd.Flags().GetBool("remove-after")

		// Resolve the target parent path (hPath->fsPath): hPath points to the parent container the new
		// document will land in, and its parent directory must already exist; when omitted or "/" is passed,
		// it lands at the notebook root.
		parentPath := "/"
		parentDir := parentDir(hPath)
		if parentDir != "/" {
			bt := treenode.GetBlockTreeRootByHPath(notebook, parentDir)
			if bt == nil {
				return fmt.Errorf("parent path not found: %s", parentDir)
			}
			parentPath = strings.TrimSuffix(bt.Path, ".sy")
		}

		if dryRun {
			fmt.Printf("[dry-run] Would convert %d shorthand(s) -> notebook %s (hPath: %s, removeAfter: %v)\n",
				len(ids), notebook, hPath, removeAfter)
			for _, id := range ids {
				fmt.Printf("[dry-run]   - %s\n", id)
			}
			return nil
		}

		type result struct {
			ID     string `json:"id"`
			Title  string `json:"title"`
			DocID  string `json:"docId"`
			Status string `json:"status"`
			Error  string `json:"error,omitempty"`
		}
		results := make([]result, 0, len(ids))
		successIDs := make([]string, 0, len(ids))
		failed := 0

		for _, id := range ids {
			sh, err := model.GetCloudShorthand(id)
			if err != nil {
				results = append(results, result{ID: id, Status: "failed", Error: err.Error()})
				failed++
				continue
			}

			title, _ := sh["shorthandTitle"].(string)
			if title == "" {
				title = "Untitled"
			}
			md, _ := sh["shorthandMd"].(string)
			// When content is empty (neither md nor a rendered body) but a source URL exists, fall back to a
			// markdown link, matching the frontend's behavior.
			if md == "" {
				if content, _ := sh["shorthandContent"].(string); content == "" {
					if url, _ := sh["shorthandURL"].(string); url != "" {
						md = "[" + title + "](" + url + ")"
					}
				}
			}

			docID := ast.NewNodeID()
			docPath := strings.TrimRight(parentPath, "/") + "/" + docID + ".sy"
			tree, err := model.CreateDocByMd(notebook, docPath, title, md, nil, nil)
			if err != nil {
				results = append(results, result{ID: id, Title: title, Status: "failed", Error: err.Error()})
				failed++
				continue
			}
			successIDs = append(successIDs, id)
			results = append(results, result{ID: id, Title: title, DocID: tree.Root.ID, Status: "created"})
		}

		// Clean up cloud originals only for items that converted successfully; failed items are kept for retrying.
		if removeAfter && len(successIDs) > 0 {
			if err := model.RemoveCloudShorthands(successIDs); err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: failed to remove cloud originals: %s\n", err)
			}
		}

		util.PushReloadFiletree()

		switch outputFormat {
		case "json":
			data, _ := json.MarshalIndent(results, "", "  ")
			fmt.Println(string(data))
		default:
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SHORTHAND\tTITLE\tDOC ID\tSTATUS")
			for _, r := range results {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.ID, r.Title, r.DocID, r.Status)
				if r.Error != "" {
					fmt.Fprintf(w, "\t\t\t  %s\n", r.Error)
				}
			}
			w.Flush()
			fmt.Printf("\nDone: %d succeeded, %d failed.\n", len(successIDs), failed)
		}
		return nil
	},
}

// parseShorthandIDs splits a comma-separated id string into a slice with whitespace trimmed and duplicates removed.
func parseShorthandIDs(s string) []string {
	if s == "" {
		return nil
	}
	raw := make([]string, 0, 4)
	for p := range strings.SplitSeq(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			raw = append(raw, p)
		}
	}
	seen := make(map[string]bool, len(raw))
	out := raw[:0]
	for _, id := range raw {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// parentDir returns the parent directory of a path: strips the last "/" segment; the root path and a single-segment path return "/".
func parentDir(p string) string {
	i := strings.LastIndex(p, "/")
	if i <= 0 {
		return "/"
	}
	return p[:i]
}

func init() {
	inboxListCmd.Flags().IntP("page", "p", 1, "page number (1-based)")
	inboxGetCmd.Flags().String("id", "", "shorthand ID (required)")
	inboxConvertCmd.Flags().String("ids", "", "comma-separated shorthand IDs (required)")
	inboxConvertCmd.Flags().String("notebook", "", "target notebook ID (required)")
	inboxConvertCmd.Flags().String("path", "/", "target hPath in the notebook (default \"/\", the notebook root)")
	inboxConvertCmd.Flags().Bool("remove-after", true, "delete cloud shorthands after successful conversion")

	rootCmd.AddCommand(inboxCmd)
	inboxCmd.AddCommand(inboxListCmd)
	inboxCmd.AddCommand(inboxGetCmd)
	inboxCmd.AddCommand(inboxConvertCmd)
}
