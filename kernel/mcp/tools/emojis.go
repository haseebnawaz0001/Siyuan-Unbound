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

package tools

import (
	"math/rand"
	"os"
	"path/filepath"
	"sync"

	"github.com/88250/gulu"

	"github.com/siyuan-note/siyuan/kernel/util"
)

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
