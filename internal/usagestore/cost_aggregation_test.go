package usagestore

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"
)

// TestSumCostCoversAllRows guards against the summary/account cost being computed
// from a capped page of events. With more than the old 1000-event page limit of
// matching rows, the total must equal the true sum of per-model costs (and of the
// per-model filtered summaries), not an undercount of the most-recent page.
func TestSumCostCoversAllRows(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(Options{Path: filepath.Join(dir, "usage.db"), RetentionDays: 30})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	base := time.Now().UnixMilli() - 5_000_000

	insert := func(n int, model string, input, output, cacheRead int64) {
		for i := 0; i < n; i++ {
			e := Event{
				TimestampMS:     base + int64(i),
				Model:           model,
				Alias:           model,
				Provider:        "openai-compatibility",
				InputTokens:     input,
				OutputTokens:    output,
				CacheReadTokens: cacheRead,
				TotalTokens:     input + output,
			}
			if err := store.Insert(ctx, e); err != nil {
				t.Fatalf("Insert: %v", err)
			}
		}
	}

	// 2400 total events, well past the old 1000-row cap.
	insert(1500, "alpha", 1000, 100, 0)
	insert(700, "beta", 2000, 200, 0)
	insert(200, "gamma", 1000, 100, 500) // exercises cache-read pricing in aggregation

	if err := store.UpsertModelPrices(ctx, []ModelPrice{
		{Model: "alpha", PromptPer1M: 1.0, CompletionPer1M: 2.0},
		{Model: "beta", PromptPer1M: 3.0, CompletionPer1M: 4.0},
		{Model: "gamma", PromptPer1M: 1.0, CompletionPer1M: 2.0, CacheReadPer1M: 0.25},
	}); err != nil {
		t.Fatalf("UpsertModelPrices: %v", err)
	}
	priceMap, err := store.LoadModelPrices(ctx)
	if err != nil {
		t.Fatalf("LoadModelPrices: %v", err)
	}

	const perM = 1_000_000.0
	wantAlpha := 1500.0 * (1000.0/perM*1.0 + 100.0/perM*2.0)
	wantBeta := 700.0 * (2000.0/perM*3.0 + 200.0/perM*4.0)
	// gamma: input billed net of cache-read tokens, cache read at its own rate.
	wantGamma := 200.0 * ((1000.0-500.0)/perM*1.0 + 100.0/perM*2.0 + 500.0/perM*0.25)
	wantTotal := wantAlpha + wantBeta + wantGamma

	total, priced, err := store.SumCost(ctx, QueryFilter{}, priceMap, nil)
	if err != nil {
		t.Fatalf("SumCost: %v", err)
	}
	if priced != 2400 {
		t.Fatalf("priced calls = %d, want 2400", priced)
	}
	if math.Abs(total-wantTotal) > 1e-6 {
		t.Fatalf("total cost = %.9f, want %.9f (capped-scan undercount regression?)", total, wantTotal)
	}

	// Per-model filtered totals must sum back to the whole.
	sumOfParts := 0.0
	for _, m := range []struct {
		name string
		want float64
	}{{"alpha", wantAlpha}, {"beta", wantBeta}, {"gamma", wantGamma}} {
		got, _, err := store.SumCost(ctx, QueryFilter{Models: []string{m.name}}, priceMap, nil)
		if err != nil {
			t.Fatalf("SumCost(%s): %v", m.name, err)
		}
		if math.Abs(got-m.want) > 1e-6 {
			t.Fatalf("SumCost(model=%s) = %.9f, want %.9f", m.name, got, m.want)
		}
		sumOfParts += got
	}
	if math.Abs(sumOfParts-total) > 1e-6 {
		t.Fatalf("sum of per-model costs %.9f != total %.9f", sumOfParts, total)
	}
}

// TestCostByAccountCoversAllRows checks per-account cost also aggregates across
// all rows and applies per-model pricing within each account group.
func TestCostByAccountCoversAllRows(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(Options{Path: filepath.Join(dir, "usage.db"), RetentionDays: 30})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	base := time.Now().UnixMilli() - 5_000_000

	insert := func(n int, authIndex, model string, input, output int64) {
		for i := 0; i < n; i++ {
			e := Event{
				TimestampMS:  base + int64(i),
				Model:        model,
				Alias:        model,
				Provider:     "openai-compatibility",
				AuthIndex:    authIndex,
				InputTokens:  input,
				OutputTokens: output,
				TotalTokens:  input + output,
			}
			if err := store.Insert(ctx, e); err != nil {
				t.Fatalf("Insert: %v", err)
			}
		}
	}

	insert(800, "auth-1", "alpha", 1000, 100)
	insert(800, "auth-2", "beta", 2000, 200)

	if err := store.UpsertModelPrices(ctx, []ModelPrice{
		{Model: "alpha", PromptPer1M: 1.0, CompletionPer1M: 2.0},
		{Model: "beta", PromptPer1M: 3.0, CompletionPer1M: 4.0},
	}); err != nil {
		t.Fatalf("UpsertModelPrices: %v", err)
	}
	priceMap, err := store.LoadModelPrices(ctx)
	if err != nil {
		t.Fatalf("LoadModelPrices: %v", err)
	}

	const perM = 1_000_000.0
	wantAuth1 := 800.0 * (1000.0/perM*1.0 + 100.0/perM*2.0)
	wantAuth2 := 800.0 * (2000.0/perM*3.0 + 200.0/perM*4.0)

	byKey, err := store.CostByAccount(ctx, QueryFilter{}, priceMap, nil)
	if err != nil {
		t.Fatalf("CostByAccount: %v", err)
	}
	got1 := byKey[AccountKey("auth-1", "", "", "openai-compatibility")]
	got2 := byKey[AccountKey("auth-2", "", "", "openai-compatibility")]
	if math.Abs(got1-wantAuth1) > 1e-6 {
		t.Fatalf("auth-1 cost = %.9f, want %.9f", got1, wantAuth1)
	}
	if math.Abs(got2-wantAuth2) > 1e-6 {
		t.Fatalf("auth-2 cost = %.9f, want %.9f", got2, wantAuth2)
	}
}
