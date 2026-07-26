package executor

import (
	"net/http"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// claudeMaxRetryAfter caps how long a single upstream hint may cool a model down.
// Anthropic's weekly/scoped limits can be days away; the model registry re-checks
// on its own cadence, so an unbounded cooldown is not useful.
const claudeMaxRetryAfter = 24 * time.Hour

// claudeRetryAfterFromResponse extracts an upstream-provided cooldown for a failed
// Claude request. Without this the scheduler falls back to a generic backoff ladder
// that starts at one second, which hammers an account that is genuinely exhausted.
//
// Sources, in order of preference:
//   - Retry-After / Retry-After-Ms response headers
//   - resets_at (RFC3339) or resets_in_seconds in the error body
//
// Returns nil when the upstream gave no usable hint, so callers keep their
// existing behavior instead of inventing a duration.
func claudeRetryAfterFromResponse(header http.Header, body []byte) *time.Duration {
	if d := claudeRetryAfterFromHeader(header); d != nil {
		return d
	}
	return claudeRetryAfterFromBody(body)
}

func claudeRetryAfterFromHeader(header http.Header) *time.Duration {
	if header == nil {
		return nil
	}
	if raw := strings.TrimSpace(header.Get("Retry-After")); raw != "" {
		// Delta-seconds form.
		if seconds, err := time.ParseDuration(raw + "s"); err == nil {
			return clampClaudeRetryAfter(seconds)
		}
		// HTTP-date form.
		if when, err := http.ParseTime(raw); err == nil {
			return clampClaudeRetryAfter(time.Until(when))
		}
	}
	if raw := strings.TrimSpace(header.Get("Retry-After-Ms")); raw != "" {
		if ms, err := time.ParseDuration(raw + "ms"); err == nil {
			return clampClaudeRetryAfter(ms)
		}
	}
	return nil
}

func claudeRetryAfterFromBody(body []byte) *time.Duration {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return nil
	}
	for _, path := range []string{"error.resets_in_seconds", "resets_in_seconds"} {
		if seconds := gjson.GetBytes(body, path).Float(); seconds > 0 {
			return clampClaudeRetryAfter(time.Duration(seconds * float64(time.Second)))
		}
	}
	for _, path := range []string{"error.resets_at", "resets_at"} {
		raw := strings.TrimSpace(gjson.GetBytes(body, path).String())
		if raw == "" {
			continue
		}
		if when, err := time.Parse(time.RFC3339, raw); err == nil {
			return clampClaudeRetryAfter(time.Until(when))
		}
		// Some payloads carry a unix timestamp instead of RFC3339.
		if unix := gjson.GetBytes(body, path).Int(); unix > 0 {
			return clampClaudeRetryAfter(time.Until(time.Unix(unix, 0)))
		}
	}
	return nil
}

// clampClaudeRetryAfter drops non-positive hints (already expired) and caps the
// upper bound. Returns nil when the value is unusable.
func clampClaudeRetryAfter(d time.Duration) *time.Duration {
	if d <= 0 {
		return nil
	}
	if d > claudeMaxRetryAfter {
		d = claudeMaxRetryAfter
	}
	return &d
}
