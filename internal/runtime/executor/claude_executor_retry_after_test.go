package executor

import (
	"net/http"
	"testing"
	"time"
)

func TestClaudeRetryAfterFromHeaderSeconds(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "120")
	got := claudeRetryAfterFromResponse(h, nil)
	if got == nil || *got != 2*time.Minute {
		t.Fatalf("retryAfter = %v, want 2m", got)
	}
}

func TestClaudeRetryAfterFromHeaderMilliseconds(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After-Ms", "1500")
	got := claudeRetryAfterFromResponse(h, nil)
	if got == nil || *got != 1500*time.Millisecond {
		t.Fatalf("retryAfter = %v, want 1.5s", got)
	}
}

func TestClaudeRetryAfterFromBodyResetsInSeconds(t *testing.T) {
	body := []byte(`{"error":{"type":"rate_limit_error","resets_in_seconds":300}}`)
	got := claudeRetryAfterFromResponse(nil, body)
	if got == nil || *got != 5*time.Minute {
		t.Fatalf("retryAfter = %v, want 5m", got)
	}
}

func TestClaudeRetryAfterFromBodyResetsAt(t *testing.T) {
	resetAt := time.Now().Add(90 * time.Minute).UTC().Format(time.RFC3339)
	body := []byte(`{"error":{"resets_at":"` + resetAt + `"}}`)
	got := claudeRetryAfterFromResponse(nil, body)
	if got == nil || *got < 80*time.Minute || *got > 95*time.Minute {
		t.Fatalf("retryAfter = %v, want ~90m", got)
	}
}

func TestClaudeRetryAfterHeaderWinsOverBody(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "60")
	body := []byte(`{"error":{"resets_in_seconds":9999}}`)
	got := claudeRetryAfterFromResponse(h, body)
	if got == nil || *got != time.Minute {
		t.Fatalf("retryAfter = %v, want 1m (header precedence)", got)
	}
}

func TestClaudeRetryAfterIgnoresUnusableValues(t *testing.T) {
	// No hint at all.
	if got := claudeRetryAfterFromResponse(http.Header{}, []byte(`{"error":{}}`)); got != nil {
		t.Fatalf("retryAfter = %v, want nil", got)
	}
	// Already-expired reset time.
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if got := claudeRetryAfterFromResponse(nil, []byte(`{"resets_at":"`+past+`"}`)); got != nil {
		t.Fatalf("expired retryAfter = %v, want nil", got)
	}
	// Non-JSON body must not panic.
	if got := claudeRetryAfterFromResponse(nil, []byte("not json")); got != nil {
		t.Fatalf("invalid body retryAfter = %v, want nil", got)
	}
}

func TestClaudeRetryAfterClampsToMax(t *testing.T) {
	body := []byte(`{"error":{"resets_in_seconds":604800}}`) // 7 days
	got := claudeRetryAfterFromResponse(nil, body)
	if got == nil || *got != claudeMaxRetryAfter {
		t.Fatalf("retryAfter = %v, want %v", got, claudeMaxRetryAfter)
	}
}
