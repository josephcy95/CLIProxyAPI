package usagestore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestPriceSyncCanonicalMatchingAndAmbiguity(t *testing.T) {
	makeEntry := func(id string) remoteCatalogPrice {
		return remoteCatalogPrice{id: id, source: SyncSourceLiteLLM, price: ModelPrice{Source: SyncSourceLiteLLM, SourceModelID: id}}
	}
	catalog := map[string]remoteCatalogPrice{"one": makeEntry("openai/gpt-test")}
	if _, _, ok := findAutomaticCatalogPrice(catalog, "gpt_test"); !ok {
		t.Fatal("unambiguous normalized tail not matched")
	}
	if _, _, ok := findAutomaticCatalogPrice(catalog, "openaigpttest"); !ok {
		t.Fatal("unambiguous normalized full ID not matched")
	}
	catalog["two"] = makeEntry("other/gpt.test")
	if _, _, ok := findAutomaticCatalogPrice(catalog, "gpt_test"); ok {
		t.Fatal("ambiguous normalized tail auto-applied")
	}
	catalog["fallback"] = remoteCatalogPrice{id: "gpt_test", source: SyncSourceOpenRouter, price: ModelPrice{Source: SyncSourceOpenRouter}}
	if price, _, ok := findAutomaticCatalogPrice(catalog, "gpt_test"); !ok || price.Source != SyncSourceOpenRouter {
		t.Fatal("unambiguous fallback was blocked")
	}
	body := `{"models":{"a/shared":{},"b/shared":{},"c/shared":{}},"providers":{"a":{"models":{"shared":{"cost":{"input":1}}}},"b":{"models":{"shared":{"cost":{"input":2}}}},"c":{"models":{"shared":{"cost":{"input":3}}}}}}`
	for i := 0; i < 30; i++ {
		catalog, _, err := parseModelsDevPrices([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, ok := findAutomaticCatalogPrice(catalog, "shared"); ok {
			t.Fatal("ambiguous canonical tail became official")
		}
		if p, _, ok := findAutomaticCatalogPrice(catalog, "b/shared"); !ok || p.PromptPer1M != 2 {
			t.Fatal("explicit provider identity not matched")
		}
	}
}

func TestPriceSyncScopedRestoreAndUnchanged(t *testing.T) {
	ctx := context.Background()
	store := priceSyncFixtureStore(t)
	if err := store.UpsertModelPrices(ctx, []ModelPrice{{Model: "gpt-test", Source: "manual", PromptPer1M: 99}, {Model: "other", Source: "manual", PromptPer1M: 42}}); err != nil {
		t.Fatal(err)
	}
	client := priceSyncFixtureClient(t, map[string]string{SyncSourceModelsDev: modelsDevOfficialFixture})
	req := PriceSyncRequest{Models: []string{"gpt-test"}, ApplyMatched: true}
	result, err := store.syncModelPrices(ctx, req, client)
	if err != nil || result.Outcomes[0].Status != "protected" {
		t.Fatalf("protected=%+v err=%v", result, err)
	}
	req.OverrideManual = true
	result, err = store.syncModelPrices(ctx, req, client)
	if err != nil || result.Imported != 1 || result.Outcomes[0].Status != "updated" || !result.Outcomes[0].HasRules {
		t.Fatalf("restore=%+v err=%v", result, err)
	}
	before, _ := store.LoadModelPrices(ctx)
	result, err = store.syncModelPrices(ctx, req, client)
	after, _ := store.LoadModelPrices(ctx)
	if err != nil || result.Imported != 0 || result.Unchanged != 1 || result.Outcomes[0].Status != "unchanged" || !reflect.DeepEqual(before, after) {
		t.Fatalf("repeat=%+v err=%v", result, err)
	}
	if after["other"].Source != "manual" || after["other"].PromptPer1M != 42 {
		t.Fatal("scoped sync changed another model")
	}
	if len(after["gpt-test"].ContextTiers) != 2 || len(after["gpt-test"].ServiceTiers) != 1 {
		t.Fatal("restore lost complete rules")
	}
}

func TestPriceSyncSavedIdentityAndWholeRecordProtection(t *testing.T) {
	ctx := context.Background()
	store := priceSyncFixtureStore(t)
	saved := ModelPrice{Model: "gpt-test", Source: SyncSourceLiteLLM, SourceModelID: "confirmed-id", PromptPer1M: 8, PromptConfigured: true, ContextTiers: []ModelPriceContextTier{{ThresholdTokens: 100, PromptPer1M: 16, PromptConfigured: true}}}
	if err := store.UpsertModelPrices(ctx, []ModelPrice{saved, {Model: "gpt_test", Source: "manual", PromptPer1M: 99}}); err != nil {
		t.Fatal(err)
	}
	bodies := map[string]string{SyncSourceModelsDev: modelsDevOfficialFixture, SyncSourceLiteLLM: `{"confirmed-id":{"input_cost_per_token":0.000004}}`}
	client := priceSyncFixtureClient(t, bodies)
	result, err := store.syncModelPrices(ctx, PriceSyncRequest{Models: []string{"gpt-test"}, ApplyMatched: true}, client)
	if err != nil || result.Imported != 1 {
		t.Fatalf("sync=%+v %v", result, err)
	}
	prices, _ := store.LoadModelPrices(ctx)
	p := prices["gpt-test"]
	if p.Source != SyncSourceLiteLLM || p.SourceModelID != "confirmed-id" || p.PromptPer1M != 4 || p.CompletionConfigured || len(p.ContextTiers) != 0 || len(p.ServiceTiers) != 0 {
		t.Fatalf("mixed or redirected record: %+v", p)
	}
	bodies[SyncSourceLiteLLM] = `{"another-id":{"input_cost_per_token":0.000002}}`
	result, err = store.syncModelPrices(ctx, PriceSyncRequest{Models: []string{"gpt-test"}, ApplyMatched: true, OverrideManual: true}, client)
	after, _ := store.LoadModelPrices(ctx)
	if err != nil || result.Outcomes[0].Status != "protected" || !reflect.DeepEqual(prices, after) {
		t.Fatalf("missing identity=%+v %v", result, err)
	}
	bodies[SyncSourceModelsDev] = `{"a":{"models":{"gpt-test":{"cost":{"input":1}}}},"b":{"models":{"gpt.test":{"cost":{"input":2}}}}}`
	result, err = store.syncModelPrices(ctx, PriceSyncRequest{Models: []string{"gpt_test"}, ApplyMatched: true, OverrideManual: true}, client)
	after, _ = store.LoadModelPrices(ctx)
	if err != nil || result.Outcomes[0].Status != "needs_matching" || result.Imported != 0 || !reflect.DeepEqual(prices, after) {
		t.Fatalf("ambiguous restore=%+v %v", result, err)
	}
}

func TestModelsDevConditionalCacheKeepsValidatedEntity(t *testing.T) {
	ctx := context.Background()
	step := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		switch step {
		case 1:
			if r.Header.Get("If-None-Match") != "" {
				t.Error("new URL sent ETag")
			}
			w.Header().Set("ETag", `"v1"`)
			fmt.Fprint(w, modelsDevOfficialFixture)
		case 2, 4:
			if r.Header.Get("If-None-Match") != `"v1"` {
				t.Errorf("missing validated ETag: %s", r.Header.Get("If-None-Match"))
			}
			w.WriteHeader(http.StatusNotModified)
		case 3:
			w.Header().Set("ETag", `"bad"`)
			fmt.Fprint(w, `{"invalid":`)
		case 5:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 6:
			w.Header().Set("ETag", `"v2"`)
			fmt.Fprint(w, `{"test":{"models":{"new":{"cost":{"input":7}}}}}`)
		}
	}))
	defer server.Close()
	first, _, err := fetchModelsDevPrices(ctx, server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := fetchModelsDevPrices(ctx, server.URL, server.Client())
	if err != nil || !sameSyncedPrice(first["openai/gpt-test"].price, second["openai/gpt-test"].price) {
		t.Fatal("304 did not reuse whole record")
	}
	if _, _, err = fetchModelsDevPrices(ctx, server.URL, server.Client()); err == nil {
		t.Fatal("invalid replacement accepted")
	}
	if _, _, err = fetchModelsDevPrices(ctx, server.URL, server.Client()); err != nil {
		t.Fatal("failed response poisoned cache", err)
	}
	if _, _, err = fetchModelsDevPrices(ctx, server.URL, server.Client()); err == nil {
		t.Fatal("outage disguised by stale cache")
	}
	recovered, _, err := fetchModelsDevPrices(ctx, server.URL, server.Client())
	if err != nil || recovered["test/new"].price.PromptPer1M != 7 {
		t.Fatal("new entity not adopted")
	}
	orphan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotModified) }))
	defer orphan.Close()
	if _, _, err = fetchModelsDevPrices(ctx, orphan.URL, orphan.Client()); err == nil {
		t.Fatal("304 without cache accepted")
	}
}

func TestPriceSyncNoSupportedRulesAndFailuresPreserveManual(t *testing.T) {
	ctx := context.Background()
	store := priceSyncFixtureStore(t)
	if err := store.UpsertModelPrices(ctx, []ModelPrice{{Model: "custom", Source: "manual", PromptPer1M: 99}}); err != nil {
		t.Fatal(err)
	}
	before, _ := store.LoadModelPrices(ctx)
	bodies := map[string]string{
		SyncSourceModelsDev: `{"test":{"models":{"custom":{"cost":{"input":1,"tiers":[{"tier":{"type":"context"
 "encoding/json"
 "os"
 "strings","size":-1},"input":3}]}},"other":{"cost":{"input":2}}}}}`,
		SyncSourceLiteLLM:    `{"other":{"input_cost_per_token":0.000002}}`,
		SyncSourceOpenRouter: `{"data":[{"id":"other","pricing":{"prompt":"0.000002"}}]}`,
	}
	client := priceSyncFixtureClient(t, bodies)
	req := PriceSyncRequest{Models: []string{"custom"}, OverrideManual: true, ApplyMatched: true}
	result, err := store.syncModelPrices(ctx, req, client)
	after, _ := store.LoadModelPrices(ctx)
	if err != nil || result.Outcomes[0].Status != "no_supported_rules" || result.Imported != 0 || !reflect.DeepEqual(before, after) {
		t.Fatalf("unsupported=%+v %v", result, err)
	}
	for k := range bodies {
		delete(bodies, k)
	}
	result, err = store.syncModelPrices(ctx, req, client)
	after, _ = store.LoadModelPrices(ctx)
	if err == nil || len(result.Outcomes) != 1 || result.Outcomes[0].Status != "source_failure" || !reflect.DeepEqual(before, after) {
		t.Fatalf("failure=%+v %v", result, err)
	}
}

func TestPriceSyncPreservesEditMadeDuringFetch(t *testing.T) {
	ctx := context.Background()
	store := priceSyncFixtureStore(t)
	client := priceSyncFixtureClient(t, map[string]string{SyncSourceModelsDev: modelsDevOfficialFixture})
	transport := client.Transport
	edited := false
	client.Transport = priceSyncFixtureTransport(func(r *http.Request) (*http.Response, error) {
		if !edited {
			edited = true
			if err := store.UpsertModelPrices(ctx, []ModelPrice{{Model: "gpt-test", Source: "manual", PromptPer1M: 99}}); err != nil {
				return nil, err
			}
		}
		return transport.RoundTrip(r)
	})
	result, err := store.syncModelPrices(ctx, PriceSyncRequest{Models: []string{"gpt-test"}, ApplyMatched: true}, client)
	prices, _ := store.LoadModelPrices(ctx)
	if err != nil || result.Outcomes[0].Status != "protected" || prices["gpt-test"].PromptPer1M != 99 {
		t.Fatalf("concurrent edit=%+v %v", result, err)
	}
}

func TestModelsDevLegacyOfficialAuthority(t *testing.T) {
	body := `{"openai":{"models":{"gpt-test":{"cost":{"input":2,"tiers":[{"tier":{"type":"context","size":100},"input":4}]}}}},"reseller":{"models":{"gpt-test":{"cost":{"input":9}},"gpt-next":{"cost":{"input":8}}}}}`
	catalog, _, err := parseModelsDevPrices([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	price, reason, ok := findAutomaticCatalogPrice(catalog, "gpt-test")
	if !ok || reason != "models.dev-official" || price.SourceModelID != "openai/gpt-test" || len(price.ContextTiers) != 1 {
		t.Fatalf("official=%+v reason=%s matched=%v", price, reason, ok)
	}
	if _, _, ok = findAutomaticCatalogPrice(catalog, "gpt-next"); ok {
		t.Fatal("reseller claimed missing official identity")
	}
	if price, _, ok = findAutomaticCatalogPrice(catalog, "reseller/gpt-next"); !ok || price.PromptPer1M != 8 {
		t.Fatal("explicit reseller identity unavailable")
	}
}

func TestModelsDevFetchedCatalogReplay(t *testing.T) {
	path := os.Getenv("CPA_MODELSDEV_REPLAY")
	if path == "" {
		t.Skip("set CPA_MODELSDEV_REPLAY to the decoded locally fetched API JSON")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var providers map[string]struct {
		Models map[string]json.RawMessage `json:"models"`
	}
	if err = json.Unmarshal(body, &providers); err != nil {
		t.Fatal(err)
	}
	rawModels := 0
	for providerID, provider := range providers {
		rawModels += len(provider.Models)
		for id := range provider.Models {
			if strings.Contains(strings.ToLower(id), "gpt-6-astra") {
				t.Fatalf("fixture now contains %s/%s; reevaluate absence reproduction", providerID, id)
			}
		}
	}
	client := priceSyncFixtureClient(t, map[string]string{SyncSourceModelsDev: string(body)})
	catalog, skipped, err := fetchModelsDevPrices(context.Background(), defaultModelsDevURL, client)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := findAutomaticCatalogPrice(catalog, "gpt-6-astra"); ok {
		t.Fatal("absent model automatically priced")
	}
	for _, id := range []string{"gpt-5.6", "gpt-5.6-sol", "gpt-5.4"} {
		price, reason, ok := findAutomaticCatalogPrice(catalog, id)
		if !ok || price.SourceModelID != "openai/"+id {
			t.Fatalf("official %s missing: %+v %s %v", id, price, reason, ok)
		}
		t.Logf("%s: source=%s id=%s context_rules=%d service_rules=%d", id, price.Source, price.SourceModelID, len(price.ContextTiers), len(price.ServiceTiers))
	}
	store := priceSyncFixtureStore(t)
	result, err := store.syncModelPrices(context.Background(), PriceSyncRequest{Models: []string{"gpt-6-astra"}, ApplyMatched: true}, client)
	if err != nil || result.Imported != 0 || len(result.Outcomes) != 1 || result.Outcomes[0].Status != "needs_matching" {
		t.Fatalf("absent-model sync=%+v err=%v", result, err)
	}
	t.Logf("providers=%d raw_models=%d usable_records=%d skipped=%d astra_outcome=%s candidates=%d", len(providers), rawModels, len(catalog), skipped, result.Outcomes[0].Status, len(result.Candidates[0].Candidates))
}

func TestPriceSyncAutomaticFallbackUpgradesButCustomMappingStays(t *testing.T) {
	for _, sourceAvailable := range []bool{true, false} {
		t.Run(fmt.Sprint(sourceAvailable), func(t *testing.T) {
			ctx := context.Background()
			store := priceSyncFixtureStore(t)
			if err := store.UpsertModelPrices(ctx, []ModelPrice{
				{Model: "gpt-test", Source: SyncSourceLiteLLM, SourceModelID: "openai/gpt-test", PromptPer1M: 8, PromptConfigured: true},
				{Model: "my-confirmed", Source: SyncSourceLiteLLM, SourceModelID: "openai/gpt-test", PromptPer1M: 8, PromptConfigured: true},
			}); err != nil {
				t.Fatal(err)
			}
			bodies := map[string]string{SyncSourceModelsDev: modelsDevOfficialFixture}
			if sourceAvailable {
				bodies[SyncSourceLiteLLM] = `{"openai/gpt-test":{"input_cost_per_token":0.000004}}`
			}
			result, err := store.syncModelPrices(ctx, PriceSyncRequest{Models: []string{"gpt-test", "my-confirmed"}, ApplyMatched: true}, priceSyncFixtureClient(t, bodies))
			if err != nil {
				t.Fatal(err)
			}
			prices, _ := store.LoadModelPrices(ctx)
			p := prices["gpt-test"]
			if p.Source != SyncSourceModelsDev || p.PromptPer1M != 2.5 || len(p.ContextTiers) != 2 || len(p.ServiceTiers) != 1 {
				t.Fatalf("automatic fallback not upgraded as a whole: %+v result=%+v", p, result)
			}
			confirmed := prices["my-confirmed"]
			want := 8.0
			if sourceAvailable {
				want = 4
			}
			if confirmed.Source != SyncSourceLiteLLM || confirmed.PromptPer1M != want || len(confirmed.ContextTiers) != 0 {
				t.Fatalf("confirmed mapping redirected: %+v", confirmed)
			}
		})
	}
}
