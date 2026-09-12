package desensitization

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func enabledCfg() config.DesensitizationConfig {
	cfg := config.DefaultDesensitizationConfig()
	cfg.Enabled = true
	return config.NormalizeDesensitizationConfig(cfg)
}

func TestMaskRestorePhoneEmailSK(t *testing.T) {
	eng := NewEngine(enabledCfg())
	sid := "sess-1"
	in := `联系我 13800138000 或 user@example.com，密钥 sk-abcdefghijklmnopqrstuvwxyz012345`
	out := eng.maskText(sid, in, nil)
	if strings.Contains(out, "13800138000") || strings.Contains(out, "user@example.com") || strings.Contains(out, "sk-abcdefghijklmnopqrstuvwxyz012345") {
		t.Fatalf("expected secrets masked, got %q", out)
	}
	if !strings.Contains(out, "{{PHONE_") || !strings.Contains(out, "{{EMAIL_") || !strings.Contains(out, "{{API_KEY_") {
		t.Fatalf("expected placeholders, got %q", out)
	}
	restored := eng.restoreText(sid, out, "", true)
	if !strings.Contains(restored, "13800138000") || !strings.Contains(restored, "user@example.com") {
		t.Fatalf("expected PII restored, got %q", restored)
	}
	// restore_secrets default false — API key stays masked
	if strings.Contains(restored, "sk-abcdefghijklmnopqrstuvwxyz012345") {
		t.Fatalf("API key should remain placeholder, got %q", restored)
	}
	if !strings.Contains(restored, "{{API_KEY_") {
		t.Fatalf("expected API_KEY placeholder kept, got %q", restored)
	}
}

func TestCustomTermAndSessionReuse(t *testing.T) {
	cfg := enabledCfg()
	cfg.CustomTerms = []config.DesensitizationTerm{{Value: "AcmeSecretCorp", Category: "TERM", WholeWord: true}}
	eng := NewEngine(cfg)
	sid := "sess-reuse"
	a := eng.maskText(sid, "call AcmeSecretCorp now", nil)
	b := eng.maskText(sid, "again AcmeSecretCorp please", nil)
	tokA := extractToken(a, "TERM")
	tokB := extractToken(b, "TERM")
	if tokA == "" || tokA != tokB {
		t.Fatalf("session reuse failed: %q vs %q", tokA, tokB)
	}
	phone1 := eng.maskText(sid, "13800138000", nil)
	phone2 := eng.maskText(sid, "tel 13800138000", nil)
	if extractToken(phone1, "PHONE") != extractToken(phone2, "PHONE") {
		t.Fatalf("phone token not stable: %q / %q", phone1, phone2)
	}
}

func TestChunkSplitRestore(t *testing.T) {
	eng := NewEngine(enabledCfg())
	sid := "sess-stream"
	masked := eng.maskText(sid, "hello 13800138000", nil)
	tok := extractToken(masked, "PHONE")
	if tok == "" {
		t.Fatal("no phone token")
	}
	// Split across {{PHO and NE_xxx}}
	mid := strings.Index(tok, "_")
	part1 := "hello " + tok[:mid]
	part2 := tok[mid:]
	out1 := eng.RestoreStreamChunk(sid, "s1", []byte(part1))
	out2 := eng.RestoreStreamChunk(sid, "s1", []byte(part2))
	flush := eng.FlushStream(sid, "s1")
	combined := string(out1) + string(out2) + string(flush)
	if combined != "hello 13800138000" {
		t.Fatalf("chunk restore = %q, want hello 13800138000 (parts %q + %q)", combined, part1, part2)
	}
}

func TestRestoreSecretsFalse(t *testing.T) {
	cfg := enabledCfg()
	falseVal := false
	cfg.RestoreSecrets = &falseVal
	eng := NewEngine(cfg)
	sid := "sess-sec"
	masked := eng.maskText(sid, "sk-abcdefghijklmnopqrstuvwxyz012345", nil)
	restored := eng.restoreText(sid, masked, "", true)
	if restored != masked {
		t.Fatalf("expected API key placeholder kept, got %q from %q", restored, masked)
	}
}

func TestWalkerNestedToolArgs(t *testing.T) {
	eng := NewEngine(enabledCfg())
	sid := "sess-walk"
	payload := map[string]any{
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "hi",
			},
		},
		"tools": []any{
			map[string]any{
				"function": map[string]any{
					"arguments": `{"phone":"13800138000","note":"x"}`,
				},
			},
		},
	}
	raw, _ := json.Marshal(payload)
	out, err := eng.MaskJSONBody(sid, "gpt", "gpt", "openai", raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "13800138000") {
		t.Fatalf("nested tool args not masked: %s", out)
	}
	if !strings.Contains(string(out), "{{PHONE_") {
		t.Fatalf("expected phone token in nested args: %s", out)
	}
}

func TestDisabledNoop(t *testing.T) {
	cfg := enabledCfg()
	cfg.Enabled = false
	eng := NewEngine(cfg)
	raw := []byte(`{"text":"13800138000"}`)
	out, err := eng.MaskJSONBody("s", "m", "m", "openai", raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(raw) {
		t.Fatalf("disabled engine should no-op, got %s", out)
	}
}

func TestPhoneRejectAllSame(t *testing.T) {
	if validatePhone("11111111111") {
		t.Fatal("all-same digits should reject")
	}
	if !validatePhone("13800138000") {
		t.Fatal("valid phone rejected")
	}
}

func extractToken(s, cat string) string {
	start := strings.Index(s, "{{"+cat+"_")
	if start < 0 {
		return ""
	}
	end := strings.Index(s[start:], "}}")
	if end < 0 {
		return ""
	}
	return s[start : start+end+2]
}
