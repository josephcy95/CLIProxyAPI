package usagestore

import "testing"

func TestFindAutomaticCatalogPriceExact(t *testing.T) {
	catalog := map[string]remoteCatalogPrice{
		"gpt-4o": {
			id:     "gpt-4o",
			source: SyncSourceLiteLLM,
			price:  ModelPrice{Model: "gpt-4o", PromptPer1M: 2.5, CompletionPer1M: 10, Source: SyncSourceLiteLLM},
		},
		"xai/grok-4.5": {
			id:     "xai/grok-4.5",
			source: SyncSourceLiteLLM,
			price:  ModelPrice{Model: "xai/grok-4.5", PromptPer1M: 1, CompletionPer1M: 3, Source: SyncSourceLiteLLM},
		},
	}
	p, reason, ok := findAutomaticCatalogPrice(catalog, "gpt-4o")
	if !ok || reason != "exact" || p.PromptPer1M != 2.5 {
		t.Fatalf("exact match failed: ok=%v reason=%s price=%+v", ok, reason, p)
	}
	p, reason, ok = findAutomaticCatalogPrice(catalog, "grok-4.5")
	if !ok || reason != "provider-prefix" || p.PromptPer1M != 1 {
		t.Fatalf("tail match failed: ok=%v reason=%s price=%+v", ok, reason, p)
	}
}

func TestFindCatalogCandidates(t *testing.T) {
	catalog := map[string]remoteCatalogPrice{
		"openai/gpt-4o-mini": {
			id: "openai/gpt-4o-mini",
			price: ModelPrice{
				Model: "openai/gpt-4o-mini", PromptPer1M: 0.15, CompletionPer1M: 0.6, Source: SyncSourceOpenRouter,
			},
		},
		"gpt-4o": {
			id:    "gpt-4o",
			price: ModelPrice{Model: "gpt-4o", PromptPer1M: 2.5, CompletionPer1M: 10, Source: SyncSourceLiteLLM},
		},
	}
	cands := findCatalogCandidates(catalog, "brand-gpt-4o-mini")
	if len(cands) == 0 {
		t.Fatal("expected candidates for brand-gpt-4o-mini")
	}
}

func TestNormalizeModelList(t *testing.T) {
	got := normalizeModelList([]string{" a ", "a", "", "b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %#v", got)
	}
}
