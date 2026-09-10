package registry

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
)

//go:embed models/model_catalog_fallback.json
var embeddedModelCatalogFallbackJSON []byte

// CatalogFallbackEntry is one resolved model in the fallback catalog.
//
// The catalog is derived ahead of time from models.dev (see
// model_catalog_fallback_join.go), so runtime lookup needs no provider or vendor
// rules: it is an exact-name map access with a small suffix-stripping retry.
type CatalogFallbackEntry struct {
	// Source records where the entry came from, e.g.
	// "models.dev/deepseek/deepseek-v4.1-flash", so a published value can be
	// traced back to the catalog that produced it.
	Source string `json:"s,omitempty"`
	// Name is the catalog display name for the model.
	Name string `json:"n,omitempty"`
	// Context is the context window in tokens.
	Context int `json:"c,omitempty"`
	// Output is the maximum output token count.
	Output int `json:"o,omitempty"`
	// Efforts lists the reasoning levels the model's creator exposes.
	Efforts []string `json:"e,omitempty"`
}

// catalogFallbackArtifact is the on-disk shape of the generated catalog.
type catalogFallbackArtifact struct {
	Generated string                          `json:"generated,omitempty"`
	Entries   map[string]CatalogFallbackEntry `json:"entries"`
}

type catalogFallbackStore struct {
	mu      sync.RWMutex
	entries map[string]CatalogFallbackEntry
}

var modelCatalogFallbackStore = &catalogFallbackStore{}

// catalogFallbackStrippableSuffixes are trailing name segments that describe a
// reasoning level rather than the model itself. Configured models frequently add
// them ("claude-opus-4-6-thinking", "gemini-3.8-flash-high"), so lookup retries
// without them.
//
// Lookup always tries the exact name first, so a catalog model whose real name
// ends with one of these still resolves to itself rather than to a shorter name.
var catalogFallbackStrippableSuffixes = map[string]struct{}{
	"fast":         {},
	"high":         {},
	"xhigh":        {},
	"max":          {},
	"ultra":        {},
	"thinking":     {},
	"non-thinking": {},
	"low":          {},
	"medium":       {},
	"minimal":      {},
}

func init() {
	if _, err := loadCatalogFallbackFromBytes(embeddedModelCatalogFallbackJSON, "embed"); err != nil {
		log.Warnf("registry: failed to parse embedded model_catalog_fallback.json (catalog fallback will stay unavailable until a valid remote refresh): %v", err)
	}
}

// LookupCatalogFallback returns catalog metadata for a model name.
//
// The matches are tried in order: the exact name, then the name with trailing
// reasoning-level segments removed. Nothing is guessed, so an unknown name
// reports no match and the caller keeps whatever it already had.
func LookupCatalogFallback(modelName string) (*CatalogFallbackEntry, bool) {
	key := normalizeCatalogFallbackKey(modelName)
	if key == "" {
		return nil, false
	}

	modelCatalogFallbackStore.mu.RLock()
	defer modelCatalogFallbackStore.mu.RUnlock()
	if len(modelCatalogFallbackStore.entries) == 0 {
		return nil, false
	}

	if entry, ok := modelCatalogFallbackStore.entries[key]; ok {
		return &entry, true
	}
	for _, candidate := range catalogFallbackStripCandidates(key) {
		if entry, ok := modelCatalogFallbackStore.entries[candidate]; ok {
			return &entry, true
		}
	}
	return nil, false
}

// GetCatalogFallbackSize reports how many entries the active catalog holds.
func GetCatalogFallbackSize() int {
	modelCatalogFallbackStore.mu.RLock()
	defer modelCatalogFallbackStore.mu.RUnlock()
	return len(modelCatalogFallbackStore.entries)
}

// normalizeCatalogFallbackKey lowercases a model name and drops the
// thinking-budget suffix written as "model(high)".
func normalizeCatalogFallbackKey(modelName string) string {
	trimmed := strings.ToLower(strings.TrimSpace(modelName))
	if trimmed == "" {
		return ""
	}
	if strings.HasSuffix(trimmed, ")") {
		if idx := strings.LastIndex(trimmed, "("); idx >= 0 {
			trimmed = strings.TrimSpace(trimmed[:idx])
		}
	}
	return trimmed
}

// catalogFallbackStripCandidates lists progressively shorter forms of a name,
// each produced by removing one trailing reasoning-level segment.
func catalogFallbackStripCandidates(key string) []string {
	candidates := make([]string, 0, 4)
	current := key
	for len(candidates) < 4 {
		idx := strings.LastIndex(current, "-")
		if idx <= 0 {
			break
		}
		suffix := current[idx+1:]
		if _, ok := catalogFallbackStrippableSuffixes[suffix]; !ok {
			break
		}
		current = current[:idx]
		if current == "" {
			break
		}
		candidates = append(candidates, current)
	}
	return candidates
}

// ValidateCatalogFallbackJSON checks that the payload can be loaded and holds at
// least one entry, so a truncated download never replaces a good catalog.
func ValidateCatalogFallbackJSON(data []byte) error {
	var artifact catalogFallbackArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return fmt.Errorf("decode model catalog fallback: %w", err)
	}
	if len(artifact.Entries) == 0 {
		return fmt.Errorf("model catalog fallback contains no entries")
	}
	return nil
}

// loadCatalogFallbackFromBytes replaces the active catalog. It reports whether
// the content actually changed.
func loadCatalogFallbackFromBytes(data []byte, source string) (bool, error) {
	if err := ValidateCatalogFallbackJSON(data); err != nil {
		return false, fmt.Errorf("%s: %w", source, err)
	}
	var artifact catalogFallbackArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return false, fmt.Errorf("%s: decode model catalog fallback: %w", source, err)
	}

	normalized := make(map[string]CatalogFallbackEntry, len(artifact.Entries))
	for name, entry := range artifact.Entries {
		key := normalizeCatalogFallbackKey(name)
		if key == "" || fallbackEntryIsEmpty(entry) {
			continue
		}
		normalized[key] = entry
	}
	if len(normalized) == 0 {
		return false, fmt.Errorf("%s: model catalog fallback contains no usable entries", source)
	}

	modelCatalogFallbackStore.mu.Lock()
	defer modelCatalogFallbackStore.mu.Unlock()
	if len(modelCatalogFallbackStore.entries) == len(normalized) {
		unchanged := true
		for name, entry := range normalized {
			existing, ok := modelCatalogFallbackStore.entries[name]
			if !ok || !catalogFallbackEntryEqual(existing, entry) {
				unchanged = false
				break
			}
		}
		if unchanged {
			return false, nil
		}
	}
	modelCatalogFallbackStore.entries = normalized
	return true, nil
}

func catalogFallbackEntryEqual(a, b CatalogFallbackEntry) bool {
	if a.Source != b.Source || a.Name != b.Name || a.Context != b.Context || a.Output != b.Output {
		return false
	}
	if len(a.Efforts) != len(b.Efforts) {
		return false
	}
	for i := range a.Efforts {
		if a.Efforts[i] != b.Efforts[i] {
			return false
		}
	}
	return true
}
