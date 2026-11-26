package openai

import (
	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/gin-gonic/gin"
)

func sendTierStreamData(c *gin.Context, info *relaycommon.RelayInfo, data string) error {
	if info == nil || info.ForcedServiceTier == "" {
		return helper.StringData(c, data)
	}
	body, err := relaycommon.PatchForcedServiceTierResponse(common.StringToByteSlice(data), info)
	if err != nil {
		return err
	}
	return helper.StringData(c, string(body))
}

func sendTierStreamObject(c *gin.Context, info *relaycommon.RelayInfo, object any) error {
	body, err := common.Marshal(object)
	if err != nil {
		return err
	}
	return sendTierStreamData(c, info, string(body))
}
