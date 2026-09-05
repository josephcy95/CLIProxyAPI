package usagestore

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// ModelPrice is the per-1M token rate book entry.
type ModelPrice struct {
	Model                   string                  `json:"model"`
	PromptPer1M             float64                 `json:"prompt_per_1m"`
	CompletionPer1M         float64                 `json:"completion_per_1m"`
	CachePer1M              float64                 `json:"cache_per_1m,omitempty"`
	CacheReadPer1M          float64                 `json:"cache_read_per_1m,omitempty"`
	CacheCreationPer1M      float64                 `json:"cache_creation_per_1m,omitempty"`
	Source                  string                  `json:"source,omitempty"`
	SourceModelID           string                  `json:"source_model_id,omitempty"`
	RawJSON                 string                  `json:"raw_json,omitempty"`
	SyncedAtMS              int64                   `json:"synced_at_ms,omitempty"`
	UpdatedAtMS             int64                   `json:"updated_at_ms,omitempty"`
	PromptConfigured        bool                    `json:"prompt_configured"`
	CompletionConfigured    bool                    `json:"completion_configured"`
	CacheConfigured         bool                    `json:"cache_configured"`
	CacheReadConfigured     bool                    `json:"cache_read_configured"`
	CacheCreationConfigured bool                    `json:"cache_creation_configured"`
	ContextTiers            []ModelPriceContextTier `json:"context_tiers,omitempty"`
	ServiceTiers            []ModelPriceServiceTier `json:"service_tiers,omitempty"`
}

type ModelPriceContextTier struct {
	ThresholdTokens         int64   `json:"threshold_tokens"`
	PromptPer1M             float64 `json:"prompt_per_1m,omitempty"`
	CompletionPer1M         float64 `json:"completion_per_1m,omitempty"`
	CachePer1M              float64 `json:"cache_per_1m,omitempty"`
	CacheReadPer1M          float64 `json:"cache_read_per_1m,omitempty"`
	CacheCreationPer1M      float64 `json:"cache_creation_per_1m,omitempty"`
	PromptConfigured        bool    `json:"prompt_configured"`
	CompletionConfigured    bool    `json:"completion_configured"`
	CacheConfigured         bool    `json:"cache_configured"`
	CacheReadConfigured     bool    `json:"cache_read_configured"`
	CacheCreationConfigured bool    `json:"cache_creation_configured"`
}

type ModelPriceServiceTier struct {
	Mode                    string  `json:"mode"`
	ServiceTier             string  `json:"service_tier"`
	PromptPer1M             float64 `json:"prompt_per_1m,omitempty"`
	CompletionPer1M         float64 `json:"completion_per_1m,omitempty"`
	CachePer1M              float64 `json:"cache_per_1m,omitempty"`
	CacheReadPer1M          float64 `json:"cache_read_per_1m,omitempty"`
	CacheCreationPer1M      float64 `json:"cache_creation_per_1m,omitempty"`
	PromptConfigured        bool    `json:"prompt_configured"`
	CompletionConfigured    bool    `json:"completion_configured"`
	CacheConfigured         bool    `json:"cache_configured"`
	CacheReadConfigured     bool    `json:"cache_read_configured"`
	CacheCreationConfigured bool    `json:"cache_creation_configured"`
}

// ModelPriceAlias maps a request model string to a priced model id.
type ModelPriceAlias struct {
	Alias       string `json:"alias"`
	TargetModel string `json:"target_model"`
	UpdatedAtMS int64  `json:"updated_at_ms,omitempty"`
}

// LoadModelPrices returns the full price book keyed by model id.
func (s *Store) LoadModelPrices(ctx context.Context) (map[string]ModelPrice, error) {
	if s == nil {
		return nil, fmt.Errorf("usagestore: nil store")
	}
	rows, err := s.readDB.QueryContext(ctx, `SELECT model, prompt_per_1m, completion_per_1m, cache_per_1m,
		cache_read_per_1m, cache_creation_per_1m, IFNULL(source,''), updated_at_ms,
		IFNULL(source_model_id,''), IFNULL(raw_json,''), synced_at_ms, context_tiers, service_tiers,
		prompt_configured, completion_configured, cache_configured, cache_read_configured, cache_creation_configured
		FROM model_prices ORDER BY model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]ModelPrice)
	for rows.Next() {
		var p ModelPrice
		var contextJSON, serviceJSON string
		if err := rows.Scan(&p.Model, &p.PromptPer1M, &p.CompletionPer1M, &p.CachePer1M,
			&p.CacheReadPer1M, &p.CacheCreationPer1M, &p.Source, &p.UpdatedAtMS,
			&p.SourceModelID, &p.RawJSON, &p.SyncedAtMS, &contextJSON, &serviceJSON,
			&p.PromptConfigured, &p.CompletionConfigured, &p.CacheConfigured, &p.CacheReadConfigured, &p.CacheCreationConfigured); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(contextJSON), &p.ContextTiers); err != nil {
			return nil, fmt.Errorf("usagestore: context tiers for %s: %w", p.Model, err)
		}
		if err := json.Unmarshal([]byte(serviceJSON), &p.ServiceTiers); err != nil {
			return nil, fmt.Errorf("usagestore: service tiers for %s: %w", p.Model, err)
		}
		out[p.Model] = p
	}
	return out, rows.Err()
}

// UpsertModelPrices inserts or updates prices without deleting others.
func (s *Store) UpsertModelPrices(ctx context.Context, prices []ModelPrice) error {
	return s.writeModelPrices(ctx, prices, false)
}

// ReplaceModelPrices validates and replaces the entire book in one transaction.
func (s *Store) ReplaceModelPrices(ctx context.Context, prices []ModelPrice) error {
	return s.writeModelPrices(ctx, prices, true)
}

func (s *Store) writeModelPrices(ctx context.Context, prices []ModelPrice, replace bool) error {
	if s == nil {
		return fmt.Errorf("usagestore: nil store")
	}
	if err := ValidateModelPrices(prices); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if replace {
		if _, err := tx.ExecContext(ctx, `DELETE FROM model_prices`); err != nil {
			return err
		}
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO model_prices (
		model, prompt_per_1m, completion_per_1m, cache_per_1m, cache_read_per_1m, cache_creation_per_1m, source, updated_at_ms,
		source_model_id, raw_json, synced_at_ms, context_tiers, service_tiers,
		prompt_configured, completion_configured, cache_configured, cache_read_configured, cache_creation_configured
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(model) DO UPDATE SET
		prompt_per_1m=excluded.prompt_per_1m, completion_per_1m=excluded.completion_per_1m,
		cache_per_1m=excluded.cache_per_1m, cache_read_per_1m=excluded.cache_read_per_1m,
		cache_creation_per_1m=excluded.cache_creation_per_1m, source=excluded.source, updated_at_ms=excluded.updated_at_ms,
		source_model_id=excluded.source_model_id, raw_json=excluded.raw_json, synced_at_ms=excluded.synced_at_ms,
		context_tiers=excluded.context_tiers, service_tiers=excluded.service_tiers,
		prompt_configured=excluded.prompt_configured, completion_configured=excluded.completion_configured,
		cache_configured=excluded.cache_configured, cache_read_configured=excluded.cache_read_configured,
		cache_creation_configured=excluded.cache_creation_configured`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().UnixMilli()
	for _, p := range prices {
		p.Model = strings.TrimSpace(p.Model)
		p.Source = strings.TrimSpace(p.Source)
		if p.Source == "" {
			p.Source = "manual"
		}
		if p.UpdatedAtMS == 0 {
			p.UpdatedAtMS = now
		}
		p.ContextTiers = sortedTiers(p.ContextTiers)
		p.ServiceTiers = append([]ModelPriceServiceTier(nil), p.ServiceTiers...)
		for i := range p.ServiceTiers {
			p.ServiceTiers[i].Mode = strings.ToLower(strings.TrimSpace(p.ServiceTiers[i].Mode))
			p.ServiceTiers[i].ServiceTier = strings.ToLower(strings.TrimSpace(p.ServiceTiers[i].ServiceTier))
		}
		contextJSON, err := json.Marshal(p.ContextTiers)
		if err != nil {
			return err
		}
		serviceJSON, err := json.Marshal(p.ServiceTiers)
		if err != nil {
			return err
		}
		if _, err := stmt.ExecContext(ctx, p.Model, p.PromptPer1M, p.CompletionPer1M, p.CachePer1M,
			p.CacheReadPer1M, p.CacheCreationPer1M, p.Source, p.UpdatedAtMS,
			strings.TrimSpace(p.SourceModelID), p.RawJSON, p.SyncedAtMS, string(contextJSON), string(serviceJSON),
			p.PromptConfigured, p.CompletionConfigured, p.CacheConfigured, p.CacheReadConfigured, p.CacheCreationConfigured); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LoadModelPriceAliases returns alias -> target model map entries.
func (s *Store) LoadModelPriceAliases(ctx context.Context) ([]ModelPriceAlias, error) {
	if s == nil {
		return nil, fmt.Errorf("usagestore: nil store")
	}
	rows, err := s.readDB.QueryContext(ctx, `SELECT alias, target_model, updated_at_ms FROM model_price_aliases ORDER BY alias`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ModelPriceAlias, 0, 16)
	for rows.Next() {
		var a ModelPriceAlias
		if err := rows.Scan(&a.Alias, &a.TargetModel, &a.UpdatedAtMS); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpsertModelPriceAliases upserts alias mappings.
func (s *Store) UpsertModelPriceAliases(ctx context.Context, aliases []ModelPriceAlias) error {
	if s == nil {
		return fmt.Errorf("usagestore: nil store")
	}
	if len(aliases) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO model_price_aliases (alias, target_model, updated_at_ms)
		VALUES (?, ?, ?)
		ON CONFLICT(alias) DO UPDATE SET target_model=excluded.target_model, updated_at_ms=excluded.updated_at_ms`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().UnixMilli()
	for _, a := range aliases {
		alias := strings.TrimSpace(a.Alias)
		target := strings.TrimSpace(a.TargetModel)
		if alias == "" || target == "" {
			continue
		}
		updated := a.UpdatedAtMS
		if updated == 0 {
			updated = now
		}
		if _, err := stmt.ExecContext(ctx, alias, target, updated); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteModelPriceAlias removes one alias.
func (s *Store) DeleteModelPriceAlias(ctx context.Context, alias string) error {
	if s == nil {
		return fmt.Errorf("usagestore: nil store")
	}
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return fmt.Errorf("alias required")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM model_price_aliases WHERE alias = ?`, alias)
	return err
}

// DeleteModelPrice removes one price entry.
func (s *Store) DeleteModelPrice(ctx context.Context, model string) error {
	if s == nil {
		return fmt.Errorf("usagestore: nil store")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("model required")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM model_prices WHERE model = ?`, model)
	return err
}

// ResolvePrice finds a price for candidate model names using aliases.
func ResolvePrice(candidates []string, prices map[string]ModelPrice, aliases map[string]string) (ModelPrice, string, bool) {
	seen := map[string]bool{}
	for _, name := range candidates {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if p, ok := prices[name]; ok {
			return p, name, true
		}
		if target, ok := aliases[name]; ok {
			target = strings.TrimSpace(target)
			if target != "" {
				if p, ok := prices[target]; ok {
					return p, target, true
				}
			}
		}
	}
	// case-insensitive fallback
	for _, name := range candidates {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		lower := strings.ToLower(name)
		for model, p := range prices {
			if strings.ToLower(model) == lower {
				return p, model, true
			}
		}
		if target, ok := aliases[name]; ok {
			_ = target
		}
		for alias, target := range aliases {
			if strings.ToLower(alias) == lower {
				if p, ok := prices[target]; ok {
					return p, target, true
				}
			}
		}
	}
	return ModelPrice{}, "", false
}

// ValidateModelPrices validates the complete price replacement/upsert payload
// without modifying caller-owned slices. Missing rule fields must be omitted;
// explicit zeros are distinguished using the corresponding configured flag.
func ValidateModelPrices(prices []ModelPrice) error {
	seen := make(map[string]bool, len(prices))
	for _, p := range prices {
		model := strings.TrimSpace(p.Model)
		if model == "" || seen[model] {
			return fmt.Errorf("invalid duplicate or empty model: %q", model)
		}
		seen[model] = true
		if err := validateRates(p.PromptPer1M, p.CompletionPer1M, p.CachePer1M, p.CacheReadPer1M, p.CacheCreationPer1M); err != nil {
			return fmt.Errorf("invalid price for %s: %w", model, err)
		}
		if p.RawJSON != "" && !json.Valid([]byte(p.RawJSON)) {
			return fmt.Errorf("invalid raw JSON for %s", model)
		}
		thresholds := map[int64]bool{}
		for _, t := range p.ContextTiers {
			if t.ThresholdTokens <= 0 || t.ThresholdTokens == math.MaxInt64 || thresholds[t.ThresholdTokens] {
				return fmt.Errorf("invalid context tier for %s", model)
			}
			thresholds[t.ThresholdTokens] = true
			if err := validateRuleRates(t.PromptPer1M, t.CompletionPer1M, t.CachePer1M, t.CacheReadPer1M, t.CacheCreationPer1M, t.PromptConfigured, t.CompletionConfigured, t.CacheConfigured, t.CacheReadConfigured, t.CacheCreationConfigured); err != nil {
				return fmt.Errorf("invalid context tier for %s: %w", model, err)
			}
		}
		ids := map[string]bool{}
		for _, t := range p.ServiceTiers {
			mode, tier := normalizeServiceTier(t.Mode), normalizeServiceTier(t.ServiceTier)
			if mode == "" || tier == "" || ids[mode] || ids[tier] {
				return fmt.Errorf("invalid service tier for %s", model)
			}
			ids[mode], ids[tier] = true, true
			if err := validateRuleRates(t.PromptPer1M, t.CompletionPer1M, t.CachePer1M, t.CacheReadPer1M, t.CacheCreationPer1M, t.PromptConfigured, t.CompletionConfigured, t.CacheConfigured, t.CacheReadConfigured, t.CacheCreationConfigured); err != nil {
				return fmt.Errorf("invalid service tier for %s: %w", model, err)
			}
		}
	}
	return nil
}

func validateRates(values ...float64) error {
	for _, v := range values {
		if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("rates must be finite and non-negative")
		}
	}
	return nil
}

func validateRuleRates(prompt, completion, cache, read, creation float64, pc, cc, cac, crc, ccc bool) error {
	if err := validateRates(prompt, completion, cache, read, creation); err != nil {
		return err
	}
	if !pc && !cc && !cac && !crc && !ccc {
		return fmt.Errorf("at least one configured rate required")
	}
	if (!pc && prompt != 0) || (!cc && completion != 0) || (!cac && cache != 0) || (!crc && read != 0) || (!ccc && creation != 0) {
		return fmt.Errorf("nonzero rule rates require configured flags")
	}
	return nil
}

func applyPriceFields(base ModelPrice, prompt, completion, cache, read, creation float64, pc, cc, cac, crc, ccc bool) ModelPrice {
	p := base
	p.ContextTiers = nil
	p.ServiceTiers = nil
	if pc {
		p.PromptPer1M = prompt
		p.PromptConfigured = true
	}
	if cc {
		p.CompletionPer1M = completion
		p.CompletionConfigured = true
	}
	if cac {
		p.CachePer1M = cache
		p.CacheConfigured = true
	}
	if crc {
		p.CacheReadPer1M = read
		p.CacheReadConfigured = true
	}
	if ccc {
		p.CacheCreationPer1M = creation
		p.CacheCreationConfigured = true
	}
	return p
}

// ResolveUsagePrice applies context rules first, then service-tier rules. Context
// thresholds use raw input tokens and strictly match input > threshold.
func ResolveUsagePrice(price ModelPrice, inputTokens int64, serviceTier string) ModelPrice {
	selected := -1
	for i, t := range price.ContextTiers {
		if inputTokens > t.ThresholdTokens && (selected < 0 || t.ThresholdTokens > price.ContextTiers[selected].ThresholdTokens) {
			selected = i
		}
	}
	if selected >= 0 {
		t := price.ContextTiers[selected]
		return applyPriceFields(price, t.PromptPer1M, t.CompletionPer1M, t.CachePer1M, t.CacheReadPer1M, t.CacheCreationPer1M, t.PromptConfigured, t.CompletionConfigured, t.CacheConfigured, t.CacheReadConfigured, t.CacheCreationConfigured)
	}
	serviceTier = normalizeServiceTier(serviceTier)
	for _, t := range price.ServiceTiers {
		if serviceTier != "" && (serviceTier == normalizeServiceTier(t.Mode) || serviceTier == normalizeServiceTier(t.ServiceTier)) {
			return applyPriceFields(price, t.PromptPer1M, t.CompletionPer1M, t.CachePer1M, t.CacheReadPer1M, t.CacheCreationPer1M, t.PromptConfigured, t.CompletionConfigured, t.CacheConfigured, t.CacheReadConfigured, t.CacheCreationConfigured)
		}
	}
	return price
}

// BillableInputTokens returns prompt tokens charged at the full prompt rate.
//
// OpenAI-compatible providers often report input_tokens inclusive of cache reads;
// when cache_read is present and not larger than input, those tokens are excluded
// here and billed at the cache-read rate instead. Anthropic-style rows (input is
// already net, cache_read may exceed input) keep the full input amount.
func BillableInputTokens(input, cacheRead int64) int64 {
	if cacheRead > 0 && input >= cacheRead {
		return input - cacheRead
	}
	return input
}

// sqlBillableInputExpr is the per-row SQL equivalent of BillableInputTokens.
// Aggregating SUM(input) then netting SUM(cache_read) is NOT equivalent when a
// range mixes inclusive-input and net-input rows: the large cache total can be
// subtracted from another period's input and under-count cost on longer windows.
const sqlBillableInputExpr = `CASE WHEN cache_read_tokens > 0 AND input_tokens >= cache_read_tokens THEN input_tokens - cache_read_tokens ELSE input_tokens END`

// sqlCachedOnlyExpr sums legacy cached_tokens only on rows that lack explicit
// cache read/creation breakdown (matches EstimateCost's residual cached path).
const sqlCachedOnlyExpr = `CASE WHEN IFNULL(cache_read_tokens,0) = 0 AND IFNULL(cache_creation_tokens,0) = 0 THEN cached_tokens ELSE 0 END`

// EstimateCost computes dollar cost for token usage on a single event (or any
// row-shaped token tuple). Prefer SumCost / CostBy* for multi-row aggregates so
// billable input is netted per row before summing.
func EstimateCost(price ModelPrice, input, output, reasoning, cacheRead, cacheCreation, cached int64) float64 {
	if cacheRead != 0 || cacheCreation != 0 {
		cached = 0
	}
	return EstimateCostParts(price, BillableInputTokens(input, cacheRead), output, reasoning, cacheRead, cacheCreation, cached)
}

// EstimateCostParts applies the price book to already-resolved token buckets.
// billableInput must already exclude inclusive cache-read tokens when appropriate
// (see BillableInputTokens / sqlBillableInputExpr); this function does not net again.
// cached must contain only residual legacy tokens from rows without explicit
// cache buckets (sqlCachedOnlyExpr), even when other rows have explicit cache.
func EstimateCostParts(price ModelPrice, billableInput, output, reasoning, cacheRead, cacheCreation, cached int64) float64 {
	const perM = 1_000_000.0
	// Prefer explicit cache read/creation; residual cached tokens use cache or cache_read rate.
	cacheReadRate := price.CacheReadPer1M
	if cacheReadRate <= 0 && !price.CacheReadConfigured {
		cacheReadRate = price.CachePer1M
	}
	cacheCreateRate := price.CacheCreationPer1M
	if cacheCreateRate <= 0 && !price.CacheCreationConfigured {
		cacheCreateRate = price.PromptPer1M * 1.25
	}
	cost := 0.0
	cost += float64(billableInput) / perM * price.PromptPer1M
	cost += float64(output) / perM * price.CompletionPer1M
	// Reasoning often billed as completion; if no separate rate use completion.
	cost += float64(reasoning) / perM * price.CompletionPer1M
	cost += float64(cacheRead) / perM * cacheReadRate
	cost += float64(cacheCreation) / perM * cacheCreateRate
	if cached > 0 {
		rate := price.CachePer1M
		if rate <= 0 && !price.CacheConfigured {
			rate = cacheReadRate
		}
		cost += float64(cached) / perM * rate
	}
	return cost
}

// AttachEventCosts fills EstimatedCost on events using the price book.
func AttachEventCosts(events []Event, prices map[string]ModelPrice, aliases map[string]string) (total float64, priced int64) {
	for i := range events {
		p, _, ok := ResolvePrice([]string{events[i].Model, events[i].Alias}, prices, aliases)
		if !ok {
			continue
		}
		p = ResolveUsagePrice(p, contextInputTokens(events[i]), effectiveServiceTier(events[i].ServiceTier, events[i].ResponseServiceTier))
		cost := EstimateCost(p, events[i].InputTokens, events[i].OutputTokens, events[i].ReasoningTokens,
			events[i].CacheReadTokens, events[i].CacheCreationTokens, events[i].CachedTokens)
		events[i].EstimatedCost = &cost
		total += cost
		priced++
	}
	return total, priced
}

// AliasMap converts alias slice to map.
func AliasMap(aliases []ModelPriceAlias) map[string]string {
	out := make(map[string]string, len(aliases))
	for _, a := range aliases {
		out[a.Alias] = a.TargetModel
	}
	return out
}

func effectiveServiceTier(request, response string) string {
	if strings.TrimSpace(response) != "" {
		return response
	}
	return request
}

func sortedTiers(v []ModelPriceContextTier) []ModelPriceContextTier {
	out := append([]ModelPriceContextTier(nil), v...)
	sort.Slice(out, func(i, j int) bool { return out[i].ThresholdTokens < out[j].ThresholdTokens })
	return out
}

// Fast is the UI mode name for the upstream priority service tier.
func normalizeServiceTier(tier string) string {
	tier = strings.ToLower(strings.TrimSpace(tier))
	if tier == "fast" {
		return "priority"
	}
	return tier
}
