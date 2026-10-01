package common

import (
	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

// Capture the caller's choice before filtering, conversion or parameter overrides.
// This describes the downstream request, not the tier ultimately used upstream.
func clientRequestedFastMode(request dto.Request) bool {
	var serviceTier string
	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		if req == nil || basecommon.Unmarshal(req.ServiceTier, &serviceTier) != nil {
			return false
		}
	case *dto.OpenAIResponsesRequest:
		if req == nil {
			return false
		}
		serviceTier = req.ServiceTier
	case *dto.OpenAIResponsesCompactionRequest:
		if req == nil {
			return false
		}
		serviceTier = req.ServiceTier
	case *dto.ClaudeRequest:
		var speed string
		return req != nil && basecommon.Unmarshal(req.Speed, &speed) == nil && speed == "fast"
	default:
		return false
	}
	return serviceTier == "fast" || serviceTier == "priority"
}
