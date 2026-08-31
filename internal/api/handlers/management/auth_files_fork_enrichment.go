package management

// Fork-only auth-file enrichment helpers: Codex plan type resolution and xAI
// failure status. Kept in a dedicated file so upstream refactors of
// auth_files.go do not silently drop them.

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func authCodexPlanType(auth *coreauth.Auth) string {
	if auth == nil || !strings.EqualFold(strings.TrimSpace(auth.Provider), "codex") {
		return ""
	}
	// Prefer stored plan fields (updated by quota refresh) over JWT claims.
	if planType := authMetadataString(auth, "plan_type"); planType != "" {
		return planType
	}
	if planType := authMetadataString(auth, "chatgpt_plan_type"); planType != "" {
		return planType
	}
	if planType := strings.TrimSpace(authAttribute(auth, "plan_type")); planType != "" {
		return planType
	}
	if claims := extractCodexIDTokenClaims(auth); claims != nil {
		if planType, _ := claims["plan_type"].(string); strings.TrimSpace(planType) != "" {
			return strings.TrimSpace(planType)
		}
	}
	return ""
}

func authMetadataString(auth *coreauth.Auth, key string) string {
	if auth == nil || auth.Metadata == nil {
		return ""
	}
	value, _ := auth.Metadata[key].(string)
	return strings.TrimSpace(value)
}

func authMetadataValue(auth *coreauth.Auth, key string) any {
	if auth == nil || auth.Metadata == nil {
		return nil
	}
	value, ok := auth.Metadata[key]
	if !ok || value == nil {
		return nil
	}
	if text, okString := value.(string); okString && strings.TrimSpace(text) == "" {
		return nil
	}
	return value
}

func authMetadataNumber(auth *coreauth.Auth, key string) (float64, bool) {
	switch value := authMetadataValue(auth, key).(type) {
	case nil:
		return 0, false
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int32:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint:
		return float64(value), true
	case uint32:
		return float64(value), true
	case uint64:
		return float64(value), true
	case json.Number:
		parsed, errParse := value.Float64()
		return parsed, errParse == nil
	case string:
		parsed, errParse := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return parsed, errParse == nil
	default:
		return 0, false
	}
}

func jsonWholeNumber(value float64) any {
	if value == float64(int64(value)) {
		return int64(value)
	}
	return value
}

func authMetadataSlice(auth *coreauth.Auth, key string) ([]any, bool) {
	if auth == nil || auth.Metadata == nil {
		return nil, false
	}
	raw, ok := auth.Metadata[key]
	if !ok || raw == nil {
		return nil, false
	}
	switch value := raw.(type) {
	case []any:
		return value, true
	case []map[string]any:
		out := make([]any, 0, len(value))
		for _, item := range value {
			out = append(out, item)
		}
		return out, true
	default:
		return nil, false
	}
}

// authCodexQuotaSnapshot copies persisted Codex subscription / reset-credit
// fields onto the management list DTO. Latest upstream values are stored on
// the auth file via PATCH and always replace the previous snapshot.
func authCodexQuotaSnapshot(auth *coreauth.Auth) gin.H {
	if auth == nil || !strings.EqualFold(strings.TrimSpace(auth.Provider), "codex") {
		return nil
	}
	out := gin.H{}
	if count, ok := authMetadataNumber(auth, "rate_limit_reset_credits_available_count"); ok {
		out["rate_limit_reset_credits_available_count"] = jsonWholeNumber(count)
	}
	if count, ok := authMetadataNumber(auth, "rate_limit_reset_credits_applicable_available_count"); ok {
		out["rate_limit_reset_credits_applicable_available_count"] = jsonWholeNumber(count)
	}
	if credits, ok := authMetadataSlice(auth, "rate_limit_reset_credits"); ok {
		out["rate_limit_reset_credits"] = credits
	}
	if checkedAt := authMetadataString(auth, "rate_limit_reset_credits_checked_at"); checkedAt != "" {
		out["rate_limit_reset_credits_checked_at"] = checkedAt
	}
	if until := authMetadataValue(auth, "chatgpt_subscription_active_until"); until != nil {
		out["chatgpt_subscription_active_until"] = until
	} else if until := authMetadataValue(auth, "subscription_active_until"); until != nil {
		out["chatgpt_subscription_active_until"] = until
	}
	for key, value := range auth.Quota.Signals {
		if strings.HasPrefix(key, "X-Codex-") && strings.TrimSpace(value) != "" {
			out[key] = value
		}
	}
	if !auth.Quota.ObservedAt.IsZero() {
		out["codex_quota_observed_at"] = auth.Quota.ObservedAt
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func xaiAuthStatus(auth *coreauth.Auth, now time.Time) (int, time.Time) {
	if auth == nil || !strings.EqualFold(strings.TrimSpace(auth.Provider), "xai") {
		return 0, time.Time{}
	}

	statusCode := 0
	if auth.LastError != nil {
		statusCode = auth.LastError.StatusCode()
	}

	cooldownUntil := auth.NextRetryAfter
	if !cooldownUntil.After(now) {
		cooldownUntil = time.Time{}
	}
	for _, state := range auth.ModelStates {
		if state == nil {
			continue
		}
		if statusCode == 0 && state.LastError != nil {
			statusCode = state.LastError.StatusCode()
		}
		if state.NextRetryAfter.After(now) &&
			(cooldownUntil.IsZero() || state.NextRetryAfter.Before(cooldownUntil)) {
			cooldownUntil = state.NextRetryAfter
		}
	}

	return statusCode, cooldownUntil
}

func authDisabledReason(auth *coreauth.Auth) string {
	if auth == nil || auth.Metadata == nil {
		return ""
	}
	reason, _ := auth.Metadata["disabled_reason"].(string)
	return strings.TrimSpace(reason)
}

func authAllowPrivateInstructionsValue(auth *coreauth.Auth) (bool, bool) {
	if auth == nil {
		return false, false
	}
	if auth.Attributes != nil {
		if raw := strings.TrimSpace(auth.Attributes["allow_private_instructions"]); raw != "" {
			parsed, errParse := strconv.ParseBool(raw)
			if errParse == nil {
				return parsed, true
			}
		}
	}
	if auth.Metadata == nil {
		return false, false
	}
	raw, ok := auth.Metadata["allow_private_instructions"]
	if !ok || raw == nil {
		return false, false
	}
	switch v := raw.(type) {
	case bool:
		return v, true
	case string:
		parsed, errParse := strconv.ParseBool(strings.TrimSpace(v))
		if errParse == nil {
			return parsed, true
		}
	}
	return false, false
}

// syncAuthFileAllowPrivateInstructionsAttribute mirrors metadata onto the
// runtime Attributes map so the scheduler private-instructions policy sees
// the flag after a management PATCH. Dropped once by the upstream auth_files
// split — keep it next to the other fork enrichment helpers.
func syncAuthFileAllowPrivateInstructionsAttribute(auth *coreauth.Auth) {
	if auth == nil {
		return
	}
	if auth.Attributes == nil {
		auth.Attributes = make(map[string]string)
	}
	allow, ok := authFileBoolValue(auth.Metadata["allow_private_instructions"])
	if !ok || !allow {
		delete(auth.Attributes, "allow_private_instructions")
		if auth.Metadata != nil {
			if !ok {
				delete(auth.Metadata, "allow_private_instructions")
			} else {
				auth.Metadata["allow_private_instructions"] = false
			}
		}
		return
	}
	auth.Attributes["allow_private_instructions"] = "true"
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	auth.Metadata["allow_private_instructions"] = true
}

func syncAuthFileCodexPlanTypeAttribute(auth *coreauth.Auth) {
	if auth == nil || !strings.EqualFold(strings.TrimSpace(auth.Provider), "codex") {
		return
	}
	if auth.Attributes == nil {
		auth.Attributes = make(map[string]string)
	}
	planType := authMetadataString(auth, "plan_type")
	if planType == "" {
		planType = authMetadataString(auth, "chatgpt_plan_type")
	}
	if planType == "" {
		delete(auth.Attributes, "plan_type")
		return
	}
	auth.Attributes["plan_type"] = planType
}
