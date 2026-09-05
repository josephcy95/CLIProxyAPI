package usagestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPricingRulesBoundariesAndZero(t *testing.T) {
	p := ModelPrice{Model: "m", PromptPer1M: 1, CompletionPer1M: 2, CachePer1M: 3, CacheReadPer1M: 4,
		ContextTiers: []ModelPriceContextTier{
			{ThresholdTokens: 200, CompletionPer1M: 8, CompletionConfigured: true, PromptConfigured: true, CacheReadConfigured: true, CacheCreationConfigured: true},
			{ThresholdTokens: 100, PromptPer1M: 5, PromptConfigured: true},
		},
		ServiceTiers: []ModelPriceServiceTier{{Mode: " Fast ", ServiceTier: " PRIORITY ", PromptPer1M: 9, PromptConfigured: true}},
	}
	if err := ValidateModelPrices([]ModelPrice{p}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		input                    int64
		tier                     string
		prompt, completion, read float64
	}{
		{100, "", 1, 2, 4}, {101, "", 5, 2, 4}, {200, "", 5, 2, 4}, {201, "priority", 0, 8, 0},
		{100, " FaSt ", 9, 2, 4}, {100, "priority", 9, 2, 4}, {101, "priority", 5, 2, 4}, {100, "default", 1, 2, 4},
	} {
		got := ResolveUsagePrice(p, tc.input, tc.tier)
		if got.PromptPer1M != tc.prompt || got.CompletionPer1M != tc.completion || got.CacheReadPer1M != tc.read {
			t.Fatalf("input=%d tier=%q: %+v", tc.input, tc.tier, got)
		}
	}
	zero := ResolveUsagePrice(p, 201, "priority")
	if got := EstimateCost(zero, 0, 0, 0, 100, 100, 0); got != 0 {
		t.Fatalf("explicit zero cache rates used fallback: %v", got)
	}
	p.CacheConfigured = true
	p.CachePer1M = 0
	if got := EstimateCost(p, 0, 0, 0, 0, 0, 100); got != 0 {
		t.Fatalf("explicit zero legacy cached rate used fallback: %v", got)
	}
	p.CacheReadConfigured = true
	p.CacheReadPer1M = 0
	p.CacheCreationConfigured = true
	if got := EstimateCost(p, 0, 0, 0, 100, 100, 0); got != 0 {
		t.Fatalf("explicit zero base cache rate used fallback: %v", got)
	}
}

func TestPricingRulesMigrationAtomicWrites(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "usage.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE model_prices (model TEXT PRIMARY KEY,prompt_per_1m REAL NOT NULL DEFAULT 0,completion_per_1m REAL NOT NULL DEFAULT 0,cache_per_1m REAL NOT NULL DEFAULT 0,cache_read_per_1m REAL NOT NULL DEFAULT 0,cache_creation_per_1m REAL NOT NULL DEFAULT 0,source TEXT,updated_at_ms INTEGER NOT NULL);
		INSERT INTO model_prices(model,prompt_per_1m,source,updated_at_ms) VALUES('old',1,'manual',123)`)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	book, err := s.LoadModelPrices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(book) != 1 || book["old"].PromptPer1M != 1 || book["old"].PromptConfigured {
		t.Fatalf("legacy book changed: %+v", book)
	}
	p := ModelPrice{Model: "m", PromptPer1M: 2, Source: "models.dev", SourceModelID: "openai/m", RawJSON: `{"cost":{"input":2}}`, SyncedAtMS: 456, CacheReadConfigured: true,
		ContextTiers: []ModelPriceContextTier{{ThresholdTokens: 200, PromptConfigured: true}, {ThresholdTokens: 100, PromptPer1M: 3, PromptConfigured: true}},
		ServiceTiers: []ModelPriceServiceTier{{Mode: " FAST ", ServiceTier: " Priority ", CacheCreationConfigured: true}},
	}
	if err = s.UpsertModelPrices(ctx, []ModelPrice{p}); err != nil {
		t.Fatal(err)
	}
	book, err = s.LoadModelPrices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := book["m"]
	if got.RawJSON != p.RawJSON || got.SourceModelID != p.SourceModelID || got.SyncedAtMS != 456 || !got.CacheReadConfigured || got.ContextTiers[0].ThresholdTokens != 100 || got.ServiceTiers[0].Mode != "fast" {
		t.Fatalf("roundtrip: %+v", got)
	}
	wire, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip ModelPrice
	if err = json.Unmarshal(wire, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, roundtrip) {
		t.Fatal("wire roundtrip lost rules/flags")
	}
	var obj map[string]json.RawMessage
	if err = json.Unmarshal(wire, &obj); err != nil {
		t.Fatal(err)
	}
	var tiers []map[string]json.RawMessage
	if err = json.Unmarshal(obj["context_tiers"], &tiers); err != nil {
		t.Fatal(err)
	}
	if string(tiers[0]["prompt_per_1m"]) != "3" || string(tiers[0]["prompt_configured"]) != "true" {
		t.Fatalf("not snake case: %s", wire)
	}
	// Force a real mid-write database failure, proving DELETE and partial inserts
	// roll back together, not just that validation happens before DELETE.
	if _, err = s.db.Exec(`CREATE TRIGGER reject_price BEFORE INSERT ON model_prices WHEN NEW.model='reject' BEGIN SELECT RAISE(ABORT,'test'); END`); err != nil {
		t.Fatal(err)
	}
	for _, replace := range []bool{false, true} {
		if err = s.writeModelPrices(ctx, []ModelPrice{{Model: "new", PromptPer1M: 4}, {Model: "reject"}}, replace); err == nil {
			t.Fatal("expected database error")
		}
		after, err := s.LoadModelPrices(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(book, after) {
			t.Fatalf("failed write changed book replace=%v", replace)
		}
		if err = s.writeModelPrices(ctx, []ModelPrice{{Model: "bad", ContextTiers: []ModelPriceContextTier{{ThresholdTokens: 1, PromptPer1M: -1, PromptConfigured: true}}}}, replace); err == nil {
			t.Fatal("expected validation error")
		}
		after, err = s.LoadModelPrices(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(book, after) {
			t.Fatal("invalid write changed book")
		}
	}
	if err = s.ReplaceModelPrices(ctx, []ModelPrice{{Model: "manual", PromptPer1M: 1}}); err != nil {
		t.Fatal(err)
	}
	book, err = s.LoadModelPrices(ctx)
	if err != nil || len(book) != 1 || book["manual"].Source != "manual" {
		t.Fatalf("replace: %+v %v", book, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
}

func TestPricingRulesValidation(t *testing.T) {
	for _, p := range []ModelPrice{
		{Model: ""}, {Model: "m", PromptPer1M: math.NaN()}, {Model: "m", CacheReadPer1M: math.Inf(1)}, {Model: "m", RawJSON: "bad"},
		{Model: "m", ContextTiers: []ModelPriceContextTier{{ThresholdTokens: 0, PromptConfigured: true}}},
		{Model: "m", ContextTiers: []ModelPriceContextTier{{ThresholdTokens: 1}}},
		{Model: "m", ContextTiers: []ModelPriceContextTier{{ThresholdTokens: 1, PromptConfigured: true}, {ThresholdTokens: 1, PromptConfigured: true}}},
		{Model: "m", ContextTiers: []ModelPriceContextTier{{ThresholdTokens: 1, CacheCreationPer1M: -1, CacheCreationConfigured: true}}},
		{Model: "m", ServiceTiers: []ModelPriceServiceTier{{Mode: "fast", ServiceTier: "priority", CompletionPer1M: math.Inf(1), CompletionConfigured: true}}},
		{Model: "m", ServiceTiers: []ModelPriceServiceTier{{Mode: "", ServiceTier: "priority", PromptConfigured: true}}},
		{Model: "m", ServiceTiers: []ModelPriceServiceTier{{Mode: "fast", ServiceTier: "priority", PromptConfigured: true}, {Mode: "PRIORITY", ServiceTier: "fast", PromptConfigured: true}}},
	} {
		if err := ValidateModelPrices([]ModelPrice{p}); err == nil {
			t.Fatalf("accepted invalid price: %+v", p)
		}
	}
	if err := ValidateModelPrices([]ModelPrice{{Model: "m"}, {Model: " m "}}); err == nil {
		t.Fatal("accepted duplicate model")
	}
}

func TestPricingRulesAggregateParity(t *testing.T) {
	ctx := context.Background()
	s, err := Open(Options{Path: filepath.Join(t.TempDir(), "usage.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	prices := map[string]ModelPrice{"m": {Model: "m", PromptPer1M: 1, CompletionPer1M: 2, CachePer1M: 0.5, CacheReadPer1M: 0.25,
		ContextTiers: []ModelPriceContextTier{{ThresholdTokens: 100, PromptPer1M: 3, PromptConfigured: true}, {ThresholdTokens: 200, PromptConfigured: true, CompletionPer1M: 8, CompletionConfigured: true}},
		ServiceTiers: []ModelPriceServiceTier{{Mode: "fast", ServiceTier: "priority", PromptPer1M: 4, PromptConfigured: true, CacheCreationConfigured: true}},
	}, "other": {Model: "other", PromptPer1M: 7, ContextTiers: []ModelPriceContextTier{{ThresholdTokens: 150, PromptPer1M: 9, PromptConfigured: true}}}}
	aliases := map[string]string{"brand": "m"}
	events := []Event{
		{Model: "m", Provider: "codex", InputTokens: 100, OutputTokens: 10},
		{Model: "m", Provider: "codex", InputTokens: 101, CacheReadTokens: 80, OutputTokens: 10}, // context cannot be billable 21
		{Model: "m", Provider: "codex", InputTokens: 200, CacheReadTokens: 80, ReasoningTokens: 4},
		{Model: "m", Provider: "codex", InputTokens: 201, OutputTokens: 10, ServiceTier: "priority"},
		{Model: "brand", Provider: "codex", InputTokens: 100, ServiceTier: " FaSt "},
		{Model: "m", Provider: "codex", InputTokens: 100, ServiceTier: "priority", ResponseServiceTier: "default"},
		{Model: "m", Provider: "codex", InputTokens: 100, ResponseServiceTier: " priority ", CacheCreationTokens: 20},
		{Model: "m", Provider: "claude", InputTokens: 20, CacheReadTokens: 70, CacheCreationTokens: 11},                                               // context 101
		{Model: "m", Provider: "openai-compatibility", ExecutorType: "ClaudeExecutor", InputTokens: 20, CacheReadTokens: 70, CacheCreationTokens: 11}, // compat wins
		{Model: "m", Provider: "gemini", InputTokens: 100, CacheReadTokens: 30, CacheCreationTokens: 20},
		{Model: "m", Provider: "codex", InputTokens: 10, CachedTokens: 30}, // residual and explicit cache share one band
		{Model: "m", Provider: "codex", InputTokens: 10, CacheReadTokens: 5},
		{Model: "M", Provider: "codex", InputTokens: 199},
		{Model: "missing", Alias: "brand", Provider: "codex", InputTokens: 101},
		{Model: "other", Provider: "codex", InputTokens: 151},
		{Model: "unpriced", Provider: "codex", InputTokens: 100},
	}
	for i := range events {
		events[i].AuthIndex = "account"
		events[i].APIKey = "key"
		if err = s.Insert(ctx, events[i]); err != nil {
			t.Fatal(err)
		}
	}
	want, count := AttachEventCosts(events, prices, aliases)
	if count != int64(len(events)-1) {
		t.Fatalf("priced=%d", count)
	}
	if math.Abs(*events[1].EstimatedCost-(21*3.0+80*0.25+10*2)/1e6) > 1e-12 {
		t.Fatal("context was reduced by cache")
	}
	if math.Abs(*events[7].EstimatedCost-(20*3.0+70*0.25+11*3*1.25)/1e6) > 1e-12 {
		t.Fatal("Anthropic context excluded cache")
	}
	got, gotCount, err := s.SumCost(ctx, QueryFilter{Limit: 1, BeforeID: 1}, prices, aliases)
	if err != nil {
		t.Fatal(err)
	}
	if gotCount != count || math.Abs(got-want) > 1e-12 {
		t.Fatalf("total=%v count=%d want=%v count=%d", got, gotCount, want, count)
	}
	accounts, err := s.CostByAccount(ctx, QueryFilter{}, prices, aliases)
	if err != nil {
		t.Fatal(err)
	}
	wantAccounts := map[string]float64{}
	for _, e := range events {
		if e.EstimatedCost != nil {
			wantAccounts[AccountKey(e.AuthIndex, e.Source, e.SourceHash, e.Provider)] += *e.EstimatedCost
		}
	}
	for key, cost := range wantAccounts {
		if math.Abs(accounts[key]-cost) > 1e-12 {
			t.Fatalf("account %q=%v want=%v", key, accounts[key], cost)
		}
	}
	keys, err := s.CostByAPIKey(ctx, QueryFilter{}, prices, aliases)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(keys[APIKeyGroupKey("key", "")]-want) > 1e-12 {
		t.Fatalf("key=%v want=%v", keys, want)
	}
	for _, e := range events {
		var sqlInput int64
		if err = s.readDB.QueryRowContext(ctx, `SELECT `+sqlContextInputExpr+` FROM usage_events WHERE model=? AND input_tokens=? AND provider=? AND IFNULL(service_tier,'')=? AND IFNULL(response_service_tier,'')=? LIMIT 1`, e.Model, e.InputTokens, e.Provider, strings.TrimSpace(e.ServiceTier), strings.TrimSpace(e.ResponseServiceTier)).Scan(&sqlInput); err != nil {
			t.Fatal(err)
		}
		if sqlInput != contextInputTokens(e) {
			t.Fatalf("context SQL=%d Go=%d", sqlInput, contextInputTokens(e))
		}
	}
}
