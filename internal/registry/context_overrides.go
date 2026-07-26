package registry

import (
	"sort"
	"strings"
	"sync"
)

// ModelContextOverride describes an operator-supplied context window for a model
// whose metadata cannot be resolved from the static catalog. Custom providers
// (openai-compatibility and friends) frequently use model names that do not
// match any catalog entry, so no context window is advertised for them. Clients
// that size their requests from the advertised window need a value, and this is
// the manual escape hatch.
type ModelContextOverride struct {
	// ContextLength overrides ModelInfo.ContextLength when greater than zero.
	ContextLength int `json:"context_length,omitempty" yaml:"context-length,omitempty"`
	// MaxCompletionTokens overrides ModelInfo.MaxCompletionTokens when greater than zero.
	MaxCompletionTokens int `json:"max_completion_tokens,omitempty" yaml:"max-completion-tokens,omitempty"`
}

// IsZero reports whether the override carries no usable value.
func (o ModelContextOverride) IsZero() bool {
	return o.ContextLength <= 0 && o.MaxCompletionTokens <= 0
}

var (
	contextOverridesMu sync.RWMutex
	// contextOverrides maps a normalized model ID to its operator override.
	contextOverrides = make(map[string]ModelContextOverride)
)

// NormalizeModelOverrideKey converts a model ID into the lookup key used for
// overrides. Matching is case-insensitive and whitespace-insensitive so that
// operators do not have to reproduce the exact upstream casing.
func NormalizeModelOverrideKey(modelID string) string {
	return strings.ToLower(strings.TrimSpace(modelID))
}

// SetModelContextOverrides replaces the override table and invalidates the
// cached model listings so the next /v1/models call reflects the new values.
func SetModelContextOverrides(overrides map[string]ModelContextOverride) {
	normalized := make(map[string]ModelContextOverride, len(overrides))
	for modelID, override := range overrides {
		key := NormalizeModelOverrideKey(modelID)
		if key == "" || override.IsZero() {
			continue
		}
		if override.ContextLength < 0 {
			override.ContextLength = 0
		}
		if override.MaxCompletionTokens < 0 {
			override.MaxCompletionTokens = 0
		}
		normalized[key] = override
	}

	contextOverridesMu.Lock()
	contextOverrides = normalized
	contextOverridesMu.Unlock()

	// Cached listings embed the previous values, so drop them.
	reg := GetGlobalRegistry()
	reg.mutex.Lock()
	reg.invalidateAvailableModelsCacheLocked()
	reg.mutex.Unlock()
}

// GetModelContextOverrides returns a copy of the current override table.
func GetModelContextOverrides() map[string]ModelContextOverride {
	contextOverridesMu.RLock()
	defer contextOverridesMu.RUnlock()

	out := make(map[string]ModelContextOverride, len(contextOverrides))
	for key, override := range contextOverrides {
		out[key] = override
	}
	return out
}

// LookupModelContextOverride returns the override registered for modelID.
func LookupModelContextOverride(modelID string) (ModelContextOverride, bool) {
	key := NormalizeModelOverrideKey(modelID)
	if key == "" {
		return ModelContextOverride{}, false
	}

	contextOverridesMu.RLock()
	defer contextOverridesMu.RUnlock()

	override, ok := contextOverrides[key]
	return override, ok
}

// applyContextOverride returns a copy of model with any operator override
// applied. The original is never mutated because registrations are shared.
func applyContextOverride(model *ModelInfo) *ModelInfo {
	if model == nil {
		return nil
	}
	override, ok := LookupModelContextOverride(model.ID)
	if !ok {
		return model
	}

	patched := *model
	if override.ContextLength > 0 {
		patched.ContextLength = override.ContextLength
		// Gemini-shaped listings read the token limit fields instead.
		patched.InputTokenLimit = override.ContextLength
	}
	if override.MaxCompletionTokens > 0 {
		patched.MaxCompletionTokens = override.MaxCompletionTokens
		patched.OutputTokenLimit = override.MaxCompletionTokens
	}
	return &patched
}

// ModelContextStatus reports the effective context window for one registered
// model along with where the value came from. The management UI uses this to
// list models that still lack a context window.
type ModelContextStatus struct {
	ID                  string   `json:"id"`
	DisplayName         string   `json:"display_name,omitempty"`
	Type                string   `json:"type,omitempty"`
	OwnedBy             string   `json:"owned_by,omitempty"`
	Providers           []string `json:"providers,omitempty"`
	ContextLength       int      `json:"context_length,omitempty"`
	MaxCompletionTokens int      `json:"max_completion_tokens,omitempty"`
	// Overridden reports whether the effective values come from an operator override.
	Overridden bool `json:"overridden"`
	// Resolved reports whether a context window is known at all.
	Resolved bool `json:"resolved"`
}

// GetModelContextStatuses returns the context-window state of every registered
// model, sorted by model ID.
func (r *ModelRegistry) GetModelContextStatuses() []ModelContextStatus {
	r.mutex.RLock()
	registrations := make([]*ModelRegistration, 0, len(r.models))
	for _, registration := range r.models {
		if registration == nil || registration.Info == nil || registration.Count <= 0 {
			continue
		}
		registrations = append(registrations, registration)
	}
	providersByModel := make(map[string][]string, len(registrations))
	for _, registration := range registrations {
		if registration.Info == nil {
			continue
		}
		providers := make([]string, 0, len(registration.Providers))
		for provider, count := range registration.Providers {
			if count <= 0 || strings.TrimSpace(provider) == "" {
				continue
			}
			providers = append(providers, provider)
		}
		sort.Strings(providers)
		providersByModel[registration.Info.ID] = providers
	}
	r.mutex.RUnlock()

	out := make([]ModelContextStatus, 0, len(registrations))
	for _, registration := range registrations {
		info := registration.Info
		override, overridden := LookupModelContextOverride(info.ID)

		contextLength := info.ContextLength
		if contextLength <= 0 {
			contextLength = info.InputTokenLimit
		}
		maxCompletion := info.MaxCompletionTokens
		if maxCompletion <= 0 {
			maxCompletion = info.OutputTokenLimit
		}
		if overridden {
			if override.ContextLength > 0 {
				contextLength = override.ContextLength
			}
			if override.MaxCompletionTokens > 0 {
				maxCompletion = override.MaxCompletionTokens
			}
		}

		out = append(out, ModelContextStatus{
			ID:                  info.ID,
			DisplayName:         info.DisplayName,
			Type:                info.Type,
			OwnedBy:             info.OwnedBy,
			Providers:           providersByModel[info.ID],
			ContextLength:       contextLength,
			MaxCompletionTokens: maxCompletion,
			Overridden:          overridden,
			Resolved:            contextLength > 0,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
