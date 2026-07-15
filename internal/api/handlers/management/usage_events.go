package management

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usagestore"
)

// SetUsageStore attaches the durable usage store used by monitoring endpoints.
func (h *Handler) SetUsageStore(store *usagestore.Store) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.usageStore = store
	h.mu.Unlock()
}

// UsageStore returns the current usage store (may be nil).
func (h *Handler) UsageStore() *usagestore.Store {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.usageStore
}

func (h *Handler) requireUsageStore(c *gin.Context) *usagestore.Store {
	store := h.UsageStore()
	if store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "usage store unavailable; enable usage-statistics-enabled and ensure usage-store-path is writable",
		})
		return nil
	}
	return store
}

type usageQueryBody struct {
	FromMS       int64    `json:"from_ms"`
	ToMS         int64    `json:"to_ms"`
	Search       string   `json:"search"`
	Models       []string `json:"models"`
	Providers    []string `json:"providers"`
	AuthIndices  []string `json:"auth_indices"`
	Sources      []string `json:"sources"`
	APIKeyHashes []string `json:"api_key_hashes"`
	FailedOnly   bool     `json:"failed_only"`
	SuccessOnly  bool     `json:"success_only"`
	Limit        int      `json:"limit"`
	BeforeID     int64    `json:"before_id"`
}

func parseUsageFilter(c *gin.Context, body *usageQueryBody) usagestore.QueryFilter {
	filter := usagestore.QueryFilter{}
	if body != nil {
		filter.FromMS = body.FromMS
		filter.ToMS = body.ToMS
		filter.Search = body.Search
		filter.Models = body.Models
		filter.Providers = body.Providers
		filter.AuthIndices = body.AuthIndices
		filter.Sources = body.Sources
		filter.APIKeyHashes = body.APIKeyHashes
		filter.FailedOnly = body.FailedOnly
		filter.SuccessOnly = body.SuccessOnly
		filter.Limit = body.Limit
		filter.BeforeID = body.BeforeID
	}
	// Query string overrides for GET convenience.
	if v := c.Query("from_ms"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.FromMS = n
		}
	}
	if v := c.Query("to_ms"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.ToMS = n
		}
	}
	if v := c.Query("search"); v != "" {
		filter.Search = v
	}
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.Limit = n
		}
	}
	if v := c.Query("before_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.BeforeID = n
		}
	}
	if v := c.Query("failed_only"); v == "1" || strings.EqualFold(v, "true") {
		filter.FailedOnly = true
	}
	if v := c.Query("success_only"); v == "1" || strings.EqualFold(v, "true") {
		filter.SuccessOnly = true
	}
	if v := c.Query("models"); v != "" {
		filter.Models = splitCSV(v)
	}
	if v := c.Query("providers"); v != "" {
		filter.Providers = splitCSV(v)
	}
	if v := c.Query("auth_indices"); v != "" {
		filter.AuthIndices = splitCSV(v)
	}
	if v := c.Query("sources"); v != "" {
		filter.Sources = splitCSV(v)
	}
	if v := c.Query("api_key_hashes"); v != "" {
		filter.APIKeyHashes = splitCSV(v)
	}
	return filter
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (h *Handler) pricingMaps(c *gin.Context, store *usagestore.Store) (map[string]usagestore.ModelPrice, map[string]string, bool) {
	prices, err := store.LoadModelPrices(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return nil, nil, false
	}
	aliases, err := store.LoadModelPriceAliases(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return nil, nil, false
	}
	return prices, usagestore.AliasMap(aliases), true
}

// GetUsageEvents lists durable usage events (newest first).
func (h *Handler) GetUsageEvents(c *gin.Context) {
	store := h.requireUsageStore(c)
	if store == nil {
		return
	}
	var body usageQueryBody
	if c.Request.Method == http.MethodPost {
		_ = c.ShouldBindJSON(&body)
	}
	filter := parseUsageFilter(c, &body)
	events, err := store.ListEvents(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	prices, aliases, ok := h.pricingMaps(c, store)
	if !ok {
		return
	}
	usagestore.AttachEventCosts(events, prices, aliases)
	var nextBeforeID int64
	if len(events) > 0 {
		nextBeforeID = events[len(events)-1].ID
	}
	c.JSON(http.StatusOK, gin.H{
		"events":         events,
		"next_before_id": nextBeforeID,
		"generated_at_ms": time.Now().UnixMilli(),
		"store_path":     store.Path(),
	})
}

// GetUsageSummary returns aggregate metrics for a range.
func (h *Handler) GetUsageSummary(c *gin.Context) {
	store := h.requireUsageStore(c)
	if store == nil {
		return
	}
	var body usageQueryBody
	if c.Request.Method == http.MethodPost {
		_ = c.ShouldBindJSON(&body)
	}
	filter := parseUsageFilter(c, &body)
	summary, err := store.GetSummary(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Cost estimate: scan events with a soft cap for large ranges.
	costFilter := filter
	costFilter.BeforeID = 0
	costFilter.Limit = 5000
	events, err := store.ListEvents(c.Request.Context(), costFilter)
	if err == nil {
		prices, aliases, ok := h.pricingMaps(c, store)
		if ok {
			total, priced := usagestore.AttachEventCosts(events, prices, aliases)
			summary.EstimatedCost = total
			summary.PricedCalls = priced
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"summary":         summary,
		"generated_at_ms": time.Now().UnixMilli(),
		"usage_statistics_enabled": h.cfg != nil && h.cfg.UsageStatisticsEnabled,
	})
}

// GetUsageFilterOptions returns distinct filter values.
func (h *Handler) GetUsageFilterOptions(c *gin.Context) {
	store := h.requireUsageStore(c)
	if store == nil {
		return
	}
	var body usageQueryBody
	if c.Request.Method == http.MethodPost {
		_ = c.ShouldBindJSON(&body)
	}
	filter := parseUsageFilter(c, &body)
	opts, err := store.GetFilterOptions(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, opts)
}

// GetUsageAccountStats returns per-account aggregates.
func (h *Handler) GetUsageAccountStats(c *gin.Context) {
	store := h.requireUsageStore(c)
	if store == nil {
		return
	}
	var body usageQueryBody
	if c.Request.Method == http.MethodPost {
		_ = c.ShouldBindJSON(&body)
	}
	filter := parseUsageFilter(c, &body)
	limit := 100
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	stats, err := store.GetAccountStats(c.Request.Context(), filter, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	prices, aliases, ok := h.pricingMaps(c, store)
	if !ok {
		return
	}
	// Approximate account cost from recent events per group is expensive; compute from listed events.
	costFilter := filter
	costFilter.Limit = 5000
	events, err := store.ListEvents(c.Request.Context(), costFilter)
	if err == nil {
		usagestore.AttachEventCosts(events, prices, aliases)
		costByKey := map[string]float64{}
		for _, e := range events {
			if e.EstimatedCost == nil {
				continue
			}
			key := e.AuthIndex + "\x00" + e.Source + "\x00" + e.SourceHash + "\x00" + e.Provider
			costByKey[key] += *e.EstimatedCost
		}
		for i := range stats {
			key := stats[i].AuthIndex + "\x00" + stats[i].Source + "\x00" + stats[i].SourceHash + "\x00" + stats[i].Provider
			stats[i].EstimatedCost = costByKey[key]
		}
	}
	c.JSON(http.StatusOK, gin.H{"accounts": stats, "generated_at_ms": time.Now().UnixMilli()})
}

// GetModelPrices returns price book + aliases + unpriced models helper.
func (h *Handler) GetModelPrices(c *gin.Context) {
	store := h.requireUsageStore(c)
	if store == nil {
		return
	}
	prices, err := store.LoadModelPrices(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	aliases, err := store.LoadModelPriceAliases(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	fromMS := time.Now().AddDate(0, 0, -30).UnixMilli()
	models, _ := store.ListDistinctModels(c.Request.Context(), fromMS, 200)
	aliasMap := usagestore.AliasMap(aliases)
	unpriced := make([]string, 0)
	for _, m := range models {
		if _, _, ok := usagestore.ResolvePrice([]string{m}, prices, aliasMap); !ok {
			unpriced = append(unpriced, m)
		}
	}
	list := make([]usagestore.ModelPrice, 0, len(prices))
	for _, p := range prices {
		list = append(list, p)
	}
	c.JSON(http.StatusOK, gin.H{
		"prices":          list,
		"aliases":         aliases,
		"unpriced_models": unpriced,
		"store_path":      store.Path(),
	})
}

// PutModelPrices upserts model prices. Body: { "prices": [...], "replace": false }
func (h *Handler) PutModelPrices(c *gin.Context) {
	store := h.requireUsageStore(c)
	if store == nil {
		return
	}
	var body struct {
		Prices  []usagestore.ModelPrice `json:"prices"`
		Replace bool                    `json:"replace"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	var err error
	if body.Replace {
		err = store.ReplaceModelPrices(c.Request.Context(), body.Prices)
	} else {
		err = store.UpsertModelPrices(c.Request.Context(), body.Prices)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "count": len(body.Prices)})
}

// PutModelPriceAliases upserts aliases. Body: { "aliases": [...] }
func (h *Handler) PutModelPriceAliases(c *gin.Context) {
	store := h.requireUsageStore(c)
	if store == nil {
		return
	}
	var body struct {
		Aliases []usagestore.ModelPriceAlias `json:"aliases"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if err := store.UpsertModelPriceAliases(c.Request.Context(), body.Aliases); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "count": len(body.Aliases)})
}

// DeleteModelPriceAlias deletes one alias: ?alias=
func (h *Handler) DeleteModelPriceAlias(c *gin.Context) {
	store := h.requireUsageStore(c)
	if store == nil {
		return
	}
	alias := strings.TrimSpace(c.Query("alias"))
	if alias == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "alias required"})
		return
	}
	if err := store.DeleteModelPriceAlias(c.Request.Context(), alias); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteModelPrice deletes one price: ?model=
func (h *Handler) DeleteModelPrice(c *gin.Context) {
	store := h.requireUsageStore(c)
	if store == nil {
		return
	}
	model := strings.TrimSpace(c.Query("model"))
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model required"})
		return
	}
	if err := store.DeleteModelPrice(c.Request.Context(), model); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
