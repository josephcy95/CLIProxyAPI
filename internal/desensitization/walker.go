package desensitization

import "strings"

// walkMask recursively visits all string leaves in decoded JSON (maps/slices).
func walkMask(v any, fn func(string) string) {
	walkMaskDepth(v, fn, 0)
}

func walkMaskDepth(v any, fn func(string) string, depth int) {
	if depth > 32 || v == nil {
		return
	}
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			switch c := child.(type) {
			case string:
				if shouldSkipBlob(k, c) {
					continue
				}
				t[k] = fn(c)
			default:
				walkMaskDepth(child, fn, depth+1)
			}
		}
	case []any:
		for i, child := range t {
			switch c := child.(type) {
			case string:
				if shouldSkipBlob("", c) {
					continue
				}
				t[i] = fn(c)
			default:
				walkMaskDepth(child, fn, depth+1)
			}
		}
	}
}

func shouldSkipBlob(key, s string) bool {
	if looksLikeBase64Blob(s) {
		return true
	}
	switch strings.ToLower(key) {
	case "image", "inline_data", "inlinedata", "audio", "video", "binary":
		if len(s) > 256 {
			return true
		}
	}
	return false
}
