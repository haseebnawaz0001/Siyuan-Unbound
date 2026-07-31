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

import "testing"

// TestSecretsResolveExplicit verifies that the {{secrets.NAME}} explicit syntax and $NAME/${NAME} only look up within the secrets store.
func TestSecretsResolveExplicit(t *testing.T) {
	s := &Secrets{Items: []*Secret{{Name: "KEY", Value: "k1"}}}
	cases := []struct{ in, want string }{
		{"{{secrets.KEY}}", "k1"},
		{"pre {{secrets.KEY}} post", "pre k1 post"},
		{"{{secrets.MISSING}}", "{{secrets.MISSING}}"}, // Not found, keep the original text
		{"$KEY", "k1"},           // Unprefixed shorthand, hits the secrets store
		{"${KEY}", "k1"},         // Unprefixed with braces, hits the secrets store
		{"$MISSING", "$MISSING"}, // No such name in the secrets store, keep the original text
		{"$100", "$100"},         // Starting with a digit does not match
		{"", ""},
	}
	for _, c := range cases {
		if got := s.Resolve(c.in); got != c.want {
			t.Errorf("Secrets.Resolve(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestVariablesResolveExplicit verifies that the {{vars.NAME}} explicit syntax and $NAME/${NAME} only look up within the variables store.
func TestVariablesResolveExplicit(t *testing.T) {
	v := &Variables{Items: []*Variable{{Name: "VAR", Value: "v1"}}}
	cases := []struct{ in, want string }{
		{"{{vars.VAR}}", "v1"},
		{"{{vars.MISSING}}", "{{vars.MISSING}}"},
		{"$VAR", "v1"},
		{"${VAR}", "v1"},
		{"$MISSING", "$MISSING"},
	}
	for _, c := range cases {
		if got := v.Resolve(c.in); got != c.want {
			t.Errorf("Variables.Resolve(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestPerStoreDollarIsolation verifies that each store's Resolve, when handling $NAME, only looks in its own
// store and never crosses over.
// secrets.Resolve("$ONLY_VAR") should not get the variables store's value, and vice versa.
func TestPerStoreDollarIsolation(t *testing.T) {
	secrets := &Secrets{Items: []*Secret{{Name: "ONLY_SECRET", Value: "s"}}}
	vars := &Variables{Items: []*Variable{{Name: "ONLY_VAR", Value: "v"}}}
	// The secrets store's Resolve should not resolve a name that only exists in the variables store
	if got := secrets.Resolve("$ONLY_VAR"); got != "$ONLY_VAR" {
		t.Errorf("secrets.Resolve($ONLY_VAR) = %q, want $ONLY_VAR (密钥库不应跨库查变量)", got)
	}
	// The variables store's Resolve should not resolve a name that only exists in the secrets store
	if got := vars.Resolve("$ONLY_SECRET"); got != "$ONLY_SECRET" {
		t.Errorf("vars.Resolve($ONLY_SECRET) = %q, want $ONLY_SECRET (变量库不应跨库查密钥)", got)
	}
}

// TestResolveSecretsVarsDollarSyntax verifies the unprefixed $NAME / ${NAME} shell-style syntax.
// It only substitutes when the corresponding name exists in the secrets or variables store; otherwise the
// original text is kept.
func TestResolveSecretsVarsDollarSyntax(t *testing.T) {
	secrets := &Secrets{Items: []*Secret{
		{Name: "WEREAD_API_KEY", Value: "wrk-secret"},
		{Name: "KEY", Value: "k1"},
	}}
	vars := &Variables{Items: []*Variable{{Name: "VAR", Value: "v1"}}}

	cases := []struct{ in, want string }{
		// shell shorthand and brace forms
		{"Bearer $WEREAD_API_KEY", "Bearer wrk-secret"},
		{"Bearer ${WEREAD_API_KEY}", "Bearer wrk-secret"},
		// Unrelated content like a digit start or regex groups is kept as-is
		{"price $100", "price $100"},
		{"regex $1 group", "regex $1 group"},
		// Original text is kept when there is no corresponding name in either store
		{"$A $B end", "$A $B end"},
		// The explicit syntax still works normally
		{"{{secrets.KEY}}-{{vars.VAR}}", "k1-v1"},
		// $VAR matches the secret first; falls back to the variables store if the secrets store has no VAR
		{"$VAR", "v1"},
		// Mixed scenario
		{"mix $KEY and ${VAR} and literal", "mix k1 and v1 and literal"},
	}
	for _, c := range cases {
		if got := ResolveSecretsVars(secrets, vars, c.in); got != c.want {
			t.Errorf("ResolveSecretsVars(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestResolveSecretsVarsPriority verifies that when a name is duplicated, an unprefixed reference prefers the secret, while the explicit syntax picks from its own store either way.
func TestResolveSecretsVarsPriority(t *testing.T) {
	secrets := &Secrets{Items: []*Secret{{Name: "dup", Value: "from-secret"}}}
	vars := &Variables{Items: []*Variable{{Name: "dup", Value: "from-var"}}}

	if got := ResolveSecretsVars(secrets, vars, "$dup"); got != "from-secret" {
		t.Errorf("$dup = %q, want from-secret", got)
	}
	if got := ResolveSecretsVars(secrets, vars, "${dup}"); got != "from-secret" {
		t.Errorf("${dup} = %q, want from-secret", got)
	}
	// The explicit syntax is unaffected by duplicate names
	if g := ResolveSecretsVars(secrets, vars, "{{secrets.dup}}"); g != "from-secret" {
		t.Errorf("{{secrets.dup}} = %q, want from-secret", g)
	}
	if g := ResolveSecretsVars(secrets, vars, "{{vars.dup}}"); g != "from-var" {
		t.Errorf("{{vars.dup}} = %q, want from-var", g)
	}
}

// TestResolveSecretsVarsNilSafe verifies that a nil argument does not trigger a panic.
func TestResolveSecretsVarsNilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ResolveSecretsVars with nil panicked: %v", r)
		}
	}()
	// Either being nil should be safely skipped; if the name isn't found, the original text is kept
	if got := ResolveSecretsVars(nil, nil, "$X"); got != "$X" {
		t.Errorf("ResolveSecretsVars(nil,nil) = %q, want $X", got)
	}
	if got := ResolveSecretsVars(nil, nil, "plain text"); got != "plain text" {
		t.Errorf("ResolveSecretsVars(nil,nil,plain) = %q, want plain text", got)
	}
}

// TestSecretsLookup verifies the lookup return value and existence flag.
func TestSecretsLookup(t *testing.T) {
	s := &Secrets{Items: []*Secret{{Name: "a", Value: "1"}}}
	if v, ok := s.lookup("a"); !ok || v != "1" {
		t.Errorf("lookup(a) = %q,%v, want 1,true", v, ok)
	}
	if _, ok := s.lookup("missing"); ok {
		t.Error("lookup(missing) should be not ok")
	}
}
