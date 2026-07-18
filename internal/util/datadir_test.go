package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataDirDefaultsAndOverride(t *testing.T) {
	t.Setenv("CLIPROXY_DATA_DIR", "")
	t.Setenv("CLI_PROXY_DATA_DIR", "")
	if got := DataDir(); got != DefaultDataDir {
		t.Fatalf("DataDir() = %q, want %q", got, DefaultDataDir)
	}

	root := t.TempDir()
	t.Setenv("CLIPROXY_DATA_DIR", root)
	if got := DataDir(); got != filepath.Clean(root) {
		t.Fatalf("DataDir() with env = %q, want %q", got, root)
	}
	if got := ConfigFilePath(); got != filepath.Join(root, "config.yaml") {
		t.Fatalf("ConfigFilePath() = %q", got)
	}
	if got := AuthDirPath(); got != filepath.Join(root, "auths") {
		t.Fatalf("AuthDirPath() = %q", got)
	}
	if got := LogsDirPath(); got != filepath.Join(root, "logs") {
		t.Fatalf("LogsDirPath() = %q", got)
	}
	if got := PluginsDirPath(); got != filepath.Join(root, "plugins") {
		t.Fatalf("PluginsDirPath() = %q", got)
	}
	if got := UsageStoreFilePath(); got != filepath.Join(root, "usage.db") {
		t.Fatalf("UsageStoreFilePath() = %q", got)
	}

	if err := EnsureDataDirLayout(); err != nil {
		t.Fatalf("EnsureDataDirLayout: %v", err)
	}
	for _, dir := range []string{AuthDirPath(), LogsDirPath(), PluginsDirPath()} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", dir)
		}
	}
}
