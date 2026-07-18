package util

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultDataDir is the single persistent root for config, auths, logs, plugins, and usage DB.
const DefaultDataDir = "/data"

// DataDir returns the persistent data root.
// Override with CLIPROXY_DATA_DIR or CLI_PROXY_DATA_DIR; otherwise DefaultDataDir.
func DataDir() string {
	for _, key := range []string{"CLIPROXY_DATA_DIR", "CLI_PROXY_DATA_DIR"} {
		if value, ok := os.LookupEnv(key); ok {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return filepath.Clean(trimmed)
			}
		}
	}
	return DefaultDataDir
}

// ConfigFilePath is DataDir/config.yaml.
func ConfigFilePath() string {
	return filepath.Join(DataDir(), "config.yaml")
}

// AuthDirPath is DataDir/auths.
func AuthDirPath() string {
	return filepath.Join(DataDir(), "auths")
}

// LogsDirPath is DataDir/logs.
func LogsDirPath() string {
	return filepath.Join(DataDir(), "logs")
}

// PluginsDirPath is DataDir/plugins.
func PluginsDirPath() string {
	return filepath.Join(DataDir(), "plugins")
}

// UsageStoreFilePath is DataDir/usage.db.
func UsageStoreFilePath() string {
	return filepath.Join(DataDir(), "usage.db")
}

// EnsureDataDirLayout creates the data root and standard subdirectories.
func EnsureDataDirLayout() error {
	for _, dir := range []string{DataDir(), AuthDirPath(), LogsDirPath(), PluginsDirPath()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}
