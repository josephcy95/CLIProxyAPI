// Package codexinstructions provides helpers for private Codex instruction routing.
package codexinstructions

import (
	"strings"
)

const (
	// AuthMetadataKey is the auth-file JSON/metadata flag that allows private instruction use.
	AuthMetadataKey = "allow_private_instructions"
	// AuthAttributeKey mirrors AuthMetadataKey on runtime auth attributes.
	AuthAttributeKey = "allow_private_instructions"
	// RequestPrivateMetadataKey marks a request as private-instructions mode.
	RequestPrivateMetadataKey = "codex_private_instructions"
)

// MarkerConfig controls how private-instruction model markers are recognized.
type MarkerConfig struct {
	Prefixes []string `yaml:"prefixes,omitempty" json:"prefixes,omitempty"`
	Suffixes []string `yaml:"suffixes,omitempty" json:"suffixes,omitempty"`
}

// DefaultMarkers returns the default private model markers.
func DefaultMarkers() MarkerConfig {
	return MarkerConfig{
		Prefixes: []string{"private/"},
		Suffixes: []string{"-private"},
	}
}

// NormalizeMarkers trims markers and falls back to defaults when both lists are empty.
func NormalizeMarkers(cfg MarkerConfig) MarkerConfig {
	out := MarkerConfig{
		Prefixes: normalizeList(cfg.Prefixes),
		Suffixes: normalizeList(cfg.Suffixes),
	}
	if len(out.Prefixes) == 0 && len(out.Suffixes) == 0 {
		return DefaultMarkers()
	}
	return out
}

// ParseModel strips configured private markers from a model id.
// Thinking suffixes such as "(high)" should already be handled by the caller.
func ParseModel(model string, markers MarkerConfig) (base string, private bool) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", false
	}
	markers = NormalizeMarkers(markers)
	base = model
	changed := true
	for changed {
		changed = false
		for _, prefix := range markers.Prefixes {
			if prefix == "" {
				continue
			}
			if strings.HasPrefix(base, prefix) {
				base = strings.TrimSpace(base[len(prefix):])
				private = true
				changed = true
			}
		}
		for _, suffix := range markers.Suffixes {
			if suffix == "" {
				continue
			}
			if strings.HasSuffix(base, suffix) {
				base = strings.TrimSpace(base[:len(base)-len(suffix)])
				private = true
				changed = true
			}
		}
	}
	if base == "" {
		return model, false
	}
	return base, private
}

// AuthAllows reports whether attributes/metadata mark an auth for private Codex instructions.
func AuthAllows(attributes map[string]string, metadata map[string]any) bool {
	if attributes != nil {
		if raw := strings.TrimSpace(attributes[AuthAttributeKey]); raw != "" {
			if parsed, err := parseBool(raw); err == nil {
				return parsed
			}
		}
	}
	if metadata == nil {
		return false
	}
	return boolFromAny(metadata[AuthMetadataKey])
}

// RequestIsPrivate reports whether execution metadata requested private instructions.
func RequestIsPrivate(meta map[string]any) bool {
	if len(meta) == 0 {
		return false
	}
	return boolFromAny(meta[RequestPrivateMetadataKey])
}

// ModelMatches reports whether a model is eligible for configured private instructions.
func ModelMatches(patterns []string, model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	if len(patterns) == 0 {
		patterns = []string{"gpt-5.5", "gpt-5*"}
	}
	for _, pattern := range patterns {
		if matchModelPattern(strings.TrimSpace(pattern), model) {
			return true
		}
	}
	// Provider-prefixed ids (e.g. kiro/gpt-5.6-luna) should still match bare patterns.
	if idx := strings.LastIndex(model, "/"); idx >= 0 && idx+1 < len(model) {
		return ModelMatches(patterns, model[idx+1:])
	}
	return false
}

// VirtualModelIDs returns private catalog ids for a base model using configured markers.
// Skips ids that already carry a private marker.
func VirtualModelIDs(baseID string, markers MarkerConfig) []string {
	baseID = strings.TrimSpace(baseID)
	if baseID == "" {
		return nil
	}
	markers = NormalizeMarkers(markers)
	if _, alreadyPrivate := ParseModel(baseID, markers); alreadyPrivate {
		return nil
	}
	out := make([]string, 0, len(markers.Prefixes)+len(markers.Suffixes))
	seen := make(map[string]struct{}, len(markers.Prefixes)+len(markers.Suffixes))
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || id == baseID {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, prefix := range markers.Prefixes {
		if prefix == "" {
			continue
		}
		add(prefix + baseID)
	}
	for _, suffix := range markers.Suffixes {
		if suffix == "" {
			continue
		}
		add(baseID + suffix)
	}
	return out
}

func matchModelPattern(pattern, value string) bool {
	if pattern == "" {
		return false
	}
	if pattern == "*" || pattern == value {
		return true
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 || !strings.HasPrefix(value, parts[0]) {
		return false
	}
	pos := len(parts[0])
	for _, part := range parts[1 : len(parts)-1] {
		idx := strings.Index(value[pos:], part)
		if idx < 0 {
			return false
		}
		pos += idx + len(part)
	}
	last := parts[len(parts)-1]
	return last == "" || strings.HasSuffix(value[pos:], last)
}

func normalizeList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func boolFromAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, err := parseBool(typed)
		return err == nil && parsed
	case []byte:
		parsed, err := parseBool(string(typed))
		return err == nil && parsed
	case float64:
		return typed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	default:
		return false
	}
}

func parseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "t", "true", "yes", "y", "on":
		return true, nil
	case "0", "f", "false", "no", "n", "off":
		return false, nil
	default:
		return false, errInvalidBool
	}
}

type invalidBoolError struct{}

func (invalidBoolError) Error() string { return "invalid bool" }

var errInvalidBool invalidBoolError
