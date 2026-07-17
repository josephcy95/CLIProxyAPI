// Package usagestore provides durable SQLite-backed storage for request usage
// events, model prices, and price aliases used by the management monitoring UI.
package usagestore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	DefaultStoreRelativePath = "data/usage.db"
	DefaultRetentionDays     = 30
	maxInsertBatch           = 64
)

// Store is a process-local SQLite database for usage monitoring.
type Store struct {
	db             *sql.DB
	path           string
	retentionDays  int
	mu             sync.RWMutex
	closed         bool
	insertCh       chan Event
	wg             sync.WaitGroup
	retentionEvery time.Duration
}

// Options configures store open behavior.
type Options struct {
	Path          string
	RetentionDays int
}

// Open creates or opens the usage database at path and starts background writers.
func Open(opts Options) (*Store, error) {
	path := strings.TrimSpace(opts.Path)
	if path == "" {
		path = DefaultStoreRelativePath
	}
	if !filepath.IsAbs(path) {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("usagestore: resolve workdir: %w", err)
		}
		path = filepath.Join(wd, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("usagestore: create dir: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", filepath.ToSlash(path))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("usagestore: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	retention := opts.RetentionDays
	if retention <= 0 {
		retention = DefaultRetentionDays
	}

	s := &Store{
		db:             db,
		path:           path,
		retentionDays:  retention,
		insertCh:       make(chan Event, 1024),
		retentionEvery: time.Hour,
	}
	s.wg.Add(1)
	go s.writerLoop()
	return s, nil
}

// Path returns the absolute database path.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// SetRetentionDays updates retention used by the cleaner.
func (s *Store) SetRetentionDays(days int) {
	if s == nil {
		return
	}
	if days <= 0 {
		days = DefaultRetentionDays
	}
	s.mu.Lock()
	s.retentionDays = days
	s.mu.Unlock()
}

// Close stops background work and closes the database.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.insertCh)
	s.mu.Unlock()
	s.wg.Wait()
	return s.db.Close()
}

// Enqueue buffers an event for async insert. Drops when closed or full.
func (s *Store) Enqueue(event Event) {
	if s == nil {
		return
	}
	s.mu.RLock()
	closed := s.closed
	s.mu.RUnlock()
	if closed {
		return
	}
	select {
	case s.insertCh <- event:
	default:
		// Prefer dropping under backpressure over blocking request path.
	}
}

// Insert writes a single event synchronously (tests / import).
func (s *Store) Insert(ctx context.Context, event Event) error {
	if s == nil {
		return fmt.Errorf("usagestore: nil store")
	}
	return s.insertBatch(ctx, []Event{event})
}

func (s *Store) writerLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.retentionEvery)
	defer ticker.Stop()
	flush := time.NewTicker(200 * time.Millisecond)
	defer flush.Stop()

	batch := make([]Event, 0, maxInsertBatch)
	flushBatch := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.insertBatch(ctx, batch)
		cancel()
		batch = batch[:0]
	}

	for {
		select {
		case event, ok := <-s.insertCh:
			if !ok {
				flushBatch()
				return
			}
			batch = append(batch, event)
			if len(batch) >= maxInsertBatch {
				flushBatch()
			}
		case <-flush.C:
			flushBatch()
		case <-ticker.C:
			flushBatch()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_ = s.PurgeExpired(ctx)
			cancel()
		}
	}
}

func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS usage_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp_ms INTEGER NOT NULL,
			request_id TEXT,
			provider TEXT,
			executor_type TEXT,
			model TEXT,
			alias TEXT,
			endpoint TEXT,
			auth_type TEXT,
			auth_index TEXT,
			source TEXT,
			source_hash TEXT,
			api_key TEXT,
			api_key_hash TEXT,
			reasoning_effort TEXT,
			service_tier TEXT,
			response_service_tier TEXT,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			reasoning_tokens INTEGER NOT NULL DEFAULT 0,
			cached_tokens INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens INTEGER NOT NULL DEFAULT 0,
			cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			latency_ms INTEGER,
			ttft_ms INTEGER,
			failed INTEGER NOT NULL DEFAULT 0,
			fail_status_code INTEGER,
			fail_summary TEXT,
			created_at_ms INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_ts ON usage_events(timestamp_ms DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_model ON usage_events(model)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_provider ON usage_events(provider)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_auth ON usage_events(auth_index)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_failed ON usage_events(failed)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_source_hash ON usage_events(source_hash)`,
		`CREATE TABLE IF NOT EXISTS model_prices (
			model TEXT PRIMARY KEY,
			prompt_per_1m REAL NOT NULL DEFAULT 0,
			completion_per_1m REAL NOT NULL DEFAULT 0,
			cache_per_1m REAL NOT NULL DEFAULT 0,
			cache_read_per_1m REAL NOT NULL DEFAULT 0,
			cache_creation_per_1m REAL NOT NULL DEFAULT 0,
			source TEXT,
			updated_at_ms INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS model_price_aliases (
			alias TEXT PRIMARY KEY,
			target_model TEXT NOT NULL,
			updated_at_ms INTEGER NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("usagestore: migrate: %w", err)
		}
	}
	// Older DBs lack api_key; ignore duplicate-column errors.
	if _, err := db.Exec(`ALTER TABLE usage_events ADD COLUMN api_key TEXT`); err != nil {
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "duplicate column") && !strings.Contains(msg, "already exists") {
			return fmt.Errorf("usagestore: migrate api_key: %w", err)
		}
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_usage_events_api_key ON usage_events(api_key)`); err != nil {
		return fmt.Errorf("usagestore: migrate api_key index: %w", err)
	}
	return nil
}

func (s *Store) insertBatch(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO usage_events (
		timestamp_ms, request_id, provider, executor_type, model, alias, endpoint,
		auth_type, auth_index, source, source_hash, api_key, api_key_hash,
		reasoning_effort, service_tier, response_service_tier,
		input_tokens, output_tokens, reasoning_tokens, cached_tokens,
		cache_read_tokens, cache_creation_tokens, total_tokens,
		latency_ms, ttft_ms, failed, fail_status_code, fail_summary, created_at_ms
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UnixMilli()
	for _, e := range events {
		if e.CreatedAtMS == 0 {
			e.CreatedAtMS = now
		}
		if e.TimestampMS == 0 {
			e.TimestampMS = now
		}
		failed := 0
		if e.Failed {
			failed = 1
		}
		var latency, ttft any
		if e.LatencyMS != nil {
			latency = *e.LatencyMS
		}
		if e.TTFTMS != nil {
			ttft = *e.TTFTMS
		}
		if _, err := stmt.ExecContext(ctx,
			e.TimestampMS, nullStr(e.RequestID), nullStr(e.Provider), nullStr(e.ExecutorType),
			nullStr(e.Model), nullStr(e.Alias), nullStr(e.Endpoint),
			nullStr(e.AuthType), nullStr(e.AuthIndex), nullStr(e.Source), nullStr(e.SourceHash),
			nullStr(e.APIKey), nullStr(e.APIKeyHash),
			nullStr(e.ReasoningEffort), nullStr(e.ServiceTier), nullStr(e.ResponseServiceTier),
			e.InputTokens, e.OutputTokens, e.ReasoningTokens, e.CachedTokens,
			e.CacheReadTokens, e.CacheCreationTokens, e.TotalTokens,
			latency, ttft, failed, nullInt(e.FailStatusCode), nullStr(e.FailSummary), e.CreatedAtMS,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PurgeExpired deletes events older than retention.
func (s *Store) PurgeExpired(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	days := s.retentionDays
	s.mu.RUnlock()
	if days <= 0 {
		return nil
	}
	cutoff := time.Now().AddDate(0, 0, -days).UnixMilli()
	_, err := s.db.ExecContext(ctx, `DELETE FROM usage_events WHERE timestamp_ms < ?`, cutoff)
	return err
}

func nullStr(v string) any {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return v
}

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

// ResolveStorePath resolves a configured store path relative to config dir when needed.
func ResolveStorePath(storePath, configFilePath string) string {
	path := strings.TrimSpace(storePath)
	if path == "" {
		path = DefaultStoreRelativePath
	}
	if filepath.IsAbs(path) {
		return path
	}
	// Prefer process workdir for Docker WORKDIR=/CLIProxyAPI defaults.
	if wd, err := os.Getwd(); err == nil && wd != "" {
		return filepath.Join(wd, path)
	}
	if configFilePath != "" {
		return filepath.Join(filepath.Dir(configFilePath), path)
	}
	return path
}
