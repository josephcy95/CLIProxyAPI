package usagestore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	SyncSourceLiteLLM    = "litellm"
	SyncSourceOpenRouter = "openrouter"

	defaultLiteLLMURL    = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
	defaultOpenRouterURL = "https://openrouter.ai/api/v1/models"

	maxSyncCandidates      = 8
	minCandidateScore      = 0.55
	minWeakCandidateScore  = 0.34
	priceSyncHTTPTimeout   = 45 * time.Second
	maxPriceSyncBodyBytes  = 32 << 20
)

// PriceSyncRequest selects which local models to match against remote catalogs.
// Empty Models matches all catalog entries for import preview of exact IDs only when
// AutoImportAll is true; otherwise models are taken from recent usage.
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
	SourceResults []PriceSyncSourceResult `json:"source_results,omitempty"`
	Prices        []ModelPrice            `json:"prices,omitempty"`
	Aliases       []ModelPriceAlias       `json:"aliases,omitempty"`
	Unpriced      []string                `json:"unpriced_models,omitempty"`
}

type remoteCatalogPrice struct {
	price  ModelPrice
	source string
	id     string
}

// SyncModelPrices fetches LiteLLM + OpenRouter catalogs, auto-applies exact
// (and unique normalized) matches, and returns fuzzy candidates for confirmation.
func (s *Store) SyncModelPrices(ctx context.Context, req PriceSyncRequest) (PriceSyncResult, error) {
	if s == nil {
		return PriceSyncResult{}, fmt.Errorf("usagestore: nil store")
	}

	models := normalizeModelList(req.Models)
	if len(models) == 0 {
		fromMS := time.Now().AddDate(0, 0, -30).UnixMilli()
		seen, err := s.ListDistinctModels(ctx, fromMS, 500)
		if err != nil {
			return PriceSyncResult{}, err
		}
		models = seen
	}
	if len(models) == 0 {
		return PriceSyncResult{Unmatched: nil}, nil
	}

	client := &http.Client{Timeout: priceSyncHTTPTimeout}
	catalog, sourceResults, sources, fetchSkipped := fetchPriceCatalogs(ctx, client)

	existing, err := s.LoadModelPrices(ctx)
	if err != nil {
		return PriceSyncResult{}, err
	}

	result := PriceSyncResult{
		Sources:       sources,
		SourceResults: sourceResults,
		Skipped:       fetchSkipped,
	}

	toImport := make([]ModelPrice, 0)
	matched := make([]ModelPrice, 0)

	for _, modelID := range models {
		if price, reason, ok := findAutomaticCatalogPrice(catalog, modelID); ok {
			existingPrice, hasExisting := existing[modelID]
			if hasExisting && strings.EqualFold(strings.TrimSpace(existingPrice.Source), "manual") && !req.OverrideManual {
				result.SkippedManual++
				continue
			}
			// Keep catalog source (litellm/openrouter); store under local model id.
			price.Model = modelID
			if price.Source == "" {
				price.Source = SyncSourceLiteLLM
			}
			_ = reason
			matched = append(matched, price)
			toImport = append(toImport, price)
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

	// Refresh book for response.
	pricesMap, err := s.LoadModelPrices(ctx)
	if err != nil {
		return PriceSyncResult{}, err
	}
	aliases, err := s.LoadModelPriceAliases(ctx)
	if err != nil {
		return PriceSyncResult{}, err
	}
	list := make([]ModelPrice, 0, len(pricesMap))
	for _, p := range pricesMap {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Model < list[j].Model })
	result.Prices = list
	result.Aliases = aliases

	aliasMap := AliasMap(aliases)
	fromMS := time.Now().AddDate(0, 0, -30).UnixMilli()
	recent, _ := s.ListDistinctModels(ctx, fromMS, 200)
	unpriced := make([]string, 0)
	for _, m := range recent {
		if _, _, ok := ResolvePrice([]string{m}, pricesMap, aliasMap); !ok {
			unpriced = append(unpriced, m)
		}
	}
	result.Unpriced = unpriced
	return result, nil
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
		source string
		url    string
		fetch  func(context.Context, string, *http.Client) (map[string]ModelPrice, int, error)
	}
	jobs := []job{
		{SyncSourceLiteLLM, defaultLiteLLMURL, fetchLiteLLMPrices},
		{SyncSourceOpenRouter, defaultOpenRouterURL, fetchOpenRouterPrices},
	}

	catalog := make(map[string]remoteCatalogPrice)
	// Prefer LiteLLM when both define the same id (process in order).
	results := make([]PriceSyncSourceResult, 0, len(jobs))
	sources := make([]string, 0, len(jobs))
	totalSkipped := 0

	for _, j := range jobs {
		sr := PriceSyncSourceResult{Source: j.source}
		prices, skipped, err := j.fetch(ctx, j.url, client)
		sr.Skipped = skipped
		if err != nil {
			sr.Error = err.Error()
			results = append(results, sr)
			continue
		}
		sr.Models = len(prices)
		results = append(results, sr)
		sources = append(sources, j.source)
		totalSkipped += skipped
		for id, p := range prices {
			if _, exists := catalog[id]; exists {
				continue
			}
			if p.Source == "" {
				p.Source = j.source
			}
			catalog[id] = remoteCatalogPrice{price: p, source: j.source, id: id}
		}
	}
	return catalog, results, sources, totalSkipped
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
	now := time.Now().UnixMilli()
	out := make(map[string]ModelPrice)
	skipped := 0
	for modelID, entry := range raw {
		prompt, hasPrompt := readFloat(entry, "input_cost_per_token")
		completion, hasCompletion := readFloat(entry, "output_cost_per_token")
		cacheRead, hasCacheRead := readFirstFloat(entry, "cache_read_input_token_cost", "input_cache_read")
		cacheCreation, hasCacheCreation := readFirstFloat(entry,
			"cache_creation_input_token_cost", "cache_write_input_token_cost", "input_cache_write", "input_cache_creation")
		if !hasPrompt && !hasCompletion && !hasCacheRead && !hasCacheCreation {
			skipped++
			continue
		}
		out[modelID] = ModelPrice{
			Model:              modelID,
			PromptPer1M:        prompt * 1_000_000,
			CompletionPer1M:    completion * 1_000_000,
			CachePer1M:         cacheRead * 1_000_000,
			CacheReadPer1M:     cacheRead * 1_000_000,
			CacheCreationPer1M: cacheCreation * 1_000_000,
			Source:             SyncSourceLiteLLM,
			UpdatedAtMS:        now,
		}
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
	now := time.Now().UnixMilli()
	out := make(map[string]ModelPrice)
	skipped := 0
	for _, entry := range raw.Data {
		modelID := strings.TrimSpace(readString(entry, "id"))
		pricing, ok := entry["pricing"].(map[string]any)
		if modelID == "" || !ok {
			skipped++
			continue
		}
		prompt, hasPrompt := readFloat(pricing, "prompt")
		completion, hasCompletion := readFloat(pricing, "completion")
		cacheRead, hasCacheRead := readFirstFloat(pricing, "input_cache_read", "cache_read_input_token_cost")
		cacheCreation, hasCacheCreation := readFirstFloat(pricing,
			"input_cache_write", "input_cache_creation", "cache_creation_input_token_cost", "cache_write_input_token_cost")
		if !hasPrompt && !hasCompletion && !hasCacheRead && !hasCacheCreation {
			skipped++
			continue
		}
		out[modelID] = ModelPrice{
			Model:              modelID,
			PromptPer1M:        prompt * 1_000_000,
			CompletionPer1M:    completion * 1_000_000,
			CachePer1M:         cacheRead * 1_000_000,
			CacheReadPer1M:     cacheRead * 1_000_000,
			CacheCreationPer1M: cacheCreation * 1_000_000,
			Source:             SyncSourceOpenRouter,
			UpdatedAtMS:        now,
		}
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
	limited := io.LimitReader(res.Body, maxPriceSyncBodyBytes)
	return io.ReadAll(limited)
}

func findAutomaticCatalogPrice(catalog map[string]remoteCatalogPrice, modelID string) (ModelPrice, string, bool) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ModelPrice{}, "", false
	}
	if entry, ok := catalog[modelID]; ok {
		return entry.price, "exact", true
	}
	keys := sortedCatalogKeys(catalog)
	if key, ok := uniqueMatch(keys, func(key string) bool {
		return strings.EqualFold(key, modelID)
	}); ok {
		return catalog[key].price, "case-insensitive", true
	}
	modelTail := canonicalModelTail(modelID)
	if modelTail != "" {
		if key, ok := uniqueMatch(keys, func(key string) bool {
			return canonicalModelTail(key) == modelTail
		}); ok {
			p := catalog[key].price
			return p, "provider-prefix", true
		}
	}
	modelCanonical := canonicalModelID(modelID)
	if modelCanonical != "" {
		if key, ok := uniqueMatch(keys, func(key string) bool {
			return canonicalModelID(key) == modelCanonical
		}); ok {
			return catalog[key].price, "normalized", true
		}
	}
	return ModelPrice{}, "", false
}

func findCatalogCandidates(catalog map[string]remoteCatalogPrice, modelID string) []PriceSyncCandidate {
	candidates := make([]PriceSyncCandidate, 0, maxSyncCandidates)
	for _, key := range sortedCatalogKeys(catalog) {
		score, reason := modelSimilarity(modelID, key)
		if score < minCandidateScore && !(score >= minWeakCandidateScore && reason == "same-model-family") {
			continue
		}
		entry := catalog[key]
		candidates = append(candidates, PriceSyncCandidate{
			SourceModelID: key,
			Score:         math.Round(score*100) / 100,
			Reason:        reason,
			Price:         entry.price,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].SourceModelID < candidates[j].SourceModelID
		}
		return candidates[i].Score > candidates[j].Score
	})
	if len(candidates) > maxSyncCandidates {
		return candidates[:maxSyncCandidates]
	}
	return candidates
}

func sortedCatalogKeys(catalog map[string]remoteCatalogPrice) []string {
	keys := make([]string, 0, len(catalog))
	for k := range catalog {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func uniqueMatch(keys []string, match func(string) bool) (string, bool) {
	matchedKey := ""
	for _, key := range keys {
		if !match(key) {
			continue
		}
		if matchedKey != "" {
			return "", false
		}
		matchedKey = key
	}
	return matchedKey, matchedKey != ""
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
	v, ok := m[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		var f float64
		_, err := fmt.Sscanf(strings.TrimSpace(n), "%f", &f)
		return f, err == nil
	default:
		return 0, false
	}
}

func readFirstFloat(m map[string]any, keys ...string) (float64, bool) {
	for _, k := range keys {
		if f, ok := readFloat(m, k); ok {
			return f, true
		}
	}
	return 0, false
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
