package usagestore

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreInsertListSummaryAndPricing(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(Options{Path: filepath.Join(dir, "usage.db"), RetentionDays: 30})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	now := time.Now().UnixMilli()
	lat := int64(1500)
	ttft := int64(400)
	events := []Event{
		{
			TimestampMS: now - 1000, Model: "brand-gpt-5.5", Alias: "brand-gpt-5.5", Provider: "openai-compatibility",
			Source: "abc@example.com", SourceHash: HashSecret("abc@example.com"), AuthIndex: "auth-1",
			APIKey: "sk-app-one", APIKeyHash: HashSecret("sk-app-one"),
			InputTokens: 1000, OutputTokens: 500, TotalTokens: 1500, LatencyMS: &lat, TTFTMS: &ttft, Failed: false,
		},
		{
			TimestampMS: now, Model: "brand-gpt-5.5", Provider: "openai-compatibility",
			Source: "abc@example.com", SourceHash: HashSecret("abc@example.com"), AuthIndex: "auth-1",
			APIKey: "sk-app-two", APIKeyHash: HashSecret("sk-app-two"),
			InputTokens: 200, OutputTokens: 50, TotalTokens: 250, Failed: true, FailStatusCode: 429, FailSummary: "rate limited",
		},
	}
	for _, e := range events {
		if err := store.Insert(context.Background(), e); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	// allow async path is not used; Insert is sync
	listed, err := store.ListEvents(context.Background(), QueryFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed = %d, want 2", len(listed))
	}
	if listed[0].Failed != true {
		t.Fatalf("newest event should be failed")
	}
	if listed[0].APIKey != "sk-app-two" {
		t.Fatalf("newest api_key = %q, want sk-app-two", listed[0].APIKey)
	}

	filtered, err := store.ListEvents(context.Background(), QueryFilter{APIKeys: []string{"sk-app-one"}, Limit: 10})
	if err != nil {
		t.Fatalf("ListEvents api_keys: %v", err)
	}
	if len(filtered) != 1 || filtered[0].APIKey != "sk-app-one" {
		t.Fatalf("api_key filter = %#v", filtered)
	}

	opts, err := store.GetFilterOptions(context.Background(), QueryFilter{})
	if err != nil {
		t.Fatalf("GetFilterOptions: %v", err)
	}
	if len(opts.APIKeys) != 2 {
		t.Fatalf("api_keys options = %#v", opts.APIKeys)
	}

	summary, err := store.GetSummary(context.Background(), QueryFilter{})
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	if summary.TotalCalls != 2 || summary.FailureCalls != 1 || summary.SuccessCalls != 1 {
		t.Fatalf("summary = %#v", summary)
	}

	if err := store.UpsertModelPrices(context.Background(), []ModelPrice{{
		Model: "gpt-5.5", PromptPer1M: 1.25, CompletionPer1M: 10,
	}}); err != nil {
		t.Fatalf("UpsertModelPrices: %v", err)
	}
	if err := store.UpsertModelPriceAliases(context.Background(), []ModelPriceAlias{{
		Alias: "brand-gpt-5.5", TargetModel: "gpt-5.5",
	}}); err != nil {
		t.Fatalf("UpsertModelPriceAliases: %v", err)
	}
	prices, err := store.LoadModelPrices(context.Background())
	if err != nil {
		t.Fatalf("LoadModelPrices: %v", err)
	}
	aliases, err := store.LoadModelPriceAliases(context.Background())
	if err != nil {
		t.Fatalf("LoadModelPriceAliases: %v", err)
	}
	amap := AliasMap(aliases)
	total, priced := AttachEventCosts(listed, prices, amap)
	if priced != 2 {
		t.Fatalf("priced = %d, want 2", priced)
	}
	if total <= 0 {
		t.Fatalf("total cost = %v, want > 0", total)
	}
	p, resolved, ok := ResolvePrice([]string{"brand-gpt-5.5"}, prices, amap)
	if !ok || resolved != "gpt-5.5" || p.PromptPer1M != 1.25 {
		t.Fatalf("resolve = ok=%v resolved=%q price=%#v", ok, resolved, p)
	}

	accounts, err := store.GetAccountStats(context.Background(), QueryFilter{}, 10)
	if err != nil {
		t.Fatalf("GetAccountStats: %v", err)
	}
	if len(accounts) == 0 || accounts[0].TotalCalls != 2 {
		t.Fatalf("accounts = %#v", accounts)
	}

	apiKeys, err := store.GetAPIKeyStats(context.Background(), QueryFilter{}, 10)
	if err != nil {
		t.Fatalf("GetAPIKeyStats: %v", err)
	}
	if len(apiKeys) != 2 {
		t.Fatalf("api key stats = %#v, want 2", apiKeys)
	}
	costByKey, err := store.CostByAPIKey(context.Background(), QueryFilter{}, prices, amap)
	if err != nil {
		t.Fatalf("CostByAPIKey: %v", err)
	}
	for i := range apiKeys {
		key := APIKeyGroupKey(apiKeys[i].APIKey, apiKeys[i].APIKeyHash)
		apiKeys[i].EstimatedCost = costByKey[key]
	}
	var sumCost float64
	for _, st := range apiKeys {
		sumCost += st.EstimatedCost
	}
	if sumCost <= 0 {
		t.Fatalf("api key cost sum = %v, want > 0", sumCost)
	}
}

func TestMaskAndHash(t *testing.T) {
	if got := MaskSource("alice@example.com"); got != "alice@example.com" {
		t.Fatalf("source = %q, want full email", got)
	}
	if HashSecret("sk-secret") == "" || HashSecret("sk-secret") == "sk-secret" {
		t.Fatalf("hash should be non-empty and not raw")
	}
}

func TestGetFilterOptionsCascadesAcrossFacets(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(Options{Path: filepath.Join(dir, "usage-cascade.db"), RetentionDays: 30})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	now := time.Now().UnixMilli()
	events := []Event{
		{
			TimestampMS: now - 3000, Model: "gpt-4o", Provider: "openai",
			Source: "alice@example.com", APIKey: "sk-alice",
			InputTokens: 10, OutputTokens: 5, TotalTokens: 15,
		},
		{
			TimestampMS: now - 2000, Model: "claude-sonnet", Provider: "anthropic",
			Source: "bob@example.com", APIKey: "sk-bob",
			InputTokens: 20, OutputTokens: 8, TotalTokens: 28,
		},
		{
			TimestampMS: now - 1000, Model: "gpt-4o-mini", Provider: "openai",
			Source: "alice@example.com", APIKey: "sk-alice",
			InputTokens: 12, OutputTokens: 4, TotalTokens: 16,
		},
	}
	for _, e := range events {
		if err := store.Insert(context.Background(), e); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	// Provider = openai should narrow models/sources/api keys to openai co-occurrence.
	opts, err := store.GetFilterOptions(context.Background(), QueryFilter{Providers: []string{"openai"}})
	if err != nil {
		t.Fatalf("GetFilterOptions provider: %v", err)
	}
	if !stringSliceEqual(opts.Models, []string{"gpt-4o", "gpt-4o-mini"}) {
		t.Fatalf("models under openai = %#v, want gpt-4o + gpt-4o-mini", opts.Models)
	}
	if !stringSliceEqual(opts.Sources, []string{"alice@example.com"}) {
		t.Fatalf("sources under openai = %#v", opts.Sources)
	}
	if !stringSliceEqual(opts.APIKeys, []string{"sk-alice"}) {
		t.Fatalf("api_keys under openai = %#v", opts.APIKeys)
	}
	// Own facet stays switchable: providers list is still full under only time range peers.
	if !stringSliceEqual(opts.Providers, []string{"anthropic", "openai"}) {
		t.Fatalf("providers with provider filter excluded = %#v", opts.Providers)
	}

	// Model = claude-sonnet should collapse everything to the anthropic row.
	opts, err = store.GetFilterOptions(context.Background(), QueryFilter{Models: []string{"claude-sonnet"}})
	if err != nil {
		t.Fatalf("GetFilterOptions model: %v", err)
	}
	if !stringSliceEqual(opts.Providers, []string{"anthropic"}) {
		t.Fatalf("providers under claude-sonnet = %#v", opts.Providers)
	}
	if !stringSliceEqual(opts.APIKeys, []string{"sk-bob"}) {
		t.Fatalf("api_keys under claude-sonnet = %#v", opts.APIKeys)
	}
	// Own facet still lists other models so the user can switch models.
	if !stringSliceEqual(opts.Models, []string{"claude-sonnet", "gpt-4o", "gpt-4o-mini"}) {
		t.Fatalf("models with model filter excluded = %#v", opts.Models)
	}

	// Combined filters cascade further.
	opts, err = store.GetFilterOptions(context.Background(), QueryFilter{
		Providers: []string{"openai"},
		APIKeys:   []string{"sk-alice"},
	})
	if err != nil {
		t.Fatalf("GetFilterOptions combined: %v", err)
	}
	if !stringSliceEqual(opts.Models, []string{"gpt-4o", "gpt-4o-mini"}) {
		t.Fatalf("models under openai+sk-alice = %#v", opts.Models)
	}
	if !stringSliceEqual(opts.Sources, []string{"alice@example.com"}) {
		t.Fatalf("sources under openai+sk-alice = %#v", opts.Sources)
	}
}

func stringSliceEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()))
}
