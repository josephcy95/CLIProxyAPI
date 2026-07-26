package management

// Fork-only auth-file enrichment helpers: Codex plan type resolution and xAI
// failure status. Kept in a dedicated file so upstream refactors of
// auth_files.go do not silently drop them.

import (
	"strconv"
	"strings"
	"time"

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
