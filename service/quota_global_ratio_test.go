package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPostWssConsumeQuotaSettlesThresholdedRatioAgainstFinalRawInput(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	const userID, tokenID, channelID = 92001, 92001, 92001
	seedUser(t, userID, 99_900)
	seedToken(t, tokenID, userID, "wss-threshold-token", 99_900)
	seedChannel(t, channelID)

	info := &relaycommon.RelayInfo{
		UserId:           userID,
		ChannelMeta:      &relaycommon.ChannelMeta{ChannelId: channelID},
		TokenId:          tokenID,
		TokenKey:         "wss-threshold-token",
		OriginModelName:  "wss-threshold-model",
		StartTime:        time.Now(),
		WssConsumedQuota: 100,
		PriceData: types.PriceData{
			ModelRatio:                       1,
			CompletionRatio:                  1,
			AudioRatio:                       1,
			AudioCompletionRatio:             1,
			SystemGlobalModelRatio:           1,
			UserGlobalModelRatio:             1,
			ChannelModelRatio:                1,
			GlobalModelRatio:                 1,
			ConfiguredSystemGlobalModelRatio: 2,
			ConfiguredUserGlobalModelRatio:   1,
			ConfiguredChannelModelRatio:      1,
			SystemGlobalRatioThreshold:       100,
			GlobalRatioConfigSnapshot:        true,
			GroupRatioInfo:                   types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	usage := &dto.RealtimeUsage{
		InputTokens: 200,
		TotalTokens: 200,
		InputTokenDetails: dto.InputTokenDetails{
			TextTokens: 200,
		},
	}

	PostWssConsumeQuota(ctx, info, info.OriginModelName, usage, "")

	require.Equal(t, 400, info.WssConsumedQuota)
	require.Equal(t, 99_600, getUserQuota(t, userID))
	require.Equal(t, 99_600, getTokenRemainQuota(t, tokenID))
}
