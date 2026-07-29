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

// TestSumCostMixedCacheStylesIsMonotoneInRange reproduces the monitoring bug where
// a 7d (or longer) window reported lower cost / net input than the nested 24h window.
//
// Recent rows use OpenAI-style inclusive input (input includes cache_read). Older rows
// use Anthropic-style net input (cache_read may exceed input on that row). Aggregating
// SUM(input) then subtracting SUM(cache_read) once lets older cache tokens cancel recent
// billable input — so longer ranges under-count both cost and net input.
func TestSumCostMixedCacheStylesIsMonotoneInRange(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(Options{Path: filepath.Join(dir, "usage.db"), RetentionDays: 30})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now().UnixMilli()
	// 24h window: heavy uncached OpenAI-style traffic (input inclusive of cache).
	for i := 0; i < 100; i++ {
		e := Event{
			TimestampMS:     now - int64(i)*60_000,
			Model:           "mixed-model",
			Alias:           "mixed-model",
			Provider:        "openai-compatibility",
			InputTokens:     1_000_000,
			OutputTokens:    1_000,
			CacheReadTokens: 50_000,
			TotalTokens:     1_001_000,
		}
		if err := store.Insert(ctx, e); err != nil {
			t.Fatalf("Insert recent: %v", err)
		}
	}
	// Older part of 7d: Anthropic-style rows. Per-row cache exceeds input (so per-event
	// pricing does not subtract), but the added cache is small enough that the 7d
	// totals still satisfy SUM(input) >= SUM(cache) — the condition that makes naive
	// aggregate netting subtract older cache from recent inclusive input.
	for i := 0; i < 100; i++ {
		e := Event{
			TimestampMS:     now - 3*24*3_600_000 - int64(i)*60_000,
			Model:           "mixed-model",
			Alias:           "mixed-model",
			Provider:        "claude",
			InputTokens:     10_000, // already net
			OutputTokens:    500,
			CacheReadTokens: 200_000, // > input on this row, but not enough to flip the 7d totals
			TotalTokens:     210_500,
		}
		if err := store.Insert(ctx, e); err != nil {
			t.Fatalf("Insert older: %v", err)
		}
	}

	if err := store.UpsertModelPrices(ctx, []ModelPrice{
		{Model: "mixed-model", PromptPer1M: 5.0, CompletionPer1M: 15.0, CacheReadPer1M: 0.5},
	}); err != nil {
		t.Fatalf("UpsertModelPrices: %v", err)
	}
	priceMap, err := store.LoadModelPrices(ctx)
	if err != nil {
		t.Fatalf("LoadModelPrices: %v", err)
	}

	filter24h := QueryFilter{FromMS: now - 24*3_600_000, ToMS: now}
	filter7d := QueryFilter{FromMS: now - 7*24*3_600_000, ToMS: now}

	sum24, err := store.GetSummary(ctx, filter24h)
	if err != nil {
		t.Fatalf("GetSummary 24h: %v", err)
	}
	sum7, err := store.GetSummary(ctx, filter7d)
	if err != nil {
		t.Fatalf("GetSummary 7d: %v", err)
	}
	if sum7.TotalCalls <= sum24.TotalCalls {
		t.Fatalf("7d calls %d should exceed 24h calls %d", sum7.TotalCalls, sum24.TotalCalls)
	}
	if sum7.NetInputTokens < sum24.NetInputTokens {
		t.Fatalf("7d net input %d < 24h net input %d (aggregate cache netting regression)", sum7.NetInputTokens, sum24.NetInputTokens)
	}

	// Naive aggregate netting (old UI + old SumCost path) under-counts vs 24h.
	var naiveNet7 int64
	if sum7.InputTokens >= sum7.CacheReadTokens {
		naiveNet7 = sum7.InputTokens - sum7.CacheReadTokens
	} else {
		naiveNet7 = sum7.InputTokens
	}
	if naiveNet7 >= sum24.NetInputTokens {
		t.Fatalf("test setup did not trigger naive under-count (naive7=%d, net24=%d, in7=%d, cr7=%d)",
			naiveNet7, sum24.NetInputTokens, sum7.InputTokens, sum7.CacheReadTokens)
	}
	if sum7.NetInputTokens <= naiveNet7 {
		t.Fatalf("NetInputTokens %d should beat naive aggregate netting %d", sum7.NetInputTokens, naiveNet7)
	}

	cost24, _, err := store.SumCost(ctx, filter24h, priceMap, nil)
	if err != nil {
		t.Fatalf("SumCost 24h: %v", err)
	}
	cost7, _, err := store.SumCost(ctx, filter7d, priceMap, nil)
	if err != nil {
		t.Fatalf("SumCost 7d: %v", err)
	}
	if cost7+1e-9 < cost24 {
		t.Fatalf("7d cost %.6f < 24h cost %.6f (longer range must not under-count)", cost7, cost24)
	}

	// Old aggregate EstimateCost path under-counts vs 24h with this mix.
	naiveCost7 := EstimateCost(priceMap["mixed-model"], sum7.InputTokens, sum7.OutputTokens,
		sum7.ReasoningTokens, sum7.CacheReadTokens, sum7.CacheCreationTokens, sum7.CachedTokens)
	if naiveCost7+1e-9 >= cost24 {
		t.Fatalf("test setup did not trigger naive cost under-count (naive7=%.6f, cost24=%.6f)", naiveCost7, cost24)
	}
	if cost7+1e-9 < naiveCost7 {
		// Correct cost should be higher than the buggy aggregate path here.
		t.Fatalf("fixed cost7 %.6f should exceed naive aggregate cost %.6f", cost7, naiveCost7)
	}

	// Per-event ground truth must match the aggregate for the full window.
	events, err := store.ListEvents(ctx, QueryFilter{FromMS: filter7d.FromMS, ToMS: filter7d.ToMS, Limit: 1000})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 200 {
		t.Fatalf("listed %d events, want 200", len(events))
	}
	truth, priced := AttachEventCosts(events, priceMap, nil)
	if priced != 200 {
		t.Fatalf("priced %d, want 200", priced)
	}
	if math.Abs(cost7-truth) > 1e-6 {
		t.Fatalf("SumCost 7d = %.9f, per-event truth = %.9f", cost7, truth)
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
