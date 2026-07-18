package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultPluginsDir = "plugins"

// dataDirRoot matches util.DataDir (kept local to avoid util↔config import cycle).
func dataDirRoot() string {
	for _, key := range []string{"CLIPROXY_DATA_DIR", "CLI_PROXY_DATA_DIR"} {
		if value, ok := os.LookupEnv(key); ok {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return filepath.Clean(trimmed)
			}
		}
	}
	return "/data"
}

// ResolvePluginsDir normalizes the plugin directory for consistent use throughout the app.
// Empty defaults to DataDir/plugins. Relative paths are under DataDir. Leading ~ expands to home.
func ResolvePluginsDir(pluginsDir string) (string, error) {
	pluginsDir = strings.TrimSpace(pluginsDir)
	if pluginsDir == "" {
		return filepath.Join(dataDirRoot(), defaultPluginsDir), nil
	}
	if strings.HasPrefix(pluginsDir, "~") {
		homeDir, errUserHomeDir := os.UserHomeDir()
		if errUserHomeDir != nil {
			return "", fmt.Errorf("resolve plugins directory: %w", errUserHomeDir)
		}
		remainder := strings.TrimPrefix(pluginsDir, "~")
		remainder = strings.TrimLeft(remainder, "/\\")
		if remainder == "" {
			return filepath.Clean(homeDir), nil
		}
		normalized := strings.ReplaceAll(remainder, "\\", "/")
		return filepath.Clean(filepath.Join(homeDir, filepath.FromSlash(normalized))), nil
	}
	if filepath.IsAbs(pluginsDir) {
		return filepath.Clean(pluginsDir), nil
	}
	return filepath.Clean(filepath.Join(dataDirRoot(), pluginsDir)), nil
}

// ResolvePluginsDir resolves and stores the effective plugin directory.
func (cfg *Config) ResolvePluginsDir() error {
	if cfg == nil {
		return nil
	}
	pluginsDir, errResolvePluginsDir := ResolvePluginsDir(cfg.Plugins.Dir)
	if errResolvePluginsDir != nil {
		return errResolvePluginsDir
	}
	cfg.Plugins.Dir = pluginsDir
	return nil
}
