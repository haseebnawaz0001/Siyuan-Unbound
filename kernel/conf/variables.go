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

import "regexp"

// Variable is a single named variable. Name is the reference name, and Value is stored as plaintext (not encrypted), for non-sensitive configuration data.
type Variable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Variables is the global variables store, counterpart to Secrets: Secrets stores sensitive data encrypted, while
// Variables stores non-sensitive data as plaintext.
// Both are referenced in the {{vars.NAME}} form by agent tools, MCP services, etc.
type Variables struct {
	Items []*Variable `json:"items"`
}

func NewVariables() *Variables {
	return &Variables{Items: []*Variable{}}
}

// varPlaceholder matches placeholders in the {{vars.NAME}} form, where NAME does not contain a } character.
var varPlaceholder = regexp.MustCompile(`\{\{vars\.([^}]+)\}\}`)

// Resolve replaces {{vars.NAME}} placeholders in the string with the corresponding variable value, and also
// handles unprefixed $NAME, ${NAME} (substituted only when the corresponding name exists in the variables store).
// The original text is kept when the name isn't found, consistent with Secrets.Resolve's behavior.
func (v *Variables) Resolve(in string) string {
	if v == nil {
		return in
	}
	in = varPlaceholder.ReplaceAllStringFunc(in, func(match string) string {
		sub := varPlaceholder.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		name := sub[1]
		for _, item := range v.Items {
			if item != nil && item.Name == name {
				return item.Value
			}
		}
		return match
	})
	return resolveDollar(in, v.lookup)
}

// lookup finds a variable value by name, returning the value and whether it exists.
func (v *Variables) lookup(name string) (string, bool) {
	if v == nil {
		return "", false
	}
	for _, item := range v.Items {
		if item != nil && item.Name == name {
			return item.Value, true
		}
	}
	return "", false
}
