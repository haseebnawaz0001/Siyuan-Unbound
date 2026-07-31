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

// BoxConf maintains the notebook configuration in .siyuan/conf.json.
type BoxConf struct {
	Name                  string         `json:"name"`                  // Notebook name
	Sort                  int            `json:"sort"`                  // Sort field
	Icon                  string         `json:"icon"`                  // Icon
	Closed                bool           `json:"closed"`                // Whether it's closed
	RefCreateSaveBox      string         `json:"refCreateSaveBox"`      // Notebook to store new documents created via block ref
	RefCreateSavePath     string         `json:"refCreateSavePath"`     // Path to store new documents created via block ref
	DocCreateSaveBox      string         `json:"docCreateSaveBox"`      // Notebook to store new documents
	DocCreateSavePath     string         `json:"docCreateSavePath"`     // Path to store new documents
	DailyNoteSavePath     string         `json:"dailyNoteSavePath"`     // Path to store new daily notes
	DailyNoteTemplatePath string         `json:"dailyNoteTemplatePath"` // Template path used for new daily notes
	SortMode              int            `json:"sortMode"`              // Sort mode
	Encrypted             bool           `json:"encrypted"`             // Whether this is an encrypted notebook
	BoxCrypt              *BoxEncryption `json:"boxCrypt"`              // Notebook encryption parameters, only set when Encrypted=true
}

// BoxEncryption maintains the key envelope parameters for a single encrypted notebook. WrappedDEK is the DEK
// encrypted with the global KEK, and can itself be persisted.
type BoxEncryption struct {
	Spec       int    `json:"spec,omitempty"` // Envelope spec version; 1 means WrappedDEK is bound to the boxID AAD
	WrappedDEK []byte `json:"wrappedDEK"`     // The DEK encrypted with the KEK via AES-GCM
	WrapNonce  []byte `json:"wrapNonce"`      // The GCM nonce used for the envelope (extracted from the encryption envelope)
	CreatedAt  int64  `json:"createdAt"`      // Creation time, in milliseconds, to facilitate future time-based key rotation
}

func NewBoxConf() *BoxConf {
	return &BoxConf{
		Name:                  "Untitled",
		Closed:                true,
		DailyNoteSavePath:     "/daily note/{{now | date \"2006/01\"}}/{{now | date \"2006-01-02\"}}",
		DailyNoteTemplatePath: "",
		SortMode:              util.SortModeFileTree,
		Encrypted:             false,
	}
}
