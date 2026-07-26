package registry

import (
	"sync"
	"testing"
)

func TestModelContextOverrideAppliesToOpenAIListing(t *testing.T) {
	t.Cleanup(func() { SetModelContextOverrides(nil) })

	reg := &ModelRegistry{
		models:               make(map[string]*ModelRegistration),
		clientModels:         make(map[string][]string),
		clientModelInfos:     make(map[string]map[string]*ModelInfo),
		clientProviders:      make(map[string]string),
		availableModelsCache: make(map[string]availableModelsCacheEntry),
		mutex:                &sync.RWMutex{},
	}

	info := &ModelInfo{ID: "my-custom-model", Object: "model", OwnedBy: "acme"}

	// Without an override a custom model advertises no context window.
	if got := reg.convertModelToMap(info, "openai"); got["context_length"] != nil {
		t.Fatalf("expected no context_length before override, got %v", got["context_length"])
	}

	SetModelContextOverrides(map[string]ModelContextOverride{
		"my-custom-model": {ContextLength: 262144, MaxCompletionTokens: 8192},
	})

	got := reg.convertModelToMap(info, "openai")
	if got["context_length"] != 262144 {
		t.Fatalf("context_length = %v, want 262144", got["context_length"])
	}
	if got["max_completion_tokens"] != 8192 {
		t.Fatalf("max_completion_tokens = %v, want 8192", got["max_completion_tokens"])
	}

	// The shared registration must not be mutated by the override.
	if info.ContextLength != 0 {
		t.Fatalf("override mutated the source ModelInfo: %d", info.ContextLength)
	}
}

func TestModelContextOverrideMatchingIsCaseInsensitive(t *testing.T) {
	t.Cleanup(func() { SetModelContextOverrides(nil) })

	SetModelContextOverrides(map[string]ModelContextOverride{
		"  Weird/Model-Name  ": {ContextLength: 1000},
	})

	if _, ok := LookupModelContextOverride("weird/model-name"); !ok {
		t.Fatal("expected trimmed lowercase lookup to match")
	}
	if _, ok := LookupModelContextOverride("WEIRD/MODEL-NAME"); !ok {
		t.Fatal("expected uppercase lookup to match")
	}
}

func TestSetModelContextOverridesDropsEmptyEntries(t *testing.T) {
	t.Cleanup(func() { SetModelContextOverrides(nil) })

	SetModelContextOverrides(map[string]ModelContextOverride{
		"keep": {ContextLength: 4096},
		"drop": {},
		"":     {ContextLength: 10},
	})

	overrides := GetModelContextOverrides()
	if len(overrides) != 1 {
		t.Fatalf("override count = %d, want 1 (%v)", len(overrides), overrides)
	}
	if overrides["keep"].ContextLength != 4096 {
		t.Fatalf("keep context length = %d, want 4096", overrides["keep"].ContextLength)
	}
}

func TestGetModelContextStatusesReportsMissingWindows(t *testing.T) {
	t.Cleanup(func() { SetModelContextOverrides(nil) })

	reg := &ModelRegistry{
		models:               make(map[string]*ModelRegistration),
		clientModels:         make(map[string][]string),
		clientModelInfos:     make(map[string]map[string]*ModelInfo),
		clientProviders:      make(map[string]string),
		availableModelsCache: make(map[string]availableModelsCacheEntry),
		mutex:                &sync.RWMutex{},
	}
	reg.models["known"] = &ModelRegistration{
		Info:      &ModelInfo{ID: "known", ContextLength: 200000},
		Count:     1,
		Providers: map[string]int{"claude": 1},
	}
	reg.models["unknown"] = &ModelRegistration{
		Info:      &ModelInfo{ID: "unknown"},
		Count:     1,
		Providers: map[string]int{"openai-compatibility": 1},
	}

	statuses := reg.GetModelContextStatuses()
	if len(statuses) != 2 {
		t.Fatalf("status count = %d, want 2", len(statuses))
	}
	// Sorted by model ID: "known" then "unknown".
	if !statuses[0].Resolved || statuses[0].ContextLength != 200000 {
		t.Fatalf("known status = %+v, want resolved 200000", statuses[0])
	}
	if statuses[1].Resolved {
		t.Fatalf("unknown status should be unresolved: %+v", statuses[1])
	}
	if len(statuses[1].Providers) != 1 || statuses[1].Providers[0] != "openai-compatibility" {
		t.Fatalf("unknown providers = %v", statuses[1].Providers)
	}

	SetModelContextOverrides(map[string]ModelContextOverride{"unknown": {ContextLength: 131072}})
	statuses = reg.GetModelContextStatuses()
	if !statuses[1].Resolved || !statuses[1].Overridden || statuses[1].ContextLength != 131072 {
		t.Fatalf("overridden status = %+v, want resolved/overridden 131072", statuses[1])
	}
}
