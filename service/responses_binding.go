package service

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/cachex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/samber/hot"
)

const ResponsesBindingContextKey = "responses_upstream_binding"

// Bind references to an owner and credential fingerprint, never credentials.
// Expiry or credential changes fail closed; clients can replay inline history.
type ResponsesBinding struct {
	RequestHash string
	ChannelID   int
	KeyIndex    int
	KeyHash     string
	RouteHash   string
	Model       string
	Group       string
}

var responsesBindingCacheOnce sync.Once
var responsesBindingCache *cachex.HybridCache[ResponsesBinding]

func getResponsesBindingCache() *cachex.HybridCache[ResponsesBinding] {
	responsesBindingCacheOnce.Do(func() {
		responsesBindingCache = cachex.NewHybridCache[ResponsesBinding](cachex.HybridCacheConfig[ResponsesBinding]{
			Namespace: cachex.Namespace("new-api:responses_binding:v1"), Redis: common.RDB,
			RedisEnabled: func() bool { return common.RedisEnabled && common.RDB != nil },
			RedisCodec:   responsesBindingCodec{},
			Memory: func() *hot.HotCache[string, ResponsesBinding] {
				return hot.NewHotCache[string, ResponsesBinding](hot.LRU, 100000).WithTTL(24 * time.Hour).WithJanitor().Build()
			},
		})
	})
	return responsesBindingCache
}

type responsesBindingCodec struct{}

func (responsesBindingCodec) Encode(value ResponsesBinding) (string, error) {
	b, e := common.Marshal(value)
	return string(b), e
}
func (responsesBindingCodec) Decode(value string) (ResponsesBinding, error) {
	var b ResponsesBinding
	e := common.UnmarshalJsonStr(value, &b)
	return b, e
}
func responsesBindingKey(c *gin.Context, id string) string {
	return fmt.Sprintf("%d:%d:%x", common.GetContextKeyInt(c, constant.ContextKeyUserId), common.GetContextKeyInt(c, constant.ContextKeyTokenId), sha256.Sum256([]byte(id)))
}
func responsesCredentialHash(key string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(key))) }
func responsesRouteHash(base, proxy string, headers map[string]interface{}) string {
	if len(headers) == 0 {
		headers = nil
	}
	data, _ := common.Marshal([]any{base, proxy, headers})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
func ResponsesBindingFromInfo(c *gin.Context, info *relaycommon.RelayInfo) ResponsesBinding {
	return ResponsesBinding{RequestHash: info.ResponsesUpstreamRequestHash, ChannelID: info.ChannelId, KeyIndex: info.ChannelMultiKeyIndex, KeyHash: responsesCredentialHash(info.ApiKey), Model: info.OriginModelName, Group: info.UsingGroup, RouteHash: responsesRouteHash(info.ChannelBaseUrl, info.ChannelSetting.Proxy, common.GetContextKeyStringMap(c, constant.ContextKeyChannelHeaderOverride))}
}
func RecordResponsesBinding(c *gin.Context, info *relaycommon.RelayInfo, id string) error {
	if id == "" || info == nil || info.ChannelMeta == nil || info.ChannelId <= 0 {
		return nil
	}
	return getResponsesBindingCache().SetWithTTL(responsesBindingKey(c, id), ResponsesBindingFromInfo(c, info), 24*time.Hour)
}
func GetResponsesBinding(c *gin.Context) *ResponsesBinding {
	if c == nil {
		return nil
	}
	if value, ok := c.Get(ResponsesBindingContextKey); ok {
		binding, _ := value.(*ResponsesBinding)
		return binding
	}
	return nil
}
func LoadResponsesBinding(c *gin.Context, id string) (*ResponsesBinding, error) {
	binding, found, err := getResponsesBindingCache().Get(responsesBindingKey(c, id))
	if err != nil {
		return nil, fmt.Errorf("cannot resolve previous_response_id binding: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("previous_response_id binding is unavailable; replay inline history without previous_response_id")
	}
	return &binding, nil
}

// Run after TokenAuth for every HTTP request and every WebSocket create.
func ValidateResponsesBinding(c *gin.Context, binding *ResponsesBinding, modelName string) (*model.Channel, error) {
	if binding.Model != modelName {
		return nil, fmt.Errorf("response continuation model changed")
	}
	if common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) {
		limits, _ := common.GetContextKeyType[map[string]bool](c, constant.ContextKeyTokenModelLimit)
		if _, ok := limits[ratio_setting.FormatMatchingModelName(modelName)]; !ok {
			return nil, fmt.Errorf("token cannot access continuation model")
		}
	}
	channel, err := model.CacheGetChannel(binding.ChannelID)
	if err != nil || channel == nil || channel.Status != common.ChannelStatusEnabled {
		return nil, fmt.Errorf("response continuation channel is unavailable")
	}
	if !setting.IsChannelAllowedForUserLevelID(common.GetContextKeyInt(c, constant.ContextKeyUserLevelID), channel.Name) {
		return nil, fmt.Errorf("response continuation channel is forbidden")
	}
	if pin := common.GetContextKeyString(c, constant.ContextKeyTokenSpecificChannelId); pin != "" && pin != fmt.Sprint(channel.Id) {
		return nil, fmt.Errorf("response continuation conflicts with token channel")
	}
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group == "auto" {
		allowed := false
		for _, candidate := range GetUserAutoGroup(common.GetContextKeyString(c, constant.ContextKeyUserGroup)) {
			if candidate == binding.Group {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, fmt.Errorf("response continuation group is forbidden")
		}
		group = binding.Group
		common.SetContextKey(c, constant.ContextKeyAutoGroup, group)
	}
	if group != binding.Group || !SubscriptionAllowsGroup(group, SubscriptionAllowedGroupsFromContext(c)) || !model.IsChannelEnabledForGroupModel(group, modelName, channel.Id) {
		return nil, fmt.Errorf("response continuation no longer has group/model access")
	}
	if _, err := ResponsesBoundKey(channel, binding); err != nil {
		return nil, err
	}
	c.Set(ResponsesBindingContextKey, binding)
	return channel, nil
}
func ResponsesBoundKey(channel *model.Channel, binding *ResponsesBinding) (string, error) {
	if channel.Id != binding.ChannelID {
		return "", fmt.Errorf("response continuation cannot switch channels")
	}
	key := channel.Key
	if channel.ChannelInfo.IsMultiKey {
		lock := model.GetChannelPollingLock(channel.Id)
		lock.Lock()
		defer lock.Unlock()
		keys := channel.GetKeys()
		if binding.KeyIndex < 0 || binding.KeyIndex >= len(keys) {
			return "", fmt.Errorf("response continuation credential removed")
		}
		status, exists := channel.ChannelInfo.MultiKeyStatusList[binding.KeyIndex]
		if exists && status != common.ChannelStatusEnabled {
			return "", fmt.Errorf("response continuation credential disabled")
		}
		key = keys[binding.KeyIndex]
	}
	if responsesCredentialHash(key) != binding.KeyHash {
		return "", fmt.Errorf("response continuation credential changed; replay inline history")
	}
	if responsesRouteHash(channel.GetBaseURL(), channel.GetSetting().Proxy, channel.GetHeaderOverride()) != binding.RouteHash {
		return "", fmt.Errorf("response continuation upstream settings changed; reconnect/replay required")
	}
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("response continuation credential is empty")
	}
	return key, nil
}

// Validate the effective request after model mapping and header overrides. A
// continuation cannot silently switch upstream accounts through a template.
func ValidateResponsesOutboundBinding(c *gin.Context, info *relaycommon.RelayInfo, request *http.Request) error {
	credentials := []any{request.URL.String(), request.Host, info.UpstreamModelName, request.Header.Values("Authorization"), request.Header.Values("Api-Key"), request.Header.Values("X-Api-Key"), request.Header.Values("Chatgpt-Account-Id"), request.Header.Values("OpenAI-Organization"), request.Header.Values("OpenAI-Project")}
	raw, err := common.Marshal(credentials)
	if err != nil {
		return err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	if binding := GetResponsesBinding(c); binding != nil && binding.RequestHash != "" && binding.RequestHash != hash {
		return fmt.Errorf("response continuation endpoint or effective credentials changed")
	}
	info.ResponsesUpstreamRequestHash = hash
	return nil
}
