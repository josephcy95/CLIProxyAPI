package executor

import (
	"net/http"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestXAIStatusErr_FreeUsageExhaustedSets24hRetryAfter(t *testing.T) {
	body := []byte(`{"code":"subscription:free-usage-exhausted","error":"You've used all the included free usage for model grok-4.5-build-free for now. Usage resets over a rolling 24-hour window — tokens (actual/limit): 1065387/1000000."}`)
	err := xaiStatusErr(http.StatusTooManyRequests, body, nil)
	if err.StatusCode() != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", err.StatusCode())
	}
	if err.RetryAfter() == nil {
		t.Fatal("expected RetryAfter for free-usage-exhausted")
	}
	if *err.RetryAfter() != 24*time.Hour {
		t.Fatalf("RetryAfter = %v, want 24h", *err.RetryAfter())
	}
}

func TestXAIStatusErr_Generic429HasNoRetryAfter(t *testing.T) {
	body := []byte(`{"code":"rate_limit","error":"too many requests"}`)
	err := xaiStatusErr(http.StatusTooManyRequests, body, nil)
	if err.RetryAfter() != nil {
		t.Fatalf("expected nil RetryAfter for generic 429, got %v", *err.RetryAfter())
	}
}

func TestXAIStatusErr_Non429Unchanged(t *testing.T) {
	body := []byte(`{"error":"nope"}`)
	err := xaiStatusErr(http.StatusBadRequest, body, nil)
	if err.RetryAfter() != nil {
		t.Fatalf("expected nil RetryAfter for 400, got %v", *err.RetryAfter())
	}
}

func TestXAIStatusErr_UsesConfiguredCooldowns(t *testing.T) {
	otherForbiddenCooldown := 9
	freeUsageCooldown := 3
	autoDisable := true
	cfg := &config.Config{XAI: config.XAIConfig{
		AutoDisablePermissionDenied:     &autoDisable,
		OtherForbiddenCooldownHours:     &otherForbiddenCooldown,
		FreeUsageExhaustedCooldownHours: &freeUsageCooldown,
	}}

	freeUsage := xaiStatusErr(http.StatusTooManyRequests, []byte(`{"code":"subscription:free-usage-exhausted","error":"included free usage exhausted"}`), cfg)
	if freeUsage.RetryAfter() == nil || *freeUsage.RetryAfter() != 3*time.Hour {
		t.Fatalf("free usage RetryAfter = %v, want 3h", freeUsage.RetryAfter())
	}

	otherForbidden := xaiStatusErr(http.StatusForbidden, []byte(`{"error":"unexpected upstream rejection"}`), cfg)
	if otherForbidden.RetryAfter() == nil || *otherForbidden.RetryAfter() != 9*time.Hour {
		t.Fatalf("other 403 RetryAfter = %v, want 9h", otherForbidden.RetryAfter())
	}

	permissionDenied := xaiStatusErr(http.StatusForbidden, []byte(`{"code":"permission-denied","error":"Access to the chat endpoint is denied."}`), cfg)
	if permissionDenied.RetryAfter() != nil {
		t.Fatalf("permission denied RetryAfter = %v, want nil", *permissionDenied.RetryAfter())
	}
}

func TestXAIStatusErr_Uses403CooldownWhenAutoDisableIsOff(t *testing.T) {
	autoDisable := false
	otherForbiddenCooldown := 7
	cfg := &config.Config{XAI: config.XAIConfig{
		AutoDisablePermissionDenied: &autoDisable,
		OtherForbiddenCooldownHours: &otherForbiddenCooldown,
	}}
	err := xaiStatusErr(http.StatusForbidden, []byte(`{"code":"permission-denied","error":"Access denied."}`), cfg)
	if err.RetryAfter() == nil || *err.RetryAfter() != 7*time.Hour {
		t.Fatalf("permission denied RetryAfter = %v, want 7h", err.RetryAfter())
	}
}
