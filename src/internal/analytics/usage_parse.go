package analytics

import "github.com/buger/jsonparser"

// TokenUsage holds the token counts parsed from an OpenAI-compatible response.
type TokenUsage struct {
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
	CachedTokens int64
}

// ParseUsage extracts the `usage` object from an OpenAI-compatible JSON response
// body. ok is false when there is no usage object (or the body is not JSON).
// Individual missing fields default to 0.
func ParseUsage(body []byte) (TokenUsage, bool) {
	var u TokenUsage
	usageBytes, dataType, _, err := jsonparser.Get(body, "usage")
	if err != nil || dataType != jsonparser.Object {
		return u, false
	}
	u.InputTokens, _ = jsonparser.GetInt(usageBytes, "prompt_tokens")
	u.OutputTokens, _ = jsonparser.GetInt(usageBytes, "completion_tokens")
	u.TotalTokens, _ = jsonparser.GetInt(usageBytes, "total_tokens")
	u.CachedTokens, _ = jsonparser.GetInt(usageBytes, "prompt_tokens_details", "cached_tokens")
	return u, true
}
