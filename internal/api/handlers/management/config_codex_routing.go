package management

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// GetCodexRoutingConfig returns the Codex account-tier routing policy.
func (h *Handler) GetCodexRoutingConfig(c *gin.Context) {
	c.JSON(http.StatusOK, h.cfg.Codex.Routing)
}

// PutCodexRoutingConfig replaces the Codex account-tier routing policy.
func (h *Handler) PutCodexRoutingConfig(c *gin.Context) {
	var body config.CodexRoutingConfig
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body", "message": err.Error()})
		return
	}
	h.cfg.Codex.Routing = body
	h.persist(c)
}
