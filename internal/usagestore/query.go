package usagestore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// QueryFilter selects usage events for list/summary APIs.
type QueryFilter struct {
	FromMS       int64
	ToMS         int64
	Search       string
	Models       []string
	Providers    []string
	AuthIndices  []string
	Sources      []string
	APIKeyHashes []string
	FailedOnly   bool
	SuccessOnly  bool
	Limit        int
	BeforeID     int64
}

// Summary is aggregate metrics for a filtered range.
type Summary struct {
	TotalCalls          int64   `json:"total_calls"`
	SuccessCalls        int64   `json:"success_calls"`
	FailureCalls        int64   `json:"failure_calls"`
	SuccessRate         float64 `json:"success_rate"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	ReasoningTokens     int64   `json:"reasoning_tokens"`
	CachedTokens        int64   `json:"cached_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	TotalTokens         int64   `json:"total_tokens"`
	AvgLatencyMS        float64 `json:"avg_latency_ms"`
	AvgTTFTMS           float64 `json:"avg_ttft_ms"`
	EstimatedCost       float64 `json:"estimated_cost"`
	PricedCalls         int64   `json:"priced_calls"`
}

// AccountStat aggregates usage by auth/source.
type AccountStat struct {
	AuthIndex     string  `json:"auth_index,omitempty"`
	Source        string  `json:"source,omitempty"`
	SourceHash    string  `json:"source_hash,omitempty"`
	Provider      string  `json:"provider,omitempty"`
	TotalCalls    int64   `json:"total_calls"`
	SuccessCalls  int64   `json:"success_calls"`
	FailureCalls  int64   `json:"failure_calls"`
	TotalTokens   int64   `json:"total_tokens"`
	InputTokens   int64   `json:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens"`
	EstimatedCost float64 `json:"estimated_cost"`
}

// FilterOptions lists distinct filter values in range.
type FilterOptions struct {
	Models       []string `json:"models"`
	Providers    []string `json:"providers"`
	AuthIndices  []string `json:"auth_indices"`
	Sources      []string `json:"sources"`
	APIKeyHashes []string `json:"api_key_hashes"`
}

func (f QueryFilter) normalize() QueryFilter {
	if f.Limit <= 0 {
		f.Limit = 100
	}
	if f.Limit > 1000 {
		f.Limit = 1000
	}
	return f
}

func buildWhere(f QueryFilter) (string, []any) {
	clauses := make([]string, 0, 12)
	args := make([]any, 0, 16)
	if f.FromMS > 0 {
		clauses = append(clauses, "timestamp_ms >= ?")
		args = append(args, f.FromMS)
	}
	if f.ToMS > 0 {
		clauses = append(clauses, "timestamp_ms <= ?")
		args = append(args, f.ToMS)
	}
	if f.BeforeID > 0 {
		clauses = append(clauses, "id < ?")
		args = append(args, f.BeforeID)
	}
	if f.FailedOnly {
		clauses = append(clauses, "failed = 1")
	} else if f.SuccessOnly {
		clauses = append(clauses, "failed = 0")
	}
	appendIn := func(column string, values []string) {
		clean := make([]string, 0, len(values))
		for _, v := range values {
			v = strings.TrimSpace(v)
			if v != "" {
				clean = append(clean, v)
			}
		}
		if len(clean) == 0 {
			return
		}
		holders := make([]string, len(clean))
		for i, v := range clean {
			holders[i] = "?"
			args = append(args, v)
		}
		clauses = append(clauses, fmt.Sprintf("%s IN (%s)", column, strings.Join(holders, ",")))
	}
	appendIn("model", f.Models)
	appendIn("provider", f.Providers)
	appendIn("auth_index", f.AuthIndices)
	appendIn("source", f.Sources)
	appendIn("api_key_hash", f.APIKeyHashes)

	search := strings.TrimSpace(f.Search)
	if search != "" {
		like := "%" + search + "%"
		clauses = append(clauses, `(
			IFNULL(model,'') LIKE ? OR IFNULL(alias,'') LIKE ? OR IFNULL(provider,'') LIKE ? OR
			IFNULL(source,'') LIKE ? OR IFNULL(auth_index,'') LIKE ? OR IFNULL(api_key_hash,'') LIKE ? OR
			IFNULL(endpoint,'') LIKE ? OR IFNULL(request_id,'') LIKE ? OR IFNULL(reasoning_effort,'') LIKE ?
		)`)
		for i := 0; i < 9; i++ {
			args = append(args, like)
		}
	}
	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

// ListEvents returns newest-first events matching the filter.
func (s *Store) ListEvents(ctx context.Context, filter QueryFilter) ([]Event, error) {
	if s == nil {
		return nil, fmt.Errorf("usagestore: nil store")
	}
	filter = filter.normalize()
	where, args := buildWhere(filter)
	query := `SELECT id, timestamp_ms, IFNULL(request_id,''), IFNULL(provider,''), IFNULL(executor_type,''),
		IFNULL(model,''), IFNULL(alias,''), IFNULL(endpoint,''), IFNULL(auth_type,''), IFNULL(auth_index,''),
		IFNULL(source,''), IFNULL(source_hash,''), IFNULL(api_key_hash,''), IFNULL(reasoning_effort,''),
		IFNULL(service_tier,''), IFNULL(response_service_tier,''),
		input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens,
		latency_ms, ttft_ms, failed, IFNULL(fail_status_code,0), IFNULL(fail_summary,''), created_at_ms
		FROM usage_events ` + where + ` ORDER BY id DESC LIMIT ?`
	args = append(args, filter.Limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Event, 0, filter.Limit)
	for rows.Next() {
		var e Event
		var latency, ttft sql.NullInt64
		var failed int
		if err := rows.Scan(
			&e.ID, &e.TimestampMS, &e.RequestID, &e.Provider, &e.ExecutorType,
			&e.Model, &e.Alias, &e.Endpoint, &e.AuthType, &e.AuthIndex,
			&e.Source, &e.SourceHash, &e.APIKeyHash, &e.ReasoningEffort,
			&e.ServiceTier, &e.ResponseServiceTier,
			&e.InputTokens, &e.OutputTokens, &e.ReasoningTokens, &e.CachedTokens,
			&e.CacheReadTokens, &e.CacheCreationTokens, &e.TotalTokens,
			&latency, &ttft, &failed, &e.FailStatusCode, &e.FailSummary, &e.CreatedAtMS,
		); err != nil {
			return nil, err
		}
		if latency.Valid {
			v := latency.Int64
			e.LatencyMS = &v
		}
		if ttft.Valid {
			v := ttft.Int64
			e.TTFTMS = &v
		}
		e.Failed = failed != 0
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetSummary aggregates metrics for the filter.
func (s *Store) GetSummary(ctx context.Context, filter QueryFilter) (Summary, error) {
	var summary Summary
	if s == nil {
		return summary, fmt.Errorf("usagestore: nil store")
	}
	// Summary ignores pagination cursor/limit.
	filter.BeforeID = 0
	filter.Limit = 0
	where, args := buildWhere(filter)
	query := `SELECT
		COUNT(*),
		SUM(CASE WHEN failed = 0 THEN 1 ELSE 0 END),
		SUM(CASE WHEN failed = 1 THEN 1 ELSE 0 END),
		IFNULL(SUM(input_tokens),0),
		IFNULL(SUM(output_tokens),0),
		IFNULL(SUM(reasoning_tokens),0),
		IFNULL(SUM(cached_tokens),0),
		IFNULL(SUM(cache_read_tokens),0),
		IFNULL(SUM(cache_creation_tokens),0),
		IFNULL(SUM(total_tokens),0),
		IFNULL(AVG(latency_ms),0),
		IFNULL(AVG(ttft_ms),0)
		FROM usage_events ` + where
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&summary.TotalCalls, &summary.SuccessCalls, &summary.FailureCalls,
		&summary.InputTokens, &summary.OutputTokens, &summary.ReasoningTokens,
		&summary.CachedTokens, &summary.CacheReadTokens, &summary.CacheCreationTokens,
		&summary.TotalTokens, &summary.AvgLatencyMS, &summary.AvgTTFTMS,
	)
	if err != nil {
		return summary, err
	}
	if summary.TotalCalls > 0 {
		summary.SuccessRate = float64(summary.SuccessCalls) / float64(summary.TotalCalls)
	}
	return summary, nil
}

// GetAccountStats groups usage by auth_index/source.
func (s *Store) GetAccountStats(ctx context.Context, filter QueryFilter, limit int) ([]AccountStat, error) {
	if s == nil {
		return nil, fmt.Errorf("usagestore: nil store")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	filter.BeforeID = 0
	filter.Limit = 0
	where, args := buildWhere(filter)
	query := `SELECT
		IFNULL(auth_index,''), IFNULL(source,''), IFNULL(source_hash,''), IFNULL(provider,''),
		COUNT(*),
		SUM(CASE WHEN failed = 0 THEN 1 ELSE 0 END),
		SUM(CASE WHEN failed = 1 THEN 1 ELSE 0 END),
		IFNULL(SUM(total_tokens),0),
		IFNULL(SUM(input_tokens),0),
		IFNULL(SUM(output_tokens),0)
		FROM usage_events ` + where + `
		GROUP BY auth_index, source, source_hash, provider
		ORDER BY COUNT(*) DESC
		LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AccountStat, 0, limit)
	for rows.Next() {
		var st AccountStat
		if err := rows.Scan(
			&st.AuthIndex, &st.Source, &st.SourceHash, &st.Provider,
			&st.TotalCalls, &st.SuccessCalls, &st.FailureCalls,
			&st.TotalTokens, &st.InputTokens, &st.OutputTokens,
		); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// GetFilterOptions returns distinct filter values for dropdowns.
func (s *Store) GetFilterOptions(ctx context.Context, filter QueryFilter) (FilterOptions, error) {
	var out FilterOptions
	if s == nil {
		return out, fmt.Errorf("usagestore: nil store")
	}
	filter.BeforeID = 0
	filter.Limit = 0
	// Drop facet-specific filters so options stay useful.
	base := filter
	base.Models = nil
	base.Providers = nil
	base.AuthIndices = nil
	base.Sources = nil
	base.APIKeyHashes = nil
	base.FailedOnly = false
	base.SuccessOnly = false
	base.Search = ""

	load := func(column string) ([]string, error) {
		where, args := buildWhere(base)
		var query string
		if where == "" {
			query = fmt.Sprintf(`SELECT DISTINCT %s FROM usage_events WHERE IFNULL(%s,'') <> '' ORDER BY %s LIMIT 200`, column, column, column)
		} else {
			query = fmt.Sprintf(`SELECT DISTINCT %s FROM usage_events %s AND IFNULL(%s,'') <> '' ORDER BY %s LIMIT 200`, column, where, column, column)
		}
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		values := make([]string, 0, 32)
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				return nil, err
			}
			values = append(values, v)
		}
		return values, rows.Err()
	}

	var err error
	if out.Models, err = load("model"); err != nil {
		return out, err
	}
	if out.Providers, err = load("provider"); err != nil {
		return out, err
	}
	if out.AuthIndices, err = load("auth_index"); err != nil {
		return out, err
	}
	if out.Sources, err = load("source"); err != nil {
		return out, err
	}
	if out.APIKeyHashes, err = load("api_key_hash"); err != nil {
		return out, err
	}
	return out, nil
}

// ListDistinctModels returns models seen in events (for unpriced helper).
func (s *Store) ListDistinctModels(ctx context.Context, fromMS int64, limit int) ([]string, error) {
	if s == nil {
		return nil, fmt.Errorf("usagestore: nil store")
	}
	if limit <= 0 {
		limit = 200
	}
	args := make([]any, 0, 2)
	query := `SELECT DISTINCT model FROM usage_events WHERE IFNULL(model,'') <> ''`
	if fromMS > 0 {
		query += ` AND timestamp_ms >= ?`
		args = append(args, fromMS)
	}
	query += ` ORDER BY model LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0, 32)
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
