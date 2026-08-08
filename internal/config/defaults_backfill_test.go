package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadConfigOptional_BackfillsMissingKeysFromDefaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	legacy := "" +
		"port: 9000\n" +
		"debug: true\n" +
		"api-keys:\n" +
		"  - \"my-real-key\"\n" +
		"request-retry: 1\n"
	if err := os.WriteFile(configPath, []byte(legacy), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfigOptional(configPath, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config")
	}

	if cfg.Port != 9000 {
		t.Fatalf("port = %d, want 9000", cfg.Port)
	}
	if !cfg.Debug {
		t.Fatal("debug should remain true")
	}
	if cfg.RequestRetry != 1 {
		t.Fatalf("request-retry = %d, want 1", cfg.RequestRetry)
	}
	if len(cfg.APIKeys) != 1 || cfg.APIKeys[0] != "my-real-key" {
		t.Fatalf("api-keys = %#v, want [my-real-key]", cfg.APIKeys)
	}

	if cfg.MaxRetryInterval != 30 {
		t.Fatalf("max-retry-interval = %d, want 30", cfg.MaxRetryInterval)
	}
	if cfg.ErrorLogsMaxFiles != 10 {
		t.Fatalf("error-logs-max-files = %d, want 10", cfg.ErrorLogsMaxFiles)
	}
	if !cfg.WebsocketAuth {
		t.Fatal("expected ws-auth=true after backfill")
	}
	if !cfg.QuotaExceeded.SwitchProject {
		t.Fatal("expected quota-exceeded.switch-project=true")
	}
	if cfg.XAI.FreeUsageExhaustedCooldownHoursValue() != 24 {
		t.Fatalf("xai free-usage cooldown = %d, want 24", cfg.XAI.FreeUsageExhaustedCooldownHoursValue())
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(data)
	for _, key := range []string{
		"max-retry-interval:",
		"disable-cooling:",
		"quota-exceeded:",
		"routing:",
		"xai:",
		"ws-auth:",
	} {
		if !strings.Contains(text, key) {
			t.Fatalf("expected backfilled key %q in file:\n%s", key, text)
		}
	}
	if strings.Contains(text, "your-api-key") {
		t.Fatalf("must not inject placeholder api-keys, got:\n%s", text)
	}
	if !strings.Contains(text, "my-real-key") {
		t.Fatalf("user api-key must remain, got:\n%s", text)
	}
}

func TestLoadConfigOptional_ExplicitFalsePreserved(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	content := "" +
		"port: 8317\n" +
		"ws-auth: false\n" +
		"request-retry: 0\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfigOptional(configPath, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional() error = %v", err)
	}
	if cfg.WebsocketAuth {
		t.Fatal("explicit ws-auth:false must be preserved")
	}
	if cfg.RequestRetry != 0 {
		t.Fatalf("explicit request-retry:0 must be preserved, got %d", cfg.RequestRetry)
	}
}

func TestParseConfigBytes_UsesBuiltinDefaults(t *testing.T) {
	cfg, err := ParseConfigBytes([]byte("port: 8317\n"))
	if err != nil {
		t.Fatalf("ParseConfigBytes() error = %v", err)
	}
	if cfg.RequestRetry != 3 {
		t.Fatalf("request-retry = %d, want 3", cfg.RequestRetry)
	}
	if cfg.MaxRetryInterval != 30 {
		t.Fatalf("max-retry-interval = %d, want 30", cfg.MaxRetryInterval)
	}
	if !cfg.WebsocketAuth {
		t.Fatal("expected ws-auth=true default")
	}
	if cfg.XAI.OtherForbiddenCooldownHoursValue() != 6 {
		t.Fatalf("xai other-403 cooldown = %d, want 6", cfg.XAI.OtherForbiddenCooldownHoursValue())
	}
	if cfg.RemoteManagement.PanelGitHubRepository != DefaultPanelGitHubRepository {
		t.Fatalf("panel repository = %q, want %q", cfg.RemoteManagement.PanelGitHubRepository, DefaultPanelGitHubRepository)
	}
	if !strings.Contains(cfg.RemoteManagement.PanelGitHubRepository, "github.com/josephcy95/") {
		t.Fatalf("panel repository must default to the fork, got %q", cfg.RemoteManagement.PanelGitHubRepository)
	}
}

func TestBackfillMissingMappingKeys_Nested(t *testing.T) {
	existing := []byte("" +
		"port: 1\n" +
		"quota-exceeded:\n" +
		"  switch-project: false\n")

	var original yaml.Node
	if err := yaml.Unmarshal(existing, &original); err != nil {
		t.Fatalf("parse existing: %v", err)
	}
	var defaults yaml.Node
	if err := yaml.Unmarshal(embeddedDefaultConfigYAML, &defaults); err != nil {
		t.Fatalf("parse defaults: %v", err)
	}
	if original.Kind != yaml.DocumentNode || defaults.Kind != yaml.DocumentNode {
		t.Fatal("expected document nodes")
	}
	changed := backfillMissingMappingKeys(original.Content[0], defaults.Content[0], nil)
	if !changed {
		t.Fatal("expected nested backfill to change document")
	}

	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&original); err != nil {
		t.Fatalf("encode: %v", err)
	}
	_ = enc.Close()
	text := buf.String()
	if !strings.Contains(text, "switch-project: false") {
		t.Fatalf("expected preserved switch-project:false, got:\n%s", text)
	}
	if !strings.Contains(text, "switch-preview-model:") {
		t.Fatalf("expected nested backfill of switch-preview-model, got:\n%s", text)
	}
	if !strings.Contains(text, "antigravity-credits:") {
		t.Fatalf("expected nested backfill of antigravity-credits, got:\n%s", text)
	}
}

func TestLoadConfigOptional_OptionalCloudDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	legacy := "port: 8317\n"
	if err := os.WriteFile(configPath, []byte(legacy), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	before, _ := os.ReadFile(configPath)
	cfg, err := LoadConfigOptional(configPath, true)
	if err != nil {
		t.Fatalf("LoadConfigOptional optional: %v", err)
	}
	after, _ := os.ReadFile(configPath)
	if string(before) != string(after) {
		t.Fatalf("optional/cloud load must not rewrite config file")
	}
	// In-memory defaults still apply.
	if cfg == nil {
		t.Fatal("expected config")
	}
	if cfg.Port != 8317 {
		t.Fatalf("port = %d, want 8317", cfg.Port)
	}
	if cfg.RemoteManagement.PanelGitHubRepository != DefaultPanelGitHubRepository {
		t.Fatalf("panel repository = %q, want %q", cfg.RemoteManagement.PanelGitHubRepository, DefaultPanelGitHubRepository)
	}
}
