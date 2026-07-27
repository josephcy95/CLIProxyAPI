package config

// Fork-only configuration: Codex private instructions and the xAI/Codex
// credential failure policies. Kept in a dedicated file so upstream splits of
// config.go do not silently drop them again.

type CodexInstructionsConfig struct {
	Enabled   bool     `yaml:"enabled" json:"enabled"`
	Mode      string   `yaml:"mode" json:"mode"`
	Content   string   `yaml:"content" json:"content"`
	File      string   `yaml:"file" json:"file"`
	Models    []string `yaml:"models" json:"models"`
	OAuthOnly *bool    `yaml:"oauth-only,omitempty" json:"oauth-only,omitempty"`
	// RequireAuthAllow restricts private-instruction requests to auths marked
	// allow_private_instructions. Defaults to true when omitted.
	RequireAuthAllow *bool `yaml:"require-auth-allow,omitempty" json:"require-auth-allow,omitempty"`
	// ReserveMarkedAuths excludes marked auths from normal (non-private) traffic.
	// Defaults to false so marked accounts still handle ordinary requests.
	ReserveMarkedAuths bool `yaml:"reserve-marked-auths,omitempty" json:"reserve-marked-auths,omitempty"`
	// UsePrefixSuffix keeps private mode opt-in through configured model prefixes and suffixes.
	// Defaults to true when omitted. When false, every eligible Codex request uses private mode.
	UsePrefixSuffix *bool `yaml:"use-prefix-suffix,omitempty" json:"use-prefix-suffix,omitempty"`
	// RequestMarkers configures model prefixes/suffixes that enable private mode.
	// Defaults to prefixes=["private/"] and suffixes=["-private"] when both empty.
	RequestMarkers CodexInstructionMarkersConfig `yaml:"request-markers,omitempty" json:"request-markers,omitempty"`
}

type CodexInstructionMarkersConfig struct {
	Prefixes []string `yaml:"prefixes,omitempty" json:"prefixes,omitempty"`
	Suffixes []string `yaml:"suffixes,omitempty" json:"suffixes,omitempty"`
}

// DefaultCodexFailurePolicy returns Codex credential failure defaults when keys are omitted.
func DefaultCodexFailurePolicy() CodexConfig {
	autoDisable := true
	authFailureAfter := 1
	usageLimitAfter := 3
	usageLimitFallbackHours := 1
	return CodexConfig{
		AutoDisableAuthFailures:         &autoDisable,
		AuthFailureDisableAfter:         &authFailureAfter,
		UsageLimitDisableAfter:          &usageLimitAfter,
		UsageLimitCooldownFallbackHours: &usageLimitFallbackHours,
	}
}

// NormalizeCodexConfig fills omitted Codex failure-policy values and clamps negatives.
func NormalizeCodexConfig(value CodexConfig) CodexConfig {
	defaults := DefaultCodexFailurePolicy()
	if value.AutoDisableAuthFailures == nil {
		value.AutoDisableAuthFailures = defaults.AutoDisableAuthFailures
	}
	if value.AuthFailureDisableAfter == nil {
		value.AuthFailureDisableAfter = defaults.AuthFailureDisableAfter
	} else if *value.AuthFailureDisableAfter < 0 {
		zero := 0
		value.AuthFailureDisableAfter = &zero
	}
	if value.UsageLimitDisableAfter == nil {
		value.UsageLimitDisableAfter = defaults.UsageLimitDisableAfter
	} else if *value.UsageLimitDisableAfter < 0 {
		zero := 0
		value.UsageLimitDisableAfter = &zero
	}
	if value.UsageLimitCooldownFallbackHours == nil {
		value.UsageLimitCooldownFallbackHours = defaults.UsageLimitCooldownFallbackHours
	} else if *value.UsageLimitCooldownFallbackHours < 0 {
		zero := 0
		value.UsageLimitCooldownFallbackHours = &zero
	}
	return value
}

func (value CodexConfig) AutoDisableAuthFailuresEnabled() bool {
	normalized := NormalizeCodexConfig(value)
	return normalized.AutoDisableAuthFailures != nil && *normalized.AutoDisableAuthFailures
}

func (value CodexConfig) AuthFailureDisableAfterValue() int {
	normalized := NormalizeCodexConfig(value)
	if !normalized.AutoDisableAuthFailuresEnabled() {
		return 0
	}
	return *normalized.AuthFailureDisableAfter
}

func (value CodexConfig) UsageLimitDisableAfterValue() int {
	normalized := NormalizeCodexConfig(value)
	return *normalized.UsageLimitDisableAfter
}

func (value CodexConfig) UsageLimitCooldownFallbackHoursValue() int {
	normalized := NormalizeCodexConfig(value)
	return *normalized.UsageLimitCooldownFallbackHours
}

// XAIConfig configures xAI/Grok request behavior and credential failure handling.
// Upstream inject-x-search lives here with the fork failure-policy fields so the
// single `xai:` YAML block cannot be split into two competing types.
type XAIConfig struct {
	// InjectXSearch injects xAI's native x_search tool when the request does not declare it.
	InjectXSearch bool `yaml:"inject-x-search" json:"inject-x-search"`
	// AutoDisablePermissionDenied permanently disables an auth file when xAI returns
	// a known permission-denied/access-denied response, or HTTP 401 after the one-shot
	// OAuth refresh retry (or when no refresh credential exists).
	AutoDisablePermissionDenied *bool `yaml:"auto-disable-permission-denied,omitempty" json:"auto-disable-permission-denied,omitempty"`
	// OtherForbiddenCooldownHours is the model cooldown for other xAI 403 responses.
	// Set to 0 to keep the model immediately eligible for a later request.
	OtherForbiddenCooldownHours *int `yaml:"other-403-cooldown-hours,omitempty" json:"other-403-cooldown-hours,omitempty"`
	// FreeUsageExhaustedCooldownHours is the model cooldown for xAI free-usage exhaustion.
	// Set to 0 to keep the model immediately eligible for a later request.
	FreeUsageExhaustedCooldownHours *int `yaml:"free-usage-exhausted-cooldown-hours,omitempty" json:"free-usage-exhausted-cooldown-hours,omitempty"`
	// FreeUsageExhaustedDisableAfter disables the auth file after this many free-usage
	// exhaustion events (post-cooldown re-hits). Persisted in auth-file runtime. 0 disables.
	FreeUsageExhaustedDisableAfter *int `yaml:"free-usage-exhausted-disable-after,omitempty" json:"free-usage-exhausted-disable-after,omitempty"`
	// OtherForbiddenDisableAfter disables the auth file after this many other-403 events
	// (post-cooldown re-hits). Persisted in auth-file runtime. 0 disables.
	OtherForbiddenDisableAfter *int `yaml:"other-403-disable-after,omitempty" json:"other-403-disable-after,omitempty"`
}

// DefaultXAIConfig returns the xAI failure policy used when the xai block is absent.
func DefaultXAIConfig() XAIConfig {
	autoDisable := true
	otherForbiddenCooldown := 6
	freeUsageExhaustedCooldown := 24
	freeUsageDisableAfter := 3
	otherForbiddenDisableAfter := 3
	return XAIConfig{
		AutoDisablePermissionDenied:     &autoDisable,
		OtherForbiddenCooldownHours:     &otherForbiddenCooldown,
		FreeUsageExhaustedCooldownHours: &freeUsageExhaustedCooldown,
		FreeUsageExhaustedDisableAfter:  &freeUsageDisableAfter,
		OtherForbiddenDisableAfter:      &otherForbiddenDisableAfter,
	}
}

// NormalizeXAIConfig fills omitted xAI policy values and clamps negative cooldowns.
func NormalizeXAIConfig(value XAIConfig) XAIConfig {
	defaults := DefaultXAIConfig()
	if value.AutoDisablePermissionDenied == nil {
		value.AutoDisablePermissionDenied = defaults.AutoDisablePermissionDenied
	}
	if value.OtherForbiddenCooldownHours == nil {
		value.OtherForbiddenCooldownHours = defaults.OtherForbiddenCooldownHours
	} else if *value.OtherForbiddenCooldownHours < 0 {
		zero := 0
		value.OtherForbiddenCooldownHours = &zero
	}
	if value.FreeUsageExhaustedCooldownHours == nil {
		value.FreeUsageExhaustedCooldownHours = defaults.FreeUsageExhaustedCooldownHours
	} else if *value.FreeUsageExhaustedCooldownHours < 0 {
		zero := 0
		value.FreeUsageExhaustedCooldownHours = &zero
	}
	if value.FreeUsageExhaustedDisableAfter == nil {
		value.FreeUsageExhaustedDisableAfter = defaults.FreeUsageExhaustedDisableAfter
	} else if *value.FreeUsageExhaustedDisableAfter < 0 {
		zero := 0
		value.FreeUsageExhaustedDisableAfter = &zero
	}
	if value.OtherForbiddenDisableAfter == nil {
		value.OtherForbiddenDisableAfter = defaults.OtherForbiddenDisableAfter
	} else if *value.OtherForbiddenDisableAfter < 0 {
		zero := 0
		value.OtherForbiddenDisableAfter = &zero
	}
	return value
}

func (value XAIConfig) AutoDisablePermissionDeniedEnabled() bool {
	normalized := NormalizeXAIConfig(value)
	return normalized.AutoDisablePermissionDenied != nil && *normalized.AutoDisablePermissionDenied
}

func (value XAIConfig) OtherForbiddenCooldownHoursValue() int {
	normalized := NormalizeXAIConfig(value)
	return *normalized.OtherForbiddenCooldownHours
}

func (value XAIConfig) FreeUsageExhaustedCooldownHoursValue() int {
	normalized := NormalizeXAIConfig(value)
	return *normalized.FreeUsageExhaustedCooldownHours
}

func (value XAIConfig) FreeUsageExhaustedDisableAfterValue() int {
	normalized := NormalizeXAIConfig(value)
	return *normalized.FreeUsageExhaustedDisableAfter
}

func (value XAIConfig) OtherForbiddenDisableAfterValue() int {
	normalized := NormalizeXAIConfig(value)
	return *normalized.OtherForbiddenDisableAfter
}
