package management

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestDesensitizationPreviewHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Desensitization: config.DefaultDesensitizationConfig()}
	h := &Handler{cfg: cfg}
	r := gin.New()
	r.POST("/preview", h.PreviewDesensitization)

	body, _ := json.Marshal(map[string]string{"text": "call 13800138000"})
	req := httptest.NewRequest(http.MethodPost, "/preview", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	masked, _ := resp["masked"].(string)
	if masked == "" || masked == "call 13800138000" {
		t.Fatalf("expected masked output, got %#v", resp)
	}
}

func TestGetDesensitizationConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Desensitization: config.DefaultDesensitizationConfig()}
	h := &Handler{cfg: cfg}
	r := gin.New()
	r.GET("/desensitization-config", h.GetDesensitizationConfig)
	req := httptest.NewRequest(http.MethodGet, "/desensitization-config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var got config.DesensitizationConfig
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Fatal("default enabled should be false")
	}
}
