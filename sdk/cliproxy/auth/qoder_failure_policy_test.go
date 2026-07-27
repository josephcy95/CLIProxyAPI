package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestManagerMarkResult_DisablesQoderInactiveToken(t *testing.T) {
	autoDisable := true
	manager := NewManager(nil, nil, nil)
	manager.SetConfig(&internalconfig.Config{Qoder: internalconfig.QoderConfig{
		AutoDisableInactiveToken: &autoDisable,
	}})

	auth := &Auth{ID: "qodercn-inactive-token", Provider: "qodercn", Metadata: map[string]any{"type": "qodercn"}}
	if _, err := manager.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	reason := "Failed to load quota: 401 token is not active"
	manager.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusUnauthorized, Message: reason},
	})

	updated, ok := manager.GetByID(auth.ID)
	if !ok || updated == nil {
		t.Fatal("updated auth not found")
	}
	if !updated.Disabled || updated.Status != StatusDisabled {
		t.Fatalf("auth disabled state = disabled:%t status:%s, want disabled", updated.Disabled, updated.Status)
	}
	if got, _ := updated.Metadata["disabled_reason"].(string); got != reason {
		t.Fatalf("disabled_reason = %q, want %q", got, reason)
	}
}

func TestManagerMarkResult_KeepsOtherQoder401Enabled(t *testing.T) {
	autoDisable := true
	manager := NewManager(nil, nil, nil)
	manager.SetConfig(&internalconfig.Config{Qoder: internalconfig.QoderConfig{
		AutoDisableInactiveToken: &autoDisable,
	}})

	auth := &Auth{ID: "qoder-unknown-401", Provider: "qoder", Metadata: map[string]any{"type": "qoder"}}
	if _, err := manager.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}
	manager.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "qmodel_preview",
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusUnauthorized, Message: "temporary authorization gateway error"},
	})

	updated, ok := manager.GetByID(auth.ID)
	if !ok || updated == nil {
		t.Fatal("updated auth not found")
	}
	if updated.Disabled {
		t.Fatal("unclassified Qoder 401 must not disable the auth")
	}
}

func TestManagerMarkResult_CoolsQueuedQoder403WithoutDisablingAuth(t *testing.T) {
	cooldownMinutes := 5
	manager := NewManager(nil, nil, nil)
	manager.SetConfig(&internalconfig.Config{Qoder: internalconfig.QoderConfig{
		QueuedForbiddenCooldownMinutes: &cooldownMinutes,
	}})

	auth := &Auth{ID: "qodercn-queued", Provider: "qodercn", Metadata: map[string]any{"type": "qodercn"}}
	if _, err := manager.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	manager.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "qmodel_preview",
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusForbidden, Message: `{"code":"403","message":"{\"code\":\"10605\",\"message\":\"{\\\"isQueued\\\":true,\\\"modelKey\\\":\\\"qmodel_preview\\\"}\"}"}`},
	})

	updated, ok := manager.GetByID(auth.ID)
	if !ok || updated == nil {
		t.Fatal("updated auth not found")
	}
	if updated.Disabled {
		t.Fatal("queued Qoder 403 must not disable the auth")
	}
	state := updated.ModelStates["qmodel_preview"]
	if state == nil || !state.Unavailable {
		t.Fatalf("queued Qoder model state = %#v, want unavailable", state)
	}
	remaining := time.Until(state.NextRetryAfter)
	if remaining < 4*time.Minute || remaining > 6*time.Minute {
		t.Fatalf("queued Qoder cooldown remaining = %v, want about 5m", remaining)
	}
}

func TestManagerMarkResult_KeepsInactiveQoderTokenWhenAutoDisableOff(t *testing.T) {
	autoDisable := false
	manager := NewManager(nil, nil, nil)
	manager.SetConfig(&internalconfig.Config{Qoder: internalconfig.QoderConfig{
		AutoDisableInactiveToken: &autoDisable,
	}})

	auth := &Auth{ID: "qoder-inactive-token-off", Provider: "qoder", Metadata: map[string]any{"type": "qoder"}}
	if _, err := manager.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}
	manager.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusUnauthorized, Message: "401 token is not active"},
	})

	updated, ok := manager.GetByID(auth.ID)
	if !ok || updated == nil {
		t.Fatal("updated auth not found")
	}
	if updated.Disabled {
		t.Fatal("inactive Qoder token must not disable when auto-disable-inactive-token is false")
	}
}
