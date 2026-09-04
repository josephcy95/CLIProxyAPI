package common

import "github.com/tidwall/gjson"

// AlignClaudeToolResults orders tool_result blocks by preceding tool_use IDs.
func AlignClaudeToolResults(content gjson.Result, toolUseIDs []string) gjson.Result {
	if !content.IsArray() || len(toolUseIDs) == 0 {
		return content
	}
	parts := content.Array()
	results := make([]gjson.Result, 0, len(toolUseIDs))
	others := make([]gjson.Result, 0, len(parts))
	for _, part := range parts {
		if part.Get("type").String() == "tool_result" {
			results = append(results, part)
		} else {
			others = append(others, part)
		}
	}
	if len(results) != len(toolUseIDs) {
		return content
	}
	ordered := make([][]byte, 0, len(parts))
	used := make([]bool, len(results))
	for _, id := range toolUseIDs {
		found := -1
		for i, result := range results {
			if !used[i] && id != "" && result.Get("tool_use_id").String() == id {
				found = i
				break
			}
		}
		if found < 0 {
			return content
		}
		used[found] = true
		ordered = append(ordered, []byte(results[found].Raw))
	}
	for _, part := range others {
		ordered = append(ordered, []byte(part.Raw))
	}
	return gjson.ParseBytes(JoinRawArray(ordered))
}
