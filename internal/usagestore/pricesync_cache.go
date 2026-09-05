package usagestore

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// One bounded, process-local catalog cache, not a persistent source database.
// Only a successfully parsed response may replace the last validated entity.
var modelsDevCache struct {
	sync.Mutex
	url  string
	etag string
	body []byte
}

func fetchCachedModelsDevPrices(ctx context.Context, url string, client *http.Client) (map[string]remoteCatalogPrice, int, error) {
	modelsDevCache.Lock()
	defer modelsDevCache.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	if modelsDevCache.url == url && modelsDevCache.etag != "" {
		req.Header.Set("If-None-Match", modelsDevCache.etag)
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		if modelsDevCache.url != url || modelsDevCache.etag == "" || len(modelsDevCache.body) == 0 {
			return nil, 0, fmt.Errorf("models.dev: 304 without validated cached entity")
		}
		if etag := strings.TrimSpace(response.Header.Get("ETag")); etag != "" {
			modelsDevCache.etag = etag
		}
		return parseModelsDevPrices(modelsDevCache.body)
	}
	if response.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("models.dev: HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPriceSyncBodyBytes+1))
	if err != nil {
		return nil, 0, err
	}
	if len(body) > maxPriceSyncBodyBytes {
		return nil, 0, fmt.Errorf("models.dev response exceeds %d bytes", maxPriceSyncBodyBytes)
	}
	prices, skipped, err := parseModelsDevPrices(body)
	if err != nil {
		return nil, skipped, err
	}
	modelsDevCache.url, modelsDevCache.etag, modelsDevCache.body = url, strings.TrimSpace(response.Header.Get("ETag")), body
	return prices, skipped, nil
}
