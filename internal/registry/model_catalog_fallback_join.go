package registry

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// This file merges the two public models.dev catalogs into the flat fallback
// table shipped as models/model_catalog_fallback.json.
//
// The merge runs ahead of time (cmd/gen_model_catalog) and on the periodic
// refresh, never on the request path, so the runtime side (LookupCatalogFallback)
// stays a plain map lookup with no provider or vendor rules.
//
// Inputs:
//   - canonical: https://models.dev/models.json  keyed "creator/model", carries
//     the curated context window and display name.
//   - providers: https://models.dev/api.json     keyed by provider, carries the
//     per-provider reasoning effort lists.
//
// Only the canonical registry carries creator attribution, and only the provider
// catalog carries effort levels, so both are required.

// catalogFallbackCanonicalModel is one entry of models.json.
type catalogFallbackCanonicalModel struct {
	Name  string `json:"name"`
	Limit struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	} `json:"limit"`
}

// catalogFallbackProviderModel is one model row of api.json.
type catalogFallbackProviderModel struct {
	Name  string `json:"name"`
	Limit struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	} `json:"limit"`
	// Values are kept as raw JSON because the upstream catalog occasionally
	// contains nulls and mixed types inside reasoning option values.
	ReasoningOptions []struct {
		Type   string `json:"type"`
		Values []any  `json:"values"`
	} `json:"reasoning_options"`
}

// BuildCatalogFallbackArtifact merges the canonical registry and the provider
// catalog into the encoded fallback artifact consumed by the registry.
//
// Resolution order per model name:
//  1. A canonical entry, which is the curated spec and therefore authoritative.
//  2. A provider row for a name no other provider claims, which stays
//     unambiguous. Names offered by several providers are skipped because the
//     catalogs disagree on their limits and picking one would be a guess.
func BuildCatalogFallbackArtifact(canonicalJSON, providerJSON []byte, generated string) ([]byte, error) {
	canonical, err := decodeCanonicalFallbackModels(canonicalJSON)
	if err != nil {
		return nil, fmt.Errorf("decode canonical model registry: %w", err)
	}
	if len(canonical) == 0 {
		return nil, fmt.Errorf("decode canonical model registry: catalog contains no entries")
	}
	// The provider catalog supplies reasoning levels only, so an empty one is
	// tolerated: the canonical registry still yields context windows.
	providers, err := decodeFallbackProviders(providerJSON)
	if err != nil {
		return nil, fmt.Errorf("decode provider model catalog: %w", err)
	}

	// Index every provider row by model name so uniqueness can be judged and so
	// a canonical entry can be joined to its creator's own row.
	rowsByTail := make(map[string][]fallbackProviderRow)
	for providerID, provider := range providers {
		for modelID, model := range provider.Models {
			tail := fallbackModelTail(modelID)
			if tail == "" {
				continue
			}
			rowsByTail[tail] = append(rowsByTail[tail], fallbackProviderRow{
				providerID: providerID,
				model:      model,
			})
		}
	}
	for tail := range rowsByTail {
		rows := rowsByTail[tail]
		sort.Slice(rows, func(i, j int) bool { return rows[i].providerID < rows[j].providerID })
		rowsByTail[tail] = rows
	}

	entries := make(map[string]CatalogFallbackEntry, len(canonical))

	// Pass 1: canonical entries. The creator's own row supplies effort levels;
	// the canonical entry supplies the curated limits and display name.
	for key, model := range canonical {
		tail := fallbackModelTail(key)
		if tail == "" {
			continue
		}
		creator := fallbackModelCreator(key)
		efforts := fallbackEffortsForCreator(providers, creator, tail, model.Name)
		entry := CatalogFallbackEntry{
			Source:  "models.dev/" + key,
			Name:    strings.TrimSpace(model.Name),
			Context: model.Limit.Context,
			Output:  model.Limit.Output,
			Efforts: efforts,
		}
		if fallbackEntryIsEmpty(entry) {
			continue
		}
		entries[tail] = entry
	}

	// Pass 2: names only a single provider offers. These are typically provider
	// specific models that never reach the canonical registry.
	for tail, rows := range rowsByTail {
		if _, exists := entries[tail]; exists {
			continue
		}
		if len(rows) != 1 {
			// Several providers claim this name and their limits disagree, so
			// no single value can be advertised honestly.
			continue
		}
		row := rows[0]
		entry := CatalogFallbackEntry{
			Source:  "models.dev/" + row.providerID,
			Name:    strings.TrimSpace(row.model.Name),
			Context: row.model.Limit.Context,
			Output:  row.model.Limit.Output,
			Efforts: fallbackModelEfforts(row.model),
		}
		if fallbackEntryIsEmpty(entry) {
			continue
		}
		entries[tail] = entry
	}

	artifact := catalogFallbackArtifact{
		Generated: generated,
		Entries:   entries,
	}
	encoded, err := json.Marshal(artifact)
	if err != nil {
		return nil, fmt.Errorf("encode model catalog fallback: %w", err)
	}
	return encoded, nil
}

// fallbackProviderRow is one provider's row for a model name.
type fallbackProviderRow struct {
	providerID string
	model      catalogFallbackProviderModel
}

// decodeCanonicalFallbackModels decodes models.json, skipping entries that are
// not objects so one malformed row cannot reject the whole catalog.
func decodeCanonicalFallbackModels(data []byte) (map[string]catalogFallbackCanonicalModel, error) {
	raw, err := decodeFallbackObjectMap(data)
	if err != nil {
		return nil, err
	}
	out := make(map[string]catalogFallbackCanonicalModel, len(raw))
	for key, value := range raw {
		var model catalogFallbackCanonicalModel
		if err := json.Unmarshal(value, &model); err != nil {
			continue
		}
		out[key] = model
	}
	return out, nil
}

// decodeFallbackProviders decodes api.json into provider -> models.
func decodeFallbackProviders(data []byte) (map[string]catalogFallbackProvider, error) {
	raw, err := decodeFallbackObjectMap(data)
	if err != nil {
		return nil, err
	}
	out := make(map[string]catalogFallbackProvider, len(raw))
	for key, value := range raw {
		var provider catalogFallbackProvider
		if err := json.Unmarshal(value, &provider); err != nil {
			continue
		}
		out[key] = provider
	}
	return out, nil
}

// catalogFallbackProvider is one provider section of api.json.
type catalogFallbackProvider struct {
	Models map[string]catalogFallbackProviderModel `json:"models"`
}

func decodeFallbackObjectMap(data []byte) (map[string]json.RawMessage, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("catalog is empty")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, fmt.Errorf("catalog is not a JSON object")
	}
	return raw, nil
}

// fallbackModelTail returns the model name without its "creator/" or
// "provider/" prefix.
func fallbackModelTail(modelID string) string {
	trimmed := strings.TrimSpace(modelID)
	if trimmed == "" {
		return ""
	}
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 {
		trimmed = trimmed[idx+1:]
	}
	return strings.TrimSpace(trimmed)
}

// fallbackModelCreator returns the leading namespace of a canonical key.
func fallbackModelCreator(key string) string {
	trimmed := strings.TrimSpace(key)
	if idx := strings.Index(trimmed, "/"); idx >= 0 {
		return strings.TrimSpace(trimmed[:idx])
	}
	return ""
}

// fallbackEffortsForCreator resolves the effort levels the creator itself
// advertises for a model. It matches the creator's row by name first and falls
// back to a case-insensitive display-name match, because a creator sometimes
// publishes a model under a shorter key than its canonical name.
func fallbackEffortsForCreator(providers map[string]catalogFallbackProvider, creator, tail, canonicalName string) []string {
	provider, ok := providers[creator]
	if !ok {
		return nil
	}
	if row, ok := provider.Models[tail]; ok {
		return fallbackModelEfforts(row)
	}
	canonicalName = strings.ToLower(strings.TrimSpace(canonicalName))
	if canonicalName == "" {
		return nil
	}
	keys := make([]string, 0, len(provider.Models))
	for modelID := range provider.Models {
		keys = append(keys, modelID)
	}
	sort.Strings(keys)
	for _, modelID := range keys {
		row := provider.Models[modelID]
		if strings.ToLower(strings.TrimSpace(row.Name)) == canonicalName {
			return fallbackModelEfforts(row)
		}
	}
	return nil
}

// fallbackModelEfforts extracts the ordered-by-sort reasoning levels one row
// exposes. A "toggle" option means reasoning can be turned off, which the
// thinking pipeline represents as the "none" level.
func fallbackModelEfforts(model catalogFallbackProviderModel) []string {
	seen := make(map[string]struct{})
	levels := make([]string, 0, 8)
	add := func(level string) {
		level = strings.ToLower(strings.TrimSpace(level))
		if level == "" {
			return
		}
		if _, exists := seen[level]; exists {
			return
		}
		seen[level] = struct{}{}
		levels = append(levels, level)
	}

	for _, option := range model.ReasoningOptions {
		switch strings.ToLower(strings.TrimSpace(option.Type)) {
		case "effort":
			for _, value := range option.Values {
				if level, ok := value.(string); ok {
					add(level)
				}
			}
		case "toggle":
			add("none")
		}
	}
	if len(levels) == 0 {
		return nil
	}
	sort.Strings(levels)
	return levels
}

// fallbackEntryIsEmpty reports whether an entry carries nothing worth shipping.
func fallbackEntryIsEmpty(entry CatalogFallbackEntry) bool {
	return entry.Context <= 0 && entry.Output <= 0 && len(entry.Efforts) == 0
}
