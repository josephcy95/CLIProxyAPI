package management

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/desensitization"
)

// GetDesensitizationConfig returns the current desensitization settings.
func (h *Handler) GetDesensitizationConfig(c *gin.Context) {
	normalized := config.NormalizeDesensitizationConfig(h.cfg.Desensitization)
	c.JSON(http.StatusOK, normalized)
}

// PutDesensitizationConfig replaces desensitization settings and persists config.
func (h *Handler) PutDesensitizationConfig(c *gin.Context) {
	var body config.DesensitizationConfig
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body", "message": err.Error()})
		return
	}
	h.cfg.Desensitization = config.NormalizeDesensitizationConfig(body)
	desensitization.Configure(h.cfg.Desensitization)
	h.persist(c)
}

// PreviewDesensitization masks a sample string without mutating live session maps.
func (h *Handler) PreviewDesensitization(c *gin.Context) {
	var req struct {
		Text string `json:"text"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body", "message": err.Error()})
		return
	}
	cfg := config.NormalizeDesensitizationConfig(h.cfg.Desensitization)
	cfg.Enabled = true
	eng := desensitization.NewEngine(cfg)
	masked, hits, err := eng.Preview(req.Text)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "preview failed", "message": err.Error()})
		return
	}
	if hits == nil {
		hits = []desensitization.Hit{}
	}
	c.JSON(http.StatusOK, gin.H{"masked": masked, "hits": hits})
}
