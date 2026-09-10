package registry

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// The fallback catalog is rebuilt from the two public models.dev catalogs. It is
// refreshed on the same cadence as the other model catalogs so newly released
// models gain context windows and reasoning levels without a fork release.

const (
	// maxCatalogFallbackCanonicalSize bounds models.json (about 300 KB today).
	maxCatalogFallbackCanonicalSize = 8 << 20
	// maxCatalogFallbackProviderSize bounds api.json (about 4.5 MB today).
	maxCatalogFallbackProviderSize = 32 << 20
)

var (
	catalogFallbackCanonicalURL = "https://models.dev/models.json"
	catalogFallbackProviderURL  = "https://models.dev/api.json"
)

var catalogFallbackUpdaterOnce sync.Once

// StartModelCatalogFallbackUpdater refreshes the fallback catalog immediately on
// startup and then every modelsRefreshInterval. Safe to call multiple times;
// only one updater will run.
func StartModelCatalogFallbackUpdater(ctx context.Context) {
	catalogFallbackUpdaterOnce.Do(func() {
		go runModelCatalogFallbackUpdater(ctx)
	})
}

func runModelCatalogFallbackUpdater(ctx context.Context) {
	tryRefreshCatalogFallback(ctx, "startup model catalog fallback refresh")

	ticker := time.NewTicker(modelsRefreshInterval)
	defer ticker.Stop()
	log.Infof("periodic model catalog fallback refresh started (interval=%s)", modelsRefreshInterval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tryRefreshCatalogFallback(ctx, "periodic model catalog fallback refresh")
		}
	}
}

// tryRefreshCatalogFallback rebuilds the catalog and keeps the current data when
// any fetch or decode step fails.
func tryRefreshCatalogFallback(ctx context.Context, label string) {
	canonical, ok := fetchCatalogSource(ctx, catalogFallbackCanonicalURL, maxCatalogFallbackCanonicalSize, "canonical model registry")
	if !ok {
		log.Warnf("%s: canonical registry unavailable, keeping current catalog", label)
		return
	}
	providers, ok := fetchCatalogSource(ctx, catalogFallbackProviderURL, maxCatalogFallbackProviderSize, "provider model catalog")
	if !ok {
		log.Warnf("%s: provider catalog unavailable, keeping current catalog", label)
		return
	}

	generated := time.Now().UTC().Format(time.RFC3339)
	encoded, err := BuildCatalogFallbackArtifact(canonical, providers, generated)
	if err != nil {
		log.Warnf("%s: rebuild failed, keeping current catalog: %v", label, err)
		return
	}

	changed, err := loadCatalogFallbackFromBytes(encoded, catalogFallbackCanonicalURL)
	if err != nil {
		log.Warnf("%s: rebuilt catalog rejected, keeping current data: %v", label, err)
		return
	}
	if !changed {
		log.Infof("%s completed, no changes detected (%d entries)", label, GetCatalogFallbackSize())
		return
	}
	log.Infof("%s completed, catalog updated (%d entries)", label, GetCatalogFallbackSize())
}

// fetchCatalogSource downloads one catalog, enforcing a size bound so a
// misbehaving endpoint cannot exhaust memory.
func fetchCatalogSource(ctx context.Context, sourceURL string, maxBytes int64, label string) ([]byte, bool) {
	reqCtx, cancel := context.WithTimeout(ctx, modelsFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, sourceURL, nil)
	if err != nil {
		log.Debugf("model catalog fallback: %s request creation failed for %s: %v", label, sourceURL, err)
		return nil, false
	}

	client := &http.Client{Timeout: modelsFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		log.Debugf("model catalog fallback: %s fetch failed from %s: %v", label, sourceURL, err)
		return nil, false
	}
	if resp.StatusCode != http.StatusOK {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Debugf("model catalog fallback: %s response close failed for %s: %v", label, sourceURL, errClose)
		}
		log.Debugf("model catalog fallback: %s fetch returned %d from %s", label, resp.StatusCode, sourceURL)
		return nil, false
	}

	data, errRead := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	errClose := resp.Body.Close()
	if errRead != nil {
		log.Debugf("model catalog fallback: %s read error from %s: %v", label, sourceURL, errRead)
		return nil, false
	}
	if errClose != nil {
		log.Debugf("model catalog fallback: %s response close failed for %s: %v", label, sourceURL, errClose)
		return nil, false
	}
	if int64(len(data)) > maxBytes {
		log.Warnf("model catalog fallback: %s from %s exceeded %d bytes", label, sourceURL, maxBytes)
		return nil, false
	}
	return data, true
}
