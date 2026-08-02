// SiYuan - Refactor your thinking
// Copyright (c) 2020-present, b3log.org
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package conf

import (
	"strings"
	"testing"
)

func TestNewAIAddsKeylessProviderFromEnvironment(t *testing.T) {
	t.Setenv("SIYUAN_OPENAI_API_KEY", "")
	t.Setenv("SIYUAN_OPENAI_API_MODEL", "local-model")
	t.Setenv("SIYUAN_OPENAI_API_BASE_URL", "http://127.0.0.1:8080/v1")

	ai := NewAI()
	if len(ai.Providers) != 1 {
		t.Fatalf("provider count = %d, want 1", len(ai.Providers))
	}
	provider := ai.Providers[0]
	if provider.APIKey != "" || provider.BaseURL != "http://127.0.0.1:8080/v1" || !provider.Enabled {
		t.Fatalf("unexpected keyless provider: %#v", provider)
	}
	if len(provider.Models) != 1 || provider.Models[0].Name != "local-model" || !provider.Models[0].Enabled {
		t.Fatalf("unexpected keyless provider models: %#v", provider.Models)
	}
}

func TestAIKeylessProviderIsAvailable(t *testing.T) {
	model := &Model{ID: "model-id", DisplayName: "Local Model", Name: "local-model", Enabled: true}
	provider := &Provider{Enabled: true, BaseURL: "http://127.0.0.1:8080/v1", Models: []*Model{model}}
	ai := &AI{Providers: []*Provider{provider}}

	if !ai.HasAnyProvider() {
		t.Fatal("keyless provider should be available")
	}
	for _, id := range []string{model.ID, model.DisplayName, model.Name} {
		gotProvider, gotModel := ai.GetModel(id)
		if gotProvider != provider || gotModel != model {
			t.Fatalf("GetModel(%q) returned provider=%p model=%p", id, gotProvider, gotModel)
		}
	}
}

func TestAIDisabledKeylessProviderOrModelIsUnavailable(t *testing.T) {
	tests := []struct {
		name     string
		provider *Provider
	}{
		{
			name:     "disabled provider",
			provider: &Provider{Models: []*Model{{ID: "model-id", Name: "local-model", Enabled: true}}},
		},
		{
			name:     "disabled model",
			provider: &Provider{Enabled: true, Models: []*Model{{ID: "model-id", Name: "local-model"}}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ai := &AI{Providers: []*Provider{test.provider}}
			if ai.HasAnyProvider() {
				t.Fatal("disabled provider or model should be unavailable")
			}
			if provider, model := ai.GetModel("model-id"); provider != nil || model != nil {
				t.Fatalf("disabled provider or model returned provider=%p model=%p", provider, model)
			}
		})
	}
}

func TestAssignDefaultModelIDsUsesKeylessProvider(t *testing.T) {
	model := &Model{ID: "model-id", Name: "local-model", Enabled: true}
	ai := &AI{
		Providers: []*Provider{{Enabled: true, Models: []*Model{model}}},
		Agent:     &Agent{},
		Editing:   &Editing{},
	}

	assignDefaultModelIDs(ai)
	if ai.Agent.ModelID != model.ID || ai.Editing.ModelID != model.ID {
		t.Fatalf("unexpected default model IDs: agent=%q editing=%q", ai.Agent.ModelID, ai.Editing.ModelID)
	}
}

// TestEncryptAPIKeysCoversEveryStoredSecret guards the failure mode where a new secret field is added to the config
// but not to EncryptAPIKeys/DecryptAPIKeys, which enumerate their fields by hand -- the key then lands in conf.json
// as plaintext and nothing complains. Every secret the AI config holds must round-trip.
func TestEncryptAPIKeysCoversEveryStoredSecret(t *testing.T) {
	ai := NewAI()
	ai.Providers = []*Provider{{APIKey: "provider-secret"}}
	ai.Embedding = &Embedding{APIKey: "embedding-secret"}
	ai.Rerank = &Rerank{APIKey: "rerank-secret"}
	ai.WebSearch = &WebSearch{ExaAPIKey: "exa-secret"}

	ai.EncryptAPIKeys()
	for name, got := range map[string]string{
		"provider":  ai.Providers[0].APIKey,
		"embedding": ai.Embedding.APIKey,
		"rerank":    ai.Rerank.APIKey,
		"webSearch": ai.WebSearch.ExaAPIKey,
	} {
		if strings.Contains(got, "-secret") {
			t.Errorf("%s key was left in plaintext by EncryptAPIKeys: %q", name, got)
		}
	}

	ai.DecryptAPIKeys()
	for name, want := range map[string]string{
		"provider":  "provider-secret",
		"embedding": "embedding-secret",
		"rerank":    "rerank-secret",
		"webSearch": "exa-secret",
	} {
		var got string
		switch name {
		case "provider":
			got = ai.Providers[0].APIKey
		case "embedding":
			got = ai.Embedding.APIKey
		case "rerank":
			got = ai.Rerank.APIKey
		case "webSearch":
			got = ai.WebSearch.ExaAPIKey
		}
		if got != want {
			t.Errorf("%s key did not round-trip: got %q, want %q", name, got, want)
		}
	}
}
