package helps

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const openAIToolResultImageOmittedText = "[image omitted: unsupported by upstream]"

// ShouldNormalizeOpenAIToolResultsForModel reports whether the selected model
// explicitly excludes image input through its input-modalities configuration.
func ShouldNormalizeOpenAIToolResultsForModel(compat *config.OpenAICompatibility, upstreamModel, requestedModel string) bool {
	if compat == nil {
		return false
	}

	if normalize, matched := openAICompatibilityModelExcludesImages(compat.Models, upstreamModel); matched {
		return normalize
	}
	normalize, _ := openAICompatibilityModelExcludesImages(compat.Models, requestedModel)
	return normalize
}

// NormalizeOpenAIToolResultsTextOnly converts tool message content to strings.
// Text parts are preserved and image parts are replaced with a short marker.
// When Claude→OpenAI translation already relayed tool images into the next user
// message, those relay user messages are dropped and the omitted marker is
// appended to the preceding tool content so text-only upstreams stay clean.
func NormalizeOpenAIToolResultsTextOnly(payload []byte) []byte {
	messages := gjson.GetBytes(payload, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return payload
	}

	rawMessages := messages.Array()
	outMessages := make([][]byte, 0, len(rawMessages))
	for i := 0; i < len(rawMessages); i++ {
		message := rawMessages[i]
		role := message.Get("role").String()
		content := message.Get("content")
		raw := []byte(message.Raw)

		if role == "tool" {
			flattened := ""
			if content.Exists() && content.Type != gjson.String {
				flattened = flattenOpenAIToolResultContent(content)
			} else if content.Type == gjson.String {
				flattened = content.String()
			}
			if i+1 < len(rawMessages) && isOpenAIToolImageRelayUserMessage(rawMessages[i+1]) {
				if flattened == "" {
					flattened = openAIToolResultImageOmittedText
				} else if !strings.Contains(flattened, openAIToolResultImageOmittedText) {
					flattened = flattened + "\n\n" + openAIToolResultImageOmittedText
				}
				i++ // skip relay user message
			}
			if flattened != "" {
				if updated, errSet := sjson.SetBytes(raw, "content", flattened); errSet == nil {
					raw = updated
				}
			}
			outMessages = append(outMessages, raw)
			continue
		}
		outMessages = append(outMessages, raw)
	}

	if updated, errSet := sjson.SetRawBytes(payload, "messages", joinRawJSONArray(outMessages)); errSet == nil {
		return updated
	}
	return payload
}

func joinRawJSONArray(items [][]byte) []byte {
	if len(items) == 0 {
		return []byte("[]")
	}
	size := 2
	for _, item := range items {
		size += len(item) + 1
	}
	buf := make([]byte, 0, size)
	buf = append(buf, '[')
	for i, item := range items {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, item...)
	}
	buf = append(buf, ']')
	return buf
}

func isOpenAIToolImageRelayUserMessage(message gjson.Result) bool {
	if message.Get("role").String() != "user" {
		return false
	}
	content := message.Get("content")
	if !content.IsArray() {
		return false
	}
	hasImage := false
	hasRelayNotice := false
	onlyRelayParts := true
	content.ForEach(func(_, item gjson.Result) bool {
		typ := strings.ToLower(strings.TrimSpace(item.Get("type").String()))
		switch typ {
		case "image", "image_url", "input_image":
			hasImage = true
		case "text":
			text := strings.TrimSpace(item.Get("text").String())
			if text == "" {
				return true
			}
			if strings.Contains(strings.ToLower(text), "images returned by the preceding tool") {
				hasRelayNotice = true
				return true
			}
			onlyRelayParts = false
		default:
			if item.Get("image_url").Exists() {
				hasImage = true
				return true
			}
			onlyRelayParts = false
		}
		return true
	})
	return hasImage && hasRelayNotice && onlyRelayParts
}

func openAICompatibilityModelExcludesImages(models []config.OpenAICompatibilityModel, model string) (bool, bool) {
	model = normalizeOpenAICompatibilityModelName(model)
	if model == "" {
		return false, false
	}

	for i := range models {
		if strings.EqualFold(model, normalizeOpenAICompatibilityModelName(models[i].Name)) {
			return inputModalitiesExcludeImages(models[i].InputModalities), true
		}
	}

	matched := false
	excludesImages := true
	for i := range models {
		if !strings.EqualFold(model, normalizeOpenAICompatibilityModelName(models[i].Alias)) {
			continue
		}
		matched = true
		if !inputModalitiesExcludeImages(models[i].InputModalities) {
			excludesImages = false
		}
	}
	return excludesImages && matched, matched
}

func inputModalitiesExcludeImages(modalities []string) bool {
	if len(modalities) == 0 {
		return false
	}

	hasText := false
	for _, rawModality := range modalities {
		switch strings.ToLower(strings.TrimSpace(rawModality)) {
		case "image":
			return false
		case "text":
			hasText = true
		}
	}
	return hasText
}

func normalizeOpenAICompatibilityModelName(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	return strings.TrimSpace(thinking.ParseSuffix(model).ModelName)
}

func flattenOpenAIToolResultContent(content gjson.Result) string {
	if content.Type == gjson.String {
		return content.String()
	}

	if content.IsArray() {
		parts := make([]string, 0, 4)
		content.ForEach(func(_, item gjson.Result) bool {
			if part, ok := openAIToolResultPartText(item); ok {
				parts = append(parts, part)
			}
			return true
		})
		return strings.Join(parts, "\n\n")
	}

	if content.IsObject() {
		if isOpenAIImageToolResultPart(content) {
			return openAIToolResultImageOmittedText
		}
		if text := content.Get("text"); text.Type == gjson.String {
			return text.String()
		}
	}

	return content.Raw
}

func openAIToolResultPartText(item gjson.Result) (string, bool) {
	if item.Type == gjson.String {
		return item.String(), true
	}
	if item.IsObject() {
		if isOpenAIImageToolResultPart(item) {
			return openAIToolResultImageOmittedText, true
		}
		if text := item.Get("text"); text.Type == gjson.String {
			return text.String(), true
		}
	}
	if item.Raw == "" {
		return "", false
	}
	return item.Raw, true
}

func isOpenAIImageToolResultPart(item gjson.Result) bool {
	if !item.IsObject() {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(item.Get("type").String())) {
	case "image", "image_url", "input_image":
		return true
	}
	return item.Get("image_url").Exists() || item.Get("input_image").Exists()
}
