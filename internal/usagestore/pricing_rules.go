package usagestore

import (
	"fmt"
	"sort"
	"strings"
)

// Stored telemetry is raw provider input, not BillableInputTokens. Anthropic
// input excludes cache reads/writes; OpenAI-compatible (including custom Claude
// aliases), Codex and Gemini input already includes cache. Keep this separate
// from legacy billable accounting, which intentionally remains unchanged.
func contextInputTokens(e Event) int64 {
	provider := strings.ToLower(strings.TrimSpace(e.Provider))
	executor := strings.ToLower(strings.TrimSpace(e.ExecutorType))
	if executor == "openaicompatexecutor" || provider == "openai-compatibility" || strings.HasPrefix(provider, "openai-compatible-") {
		return e.InputTokens
	}
	if strings.Contains(provider+" "+executor, "claude") || strings.Contains(provider+" "+executor, "anthropic") {
		return e.InputTokens + e.CacheReadTokens + e.CacheCreationTokens
	}
	return e.InputTokens
}

// SQL equivalent of contextInputTokens; only used to classify a pricing band,
// never to change billable token buckets.
const sqlContextInputExpr = `CASE
	WHEN LOWER(TRIM(IFNULL(executor_type,''))) = 'openaicompatexecutor'
		OR LOWER(TRIM(IFNULL(provider,''))) = 'openai-compatibility'
		OR LOWER(TRIM(IFNULL(provider,''))) LIKE 'openai-compatible-%' THEN input_tokens
	WHEN LOWER(IFNULL(provider,'') || ' ' || IFNULL(executor_type,'')) LIKE '%claude%'
		OR LOWER(IFNULL(provider,'') || ' ' || IFNULL(executor_type,'')) LIKE '%anthropic%'
		THEN input_tokens + cache_read_tokens + cache_creation_tokens
	ELSE input_tokens END`

const sqlEffectiveServiceTierExpr = `LOWER(TRIM(COALESCE(NULLIF(TRIM(response_service_tier),''),service_tier,'')))`

// A representative input in each band preserves every model's rule boundary.
// Group by bands rather than exact token counts so large histories stay in SQL;
// aliases and case-insensitive model resolution still use ResolvePrice in Go.
func sqlPricingBandExpr(prices map[string]ModelPrice) string {
	thresholds := make(map[int64]bool)
	for _, p := range prices {
		for _, tier := range p.ContextTiers {
			thresholds[tier.ThresholdTokens] = true
		}
	}
	if len(thresholds) == 0 {
		return "CAST(0 AS INTEGER)"
	}
	ordered := make([]int64, 0, len(thresholds))
	for threshold := range thresholds {
		ordered = append(ordered, threshold)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] > ordered[j] })
	var sql strings.Builder
	sql.WriteString("CASE")
	for _, threshold := range ordered {
		fmt.Fprintf(&sql, " WHEN (%s) > %d THEN %d", sqlContextInputExpr, threshold, threshold+1)
	}
	sql.WriteString(" ELSE 0 END")
	return sql.String()
}
