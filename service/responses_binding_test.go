package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestResponsesBindingRedisOwnerExpiryAndCredentials(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	originalRedis, originalEnabled := common.RDB, common.RedisEnabled
	originalCache := responsesBindingCache
	common.RDB, common.RedisEnabled = client, true
	responsesBindingCache = nil
	responsesBindingCacheOnce = sync.Once{}
	t.Cleanup(func() {
		common.RDB, common.RedisEnabled = originalRedis, originalEnabled
		responsesBindingCache = originalCache
		responsesBindingCacheOnce = sync.Once{}
		if originalCache != nil {
			responsesBindingCacheOnce.Do(func() {})
		}
	})
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserId, 42)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 77)
	info := &relaycommon.RelayInfo{OriginModelName: "m", UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 88, ApiKey: "secret", ChannelBaseUrl: "https://example.com"}}
	request, _ := http.NewRequest("POST", "https://example.com/v1/responses", nil)
	request.Header.Set("Authorization", "Bearer secret")
	require.NoError(t, ValidateResponsesOutboundBinding(ctx, info, request))
	require.NoError(t, RecordResponsesBinding(ctx, info, "resp_secret"))
	binding, err := LoadResponsesBinding(ctx, "resp_secret")
	require.NoError(t, err)
	require.Equal(t, 88, binding.ChannelID)
	keys := server.Keys()
	require.Len(t, keys, 1)
	stored, err := server.Get(keys[0])
	require.NoError(t, err)
	require.NotContains(t, stored, "secret")
	require.NotContains(t, keys[0], "resp_secret")
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 78)
	_, err = LoadResponsesBinding(ctx, "resp_secret")
	require.Error(t, err)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 77)
	ctx.Set(ResponsesBindingContextKey, binding)
	require.NoError(t, ValidateResponsesOutboundBinding(ctx, info, request))
	request.Header.Set("Authorization", "Bearer another-account")
	require.Error(t, ValidateResponsesOutboundBinding(ctx, info, request))
	request.Header.Set("Authorization", "Bearer secret")
	info.UpstreamModelName = "different-mapped-model"
	require.Error(t, ValidateResponsesOutboundBinding(ctx, info, request))
	server.FastForward(25 * time.Hour)
	_, err = LoadResponsesBinding(ctx, "resp_secret")
	require.Error(t, err)
}
func TestResponsesBoundKeyRejectsDisabledOrChangedCredential(t *testing.T) {
	base := "https://example.com"
	channel := &model.Channel{Id: 88, Key: "first\nsecond", BaseURL: &base}
	channel.ChannelInfo.IsMultiKey = true
	binding := &ResponsesBinding{ChannelID: 88, KeyIndex: 1, KeyHash: responsesCredentialHash("second"), RouteHash: responsesRouteHash(base, "", nil)}
	key, err := ResponsesBoundKey(channel, binding)
	require.NoError(t, err)
	require.Equal(t, "second", key)
	channel.ChannelInfo.MultiKeyStatusList = map[int]int{1: common.ChannelStatusManuallyDisabled}
	_, err = ResponsesBoundKey(channel, binding)
	require.Error(t, err)
	channel.ChannelInfo.MultiKeyStatusList = nil
	binding.KeyHash = responsesCredentialHash("replaced")
	_, err = ResponsesBoundKey(channel, binding)
	require.Error(t, err)
}
