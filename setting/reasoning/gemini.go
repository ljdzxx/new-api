package reasoning

import "strings"

// Gemini4ThinkingLevel is the provider default, applied before channel rules.
// A channel's explicit target effort must not be passed through this again.
func Gemini4ThinkingLevel(model, effort string) string {
	model = strings.TrimPrefix(strings.ToLower(model), "models/")
	if !strings.HasPrefix(model, "gemini-4-") || effort == "" {
		return effort
	}
	switch level := strings.ToLower(strings.TrimSpace(effort)); level {
	case "minimal", "none":
		return "low"
	case "xhigh", "max":
		if strings.HasPrefix(model, "gemini-4-hydrogen") || strings.HasPrefix(model, "gemini-4-neon") {
			return "xhigh"
		}
		return "high"
	case "low", "medium", "high":
		return level
	default:
		return "medium"
	}
}
