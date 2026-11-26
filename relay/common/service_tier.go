package common

import (
	"fmt"

	"github.com/QuantumNous/new-api/constant"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func (info *RelayInfo) MatchedServiceTier() string {
	if info == nil || info.ChannelMeta == nil || info.ChannelType != constant.ChannelTypeOpenAI {
		return ""
	}
	switch info.RelayFormat {
	case types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponsesCompaction:
	default:
		return ""
	}
	switch info.RelayMode {
	case relayconstant.RelayModeChatCompletions, relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
	default:
		return ""
	}
	config := info.ChannelOtherSettings.ServiceTierBilling
	if config == nil || !config.Enabled || info.PriceData.ServiceTier == nil {
		return ""
	}
	return info.PriceData.ServiceTier.Tier
}

// SetServiceTierJSON removes all copies of the member, including escaped keys,
// without decoding unrelated numbers or provider extensions.
func SetServiceTierJSON(body []byte, path, tier string) ([]byte, error) {
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return nil, fmt.Errorf("set service tier: expected a JSON object")
	}
	body, err := deleteJSONPath(body, path)
	if err != nil {
		return nil, err
	}
	return sjson.SetBytes(body, path, tier)
}

// PatchForcedServiceTierResponse only changes the client copy, using the value
// matched when preparing this attempt rather than mutable settings. It does not
// depend on whether the outbound request's tier was overridden.
func PatchForcedServiceTierResponse(body []byte, info *RelayInfo) ([]byte, error) {
	if info == nil || info.ForcedServiceTier == "" {
		return body, nil
	}
	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		if responseError := gjson.GetBytes(body, "error"); responseError.Exists() && responseError.Type != gjson.Null {
			return body, nil
		}
	case types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponsesCompaction:
		if gjson.GetBytes(body, "type").Exists() {
			if !gjson.GetBytes(body, "response").IsObject() {
				return body, nil
			}
			return SetServiceTierJSON(body, "response.service_tier", info.ForcedServiceTier)
		}
		if responseError := gjson.GetBytes(body, "error"); responseError.Exists() && responseError.Type != gjson.Null {
			return body, nil
		}
	default:
		return body, nil
	}
	return SetServiceTierJSON(body, "service_tier", info.ForcedServiceTier)
}
