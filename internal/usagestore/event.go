package usagestore

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

// Event is one durable usage row exposed to management APIs.
type Event struct {
	ID                  int64  `json:"id"`
	TimestampMS         int64  `json:"timestamp_ms"`
	RequestID           string `json:"request_id,omitempty"`
	Provider            string `json:"provider,omitempty"`
	ExecutorType        string `json:"executor_type,omitempty"`
	Model               string `json:"model,omitempty"`
	Alias               string `json:"alias,omitempty"`
	Endpoint            string `json:"endpoint,omitempty"`
	AuthType            string `json:"auth_type,omitempty"`
	AuthIndex           string `json:"auth_index,omitempty"`
	Source              string `json:"source,omitempty"`
	SourceHash          string `json:"source_hash,omitempty"`
	APIKeyHash          string `json:"api_key_hash,omitempty"`
	ReasoningEffort     string `json:"reasoning_effort,omitempty"`
	ServiceTier         string `json:"service_tier,omitempty"`
	ResponseServiceTier string `json:"response_service_tier,omitempty"`
	InputTokens         int64  `json:"input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	ReasoningTokens     int64  `json:"reasoning_tokens"`
	CachedTokens        int64  `json:"cached_tokens"`
	CacheReadTokens     int64  `json:"cache_read_tokens"`
	CacheCreationTokens int64  `json:"cache_creation_tokens"`
	TotalTokens         int64  `json:"total_tokens"`
	LatencyMS           *int64 `json:"latency_ms,omitempty"`
	TTFTMS              *int64 `json:"ttft_ms,omitempty"`
	Failed              bool   `json:"failed"`
	FailStatusCode      int    `json:"fail_status_code,omitempty"`
	FailSummary         string `json:"fail_summary,omitempty"`
	CreatedAtMS         int64  `json:"created_at_ms,omitempty"`
	EstimatedCost       *float64 `json:"estimated_cost,omitempty"`
}

// HashSecret returns a short stable hash for secrets (api keys, raw sources).
func HashSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

// MaskSource redacts emails and long tokens for display while keeping a hint.
func MaskSource(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "@") {
		parts := strings.SplitN(value, "@", 2)
		local := parts[0]
		domain := parts[1]
		if utf8.RuneCountInString(local) <= 2 {
			return local[:1] + "***@" + domain
		}
		runes := []rune(local)
		return string(runes[:2]) + "***@" + domain
	}
	if len(value) <= 8 {
		return value[:1] + "***"
	}
	return value[:4] + "***" + value[len(value)-2:]
}

// SummarizeFailBody produces a short, non-sensitive failure summary.
func SummarizeFailBody(body string, maxLen int) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	body = strings.ReplaceAll(body, "\n", " ")
	body = strings.Join(strings.Fields(body), " ")
	if maxLen <= 0 {
		maxLen = 240
	}
	if utf8.RuneCountInString(body) <= maxLen {
		return body
	}
	runes := []rune(body)
	return string(runes[:maxLen]) + "…"
}
