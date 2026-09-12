package config

import "testing"

func TestNormalizeDesensitizationScopeDefault(t *testing.T) {
	got := NormalizeDesensitizationConfig(DesensitizationConfig{Enabled: true})
	if got.Scope != DesensitizationScopeAll {
		t.Fatalf("scope = %q, want all", got.Scope)
	}
	got = NormalizeDesensitizationConfig(DesensitizationConfig{Scope: "  TARGETED "})
	if got.Scope != DesensitizationScopeTargeted {
		t.Fatalf("scope = %q, want targeted", got.Scope)
	}
	got = NormalizeDesensitizationConfig(DesensitizationConfig{Scope: "weird"})
	if got.Scope != DesensitizationScopeAll {
		t.Fatalf("invalid scope = %q, want all", got.Scope)
	}
}

func TestAppliesDisabled(t *testing.T) {
	cfg := DefaultDesensitizationConfig()
	cfg.Enabled = false
	cfg.Scope = DesensitizationScopeAll
	if cfg.Applies("k", "claude", "oauth") {
		t.Fatal("disabled should never apply")
	}
}

func TestAppliesScopeAll(t *testing.T) {
	cfg := DefaultDesensitizationConfig()
	cfg.Enabled = true
	if !cfg.Applies("", "", "") {
		t.Fatal("scope all should apply without key/provider")
	}
}

func TestAppliesTargetedEmpty(t *testing.T) {
	cfg := DefaultDesensitizationConfig()
	cfg.Enabled = true
	cfg.Scope = DesensitizationScopeTargeted
	if cfg.Applies("any-key", "claude", "oauth") {
		t.Fatal("targeted with empty lists should mask nothing")
	}
}

func TestAppliesTargetedKeyHitMiss(t *testing.T) {
	cfg := DefaultDesensitizationConfig()
	cfg.Enabled = true
	cfg.Scope = DesensitizationScopeTargeted
	cfg.APIKeys = []string{"  secret-key  "}
	if !cfg.Applies("secret-key", "", "") {
		t.Fatal("exact inbound key should apply before provider is known")
	}
	if cfg.Applies("other-key", "", "") {
		t.Fatal("non-matching inbound key should not apply")
	}
}

func TestAppliesTargetedOAuthHitMiss(t *testing.T) {
	cfg := DefaultDesensitizationConfig()
	cfg.Enabled = true
	cfg.Scope = DesensitizationScopeTargeted
	cfg.OAuthProviders = []string{"Claude"}
	if !cfg.Applies("", "claude", "oauth") {
		t.Fatal("oauth provider should match case-insensitively")
	}
	if cfg.Applies("", "codex", "oauth") {
		t.Fatal("unlisted oauth provider should miss")
	}
	if cfg.Applies("", "claude", "apikey") {
		t.Fatal("oauth list must not match apikey credentials")
	}
}

func TestAppliesTargetedAPIProviderHitMiss(t *testing.T) {
	cfg := DefaultDesensitizationConfig()
	cfg.Enabled = true
	cfg.Scope = DesensitizationScopeTargeted
	cfg.APIProviders = []string{"my-compat"}
	if !cfg.Applies("", "openai-compatible-my-compat", "apikey") {
		t.Fatal("compat Auth.Provider should match configured name")
	}
	if !cfg.Applies("", "my-compat", "API_KEY") {
		t.Fatal("named compat provider should match apikey kind aliases")
	}
	if cfg.Applies("", "openai-compatible-other", "apikey") {
		t.Fatal("other compat name should miss")
	}
	if cfg.Applies("", "my-compat", "oauth") {
		t.Fatal("api provider list must not match oauth credentials")
	}
}

func TestAppliesMixtureKeyOrProvider(t *testing.T) {
	cfg := DefaultDesensitizationConfig()
	cfg.Enabled = true
	cfg.Scope = DesensitizationScopeTargeted
	cfg.APIKeys = []string{"priv-key"}
	cfg.OAuthProviders = []string{"codex"}
	cfg.APIProviders = []string{"gemini"}
	if !cfg.Applies("priv-key", "xai", "oauth") {
		t.Fatal("key hit should apply even when provider misses")
	}
	if !cfg.Applies("other", "CODEX", "oauth") {
		t.Fatal("oauth provider hit should apply when key misses")
	}
	if !cfg.Applies("other", "gemini", "apikey") {
		t.Fatal("api provider hit should apply when key misses")
	}
	if cfg.Applies("other", "xai", "oauth") {
		t.Fatal("mixture is OR; no matching list should miss")
	}
}

func TestAppliesOpenAICompatibilityAliases(t *testing.T) {
	cfg := DefaultDesensitizationConfig()
	cfg.Enabled = true
	cfg.Scope = DesensitizationScopeTargeted
	cfg.APIProviders = []string{"openai-compatibility"}
	if !cfg.Applies("", "openai-compatible-foo", "apikey") {
		t.Fatal("openai-compatibility should alias openai-compatible-* providers")
	}
	if !cfg.Applies("", "openai", "apikey") {
		t.Fatal("openai alias should match openai-compatibility selection")
	}
}

func TestAppliesInteractionsAlias(t *testing.T) {
	cfg := DefaultDesensitizationConfig()
	cfg.Enabled = true
	cfg.Scope = DesensitizationScopeTargeted
	cfg.APIProviders = []string{"interactions"}
	if !cfg.Applies("", "gemini-interactions", "apikey") {
		t.Fatal("interactions should alias gemini-interactions")
	}
}
