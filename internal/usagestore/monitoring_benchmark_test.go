package usagestore

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// Run with: go test ./internal/usagestore -run '^$' -bench BenchmarkMonitoring -benchtime=3x -count=1
// Synthetic data only. LegacyCore reproduces the old request list's sequential
// database work; CurrentCore omits full account statistics and unused facets.
func BenchmarkMonitoring(b *testing.B) {
	for _, n := range []int{100_000, 1_000_000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			s, err := Open(Options{Path: filepath.Join(b.TempDir(), "usage.db")})
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			ctx := context.Background()
			now := time.Now()
			day := int64(24 * time.Hour / time.Millisecond)
			_, err = s.db.Exec(`WITH RECURSIVE seq(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM seq WHERE x < ?)
    INSERT INTO usage_events(timestamp_ms,model,alias,provider,auth_index,source,source_hash,api_key,api_key_hash,input_tokens,output_tokens,total_tokens,failed,created_at_ms,latency_ms,ttft_ms)
    SELECT ?-((?-x)*?/ ?),'m'||(x%32),'m'||(x%32),'p'||(x%4),'a'||(x%200),'s'||(x%200),'h'||(x%200),'k'||(x%12),'kh'||(x%12),1000,100,1100,x%20=0,?,900,100 FROM seq`, n, now.UnixMilli(), n, 30*day, n, now.UnixMilli())
			if err != nil {
				b.Fatal(err)
			}
			prices := map[string]ModelPrice{}
			for i := 0; i < 32; i++ {
				prices[fmt.Sprint("m", i)] = ModelPrice{PromptPer1M: 1, CompletionPer1M: 2}
			}
			for _, r := range []struct {
				name   string
				filter QueryFilter
			}{
				{"24h", QueryFilter{FromMS: now.UnixMilli() - day, ToMS: now.UnixMilli(), Limit: 200}},
				{"30d", QueryFilter{FromMS: now.UnixMilli() - 30*day, ToMS: now.UnixMilli(), Limit: 200}},
				{"All", QueryFilter{Limit: 200}},
				{"Historical", QueryFilter{FromMS: now.UnixMilli() - 20*day, ToMS: now.UnixMilli() - 10*day, Limit: 200}},
			} {
				for _, mode := range []string{"Events", "LegacyCore", "CurrentCore"} {
					b.Run(r.name+"/"+mode, func(b *testing.B) {
						b.ReportAllocs()
						for i := 0; i < b.N; i++ {
							events, err := s.ListEvents(ctx, r.filter)
							if err != nil {
								b.Fatal(err)
							}
							if mode == "Events" {
								continue
							}
							if _, err = s.GetSummary(ctx, r.filter); err != nil {
								b.Fatal(err)
							}
							if _, _, err = s.SumCost(ctx, r.filter, prices, nil); err != nil {
								b.Fatal(err)
							}
							if mode == "LegacyCore" {
								if _, err = s.GetFilterOptions(ctx, r.filter); err != nil {
									b.Fatal(err)
								}
								if _, err = s.GetAccountStats(ctx, r.filter, 100); err != nil {
									b.Fatal(err)
								}
								if _, err = s.CostByAccount(ctx, r.filter, prices, nil); err != nil {
									b.Fatal(err)
								}
							} else {
								if _, err = s.GetFilterOptions(ctx, r.filter, "models", "providers", "sources", "api_keys"); err != nil {
									b.Fatal(err)
								}
								groups := make([]AccountRecentRequests, 0, len(events))
								for _, e := range events {
									groups = append(groups, AccountRecentRequests{AuthIndex: e.AuthIndex, Source: e.Source, SourceHash: e.SourceHash, Provider: e.Provider})
								}
								if _, err = s.GetAccountRecentRequests(ctx, r.filter, groups, now); err != nil {
									b.Fatal(err)
								}
							}
						}
					})
				}
			}
		})
	}
}
