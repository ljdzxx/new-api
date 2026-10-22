package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestGeminiEmbeddingContentPartsRefund(t *testing.T) {
	db := setupResponsesRelayBillingTest(t)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()
	c, recorder, _ := newRelayMockContext(false)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"gpt-4o-mini","input":["hello",{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("model_mapping", `{"gpt-4o-mini":"gemini-embedding-001"}`)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeGemini)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
	Relay(c, types.RelayFormatOpenAI)
	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.Zero(t, calls.Load())
	assertGeminiResponsesRefund(t, db)
	var count int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&count).Error)
	require.Zero(t, count)
}
