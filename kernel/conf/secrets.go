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
	"encoding/hex"
	"regexp"

	"github.com/siyuan-note/siyuan/kernel/util"
)

// Secret is a single named secret. Name is the reference name (e.g. weread_key); Value is plaintext at runtime and is encrypted by Secrets.Encrypt when persisted.
type Secret struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Secrets is the global secrets store, existing independently of the AI configuration, for reference in the
// {{secret:name}} form by the agent's http_request tool, MCP service headers, etc. Value is AES-encrypted when
// persisted and plaintext at runtime.
type Secrets struct {
	Items []*Secret `json:"items"`
}

func NewSecrets() *Secrets {
	return &Secrets{Items: []*Secret{}}
}

// Encrypt encrypts the in-memory plaintext into ciphertext, called before AppConf.Save() serializes it.
func (s *Secrets) Encrypt() {
	if s == nil {
		return
	}
	for _, item := range s.Items {
		if item == nil || item.Value == "" {
			continue
		}
		item.Value = util.AESEncrypt(item.Value)
	}
}

// Decrypt decrypts the ciphertext back to plaintext. util.AESDecrypt returns hex text, which must go through
// hex.DecodeString once more to get the real plaintext, following the same double-hex pattern as
// conf.AI.DecryptAPIKeys.
func (s *Secrets) Decrypt() {
	if s == nil {
		return
	}
	for _, item := range s.Items {
		if item == nil || item.Value == "" {
			continue
		}
		dec := util.AESDecrypt(item.Value)
		if dec == nil {
			continue
		}
		if plain, err := hex.DecodeString(string(dec)); err == nil {
			item.Value = string(plain)
		}
	}
}

// secretPlaceholder matches placeholders in the {{secrets.NAME}} form, where NAME does not contain a } character.
var secretPlaceholder = regexp.MustCompile(`\{\{secrets\.([^}]+)\}\}`)

// Resolve replaces {{secrets.NAME}} placeholders in the string with the corresponding plaintext secret, and also
// handles unprefixed $NAME, ${NAME} (substituted only when the corresponding name exists in the secrets store).
// The original text is kept when the name isn't found, so the caller/LLM can spot secrets that haven't been
// configured yet.
// Must be called while the in-memory state is plaintext (after InitConf decrypts, or after AppConf.Save's defer
// restores it).
func (s *Secrets) Resolve(in string) string {
	if s == nil {
		return in
	}
	in = secretPlaceholder.ReplaceAllStringFunc(in, func(match string) string {
		sub := secretPlaceholder.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		name := sub[1]
		for _, item := range s.Items {
			if item != nil && item.Name == name {
				return item.Value
			}
		}
		return match
	})
	return resolveDollar(in, s.lookup)
}

// lookup finds a secret value by name, returning the value and whether it exists.
func (s *Secrets) lookup(name string) (string, bool) {
	if s == nil {
		return "", false
	}
	for _, item := range s.Items {
		if item != nil && item.Name == name {
			return item.Value, true
		}
	}
	return "", false
}

// dollarPlaceholder matches unprefixed shell-style variable references: ${NAME} and $NAME.
// NAME is restricted to letters/digits/underscores, to avoid mis-matching things like $100 or regex.
var dollarPlaceholder = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// resolveDollar replaces $NAME, ${NAME} (shell-style) in the string. It tries the lookups in order; the first
// match's value takes effect, and if none match, the original text is kept. It only substitutes when the name can
// be found, so unrelated content like $100 or regex is unaffected.
func resolveDollar(in string, lookups ...func(string) (string, bool)) string {
	return dollarPlaceholder.ReplaceAllStringFunc(in, func(match string) string {
		sub := dollarPlaceholder.FindStringSubmatch(match)
		// sub[1] is the ${NAME} capture group, sub[2] is the $NAME capture group.
		name := sub[1]
		if name == "" {
			name = sub[2]
		}
		if name == "" {
			return match
		}
		for _, lk := range lookups {
			if v, ok := lk(name); ok {
				return v
			}
		}
		return match
	})
}

// ResolveSecretsVars replaces {{secrets.NAME}}, {{vars.NAME}}, and the unprefixed $NAME, ${NAME}.
// Secrets.Resolve / Variables.Resolve each already handle the explicit syntax and unprefixed references (looking
// only in their own store); calling them in sequence yields the "$NAME prefers secret over variable" priority. It
// only substitutes when the name exists in the store, keeping the original text otherwise -- so unrelated content
// like $100 or regex is unaffected.
// Used by the agent's http_request tool, MCP service headers, etc, to uniformly consume secrets and variables.
func ResolveSecretsVars(secrets *Secrets, vars *Variables, in string) string {
	in = secrets.Resolve(in)
	return vars.Resolve(in)
}
