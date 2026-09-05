package usagestore

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestMonitoringReadersAndRecentRequests(t *testing.T) {
	s, err := Open(Options{Path: filepath.Join(t.TempDir(), "usage.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	now := time.Now()
	for i, source := range []string{"one", "two"} {
		if err := s.Insert(ctx, Event{TimestampMS: now.UnixMilli(), Source: source, Provider: "codex", Model: "test", Failed: i == 1}); err != nil {
			t.Fatal(err)
		}
	}
	filter := QueryFilter{FromMS: now.Add(-time.Hour).UnixMilli()}
	all, err := s.GetAccountStats(ctx, filter, 100)
	if err != nil {
		t.Fatal(err)
	}
	recent, err := s.GetAccountRecentRequests(ctx, filter, []AccountRecentRequests{{Source: "one", Provider: "codex"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 {
		t.Fatalf("recent = %+v", recent)
	}
	for _, account := range all {
		if account.Source == "one" && !reflect.DeepEqual(recent[0].RecentRequests, account.RecentRequests) {
			t.Fatal("lightweight buckets differ from account statistics")
		}
	}
	history, err := s.GetAccountRecentRequests(ctx, QueryFilter{ToMS: now.Add(-24 * time.Hour).UnixMilli()}, recent, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, bucket := range history[0].RecentRequests {
		if bucket.Success+bucket.Failed != 0 {
			t.Fatal("historical range leaked recent requests")
		}
	}
	facets, err := s.GetFilterOptions(ctx, filter, "models", "providers")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := s.GetFilterOptions(ctx, filter)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(facets.Models, legacy.Models) || !reflect.DeepEqual(facets.Providers, legacy.Providers) || len(facets.Sources) != 0 {
		t.Fatalf("facets = %+v", facets)
	}
	if _, err := s.GetFilterOptions(ctx, filter, "invalid"); err == nil {
		t.Fatal("invalid facet accepted")
	}
	// A held read snapshot must not occupy the writer connection or block inserts in WAL.
	tx, err := s.readDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_events").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := s.Insert(ctx, Event{TimestampMS: now.UnixMilli(), Model: "new"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertModelPrices(ctx, []ModelPrice{{Model: "test", PromptPer1M: 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadModelPrices(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.readDB.ExecContext(ctx, "DELETE FROM usage_events"); err == nil {
		t.Fatal("reader accepted a write")
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := s.GetSummary(canceled, filter); err == nil {
		t.Fatal("canceled read succeeded")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	s.Enqueue(Event{TimestampMS: now.UnixMilli(), Model: "flushed"})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(Options{Path: s.Path()})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	sum, err := reopened.GetSummary(ctx, QueryFilter{})
	if err != nil || sum.TotalCalls != 4 {
		t.Fatalf("shutdown flush: %+v, %v", sum, err)
	}
}
