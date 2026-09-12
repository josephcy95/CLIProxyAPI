package desensitization

import "strings"

func (e *Engine) restoreText(sessionID, text, _channel string, _final bool) string {
	if text == "" || !strings.Contains(text, "{{") {
		return text
	}
	return rePlaceholder.ReplaceAllStringFunc(text, func(tok string) string {
		orig, cat, ok := e.lookup(sessionID, tok)
		if !ok {
			return tok
		}
		if isSecretCategory(cat) && !e.cfg.RestoreSecretsEnabled() {
			return tok
		}
		return orig
	})
}
