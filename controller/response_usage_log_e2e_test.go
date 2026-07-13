package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	openaichannel "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestSQLiteResponseUsageMatchesConsumeLogTokens(t *testing.T) {
	db := setupRelayMockTestDB(t)
	seedRelayMockBillingRows(t, db)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	requestID := "response-usage-log-e2e"
	c.Set(common.RequestIdKey, requestID)
	c.Set("username", "api-user")
	c.Set("token_name", "real-api-token")

	info := &relaycommon.RelayInfo{
		UserId:                42,
		TokenId:               77,
		OriginModelName:       "gpt-4o-mini",
		UsingGroup:            "default",
		RelayFormat:           types.RelayFormatOpenAIResponses,
		StartTime:             time.Now(),
		FinalPreConsumedQuota: 137,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         88,
			UpstreamModelName: "gpt-4o-mini",
			ChannelSetting:    dto.ChannelSettings{},
		},
		PriceData: types.PriceData{
			ModelRatio:           1,
			CompletionRatio:      1,
			CacheRatio:           1,
			CacheCreationRatio:   1,
			CacheCreation5mRatio: 1,
			CacheCreation1hRatio: 1,
			ImageRatio:           1,
			AudioRatio:           1,
			AudioCompletionRatio: 1,
			GlobalModelRatio:     1.25,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio:     1,
				BaseGroupRatio: 1,
			},
		},
	}

	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(
			`{"id":"resp_e2e","usage":{"input_tokens":101,"output_tokens":9,"total_tokens":110}}`,
		)),
	}

	rawUsage, apiErr := openaichannel.OaiResponsesHandler(c, info, upstream)
	require.Nil(t, apiErr)
	require.NotNil(t, rawUsage)
	require.Equal(t, 101, rawUsage.PromptTokens)
	require.Equal(t, 9, rawUsage.CompletionTokens)

	service.PostTextConsumeQuota(c, info, rawUsage, nil)

	var consumeLog model.Log
	require.NoError(t, model.LOG_DB.Where("request_id = ? AND type = ?", requestID, model.LogTypeConsume).First(&consumeLog).Error)

	responsePromptTokens := int(gjson.GetBytes(recorder.Body.Bytes(), "usage.input_tokens").Int())
	responseCompletionTokens := int(gjson.GetBytes(recorder.Body.Bytes(), "usage.output_tokens").Int())
	responseTotalTokens := int(gjson.GetBytes(recorder.Body.Bytes(), "usage.total_tokens").Int())

	require.Equal(t, 126, responsePromptTokens)
	require.Equal(t, 11, responseCompletionTokens)
	require.Equal(t, 137, responseTotalTokens)
	require.Equal(t, responsePromptTokens, consumeLog.PromptTokens)
	require.Equal(t, responseCompletionTokens, consumeLog.CompletionTokens)
	require.Equal(t, responseTotalTokens, consumeLog.PromptTokens+consumeLog.CompletionTokens)
	require.Equal(t, 137, consumeLog.Quota)
}
