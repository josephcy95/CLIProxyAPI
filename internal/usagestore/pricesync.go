package usagestore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	SyncSourceModelsDev  = "models.dev"
	SyncSourceLiteLLM    = "litellm"
	SyncSourceOpenRouter = "openrouter"

	defaultModelsDevURL = "https://models.dev/api.json"

	defaultLiteLLMURL    = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
	defaultOpenRouterURL = "https://openrouter.ai/api/v1/models"

	maxSyncCandidates     = 8
	minCandidateScore     = 0.55
	minWeakCandidateScore = 0.34
	priceSyncHTTPTimeout  = 45 * time.Second
	maxPriceSyncBodyBytes = 32 << 20
)

// PriceSyncRequest selects which local models to match against remote catalogs.
// Empty Models refreshes existing synced entries as well as models found in usage.
type PriceSyncRequest struct {
	Models         []string `json:"models"`
	OverrideManual bool     `json:"override_manual"`
	// ApplyMatched, when true, upserts automatic matches. When false, only reports.
	ApplyMatched bool `json:"apply_matched"`
}

// PriceSyncCandidate is a non-exact remote match that needs user confirmation.
type PriceSyncCandidate struct {
	SourceModelID string     `json:"source_model_id"`
	Score         float64    `json:"score"`
	Reason        string     `json:"reason"`
	Price         ModelPrice `json:"price"`
}

// PriceSyncCandidateSet groups candidates for one local model id.
type PriceSyncCandidateSet struct {
	Model      string               `json:"model"`
	Candidates []PriceSyncCandidate `json:"candidates"`
}

// PriceSyncSourceResult is per-catalog fetch status.
type PriceSyncSourceResult struct {
	Source  string `json:"source"`
	Models  int    `json:"models"`
	Skipped int    `json:"skipped"`
	Error   string `json:"error,omitempty"`
}

// PriceSyncResult is returned by SyncModelPrices.
type PriceSyncResult struct {
	Sources       []string                `json:"sources,omitempty"`
	Imported      int                     `json:"imported"`
	Skipped       int                     `json:"skipped"`
	SkippedManual int                     `json:"skipped_manual"`
	Matched       []ModelPrice            `json:"matched,omitempty"`
	Candidates    []PriceSyncCandidateSet `json:"candidates,omitempty"`
	Unmatched     []string                `json:"unmatched,omitempty"`
	Preserved     []string                `json:"preserved,omitempty"`
	SourceResults []PriceSyncSourceResult `json:"source_results,omitempty"`
	Prices        []ModelPrice            `json:"prices,omitempty"`
	Aliases       []ModelPriceAlias       `json:"aliases,omitempty"`
	Unpriced      []string                `json:"unpriced_models,omitempty"`
}

type remoteCatalogPrice struct {
	price  ModelPrice
	source string
	id     string
	// Canonical models.dev catalogs only allow official identity or an explicit provider ID.
	directOnly  bool
	officialIDs []string
}

// SyncModelPrices prefers models.dev, then LiteLLM and OpenRouter. Only
// identity matches are automatic; fuzzy matches require confirmation.
func (s *Store) SyncModelPrices(ctx context.Context, req PriceSyncRequest) (PriceSyncResult, error) {
	return s.syncModelPrices(ctx, req, &http.Client{Timeout: priceSyncHTTPTimeout})
}

func (s *Store) syncModelPrices(ctx context.Context, req PriceSyncRequest, client *http.Client) (PriceSyncResult, error) {
	if s == nil {
		return PriceSyncResult{}, fmt.Errorf("usagestore: nil store")
	}

	existing, err := s.LoadModelPrices(ctx)
	if err != nil {
		return PriceSyncResult{}, err
	}
	aliases, err := s.LoadModelPriceAliases(ctx)
	if err != nil {
		return PriceSyncResult{}, err
	}
	seenModels, err := s.ListDistinctModels(ctx, 0, 2000)
	if err != nil {
		return PriceSyncResult{}, err
	}
	models := normalizeModelList(req.Models)
	if len(models) == 0 {
		models = append(models, seenModels...)
		for model, price := range existing {
			if !isProtectedPriceSource(price.Source) {
				models = append(models, model)
			}
		}
		models = normalizeModelList(models)
		sort.Strings(models)
	}
	if len(models) == 0 {
		return PriceSyncResult{}, nil
	}
	catalog, sourceResults, sources, fetchSkipped := fetchPriceCatalogs(ctx, client)
	if len(sources) == 0 {
		return PriceSyncResult{SourceResults: sourceResults}, fmt.Errorf("price sync: all sources failed")
	}
	if err := ctx.Err(); err != nil {
		return PriceSyncResult{SourceResults: sourceResults}, err
	}
	aliasMap := AliasMap(aliases)
	available := make(map[string]bool, len(sources))
	for _, source := range sources {
		available[source] = true
	}

	result := PriceSyncResult{
		Sources:       sources,
		SourceResults: sourceResults,
		Skipped:       fetchSkipped,
	}

	toImport := make([]ModelPrice, 0)
	matched := make([]ModelPrice, 0)

	for _, modelID := range models {
		resolved, target, hasPrice := ResolvePrice([]string{modelID}, existing, aliasMap)
		if !req.OverrideManual {
			// Alias mappings are intentional, even when the target is currently missing.
			aliased := false
			for alias := range aliasMap {
				if strings.EqualFold(alias, modelID) {
					aliased = true
					break
				}
			}
			if aliased || (hasPrice && isProtectedPriceSource(resolved.Source)) {
				result.SkippedManual++
				continue
			}
		}
		price, _, ok := findAutomaticCatalogPrice(catalog, modelID)
		if hasPrice && !isProtectedPriceSource(resolved.Source) {
			source := strings.ToLower(strings.TrimSpace(resolved.Source))
			// Retain a last-known price when its source failed; an available higher-priority
			// source may still upgrade it, but a lower-priority fallback must not downgrade it.
			if !available[source] && (!ok || sourceRank(price.Source) >= sourceRank(source)) {
				result.Skipped++
				result.Preserved = append(result.Preserved, modelID)
				continue
			}
			// Previously confirmed mappings refresh by their source identity, not fuzzy guesses.
			if entry, found := catalog[source+"\x00"+resolved.SourceModelID]; found &&
				(!ok || sourceRank(entry.source) < sourceRank(price.Source)) {
				price, ok = entry.price, true
			}
		}
		if ok {
			price.Model = modelID
			// ResolvePrice also accepts case-insensitive direct IDs; refresh the same row.
			if hasPrice && strings.EqualFold(target, modelID) {
				price.Model = target
			}
			matched = append(matched, price)
			toImport = append(toImport, price)
			continue
		}

		// Fuzzy candidates only for models that still have no pricing.
		if _, _, ok := ResolvePrice([]string{modelID}, existing, aliasMap); ok {
			result.Skipped++
			continue
		}
		cands := findCatalogCandidates(catalog, modelID)
		if len(cands) > 0 {
			result.Candidates = append(result.Candidates, PriceSyncCandidateSet{
				Model:      modelID,
				Candidates: cands,
			})
			continue
		}
		result.Unmatched = append(result.Unmatched, modelID)
	}

	result.Matched = matched
	if req.ApplyMatched && len(toImport) > 0 {
		if err := s.UpsertModelPrices(ctx, toImport); err != nil {
			return PriceSyncResult{}, err
		}
		result.Imported = len(toImport)
	}

	// Build the response from the same book used for selection. No fallible reads
	// after commit: a response read failure must not disguise a successful write.
	pricesMap := existing
	if req.ApplyMatched {
		for _, p := range toImport {
			pricesMap[p.Model] = p
		}
	}
	list := make([]ModelPrice, 0, len(pricesMap))
	for _, p := range pricesMap {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Model < list[j].Model })
	result.Prices = list
	result.Aliases = aliases
	aliasMapOut := aliasMap
	unpriced := make([]string, 0)
	for _, m := range seenModels {
		if _, _, ok := ResolvePrice([]string{m}, pricesMap, aliasMapOut); !ok {
			unpriced = append(unpriced, m)
		}
	}
	result.Unpriced = unpriced
	return result, nil
}

func isProtectedPriceSource(source string) bool {
	s := strings.ToLower(strings.TrimSpace(source))
	return s == "" || s == "manual" || s == "override"
}

func normalizeModelList(models []string) []string {
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

func fetchPriceCatalogs(ctx context.Context, client *http.Client) (map[string]remoteCatalogPrice, []PriceSyncSourceResult, []string, int) {
	type job struct {
		source, url string
		fetch       func(context.Context, string, *http.Client) (map[string]remoteCatalogPrice, int, error)
	}
	jobs := []job{
		{SyncSourceModelsDev, defaultModelsDevURL, fetchModelsDevPrices},
		{SyncSourceLiteLLM, defaultLiteLLMURL, catalogFetcher(fetchLiteLLMPrices)},
		{SyncSourceOpenRouter, defaultOpenRouterURL, catalogFetcher(fetchOpenRouterPrices)},
	}
	catalog := make(map[string]remoteCatalogPrice)
	results := make([]PriceSyncSourceResult, 0, len(jobs))
	sources := make([]string, 0, len(jobs))
	totalSkipped := 0
	for _, j := range jobs {
		prices, skipped, err := j.fetch(ctx, j.url, client)
		if err == nil && len(prices) == 0 {
			err = fmt.Errorf("%s: no usable prices", j.source)
		}
		sr := PriceSyncSourceResult{Source: j.source, Skipped: skipped}
		totalSkipped += skipped
		if err != nil {
			sr.Error = err.Error()
		} else {
			sr.Models = len(prices)
			sources = append(sources, j.source)
			for id, entry := range prices {
				catalog[j.source+"\x00"+id] = entry
			}
		}
		results = append(results, sr)
	}
	return catalog, results, sources, totalSkipped
}

func catalogFetcher(fetch func(context.Context, string, *http.Client) (map[string]ModelPrice, int, error)) func(context.Context, string, *http.Client) (map[string]remoteCatalogPrice, int, error) {
	return func(ctx context.Context, url string, client *http.Client) (map[string]remoteCatalogPrice, int, error) {
		prices, skipped, err := fetch(ctx, url, client)
		catalog := make(map[string]remoteCatalogPrice, len(prices))
		for id, price := range prices {
			catalog[id] = remoteCatalogPrice{id: id, source: price.Source, price: price}
		}
		return catalog, skipped, err
	}
}

func fetchLiteLLMPrices(ctx context.Context, syncURL string, client *http.Client) (map[string]ModelPrice, int, error) {
	body, err := httpGetBody(ctx, client, syncURL)
	if err != nil {
		return nil, 0, err
	}
	var raw map[string]map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, 0, fmt.Errorf("litellm decode: %w", err)
	}
	out := make(map[string]ModelPrice)
	skipped := 0
	for modelID, entry := range raw {
		rates, ok := readSyncRates(entry, 1_000_000, liteLLMRateKeys)
		if strings.TrimSpace(modelID) == "" || !ok {
			skipped++
			continue
		}
		rawJSON, err := json.Marshal(entry)
		if err != nil {
			skipped++
			continue
		}
		out[modelID] = newSyncedPrice(modelID, SyncSourceLiteLLM, string(rawJSON), rates)
	}
	return out, skipped, nil
}

func fetchOpenRouterPrices(ctx context.Context, syncURL string, client *http.Client) (map[string]ModelPrice, int, error) {
	body, err := httpGetBody(ctx, client, syncURL)
	if err != nil {
		return nil, 0, err
	}
	var raw struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, 0, fmt.Errorf("openrouter decode: %w", err)
	}
	out := make(map[string]ModelPrice)
	skipped := 0
	for _, entry := range raw.Data {
		modelID := strings.TrimSpace(readString(entry, "id"))
		pricing, ok := entry["pricing"].(map[string]any)
		if modelID == "" || !ok {
			skipped++
			continue
		}
		rates, ok := readSyncRates(pricing, 1_000_000, openRouterRateKeys)
		if strings.TrimSpace(modelID) == "" || !ok {
			skipped++
			continue
		}
		rawJSON, err := json.Marshal(entry)
		if err != nil {
			skipped++
			continue
		}
		out[modelID] = newSyncedPrice(modelID, SyncSourceOpenRouter, string(rawJSON), rates)
	}
	return out, skipped, nil
}

func httpGetBody(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "CLIProxyAPI-usage-price-sync/1.0")
	if client == nil {
		client = &http.Client{Timeout: priceSyncHTTPTimeout}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("price sync fetch failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("price sync fetch failed: %s", res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxPriceSyncBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxPriceSyncBodyBytes {
		return nil, fmt.Errorf("price sync response exceeds %d bytes", maxPriceSyncBodyBytes)
	}
	return body, nil
}

func findAutomaticCatalogPrice(catalog map[string]remoteCatalogPrice, modelID string) (ModelPrice, string, bool) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ModelPrice{}, "", false
	}
	for _, source := range []string{SyncSourceModelsDev, SyncSourceLiteLLM, SyncSourceOpenRouter} {
		// Match within each source before falling back; another source's exact key
		// must not defeat an official models.dev identity.
		for _, reason := range []string{"models.dev-official", "exact", "case-insensitive", "provider-prefix"} {
			var found ModelPrice
			count := 0
			for _, e := range catalog {
				if e.source != source {
					continue
				}
				match := false
				switch reason {
				case "models.dev-official":
					for _, id := range e.officialIDs {
						if strings.EqualFold(id, modelID) {
							match = true
							break
						}
					}
				case "exact":
					match = e.id == modelID
				case "case-insensitive":
					match = strings.EqualFold(e.id, modelID)
				case "provider-prefix":
					// Punctuation/name normalization is fuzzy and is never auto-applied.
					match = !e.directOnly && strings.EqualFold(lastModelSegment(e.id), lastModelSegment(modelID))
				}
				if match {
					found = e.price
					count++
				}
			}
			if count == 1 {
				return found, reason, true
			}
			if count > 1 {
				break
			}
		}
	}
	return ModelPrice{}, "", false
}

func sourceRank(source string) int {
	switch source {
	case SyncSourceModelsDev:
		return 0
	case SyncSourceLiteLLM:
		return 1
	case SyncSourceOpenRouter:
		return 2
	}
	return 3
}

func findCatalogCandidates(catalog map[string]remoteCatalogPrice, modelID string) []PriceSyncCandidate {
	candidates := make([]PriceSyncCandidate, 0)
	for _, entry := range catalog {
		score, reason := modelSimilarity(modelID, entry.id)
		if score < minCandidateScore && !(score >= minWeakCandidateScore && reason == "same-model-family") {
			continue
		}
		candidates = append(candidates, PriceSyncCandidate{SourceModelID: entry.id, Score: math.Round(score*100) / 100, Reason: reason, Price: entry.price})
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Price.Source != b.Price.Source {
			return sourceRank(a.Price.Source) < sourceRank(b.Price.Source)
		}
		return a.SourceModelID < b.SourceModelID
	})
	// Limit per source, not globally: one catalog must not crowd out another.
	counts := map[string]int{}
	out := make([]PriceSyncCandidate, 0, len(candidates))
	for _, c := range candidates {
		if counts[c.Price.Source] >= maxSyncCandidates {
			continue
		}
		counts[c.Price.Source]++
		out = append(out, c)
	}
	return out
}

func modelSimilarity(left, right string) (float64, string) {
	leftTail := canonicalModelTail(left)
	rightTail := canonicalModelTail(right)
	if leftTail != "" && rightTail != "" {
		if leftTail == rightTail {
			return 0.94, "same-model-with-provider-prefix"
		}
		if strings.Contains(leftTail, rightTail) || strings.Contains(rightTail, leftTail) {
			return 0.78, "model-name-contains"
		}
	}
	leftCanonical := canonicalModelID(left)
	rightCanonical := canonicalModelID(right)
	if leftCanonical != "" && rightCanonical != "" {
		if leftCanonical == rightCanonical {
			return 0.9, "normalized-model-name"
		}
		if strings.Contains(leftCanonical, rightCanonical) || strings.Contains(rightCanonical, leftCanonical) {
			return 0.74, "normalized-name-contains"
		}
	}
	leftTokens := modelTokens(left)
	rightTokens := modelTokens(right)
	tokenScore := tokenJaccard(leftTokens, rightTokens)
	if tokenScore >= 0.65 {
		return math.Max(tokenScore*0.86, 0.72), "shared-model-tokens"
	}
	if tokenScore >= 0.4 {
		return math.Max(tokenScore*0.86, 0.58), "shared-model-tokens"
	}
	if sameModelFamily(leftTokens, rightTokens) {
		return 0.46, "same-model-family"
	}
	return tokenScore, "weak-similarity"
}

func canonicalModelID(value string) string {
	return strings.Join(modelTokens(value), "")
}

func canonicalModelTail(value string) string {
	return strings.Join(modelTokens(lastModelSegment(value)), "")
}

func lastModelSegment(value string) string {
	parts := strings.Split(strings.TrimSpace(value), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		part := strings.TrimSpace(parts[i])
		if part == "" || strings.EqualFold(part, "models") {
			continue
		}
		return part
	}
	return strings.TrimSpace(value)
}

func modelTokens(value string) []string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	tokens := make([]string, 0, 8)
	var builder strings.Builder
	flush := func() {
		if builder.Len() == 0 {
			return
		}
		token := builder.String()
		if token != "models" {
			tokens = append(tokens, token)
		}
		builder.Reset()
	}
	for _, r := range normalized {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func tokenJaccard(left, right []string) float64 {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	set := make(map[string]struct{}, len(left))
	for _, t := range left {
		set[t] = struct{}{}
	}
	inter := 0
	for _, t := range right {
		if _, ok := set[t]; ok {
			inter++
		}
	}
	union := len(left) + len(right) - inter
	if union <= 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func sameModelFamily(left, right []string) bool {
	families := []string{"gpt", "claude", "gemini", "grok", "llama", "mistral", "deepseek", "qwen", "o1", "o3", "o4"}
	has := func(tokens []string, fam string) bool {
		for _, t := range tokens {
			if t == fam || strings.HasPrefix(t, fam) {
				return true
			}
		}
		return false
	}
	for _, fam := range families {
		if has(left, fam) && has(right, fam) {
			return true
		}
	}
	return false
}

func readFloat(m map[string]any, key string) (float64, bool) {
	var value float64
	var err error
	switch n := m[key].(type) {
	case float64:
		value = n
	case json.Number:
		value, err = n.Float64()
	case string:
		value, err = strconv.ParseFloat(strings.TrimSpace(n), 64)
	default:
		return 0, false
	}
	return value, err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func readString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
