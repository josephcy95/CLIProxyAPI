package cliproxy

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

// The catalog fallback exists so models reached through custom endpoints, which
// no static catalog knows about, still advertise a context window and reasoning
// levels instead of nothing.

func TestOpenAICompatModelsUseCatalogFallbackMetadata(t *testing.T) {
	models := buildOpenAICompatibilityConfigModels(&config.OpenAICompatibility{
		Name: "cpa",
		Models: []config.OpenAICompatibilityModel{
			{Name: "deepseek-v4.1-flash"},
		},
	})
	if len(models) != 1 {
		t.Fatalf("got %d models, want 1", len(models))
	}
	model := models[0]
	if model.ContextLength <= 0 {
		t.Errorf("context length = %d, want a catalog value", model.ContextLength)
	}
	if model.MaxCompletionTokens <= 0 {
		t.Errorf("max completion tokens = %d, want a catalog value", model.MaxCompletionTokens)
	}
	if model.Thinking == nil || len(model.Thinking.Levels) == 0 {
		t.Fatalf("thinking = %+v, want catalog reasoning levels", model.Thinking)
	}
}

func TestOpenAICompatModelsKeepGenericLevelsForUnknownModel(t *testing.T) {
	models := buildOpenAICompatibilityConfigModels(&config.OpenAICompatibility{
		Name:   "cpa",
		Models: []config.OpenAICompatibilityModel{{Name: "totally-unknown-model-xyz"}},
	})
	if len(models) != 1 {
		t.Fatalf("got %d models, want 1", len(models))
	}
	model := models[0]
	if model.ContextLength != 0 {
		t.Errorf("context length = %d, want 0 so nothing is guessed", model.ContextLength)
	}
	if model.Thinking == nil || len(model.Thinking.Levels) != 3 {
		t.Fatalf("thinking = %+v, want the existing generic level set", model.Thinking)
	}
}

func TestConfiguredContextLengthWinsOverCatalogFallback(t *testing.T) {
	const want = 200000

	models := buildOpenAICompatibilityConfigModels(&config.OpenAICompatibility{
		Name: "cpa",
		Models: []config.OpenAICompatibilityModel{{
			Name:             "deepseek-v4.1-flash",
			MaxContextLength: want,
		}},
	})
	if len(models) != 1 {
		t.Fatalf("got %d models, want 1", len(models))
	}
	if models[0].ContextLength != want {
		t.Errorf("context length = %d, want the configured %d", models[0].ContextLength, want)
	}
}

func TestConfiguredThinkingWinsOverCatalogFallback(t *testing.T) {
	models := buildOpenAICompatibilityConfigModels(&config.OpenAICompatibility{
		Name: "cpa",
		Models: []config.OpenAICompatibilityModel{{
			Name:     "deepseek-v4.1-flash",
			Thinking: &registry.ThinkingSupport{Levels: []string{"low"}},
		}},
	})
	if len(models) != 1 {
		t.Fatalf("got %d models, want 1", len(models))
	}
	thinking := models[0].Thinking
	if thinking == nil || len(thinking.Levels) != 1 || thinking.Levels[0] != "low" {
		t.Fatalf("thinking = %+v, want only the configured level", thinking)
	}
}

func TestConfigModelsFillMissingLevelsForBudgetOnlyStaticModel(t *testing.T) {
	// The antigravity catalog describes this model with a thinking budget range
	// but no levels, which previously left it with an empty level list.
	models := buildCodexConfigModels(&config.CodexKey{
		Models: []config.CodexModel{{Name: "claude-opus-4-6-thinking"}},
	})
	if len(models) != 1 {
		t.Fatalf("got %d models, want 1", len(models))
	}
	thinking := models[0].Thinking
	if thinking == nil || len(thinking.Levels) == 0 {
		t.Fatalf("thinking = %+v, want levels filled from the catalog fallback", thinking)
	}
}

func TestConfigModelsResolveCatalogFallbackThroughAlias(t *testing.T) {
	// The lookup ladder is upstream name first, then the client-facing alias, so
	// either declaration order resolves the same catalog entry.
	models := buildCodexConfigModels(&config.CodexKey{
		Models: []config.CodexModel{{
			Name:  "muse-spark-1.3",
			Alias: "my-favourite-model",
		}},
	})
	if len(models) != 1 {
		t.Fatalf("got %d models, want 1", len(models))
	}
	if models[0].ID != "my-favourite-model" {
		t.Fatalf("model ID = %q, want the configured alias", models[0].ID)
	}
	if models[0].ContextLength <= 0 {
		t.Errorf("context length = %d, want the aliased model's catalog value", models[0].ContextLength)
	}
}
