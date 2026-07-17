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
}

func TestMaskAndHash(t *testing.T) {
	if got := MaskSource("alice@example.com"); got != "alice@example.com" {
		t.Fatalf("source = %q, want full email", got)
	}
	if HashSecret("sk-secret") == "" || HashSecret("sk-secret") == "sk-secret" {
		t.Fatalf("hash should be non-empty and not raw")
	}
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
