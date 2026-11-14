package dto

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/tidwall/gjson"
)

// ValidateStatelessConversion rejects hosted state that another protocol cannot
// represent. A successful conversion must not silently discard an agent tree.
func (r *OpenAIResponsesRequest) ValidateStatelessConversion() error {
	if r == nil {
		return fmt.Errorf("request is nil")
	}
	if len(r.MultiAgent) > 0 {
		var config struct {
			Enabled *bool `json:"enabled"`
		}
		if err := common.Unmarshal(r.MultiAgent, &config); err != nil {
			return fmt.Errorf("invalid multi_agent: %w", err)
		}
		if config.Enabled != nil && *config.Enabled {
			return fmt.Errorf("hosted multi_agent requires a native Responses upstream")
		}
	}
	for _, item := range gjson.ParseBytes(r.Input).Array() {
		if agent := item.Get("agent"); agent.Exists() && agent.Type != gjson.Null {
			return fmt.Errorf("agent-attributed history requires a native Responses upstream")
		}
		switch item.Get("type").String() {
		case "multi_agent_call", "multi_agent_call_output", "agent_message":
			return fmt.Errorf("%s requires a native Responses upstream", item.Get("type").String())
		}
	}
	return nil
}

// Internal reverse mapping for converted Responses tool names.
type ResponsesToolIdentity struct {
	Namespace string
	Name      string
	Custom    bool
}
