package config

import "strings"

// DesensitizationConfig controls built-in request/response PII and secret masking.
type DesensitizationConfig struct {
	// Enabled turns the feature on. Default false so production is not surprised.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Restore replaces placeholders with originals in responses. Default true.
	Restore *bool `yaml:"restore,omitempty" json:"restore,omitempty"`
	// RestoreSecrets also restores API keys / PEM / tokens / JWTs / connstr passwords.
	// Default false — keep those as {{API_KEY_…}} etc. in replies.
	RestoreSecrets *bool `yaml:"restore_secrets,omitempty" json:"restore_secrets,omitempty"`
	// FailClosed rejects the request on engine/parse errors. Default false (pass through).
	FailClosed bool `yaml:"fail_closed" json:"fail_closed"`
	// SessionTTLMinutes is the in-memory mapping TTL (sliding). Default 20.
	SessionTTLMinutes *int `yaml:"session_ttl_minutes,omitempty" json:"session_ttl_minutes,omitempty"`
	// Categories toggles built-in detectors. Omitted keys use defaults.
	Categories DesensitizationCategories `yaml:"categories" json:"categories"`
	// CustomTerms are exact (optionally whole-word) replacements under category TERM.
	CustomTerms []DesensitizationTerm `yaml:"custom_terms" json:"custom_terms"`
	// CustomRegex are user-supplied patterns; invalid patterns are skipped at normalize time.
	CustomRegex []DesensitizationRegex `yaml:"custom_regex" json:"custom_regex"`
	// SecretPrefixes are high-signal credential prefixes (sk-, ghp_, …).
	SecretPrefixes []string `yaml:"secret_prefixes" json:"secret_prefixes"`
	// SkipModels skips masking when model or requested model matches (case-insensitive).
	SkipModels []string `yaml:"skip_models" json:"skip_models"`
	// SkipFormats skips masking when source format matches (case-insensitive).
	SkipFormats []string `yaml:"skip_formats" json:"skip_formats"`
}

// DesensitizationCategories holds per-detector switches. Nil pointer = use default.
type DesensitizationCategories struct {
	APIKey           *bool `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	Token            *bool `yaml:"token,omitempty" json:"token,omitempty"`
	PrivateKey       *bool `yaml:"private_key,omitempty" json:"private_key,omitempty"`
	Connstr          *bool `yaml:"connstr,omitempty" json:"connstr,omitempty"`
	Email            *bool `yaml:"email,omitempty" json:"email,omitempty"`
	Phone            *bool `yaml:"phone,omitempty" json:"phone,omitempty"`
	IDCard           *bool `yaml:"idcard,omitempty" json:"idcard,omitempty"`
	Card             *bool `yaml:"card,omitempty" json:"card,omitempty"`
	JWT              *bool `yaml:"jwt,omitempty" json:"jwt,omitempty"`
	IPPrivate        *bool `yaml:"ip_private,omitempty" json:"ip_private,omitempty"`
	IPInternal       *bool `yaml:"ip_internal,omitempty" json:"ip_internal,omitempty"`
	MAC              *bool `yaml:"mac,omitempty" json:"mac,omitempty"`
	Plate            *bool `yaml:"plate,omitempty" json:"plate,omitempty"`
	Landline         *bool `yaml:"landline,omitempty" json:"landline,omitempty"`
	AccessKey        *bool `yaml:"access_key,omitempty" json:"access_key,omitempty"`
	SecretAssignment *bool `yaml:"secret_assignment,omitempty" json:"secret_assignment,omitempty"`
}

// DesensitizationTerm is a custom exact-match term.
type DesensitizationTerm struct {
	Value     string `yaml:"value" json:"value"`
	Category  string `yaml:"category" json:"category"`
	WholeWord bool   `yaml:"whole_word" json:"whole_word"`
}

// DesensitizationRegex is a custom regex detector.
type DesensitizationRegex struct {
	Pattern  string `yaml:"pattern" json:"pattern"`
	Category string `yaml:"category" json:"category"`
}

// DefaultDesensitizationConfig returns conservative defaults (feature off until enabled).
func DefaultDesensitizationConfig() DesensitizationConfig {
	restore := true
	restoreSecrets := false
	ttl := 20
	on := true
	off := false
	return DesensitizationConfig{
		Enabled:           false,
		Restore:           &restore,
		RestoreSecrets:    &restoreSecrets,
		FailClosed:        false,
		SessionTTLMinutes: &ttl,
		Categories: DesensitizationCategories{
			APIKey:           &on,
			Token:            &on,
			PrivateKey:       &on,
			Connstr:          &on,
			Email:            &on,
			Phone:            &on,
			IDCard:           &on,
			Card:             &off,
			JWT:              &off,
			IPPrivate:        &off,
			IPInternal:       &off,
			MAC:              &off,
			Plate:            &off,
			Landline:         &off,
			AccessKey:        &off,
			SecretAssignment: &off,
		},
		SecretPrefixes: []string{"sk-", "ghp_", "github_pat_", "xoxb-", "AKIA"},
		CustomTerms:    nil,
		CustomRegex:    nil,
		SkipModels:     nil,
		SkipFormats:    nil,
	}
}

// NormalizeDesensitizationConfig fills omitted fields and clamps values.
func NormalizeDesensitizationConfig(value DesensitizationConfig) DesensitizationConfig {
	defaults := DefaultDesensitizationConfig()
	if value.Restore == nil {
		value.Restore = defaults.Restore
	}
	if value.RestoreSecrets == nil {
		value.RestoreSecrets = defaults.RestoreSecrets
	}
	if value.SessionTTLMinutes == nil {
		value.SessionTTLMinutes = defaults.SessionTTLMinutes
	} else if *value.SessionTTLMinutes < 1 {
		one := 1
		value.SessionTTLMinutes = &one
	} else if *value.SessionTTLMinutes > 24*60 {
		max := 24 * 60
		value.SessionTTLMinutes = &max
	}
	value.Categories = normalizeDesensitizationCategories(value.Categories, defaults.Categories)
	if value.SecretPrefixes == nil {
		value.SecretPrefixes = append([]string(nil), defaults.SecretPrefixes...)
	} else {
		cleaned := make([]string, 0, len(value.SecretPrefixes))
		seen := map[string]struct{}{}
		for _, p := range value.SecretPrefixes {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			cleaned = append(cleaned, p)
		}
		value.SecretPrefixes = cleaned
	}
	terms := make([]DesensitizationTerm, 0, len(value.CustomTerms))
	for _, t := range value.CustomTerms {
		t.Value = strings.TrimSpace(t.Value)
		if t.Value == "" {
			continue
		}
		t.Category = strings.TrimSpace(t.Category)
		if t.Category == "" {
			t.Category = "TERM"
		}
		terms = append(terms, t)
	}
	value.CustomTerms = terms
	regexes := make([]DesensitizationRegex, 0, len(value.CustomRegex))
	for _, r := range value.CustomRegex {
		r.Pattern = strings.TrimSpace(r.Pattern)
		if r.Pattern == "" {
			continue
		}
		r.Category = strings.TrimSpace(r.Category)
		if r.Category == "" {
			r.Category = "CUSTOM"
		}
		regexes = append(regexes, r)
	}
	value.CustomRegex = regexes
	value.SkipModels = trimStringSlice(value.SkipModels)
	value.SkipFormats = trimStringSlice(value.SkipFormats)
	return value
}

func normalizeDesensitizationCategories(value, defaults DesensitizationCategories) DesensitizationCategories {
	if value.APIKey == nil {
		value.APIKey = defaults.APIKey
	}
	if value.Token == nil {
		value.Token = defaults.Token
	}
	if value.PrivateKey == nil {
		value.PrivateKey = defaults.PrivateKey
	}
	if value.Connstr == nil {
		value.Connstr = defaults.Connstr
	}
	if value.Email == nil {
		value.Email = defaults.Email
	}
	if value.Phone == nil {
		value.Phone = defaults.Phone
	}
	if value.IDCard == nil {
		value.IDCard = defaults.IDCard
	}
	if value.Card == nil {
		value.Card = defaults.Card
	}
	if value.JWT == nil {
		value.JWT = defaults.JWT
	}
	if value.IPPrivate == nil {
		value.IPPrivate = defaults.IPPrivate
	}
	if value.IPInternal == nil {
		value.IPInternal = defaults.IPInternal
	}
	if value.MAC == nil {
		value.MAC = defaults.MAC
	}
	if value.Plate == nil {
		value.Plate = defaults.Plate
	}
	if value.Landline == nil {
		value.Landline = defaults.Landline
	}
	if value.AccessKey == nil {
		value.AccessKey = defaults.AccessKey
	}
	if value.SecretAssignment == nil {
		value.SecretAssignment = defaults.SecretAssignment
	}
	return value
}

func trimStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// RestoreEnabled reports whether response restore is on.
func (c DesensitizationConfig) RestoreEnabled() bool {
	n := NormalizeDesensitizationConfig(c)
	return n.Restore != nil && *n.Restore
}

// RestoreSecretsEnabled reports whether secret-category restore is on.
func (c DesensitizationConfig) RestoreSecretsEnabled() bool {
	n := NormalizeDesensitizationConfig(c)
	return n.RestoreSecrets != nil && *n.RestoreSecrets
}

// SessionTTLMinutesValue returns the TTL in minutes.
func (c DesensitizationConfig) SessionTTLMinutesValue() int {
	n := NormalizeDesensitizationConfig(c)
	if n.SessionTTLMinutes == nil {
		return 20
	}
	return *n.SessionTTLMinutes
}

// CategoryEnabled reports whether a detector category is enabled.
func (c DesensitizationConfig) CategoryEnabled(name string) bool {
	n := NormalizeDesensitizationConfig(c)
	cats := n.Categories
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "API_KEY":
		return cats.APIKey != nil && *cats.APIKey
	case "TOKEN":
		return cats.Token != nil && *cats.Token
	case "PRIVATE_KEY":
		return cats.PrivateKey != nil && *cats.PrivateKey
	case "CONNSTR":
		return cats.Connstr != nil && *cats.Connstr
	case "EMAIL":
		return cats.Email != nil && *cats.Email
	case "PHONE":
		return cats.Phone != nil && *cats.Phone
	case "IDCARD":
		return cats.IDCard != nil && *cats.IDCard
	case "CARD":
		return cats.Card != nil && *cats.Card
	case "JWT":
		return cats.JWT != nil && *cats.JWT
	case "IP_PRIVATE":
		return cats.IPPrivate != nil && *cats.IPPrivate
	case "IP_INTERNAL":
		return cats.IPInternal != nil && *cats.IPInternal
	case "MAC":
		return cats.MAC != nil && *cats.MAC
	case "PLATE":
		return cats.Plate != nil && *cats.Plate
	case "LANDLINE":
		return cats.Landline != nil && *cats.Landline
	case "ACCESS_KEY":
		return cats.AccessKey != nil && *cats.AccessKey
	case "SECRET_ASSIGNMENT", "SECRET":
		return cats.SecretAssignment != nil && *cats.SecretAssignment
	case "TERM", "CUSTOM":
		return true
	default:
		return true
	}
}
