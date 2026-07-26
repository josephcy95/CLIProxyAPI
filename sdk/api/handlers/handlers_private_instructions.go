package handlers

// Fork-only private Codex instructions support. Kept in a dedicated file so
// upstream refactors of handlers.go do not silently drop it.

import (
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/codexinstructions"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

func applyPrivateCodexInstructionModel(manager *coreauth.Manager, modelName string, meta map[string]any) (string, map[string]any) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return modelName, meta
	}
	parsed := thinking.ParseSuffix(modelName)
	base := strings.TrimSpace(parsed.ModelName)
	stripped := base
	private := manager.CodexInstructionsApplyWithoutPrefixSuffix(base)
	if !private {
		stripped, private = codexinstructions.ParseModel(base, codexInstructionMarkersFromManager(manager))
	}
	if !private {
		return modelName, meta
	}
	if meta == nil {
		meta = make(map[string]any)
	}
	meta[coreexecutor.CodexPrivateInstructionsMetadataKey] = true
	if parsed.HasSuffix {
		return fmt.Sprintf("%s(%s)", stripped, parsed.RawSuffix), meta
	}
	return stripped, meta
}

func codexInstructionMarkersFromManager(manager *coreauth.Manager) codexinstructions.MarkerConfig {
	if manager == nil {
		return codexinstructions.DefaultMarkers()
	}
	return manager.CodexInstructionMarkers()
}
