package dto

import (
	"fmt"
	"net/url"
	"strings"
)

const AdvancedCustomGeminiToResponses = "gemini_generate_content_to_openai_responses"

// Wire names match upstream. This backport registers the Gemini -> Responses
// converter; unsupported converter IDs are rejected instead of silently passed.
type AdvancedCustomConfig struct {
	Routes []AdvancedCustomRoute `json:"advanced_routes"`
}
type AdvancedCustomRoute struct {
	IncomingPath string                   `json:"incoming_path"`
	UpstreamPath string                   `json:"upstream_path"`
	Converter    string                   `json:"converter"`
	Auth         *AdvancedCustomRouteAuth `json:"auth,omitempty"`
}
type AdvancedCustomRouteAuth struct {
	Type  string `json:"type"`
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

func (c *AdvancedCustomConfig) Validate() error {
	if c == nil || len(c.Routes) == 0 || len(c.Routes) > 64 {
		return fmt.Errorf("advanced_custom requires 1 to 64 routes")
	}
	seen := map[string]bool{}
	for i, r := range c.Routes {
		path := strings.ReplaceAll(r.IncomingPath, ":streamGenerateContent", ":generateContent")
		if path != "/v1/models/{model}:generateContent" && path != "/v1beta/models/{model}:generateContent" {
			return fmt.Errorf("advanced_custom route %d must use a Gemini generateContent path", i)
		}
		if seen[path] {
			return fmt.Errorf("advanced_custom route %d duplicates incoming path", i)
		}
		seen[path] = true
		if r.Converter != AdvancedCustomGeminiToResponses {
			return fmt.Errorf("advanced_custom route %d has an unsupported converter", i)
		}
		u, err := url.Parse(r.UpstreamPath)
		if err != nil || strings.TrimSpace(r.UpstreamPath) != r.UpstreamPath || r.UpstreamPath == "" || strings.ContainsAny(r.UpstreamPath, "\r\n\\{}") || u.Fragment != "" || u.User != nil || strings.HasPrefix(r.UpstreamPath, "//") {
			return fmt.Errorf("advanced_custom route %d has an invalid upstream path", i)
		}
		if u.IsAbs() {
			if (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				return fmt.Errorf("advanced_custom route %d requires HTTP(S)", i)
			}
		} else if !strings.HasPrefix(r.UpstreamPath, "/") {
			return fmt.Errorf("advanced_custom route %d requires an absolute path", i)
		}
		if a := r.Auth; a != nil {
			if a.Type != "none" && a.Type != "header" && a.Type != "query" {
				return fmt.Errorf("advanced_custom route %d has invalid auth type", i)
			}
			if a.Type != "none" && (a.Name == "" || a.Value == "" || strings.ContainsAny(a.Name, " \t\r\n:") || strings.ContainsAny(a.Value, "\r\n")) {
				return fmt.Errorf("advanced_custom route %d has invalid auth fields", i)
			}
			if a.Type == "header" {
				switch strings.ToLower(a.Name) {
				case "host", "content-length", "transfer-encoding", "connection":
					return fmt.Errorf("advanced_custom route %d cannot use transport headers for auth", i)
				}
			}
		}
	}
	return nil
}

func (c *AdvancedCustomConfig) MatchPath(path string) (AdvancedCustomRoute, bool) {
	if c == nil {
		return AdvancedCustomRoute{}, false
	}
	path = strings.ReplaceAll(strings.SplitN(path, "?", 2)[0], ":streamGenerateContent", ":generateContent")
	for _, route := range c.Routes {
		template := strings.ReplaceAll(route.IncomingPath, ":streamGenerateContent", ":generateContent")
		prefix, suffix, ok := strings.Cut(template, "{model}")
		if ok && strings.HasPrefix(path, prefix) && strings.HasSuffix(path, suffix) {
			model := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
			if model != "" && !strings.Contains(model, "/") {
				return route, true
			}
		}
	}
	return AdvancedCustomRoute{}, false
}
