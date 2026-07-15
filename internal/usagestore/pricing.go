package usagestore

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ModelPrice is the per-1M token rate book entry.
type ModelPrice struct {
	Model            string  `json:"model"`
	PromptPer1M      float64 `json:"prompt_per_1m"`
	CompletionPer1M  float64 `json:"completion_per_1m"`
	CachePer1M       float64 `json:"cache_per_1m,omitempty"`
	CacheReadPer1M   float64 `json:"cache_read_per_1m,omitempty"`
	CacheCreationPer1M float64 `json:"cache_creation_per_1m,omitempty"`
	Source           string  `json:"source,omitempty"`
	UpdatedAtMS      int64   `json:"updated_at_ms,omitempty"`
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
	rows, err := s.db.QueryContext(ctx, `SELECT model, prompt_per_1m, completion_per_1m, cache_per_1m,
		cache_read_per_1m, cache_creation_per_1m, IFNULL(source,''), updated_at_ms FROM model_prices ORDER BY model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]ModelPrice)
	for rows.Next() {
		var p ModelPrice
		if err := rows.Scan(&p.Model, &p.PromptPer1M, &p.CompletionPer1M, &p.CachePer1M,
			&p.CacheReadPer1M, &p.CacheCreationPer1M, &p.Source, &p.UpdatedAtMS); err != nil {
			return nil, err
		}
		out[p.Model] = p
	}
	return out, rows.Err()
}

// UpsertModelPrices inserts or updates prices without deleting others.
func (s *Store) UpsertModelPrices(ctx context.Context, prices []ModelPrice) error {
	if s == nil {
		return fmt.Errorf("usagestore: nil store")
	}
	if len(prices) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO model_prices (
		model, prompt_per_1m, completion_per_1m, cache_per_1m, cache_read_per_1m, cache_creation_per_1m, source, updated_at_ms
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(model) DO UPDATE SET
		prompt_per_1m=excluded.prompt_per_1m,
		completion_per_1m=excluded.completion_per_1m,
		cache_per_1m=excluded.cache_per_1m,
		cache_read_per_1m=excluded.cache_read_per_1m,
		cache_creation_per_1m=excluded.cache_creation_per_1m,
		source=excluded.source,
		updated_at_ms=excluded.updated_at_ms`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().UnixMilli()
	for _, p := range prices {
		model := strings.TrimSpace(p.Model)
		if model == "" {
			continue
		}
		updated := p.UpdatedAtMS
		if updated == 0 {
			updated = now
		}
		source := strings.TrimSpace(p.Source)
		if source == "" {
			source = "manual"
		}
		if _, err := stmt.ExecContext(ctx, model, p.PromptPer1M, p.CompletionPer1M, p.CachePer1M,
			p.CacheReadPer1M, p.CacheCreationPer1M, source, updated); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReplaceModelPrices replaces the entire price book.
func (s *Store) ReplaceModelPrices(ctx context.Context, prices []ModelPrice) error {
	if s == nil {
		return fmt.Errorf("usagestore: nil store")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM model_prices`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.UpsertModelPrices(ctx, prices)
}

// LoadModelPriceAliases returns alias -> target model map entries.
func (s *Store) LoadModelPriceAliases(ctx context.Context) ([]ModelPriceAlias, error) {
	if s == nil {
		return nil, fmt.Errorf("usagestore: nil store")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT alias, target_model, updated_at_ms FROM model_price_aliases ORDER BY alias`)
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

// EstimateCost computes dollar cost for token usage.
func EstimateCost(price ModelPrice, input, output, reasoning, cacheRead, cacheCreation, cached int64) float64 {
	const perM = 1_000_000.0
	// Prefer explicit cache read/creation; residual cached tokens use cache or cache_read rate.
	cacheReadRate := price.CacheReadPer1M
	if cacheReadRate <= 0 {
		cacheReadRate = price.CachePer1M
	}
	cacheCreateRate := price.CacheCreationPer1M
	if cacheCreateRate <= 0 {
		cacheCreateRate = price.PromptPer1M * 1.25
	}
	// Input billed net of cache-read tokens when cache read is present.
	billableInput := input
	if cacheRead > 0 && billableInput >= cacheRead {
		billableInput -= cacheRead
	}
	cost := 0.0
	cost += float64(billableInput) / perM * price.PromptPer1M
	cost += float64(output) / perM * price.CompletionPer1M
	// Reasoning often billed as completion; if no separate rate use completion.
	cost += float64(reasoning) / perM * price.CompletionPer1M
	cost += float64(cacheRead) / perM * cacheReadRate
	cost += float64(cacheCreation) / perM * cacheCreateRate
	if cacheRead == 0 && cacheCreation == 0 && cached > 0 {
		rate := price.CachePer1M
		if rate <= 0 {
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
