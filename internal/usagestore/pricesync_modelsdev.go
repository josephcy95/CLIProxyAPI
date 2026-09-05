package usagestore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

var modelsDevRateKeys = [4][]string{{"input"}, {"output"}, {"cache_read"}, {"cache_write"}}
var liteLLMRateKeys = [4][]string{
	{"input_cost_per_token"}, {"output_cost_per_token"},
	{"cache_read_input_token_cost", "input_cache_read"},
	{"cache_creation_input_token_cost", "cache_write_input_token_cost", "input_cache_write", "input_cache_creation"},
}
var openRouterRateKeys = [4][]string{
	{"prompt"}, {"completion"}, {"input_cache_read", "cache_read_input_token_cost"},
	{"input_cache_write", "input_cache_creation", "cache_creation_input_token_cost", "cache_write_input_token_cost"},
}

// readSyncRates rejects malformed, negative and non-finite fields instead of
// silently turning an invalid upstream rate into free pricing.
func readSyncRates(raw map[string]any, multiplier float64, keys [4][]string) (ModelPriceContextTier, bool) {
	var values [4]float64
	var configured [4]bool
	anyConfigured := false
	for i, aliases := range keys {
		for _, key := range aliases {
			if raw[key] == nil {
				continue
			}
			value, ok := readFloat(raw, key)
			if !ok || math.IsInf(value*multiplier, 0) {
				return ModelPriceContextTier{}, false
			}
			values[i], configured[i] = value*multiplier, true
			anyConfigured = true
			break
		}
	}
	return ModelPriceContextTier{
		PromptPer1M: values[0], CompletionPer1M: values[1],
		CachePer1M: values[2], CacheReadPer1M: values[2], CacheCreationPer1M: values[3],
		PromptConfigured: configured[0], CompletionConfigured: configured[1],
		CacheConfigured: configured[2], CacheReadConfigured: configured[2], CacheCreationConfigured: configured[3],
	}, anyConfigured
}

func newSyncedPrice(id, source, rawJSON string, rates ModelPriceContextTier) ModelPrice {
	now := time.Now().UnixMilli()
	return ModelPrice{
		Model: id, Source: source, SourceModelID: id, RawJSON: rawJSON, UpdatedAtMS: now, SyncedAtMS: now,
		PromptPer1M: rates.PromptPer1M, CompletionPer1M: rates.CompletionPer1M,
		CachePer1M: rates.CachePer1M, CacheReadPer1M: rates.CacheReadPer1M, CacheCreationPer1M: rates.CacheCreationPer1M,
		PromptConfigured: rates.PromptConfigured, CompletionConfigured: rates.CompletionConfigured,
		CacheConfigured: rates.CacheConfigured, CacheReadConfigured: rates.CacheReadConfigured, CacheCreationConfigured: rates.CacheCreationConfigured,
	}
}

func fetchModelsDevPrices(ctx context.Context, syncURL string, client *http.Client) (map[string]remoteCatalogPrice, int, error) {
	body, err := httpGetBody(ctx, client, syncURL)
	if err != nil {
		return nil, 0, err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, 0, fmt.Errorf("models.dev decode: %w", err)
	}
	providers := root
	var canonical map[string]json.RawMessage
	_, hasCanonical := root["providers"]
	if hasCanonical {
		if err := json.Unmarshal(root["providers"], &providers); err != nil {
			return nil, 0, fmt.Errorf("models.dev providers: %w", err)
		}
		if err := json.Unmarshal(root["models"], &canonical); err != nil || len(canonical) == 0 {
			return nil, 0, fmt.Errorf("models.dev: missing canonical models")
		}
	}
	// Only unambiguous canonical tails identify an official provider. Keep an
	// ambiguous identity empty permanently, even if a third provider repeats it.
	canonicalIDs := map[string]string{}
	canonicalTails := map[string]string{}
	for id := range canonical {
		id = strings.TrimSpace(id)
		normalized := strings.ToLower(id)
		canonicalIDs[normalized] = normalized
		_, tail, ok := strings.Cut(normalized, "/")
		if !ok || tail == "" {
			continue
		}
		if previous, exists := canonicalTails[tail]; exists && previous != normalized {
			canonicalTails[tail] = ""
		} else {
			canonicalTails[tail] = normalized
		}
	}
	out := map[string]remoteCatalogPrice{}
	skipped := 0
	for providerID, rawProvider := range providers {
		var provider struct {
			Models map[string]json.RawMessage `json:"models"`
		}
		if err := json.Unmarshal(rawProvider, &provider); err != nil {
			return nil, skipped, fmt.Errorf("models.dev provider: %w", err)
		}
		for modelID, rawModel := range provider.Models {
			if strings.TrimSpace(providerID) == "" || strings.TrimSpace(modelID) == "" {
				skipped++
				continue
			}
			var entry map[string]any
			decoder := json.NewDecoder(bytes.NewReader(rawModel))
			decoder.UseNumber()
			if decoder.Decode(&entry) != nil {
				skipped++
				continue
			}
			cost, _ := entry["cost"].(map[string]any)
			// models.dev api.json prices are USD per MILLION, unlike fallback catalogs.
			rates, ok := readSyncRates(cost, 1, modelsDevRateKeys)
			if !ok {
				skipped++
				continue
			}
			id := strings.TrimSpace(providerID) + "/" + strings.TrimSpace(modelID)
			price := newSyncedPrice(id, SyncSourceModelsDev, string(rawModel), rates)
			price.ContextTiers = readModelsDevContextTiers(cost)
			price.ServiceTiers = readModelsDevServiceTiers(entry)
			remote := remoteCatalogPrice{id: id, source: SyncSourceModelsDev, price: price, directOnly: hasCanonical}
			normalized := strings.ToLower(id)
			if _, official := canonicalIDs[normalized]; official {
				remote.officialIDs = []string{id}
				_, tail, _ := strings.Cut(normalized, "/")
				if canonicalTails[tail] == normalized {
					remote.officialIDs = append(remote.officialIDs, tail)
				}
			}
			out[id] = remote
		}
	}
	if len(out) == 0 {
		return nil, skipped, fmt.Errorf("models.dev: no usable prices")
	}
	return out, skipped, nil
}

func readModelsDevContextTiers(cost map[string]any) []ModelPriceContextTier {
	raw, _ := cost["tiers"].([]any)
	var tiers []ModelPriceContextTier
	seen := map[int64]bool{}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil
		}
		descriptor, ok := entry["tier"].(map[string]any)
		if !ok {
			return nil
		}
		if !strings.EqualFold(readString(descriptor, "type"), "context") {
			continue
		}
		threshold, ok := readFloat(descriptor, "size")
		// Reject fractional, nonpositive and inexact integer thresholds. No real
		// context window needs a threshold beyond the exact float64 integer range.
		if !ok || threshold <= 0 || threshold > 1<<53-1 || threshold != math.Trunc(threshold) {
			return nil
		}
		size := int64(threshold)
		if seen[size] {
			return nil
		}
		seen[size] = true
		tier, ok := readSyncRates(entry, 1, modelsDevRateKeys)
		if !ok {
			return nil
		}
		tier.ThresholdTokens = size
		tiers = append(tiers, tier)
	}
	sort.Slice(tiers, func(i, j int) bool { return tiers[i].ThresholdTokens < tiers[j].ThresholdTokens })
	return tiers
}

func readModelsDevServiceTiers(entry map[string]any) []ModelPriceServiceTier {
	experimental, _ := entry["experimental"].(map[string]any)
	modes, _ := experimental["modes"].(map[string]any)
	fast, _ := modes["fast"].(map[string]any)
	cost, _ := fast["cost"].(map[string]any)
	rates, ok := readSyncRates(cost, 1, modelsDevRateKeys)
	if !ok {
		return nil
	}
	return []ModelPriceServiceTier{{
		Mode: "fast", ServiceTier: "priority",
		PromptPer1M: rates.PromptPer1M, CompletionPer1M: rates.CompletionPer1M,
		CachePer1M: rates.CachePer1M, CacheReadPer1M: rates.CacheReadPer1M, CacheCreationPer1M: rates.CacheCreationPer1M,
		PromptConfigured: rates.PromptConfigured, CompletionConfigured: rates.CompletionConfigured,
		CacheConfigured: rates.CacheConfigured, CacheReadConfigured: rates.CacheReadConfigured, CacheCreationConfigured: rates.CacheCreationConfigured,
	}}
}
