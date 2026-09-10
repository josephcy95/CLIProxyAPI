package registry

import (
	"encoding/json"
	"testing"
)

func TestLookupCatalogFallbackMatchesExactName(t *testing.T) {
	entry, ok := LookupCatalogFallback("deepseek-v4.1-flash")
	if !ok || entry == nil {
		t.Fatalf("LookupCatalogFallback(deepseek-v4.1-flash) = %+v, %v; want the embedded catalog entry", entry, ok)
	}
	if entry.Context <= 0 {
		t.Fatalf("context = %d, want a positive window", entry.Context)
	}
	if len(entry.Efforts) == 0 {
		t.Fatal("expected reasoning levels from the creator catalog")
	}
}

func TestLookupCatalogFallbackStripsReasoningSuffixes(t *testing.T) {
	// claude-opus-4-6 is a canonical entry; the thinking variant is not.
	base, okBase := LookupCatalogFallback("claude-opus-4-6")
	stripped, okStripped := LookupCatalogFallback("claude-opus-4-6-thinking")
	if !okBase || !okStripped {
		t.Fatalf("lookups failed: base=%v stripped=%v", okBase, okStripped)
	}
	if stripped.Context != base.Context {
		t.Fatalf("stripped context = %d, want %d", stripped.Context, base.Context)
	}
}

func TestLookupCatalogFallbackPrefersExactOverStripped(t *testing.T) {
	exact, ok := LookupCatalogFallback("glm-5.3-flash")
	if !ok {
		t.Fatal("expected glm-5.3-flash in the embedded catalog")
	}
	if exact.Source != "models.dev/zhipuai/glm-5.3-flash" {
		t.Fatalf("source = %q, want the canonical creator entry", exact.Source)
	}
}

func TestLookupCatalogFallbackIgnoresUnknownNames(t *testing.T) {
	if entry, ok := LookupCatalogFallback("totally-unknown-model-xyz"); ok {
		t.Fatalf("LookupCatalogFallback() = %+v, want no match so nothing is guessed", entry)
	}
	if entry, ok := LookupCatalogFallback(""); ok {
		t.Fatalf("LookupCatalogFallback(\"\") = %+v, want no match", entry)
	}
}

func TestLookupCatalogFallbackHandlesThinkingBudgetSuffix(t *testing.T) {
	exact, ok := LookupCatalogFallback("deepseek-v4.1-flash")
	if !ok {
		t.Fatal("expected deepseek-v4.1-flash in the embedded catalog")
	}
	withBudget, okBudget := LookupCatalogFallback("deepseek-v4.1-flash(high)")
	if !okBudget {
		t.Fatal("expected the parenthesized budget suffix to be ignored")
	}
	if withBudget.Context != exact.Context {
		t.Fatalf("context = %d, want %d", withBudget.Context, exact.Context)
	}
}

func TestBuildCatalogFallbackArtifactMergesBothCatalogs(t *testing.T) {
	canonical := []byte(`{
		"acme/acme-one": {"name": "Acme One", "limit": {"context": 128000, "output": 4096}},
		"acme/acme-two": {"name": "Acme Two", "limit": {"context": 64000, "output": 2048}}
	}`)
	providers := []byte(`{
		"acme": {"models": {
			"acme-one": {
				"name": "Acme One",
				"limit": {"context": 128000, "output": 4096},
				"reasoning_options": [{"type": "effort", "values": ["low", "high"]}]
			},
			"acme-two": {"name": "Acme Two", "limit": {"context": 64000, "output": 2048}}
		}},
		"solo": {"models": {
			"solo-model": {
				"name": "Solo Model",
				"limit": {"context": 32000, "output": 1024},
				"reasoning_options": [{"type": "toggle"}]
			}
		}},
		"multi-a": {"models": {"shared-model": {"name": "Shared", "limit": {"context": 1000}}}},
		"multi-b": {"models": {"shared-model": {"name": "Shared", "limit": {"context": 2000}}}}
	}`)

	encoded, err := BuildCatalogFallbackArtifact(canonical, providers, "test")
	if err != nil {
		t.Fatalf("BuildCatalogFallbackArtifact() error = %v", err)
	}
	if err := ValidateCatalogFallbackJSON(encoded); err != nil {
		t.Fatalf("generated artifact is invalid: %v", err)
	}

	var artifact catalogFallbackArtifact
	if err := json.Unmarshal(encoded, &artifact); err != nil {
		t.Fatalf("decode artifact: %v", err)
	}

	one, ok := artifact.Entries["acme-one"]
	if !ok {
		t.Fatal("canonical entry acme-one is missing")
	}
	if one.Context != 128000 || one.Source != "models.dev/acme/acme-one" {
		t.Fatalf("acme-one = %+v, want canonical limits and source", one)
	}
	if len(one.Efforts) != 2 || one.Efforts[0] != "high" || one.Efforts[1] != "low" {
		t.Fatalf("acme-one efforts = %v, want [high low] joined from the creator row", one.Efforts)
	}

	solo, ok := artifact.Entries["solo-model"]
	if !ok {
		t.Fatal("unique-provider entry solo-model is missing")
	}
	if solo.Context != 32000 {
		t.Fatalf("solo-model context = %d, want 32000", solo.Context)
	}
	if len(solo.Efforts) != 1 || solo.Efforts[0] != "none" {
		t.Fatalf("solo-model efforts = %v, want [none] from the toggle option", solo.Efforts)
	}

	if _, ok := artifact.Entries["shared-model"]; ok {
		t.Fatal("a model offered by several providers must be skipped rather than guessed")
	}
}

func TestBuildCatalogFallbackArtifactRejectsEmptyInput(t *testing.T) {
	if _, err := BuildCatalogFallbackArtifact([]byte(`{}`), []byte(`{}`), "test"); err == nil {
		t.Fatal("expected an error for an empty canonical registry")
	}
	if _, err := BuildCatalogFallbackArtifact([]byte(`{"a/b": {"limit": {"context": 1}}}`), []byte(`{}`), "test"); err != nil {
		t.Fatalf("a providers-less catalog should still build: %v", err)
	}
}

func TestValidateCatalogFallbackJSONRejectsEmptyEntries(t *testing.T) {
	if err := ValidateCatalogFallbackJSON([]byte(`{"entries":{}}`)); err == nil {
		t.Fatal("expected an empty catalog to be rejected")
	}
	if err := ValidateCatalogFallbackJSON([]byte(`not json`)); err == nil {
		t.Fatal("expected malformed JSON to be rejected")
	}
}

func TestCatalogFallbackStripCandidatesAreBounded(t *testing.T) {
	candidates := catalogFallbackStripCandidates("gemini-3.8-flash-high-xhigh-max")
	if len(candidates) == 0 || len(candidates) > 4 {
		t.Fatalf("candidates = %v, want a bounded list", candidates)
	}
	if candidates[0] != "gemini-3.8-flash-high-xhigh" {
		t.Fatalf("first candidate = %q, want the name minus its last segment", candidates[0])
	}
	if got := catalogFallbackStripCandidates("deepseek-v4.1-flash"); len(got) != 0 {
		t.Fatalf("candidates = %v, want none when the trailing segment is not a level", got)
	}
}
