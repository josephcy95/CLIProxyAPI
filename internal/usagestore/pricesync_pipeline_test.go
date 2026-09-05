package usagestore

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type priceSyncFixtureTransport func(*http.Request) (*http.Response, error)

func (f priceSyncFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Every request is redirected to a local HTTP server; unknown URLs fail closed.
func priceSyncFixtureClient(t *testing.T, bodies map[string]string) *http.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := bodies[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.Error(w, "fixture outage", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	local, _ := url.Parse(server.URL)
	transport := server.Client().Transport
	return &http.Client{Transport: priceSyncFixtureTransport(func(r *http.Request) (*http.Response, error) {
		source := ""
		switch r.URL.String() {
		case defaultModelsDevURL:
			source = SyncSourceModelsDev
		case defaultLiteLLMURL:
			source = SyncSourceLiteLLM
		case defaultOpenRouterURL:
			source = SyncSourceOpenRouter
		default:
			return nil, fmt.Errorf("unexpected external request: %s", r.URL)
		}
		copy := r.Clone(r.Context())
		copy.URL.Scheme, copy.URL.Host, copy.URL.Path = local.Scheme, local.Host, "/"+source
		return transport.RoundTrip(copy)
	})}
}

func priceSyncFixtureStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(Options{Path: filepath.Join(t.TempDir(), "usage.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

const modelsDevOfficialFixture = `{
 "models":{"openai/gpt-test":{}},
 "providers":{
  "reseller":{"models":{"gpt-test":{"cost":{"input":9,"output":18}}}},
  "openai":{"models":{"gpt-test":{
   "cost":{"input":2.5,"output":10,"cache_read":0,"tiers":[
    {"tier":{"type":"context","size":200000},"input":5,"cache_write":0},
    {"tier":{"type":"context","size":32000},"output":12}
   ]},
   "experimental":{"modes":{"fast":{"cost":{"input":6,"output":24,"cache_read":0}}}}
  }}}
 }
}`

func TestPriceSyncPipelineOfficialUnitsAndSourceFallback(t *testing.T) {
	bodies := map[string]string{
		SyncSourceModelsDev:  modelsDevOfficialFixture,
		SyncSourceLiteLLM:    `{"gpt-test":{"input_cost_per_token":0.000008},"fallback":{"input_cost_per_token":0.000003,"output_cost_per_token":0.000004,"cache_read_input_token_cost":0}}`,
		SyncSourceOpenRouter: `{"data":[{"id":"fallback","pricing":{"prompt":"0.000009"}},{"id":"router-only","pricing":{"prompt":"0.0000015","completion":"0.000006","input_cache_write":"0.000002"}}]}`,
	}
	client := priceSyncFixtureClient(t, bodies)
	catalog, results, sources, _ := fetchPriceCatalogs(context.Background(), client)
	if len(sources) != 3 || len(catalog) != 6 {
		t.Fatalf("catalog=%d sources=%v results=%+v", len(catalog), sources, results)
	}
	for _, tc := range []struct {
		model, source, id string
		prompt            float64
	}{
		{"gpt-test", SyncSourceModelsDev, "openai/gpt-test", 2.5},
		{"OPENAI/GPT-TEST", SyncSourceModelsDev, "openai/gpt-test", 2.5},
		{"reseller/gpt-test", SyncSourceModelsDev, "reseller/gpt-test", 9},
		{"fallback", SyncSourceLiteLLM, "fallback", 3},
		{"router-only", SyncSourceOpenRouter, "router-only", 1.5},
	} {
		p, _, ok := findAutomaticCatalogPrice(catalog, tc.model)
		if !ok || p.Source != tc.source || p.SourceModelID != tc.id || p.PromptPer1M != tc.prompt || !p.PromptConfigured || p.SyncedAtMS == 0 || p.RawJSON == "" {
			t.Fatalf("%s: %+v, matched=%v", tc.model, p, ok)
		}
	}
	p, _, _ := findAutomaticCatalogPrice(catalog, "gpt-test")
	if p.CompletionPer1M != 10 || !p.CacheReadConfigured || p.CacheReadPer1M != 0 || p.CacheCreationConfigured {
		t.Fatalf("base rates: %+v", p)
	}
	if len(p.ContextTiers) != 2 || p.ContextTiers[0].ThresholdTokens != 32000 || p.ContextTiers[1].ThresholdTokens != 200000 || p.ContextTiers[1].PromptPer1M != 5 || !p.ContextTiers[1].CacheCreationConfigured {
		t.Fatalf("context tiers: %+v", p.ContextTiers)
	}
	if len(p.ServiceTiers) != 1 || p.ServiceTiers[0].Mode != "fast" || p.ServiceTiers[0].ServiceTier != "priority" || p.ServiceTiers[0].PromptPer1M != 6 || !p.ServiceTiers[0].CacheReadConfigured {
		t.Fatalf("service tiers: %+v", p.ServiceTiers)
	}
	// Missing official pricing cannot let a reseller claim the bare canonical ID.
	bodies[SyncSourceModelsDev] = `{"models":{"openai/gpt-test":{}},"providers":{"reseller":{"models":{"gpt-test":{"cost":{"input":9}}}}}}`
	catalog, _, _, _ = fetchPriceCatalogs(context.Background(), client)
	p, _, ok := findAutomaticCatalogPrice(catalog, "gpt-test")
	if !ok || p.Source != SyncSourceLiteLLM {
		t.Fatalf("non-official provider won: %+v", p)
	}
}

func TestPriceSyncPipelineRefreshProtectAndOutage(t *testing.T) {
	ctx := context.Background()
	store := priceSyncFixtureStore(t)
	initial := []ModelPrice{
		{Model: "gpt-test", Source: SyncSourceModelsDev, SourceModelID: "openai/gpt-test", PromptPer1M: 99},
		{Model: "manual", Source: "manual", PromptPer1M: 42},
		{Model: "override", Source: "override", PromptPer1M: 43},
		{Model: "legacy-manual", PromptPer1M: 44},
		{Model: "confirmed", Source: SyncSourceModelsDev, SourceModelID: "openai/gpt-test", PromptPer1M: 98},
	}
	if err := store.UpsertModelPrices(ctx, initial); err != nil {
		t.Fatal(err)
	}
	aliases := []ModelPriceAlias{{Alias: "aliased", TargetModel: "gpt-test"}, {Alias: "dangling", TargetModel: "missing"}}
	if err := store.UpsertModelPriceAliases(ctx, aliases); err != nil {
		t.Fatal(err)
	}
	bodies := map[string]string{
		SyncSourceModelsDev: modelsDevOfficialFixture,
		SyncSourceLiteLLM:   `{"gpt-test":{"input_cost_per_token":0.000001},"fallback":{"input_cost_per_token":0.000003},"manual":{"input_cost_per_token":0.000001},"override":{"input_cost_per_token":0.000001},"legacy-manual":{"input_cost_per_token":0.000001},"aliased":{"input_cost_per_token":0.000001},"dangling":{"input_cost_per_token":0.000001}}`,
	}
	client := priceSyncFixtureClient(t, bodies)
	before, _ := store.LoadModelPrices(ctx)
	req := PriceSyncRequest{Models: []string{"gpt-test", "manual", "override", "legacy-manual", "aliased", "dangling", "confirmed"}}
	preview, err := store.syncModelPrices(ctx, req, client)
	if err != nil || len(preview.Matched) != 2 || preview.SkippedManual != 5 || preview.Imported != 0 {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	after, _ := store.LoadModelPrices(ctx)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("preview wrote prices")
	}
	req.ApplyMatched = true
	result, err := store.syncModelPrices(ctx, req, client)
	if err != nil || result.Imported != 2 {
		t.Fatalf("refresh=%+v err=%v", result, err)
	}
	after, err = store.LoadModelPrices(ctx)
	if err != nil || after["gpt-test"].PromptPer1M != 2.5 || after["confirmed"].PromptPer1M != 2.5 || len(after["gpt-test"].ContextTiers) != 2 || len(after["gpt-test"].ServiceTiers) != 1 {
		t.Fatalf("stored=%+v err=%v", after, err)
	}
	// Failed preferred source preserves the entire last-good entry while new models use fallback.
	delete(bodies, SyncSourceModelsDev)
	result, err = store.syncModelPrices(ctx, PriceSyncRequest{Models: []string{"gpt-test", "fallback"}, ApplyMatched: true, OverrideManual: true}, client)
	if err != nil || result.Imported != 1 || !reflect.DeepEqual(result.Preserved, []string{"gpt-test"}) {
		t.Fatalf("outage=%+v err=%v", result, err)
	}
	preserved, _ := store.LoadModelPrices(ctx)
	if !reflect.DeepEqual(after["gpt-test"], preserved["gpt-test"]) {
		t.Fatal("outage changed preferred price")
	}
	// Empty request also refreshes stored sync entries with no usage rows.
	bodies[SyncSourceModelsDev] = strings.ReplaceAll(modelsDevOfficialFixture, `"input":2.5`, `"input":3.5`)
	result, err = store.syncModelPrices(ctx, PriceSyncRequest{ApplyMatched: true}, client)
	if err != nil || result.Imported != 2 || result.Unchanged != 1 {
		t.Fatalf("recovery=%+v err=%v", result, err)
	}
	recovered, _ := store.LoadModelPrices(ctx)
	if recovered["gpt-test"].PromptPer1M != 3.5 {
		t.Fatalf("recovery price=%+v", recovered["gpt-test"])
	}
	req.Models, req.OverrideManual = []string{"manual", "aliased", "dangling"}, true
	result, err = store.syncModelPrices(ctx, req, client)
	if err != nil || result.Imported != 3 {
		t.Fatalf("override=%+v err=%v", result, err)
	}
	aliasesAfter, _ := store.LoadModelPriceAliases(ctx)
	if !reflect.DeepEqual(AliasMap(aliases), AliasMap(aliasesAfter)) {
		t.Fatal("sync rewrote aliases")
	}
}

func TestPriceSyncPipelineFailuresAndFuzzyConfirmation(t *testing.T) {
	ctx := context.Background()
	store := priceSyncFixtureStore(t)
	if err := store.UpsertModelPrices(ctx, []ModelPrice{{Model: "saved", Source: "manual", PromptPer1M: 42}}); err != nil {
		t.Fatal(err)
	}
	bodies := map[string]string{SyncSourceModelsDev: `{}`, SyncSourceLiteLLM: `not-json`, SyncSourceOpenRouter: `{"data":[]}`}
	client := priceSyncFixtureClient(t, bodies)
	before, _ := store.LoadModelPrices(ctx)
	result, err := store.syncModelPrices(ctx, PriceSyncRequest{Models: []string{"gpt-test"}, ApplyMatched: true}, client)
	if err == nil || len(result.SourceResults) != 3 {
		t.Fatalf("all failures: result=%+v err=%v", result, err)
	}
	after, _ := store.LoadModelPrices(ctx)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed sync wrote prices")
	}
	bodies[SyncSourceModelsDev] = modelsDevOfficialFixture
	bodies[SyncSourceLiteLLM] = `{"openai/gpt-test":{"input_cost_per_token":0.000008}}`
	bodies[SyncSourceOpenRouter] = `{"data":[{"id":"openai/gpt-test","pricing":{"prompt":"0.000009"}}]}`
	result, err = store.syncModelPrices(ctx, PriceSyncRequest{Models: []string{"brand-gpt-test", "gpt-test-custom"}, ApplyMatched: true}, client)
	if err != nil || result.Imported != 0 || len(result.Candidates) != 2 {
		t.Fatalf("fuzzy=%+v err=%v", result, err)
	}
	for _, set := range result.Candidates {
		sources := map[string]bool{}
		for _, c := range set.Candidates {
			sources[c.Price.Source] = true
		}
		if len(sources) != 3 {
			t.Fatalf("lost source candidates: %+v", set)
		}
	}
	after, _ = store.LoadModelPrices(ctx)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("fuzzy suggestions wrote prices")
	}
}

func TestPriceSyncPipelineRejectUnsafeRatesAndTiers(t *testing.T) {
	for _, size := range []string{"0", "-1", "1.5", "9223372036854775807", `"NaN"`, `"32000junk"`} {
		t.Run(size, func(t *testing.T) {
			body := `{"test":{"models":{"model":{"cost":{"input":2.5,"tiers":[{"input":5,"tier":{"type":"context","size":` + size + `}}]}}}}}`
			catalog, _, err := fetchModelsDevPrices(context.Background(), defaultModelsDevURL, priceSyncFixtureClient(t, map[string]string{SyncSourceModelsDev: body}))
			if err == nil || len(catalog) != 0 {
				t.Fatalf("unsafe threshold activated: %+v err=%v", catalog, err)
			}
		})
	}
	body := `{"test":{"models":{
  "duplicate":{"cost":{"input":1,"tiers":[{"input":2,"tier":{"type":"context","size":32}},{"input":3,"tier":{"type":"context","size":32}}]}},
  "negative":{"cost":{"input":-1,"output":2}},
  "malformed":{"cost":{"input":"2junk"}},
  "infinite":{"cost":{"input":"Inf"}},
  "zero":{"cost":{"input":0,"output":0}}
 }}}`
	catalog, skipped, err := fetchModelsDevPrices(context.Background(), defaultModelsDevURL, priceSyncFixtureClient(t, map[string]string{SyncSourceModelsDev: body}))
	if err != nil || skipped != 4 || len(catalog) != 1 || len(catalog["test/duplicate"].price.ContextTiers) != 0 || !catalog["test/zero"].price.PromptConfigured {
		t.Fatalf("catalog=%+v skipped=%d err=%v", catalog, skipped, err)
	}
}
