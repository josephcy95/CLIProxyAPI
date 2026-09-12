package auth

import (
	"context"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

// newCooldownMonotonicManager is shared by ResultPolicy and related cooldown tests.
// Upstream monotonic deadline-preservation tests (#5501) were not ported: this
// fork's MarkResult path combines adaptive Codex observation, xAI/Codex
// exhaustion, and ResultPolicy hooks that differ from upstream's model.
func newCooldownMonotonicManager(t *testing.T, models ...string) (*Manager, *Auth) {
	t.Helper()
	m := NewManager(nil, nil, nil)
	auth := &Auth{ID: "auth-monotonic-" + models[0], Provider: "claude"}
	reg := registry.GetGlobalRegistry()
	infos := make([]*registry.ModelInfo, 0, len(models))
	now := time.Now().Unix()
	for _, model := range models {
		infos = append(infos, &registry.ModelInfo{ID: model, Created: now})
	}
	reg.RegisterClient(auth.ID, auth.Provider, infos)
	t.Cleanup(func() { reg.UnregisterClient(auth.ID) })
	if _, errRegister := m.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}
	return m, auth
}
