package main

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestShouldEnableExampleAPIKeySafeMode(t *testing.T) {
	cfgWithExampleKey := &config.Config{
		SDKConfig: config.SDKConfig{
			APIKeys: []string{"real-key", " your-api-key-1 "},
		},
	}
	cfgWithRealKey := &config.Config{
		SDKConfig: config.SDKConfig{
			APIKeys: []string{"real-key"},
		},
	}

	tests := []struct {
		name               string
		cfg                *config.Config
		commandMode        bool
		tuiMode            bool
		standalone         bool
		cloudConfigMissing bool
		homeMode           bool
		want               bool
	}{
		{
			name: "normal server with example key",
			cfg:  cfgWithExampleKey,
			want: true,
		},
		{
			name:       "standalone tui with example key",
			cfg:        cfgWithExampleKey,
			tuiMode:    true,
			standalone: true,
			want:       true,
		},
		{
			name:        "pure tui client is not blocked",
			cfg:         cfgWithExampleKey,
			tuiMode:     true,
			standalone:  false,
			commandMode: false,
			want:        false,
		},
		{
			name:        "one-shot command is not blocked",
			cfg:         cfgWithExampleKey,
			commandMode: true,
			want:        false,
		},
		{
			name:     "home mode is not blocked",
			cfg:      cfgWithExampleKey,
			homeMode: true,
			want:     false,
		},
		{
			name:               "cloud standby without config is not blocked",
			cfg:                cfgWithExampleKey,
			cloudConfigMissing: true,
			want:               false,
		},
		{
			name: "normal server with real key",
			cfg:  cfgWithRealKey,
			want: false,
		},
		{
			name: "nil config",
			cfg:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldEnableExampleAPIKeySafeMode(tt.cfg, tt.commandMode, tt.tuiMode, tt.standalone, tt.cloudConfigMissing, tt.homeMode)
			if got != tt.want {
				t.Fatalf("shouldEnableExampleAPIKeySafeMode() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestModelCatalogUpdaterPlan(t *testing.T) {
	tests := []struct {
		name                string
		localModel          bool
		homeEnabled         bool
		wantModels          bool
		wantCodexClient     bool
		wantCatalogFallback bool
	}{
		{
			name:                "normal CPA refreshes all catalogs",
			localModel:          false,
			homeEnabled:         false,
			wantModels:          true,
			wantCodexClient:     true,
			wantCatalogFallback: true,
		},
		{
			name:                "home mode keeps models.json local and refreshes edge-local metadata",
			localModel:          false,
			homeEnabled:         true,
			wantModels:          false,
			wantCodexClient:     true,
			wantCatalogFallback: true,
		},
		{
			name:                "local-model disables every remote catalog",
			localModel:          true,
			homeEnabled:         false,
			wantModels:          false,
			wantCodexClient:     false,
			wantCatalogFallback: false,
		},
		{
			name:                "local-model disables every remote catalog even under home",
			localModel:          true,
			homeEnabled:         true,
			wantModels:          false,
			wantCodexClient:     false,
			wantCatalogFallback: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotModels, gotCodex, gotCatalogFallback := modelCatalogUpdaterPlan(tt.localModel, tt.homeEnabled)
			if gotModels != tt.wantModels || gotCodex != tt.wantCodexClient || gotCatalogFallback != tt.wantCatalogFallback {
				t.Fatalf("modelCatalogUpdaterPlan(%v, %v) = (%v, %v, %v), want (%v, %v, %v)",
					tt.localModel, tt.homeEnabled, gotModels, gotCodex, gotCatalogFallback,
					tt.wantModels, tt.wantCodexClient, tt.wantCatalogFallback)
			}
		})
	}
}
