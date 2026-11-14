package controller

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponsesHTTPContinuationPinsKeyAndOwner(t *testing.T) {
	db := setupResponsesRelayBillingTest(t)
	oldMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemory })
	require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.ChannelDailyMark{}))
	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", 77).Update("key", "bindingtoken").Error)
	headers := make(chan string, 5)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_http_binding","object":"response","status":"completed","output":[],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`)
	}))
	defer upstream.Close()
	var channel model.Channel
	require.NoError(t, db.First(&channel, 88).Error)
	channel.Key = "key-one\nkey-two"
	channel.ChannelInfo.IsMultiKey = true
	channel.ChannelInfo.MultiKeyMode = constant.MultiKeyModePolling
	channel.ChannelInfo.MultiKeySize = 2
	channel.BaseURL = &upstream.URL
	settings, _ := common.Marshal(dto.ChannelSettings{ModelHealthEnabled: common.GetPointer(false)})
	channel.Setting = common.GetPointer(string(settings))
	require.NoError(t, db.Save(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	service.InitHttpClient()
	engine := gin.New()
	engine.POST("/v1/responses", middleware.BodyStorageCleanup(), middleware.TokenAuth(), middleware.Distribute(), func(c *gin.Context) { Relay(c, types.RelayFormatOpenAIResponses) })
	gateway := httptest.NewServer(engine)
	defer gateway.Close()
	request := func(token, previous string) (int, string) {
		raw := fmt.Sprintf(`{"model":"gpt-4o-mini","input":"hi","previous_response_id":%q}`, previous)
		r, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/responses", strings.NewReader(raw))
		require.NoError(t, err)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(r)
		require.NoError(t, err)
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		return response.StatusCode, string(body)
	}
	status, body := request("bindingtoken", "")
	require.Equal(t, 200, status, body)
	first := <-headers
	require.Equal(t, "Bearer key-one", first)
	status, body = request("bindingtoken", "resp_http_binding")
	require.Equal(t, 200, status, body)
	require.Equal(t, first, <-headers)
	// Another token belonging to the same user cannot inherit this reference.
	token := model.Token{Id: 78, UserId: 42, Key: "secondtoken", Status: common.TokenStatusEnabled, UnlimitedQuota: true, ExpiredTime: -1, Group: "default"}
	require.NoError(t, db.Create(&token).Error)
	status, body = request("secondtoken", "resp_http_binding")
	require.Equal(t, 409, status, body)
	// Removed or changed credentials must not fall back to a different key.
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 88).Update("key", "changed-key\nkey-two").Error)
	status, body = request("bindingtoken", "resp_http_binding")
	require.Equal(t, 409, status, body)
	select {
	case h := <-headers:
		t.Fatalf("unexpected upstream continuation: %s", h)
	default:
	}
}
