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

import (
	"github.com/siyuan-note/siyuan/kernel/util"
)

type FileTree struct {
	AlwaysSelectOpenedFile   bool   `json:"alwaysSelectOpenedFile"`   // Whether to automatically select the currently open file
	OpenFilesUseCurrentTab   bool   `json:"openFilesUseCurrentTab"`   // Open files in the current tab
	DocIconClickExpand       bool   `json:"docIconClickExpand"`       // Expand or collapse child documents when clicking the document icon
	ParentDocClickExpand     bool   `json:"parentDocClickExpand"`     // Expand or collapse child documents when clicking the parent document title
	BoxDocEnabled            *bool  `json:"boxDocEnabled"`            // Whether the top-level notebook document is enabled
	RefCreateSaveBox         string `json:"refCreateSaveBox"`         // Notebook to store new documents created via block ref
	RefCreateSavePath        string `json:"refCreateSavePath"`        // Path to store new documents created via block ref
	DocCreateSaveBox         string `json:"docCreateSaveBox"`         // Notebook to store new documents
	DocCreateSavePath        string `json:"docCreateSavePath"`        // Path to store new documents
	ShorthandSaveBox         string `json:"shorthandSaveBox"`         // Notebook to store quick notes
	ShorthandSavePath        string `json:"shorthandSavePath"`        // Path to store quick notes
	MaxListCount             int    `json:"maxListCount"`             // Max list count
	MaxOpenTabCount          int    `json:"maxOpenTabCount"`          // Max open tab count
	AllowCreateDeeper        bool   `json:"allowCreateDeeper"`        // Allow creating child documents deeper than 7 levels
	RemoveDocWithoutConfirm  bool   `json:"removeDocWithoutConfirm"`  // Whether confirmation is skipped when removing a document
	CloseTabsOnStart         bool   `json:"closeTabsOnStart"`         // Close all tabs on startup
	UseSingleLineSave        bool   `json:"useSingleLineSave"`        // Save document .sy and attribute view .json as a single line
	LargeFileWarningSize     int    `json:"largeFileWarningSize"`     // Large file warning size (in MB)
	CreateDocAtTop           *bool  `json:"createDocAtTop"`           // Create new documents at the top https://github.com/siyuan-note/siyuan/issues/16327
	Sort                     int    `json:"sort"`                     // Sort mode
	RecentDocsMaxListCount   int    `json:"recentDocsMaxListCount"`   // Max list count for recent documents
	NoSplitScreenWhenOpenTab bool   `json:"noSplitScreenWhenOpenTab"` // Do not split the screen when opening a tab https://github.com/siyuan-note/siyuan/issues/16833
}

func NewFileTree() *FileTree {
	return &FileTree{
		AlwaysSelectOpenedFile:   false,
		OpenFilesUseCurrentTab:   false,
		DocIconClickExpand:       false,
		ParentDocClickExpand:     false,
		BoxDocEnabled:            func() *bool { b := true; return &b }(),
		Sort:                     util.SortModeCustom,
		MaxListCount:             512,
		MaxOpenTabCount:          8,
		AllowCreateDeeper:        false,
		CloseTabsOnStart:         false,
		UseSingleLineSave:        util.UseSingleLineSave,
		LargeFileWarningSize:     util.LargeFileWarningSize,
		CreateDocAtTop:           func() *bool { b := false; return &b }(),
		NoSplitScreenWhenOpenTab: false,
	}
}

const (
	MinFileTreeRecentDocsListCount = 32
	MaxFileTreeRecentDocsListCount = 256
)
