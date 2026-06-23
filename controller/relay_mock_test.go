package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

func setupRelayMockTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	common.DataExportEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	model.DB = db
	model.LOG_DB = db
	if err := db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}); err != nil {
		t.Fatalf("failed to migrate test tables: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	return db
}

func seedRelayMockBillingRows(t *testing.T, db *gorm.DB) {
	t.Helper()

	user := model.User{
		Id:               42,
		Username:         "api-user",
		Password:         "password123",
		Role:             common.RoleCommonUser,
		Status:           common.UserStatusEnabled,
		Quota:            1000,
		Group:            "default",
		GlobalModelRatio: 1,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	token := model.Token{
		Id:              77,
		UserId:          user.Id,
		Name:            "real-api-token",
		Key:             "sk-real-token",
		Status:          common.TokenStatusEnabled,
		RemainQuota:     1000,
		UnlimitedQuota:  false,
		CreatedTime:     1,
		AccessedTime:    1,
		ExpiredTime:     -1,
		Group:           "default",
		CrossGroupRetry: false,
	}
	if err := db.Create(&token).Error; err != nil {
		t.Fatalf("failed to create token: %v", err)
	}
	channel := model.Channel{
		Id:          88,
		Name:        "mock-channel",
		Type:        constant.ChannelTypeOpenAI,
		Key:         "sk-upstream",
		Status:      common.ChannelStatusEnabled,
		Models:      "gpt-4o-mini",
		Group:       "default",
		CreatedTime: 1,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}
}

func newRelayMockContext(stream bool) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
	common.SetContextKey(c, constant.ContextKeyUserId, 42)
	common.SetContextKey(c, constant.ContextKeyUserName, "api-user")
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(c, constant.ContextKeyUserQuota, 1000)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-4o-mini")
	common.SetContextKey(c, constant.ContextKeyTokenId, 77)
	common.SetContextKey(c, constant.ContextKeyTokenKey, "sk-real-token")
	common.SetContextKey(c, constant.ContextKeyTokenUnlimited, false)
	c.Set("token_name", "real-api-token")
	common.SetContextKey(c, constant.ContextKeyChannelId, 88)
	common.SetContextKey(c, constant.ContextKeyChannelName, "mock-channel")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "sk-upstream")
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{MockTest: true})
	common.SetContextKey(c, constant.ContextKeyChannelModelRatio, float64(1))
	common.SetContextKey(c, constant.ContextKeyChannelAllowWallet, true)
	common.SetContextKey(c, constant.ContextKeyChannelAllowSubscription, true)

	request := &dto.GeneralOpenAIRequest{
		Model:  "gpt-4o-mini",
		Stream: lo.ToPtr(stream),
		Messages: []dto.Message{
			{Role: "user", Content: "hello"},
		},
	}
	if stream {
		request.StreamOptions = &dto.StreamOptions{IncludeUsage: true}
	}
	info, err := relaycommon.GenRelayInfo(c, types.RelayFormatOpenAI, request, nil)
	if err != nil {
		panic(err)
	}
	return c, recorder, info
}

func TestMockTestRelayUsesPathBoundJSHandler(t *testing.T) {
	db := setupRelayMockTestDB(t)
	seedRelayMockBillingRows(t, db)
	c, recorder, info := newRelayMockContext(false)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{
		MockTest: true,
		MockJSHandlers: map[string]string{
			"/v1/chat/completions": `function process(body){
  return JSON.parse(body).messages[0].content.toUpperCase();
}`,
		},
	})

	if !shouldMockTestRelay(c, info) {
		t.Fatal("expected mock test relay to be enabled")
	}
	if apiErr := handleMockTestRelay(c, info); apiErr != nil {
		t.Fatalf("mock relay returned error: %v", apiErr)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "HELLO") {
		t.Fatalf("expected js handler output in mock response, body: %s", body)
	}

	var log model.Log
	if err := db.Order("id desc").First(&log).Error; err != nil {
		t.Fatalf("failed to read consume log: %v", err)
	}
	if log.Quota != 0 {
		t.Fatalf("expected js mock consume log quota 0, got %d", log.Quota)
	}
	if log.CompletionTokens <= 0 {
		t.Fatalf("expected js mock completion tokens to be simulated, got %d", log.CompletionTokens)
	}
}

func TestMockTestRelayUsesPathBoundJSHandlerForStream(t *testing.T) {
	db := setupRelayMockTestDB(t)
	seedRelayMockBillingRows(t, db)
	c, recorder, info := newRelayMockContext(true)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{
		MockTest: true,
		MockJSHandlers: map[string]string{
			"/v1/chat/completions": `function process(body){
  return "stream-" + JSON.parse(body).messages[0].content;
}`,
		},
	})

	if !shouldMockTestRelay(c, info) {
		t.Fatal("expected mock test relay to be enabled")
	}
	if apiErr := handleMockTestRelay(c, info); apiErr != nil {
		t.Fatalf("mock relay returned error: %v", apiErr)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "stream-hello") {
		t.Fatalf("expected js handler output in stream mock response, body: %s", body)
	}
	if !strings.Contains(body, "usage") {
		t.Fatalf("expected stream mock response to include usage, body: %s", body)
	}

	var log model.Log
	if err := db.Order("id desc").First(&log).Error; err != nil {
		t.Fatalf("failed to read consume log: %v", err)
	}
	if log.Quota != 0 {
		t.Fatalf("expected stream js mock consume log quota 0, got %d", log.Quota)
	}
}

func TestMockTestRelayFallsBackWhenNoJSHandlerMatchesPath(t *testing.T) {
	db := setupRelayMockTestDB(t)
	seedRelayMockBillingRows(t, db)
	c, recorder, info := newRelayMockContext(false)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{
		MockTest: true,
		MockJSHandlers: map[string]string{
			"/v1/responses": `function process(body){ return "responses"; }`,
		},
	})

	if !shouldMockTestRelay(c, info) {
		t.Fatal("expected mock test relay to be enabled")
	}
	if apiErr := handleMockTestRelay(c, info); apiErr != nil {
		t.Fatalf("mock relay returned error: %v", apiErr)
	}
	if body := recorder.Body.String(); !strings.Contains(body, mockTestResponseText) {
		t.Fatalf("expected default mock response when no js handler matches, body: %s", body)
	}
}

func TestMockTestRelayReturnsErrorWhenJSHandlerInvalid(t *testing.T) {
	db := setupRelayMockTestDB(t)
	seedRelayMockBillingRows(t, db)
	c, _, info := newRelayMockContext(false)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{
		MockTest: true,
		MockJSHandlers: map[string]string{
			"/v1/chat/completions": `function notProcess(body){ return "bad"; }`,
		},
	})

	if !shouldMockTestRelay(c, info) {
		t.Fatal("expected mock test relay to be enabled")
	}
	if apiErr := handleMockTestRelay(c, info); apiErr == nil {
		t.Fatal("expected invalid js handler to return error")
	}

	var count int64
	if err := db.Model(&model.Log{}).Count(&count).Error; err != nil {
		t.Fatalf("failed to count logs: %v", err)
	}
	if count != 0 {
		t.Fatalf("invalid js handler should not write success consume log, got %d logs", count)
	}
}

func TestMockTestRelayRecordsZeroQuotaAndRealExternalTokenName(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream_%t", stream), func(t *testing.T) {
			db := setupRelayMockTestDB(t)
			seedRelayMockBillingRows(t, db)
			c, recorder, info := newRelayMockContext(stream)

			if !shouldMockTestRelay(c, info) {
				t.Fatal("expected mock test relay to be enabled")
			}
			if apiErr := handleMockTestRelay(c, info); apiErr != nil {
				t.Fatalf("mock relay returned error: %v", apiErr)
			}
			body := recorder.Body.String()
			if !strings.Contains(body, mockTestResponseText) {
				t.Fatalf("mock response missing fixed text, body: %s", body)
			}
			if !strings.Contains(body, "usage") {
				t.Fatalf("mock response missing usage payload, body: %s", body)
			}

			var log model.Log
			if err := db.Order("id desc").First(&log).Error; err != nil {
				t.Fatalf("failed to read consume log: %v", err)
			}
			if log.Type != model.LogTypeConsume {
				t.Fatalf("expected consume log type, got %d", log.Type)
			}
			if log.Quota != 0 {
				t.Fatalf("expected mock consume log quota 0, got %d", log.Quota)
			}
			if log.TokenName != "real-api-token" {
				t.Fatalf("expected real external token name, got %q", log.TokenName)
			}
			if log.Content != "Mock测试" {
				t.Fatalf("expected mock content marker, got %q", log.Content)
			}
			if log.TokenId != 77 {
				t.Fatalf("expected token id 77, got %d", log.TokenId)
			}
			var other map[string]interface{}
			if err := common.Unmarshal([]byte(log.Other), &other); err != nil {
				t.Fatalf("failed to decode log other: %v", err)
			}
			if other["mock_test"] != true {
				t.Fatalf("expected other.mock_test=true, got %#v", other["mock_test"])
			}
			if _, ok := other["frt"].(float64); !ok {
				t.Fatalf("expected mock consume log to include numeric frt, got %#v", other["frt"])
			}

			var user model.User
			if err := db.First(&user, 42).Error; err != nil {
				t.Fatalf("failed to read user: %v", err)
			}
			if user.Quota != 1000 || user.UsedQuota != 0 || user.RequestCount != 0 {
				t.Fatalf("mock should not mutate user quota counters, got quota=%d used=%d requests=%d", user.Quota, user.UsedQuota, user.RequestCount)
			}
			var channel model.Channel
			if err := db.First(&channel, 88).Error; err != nil {
				t.Fatalf("failed to read channel: %v", err)
			}
			if channel.UsedQuota != 0 {
				t.Fatalf("mock should not mutate channel used quota, got %d", channel.UsedQuota)
			}
			var token model.Token
			if err := db.First(&token, 77).Error; err != nil {
				t.Fatalf("failed to read token: %v", err)
			}
			if token.RemainQuota != 1000 || token.UsedQuota != 0 {
				t.Fatalf("mock should not mutate token quota, got remain=%d used=%d", token.RemainQuota, token.UsedQuota)
			}
		})
	}
}

func TestMockTestRelayFallsBackToModelTestTokenNameForConsoleTest(t *testing.T) {
	db := setupRelayMockTestDB(t)
	seedRelayMockBillingRows(t, db)
	c, _, info := newRelayMockContext(false)
	c.Set("token_name", "")
	info.IsChannelTest = true

	if apiErr := handleMockTestRelay(c, info); apiErr != nil {
		t.Fatalf("mock relay returned error: %v", apiErr)
	}

	var log model.Log
	if err := db.Order("id desc").First(&log).Error; err != nil {
		t.Fatalf("failed to read consume log: %v", err)
	}
	if log.Quota != 0 {
		t.Fatalf("expected console mock consume log quota 0, got %d", log.Quota)
	}
	if log.TokenName != "模型测试" {
		t.Fatalf("expected console fallback token name, got %q", log.TokenName)
	}
	if log.Content != "模型测试" {
		t.Fatalf("expected console test content marker, got %q", log.Content)
	}
}

func TestNonMockTextConsumeQuotaStillBillsAndLogsRealTokenName(t *testing.T) {
	db := setupRelayMockTestDB(t)
	seedRelayMockBillingRows(t, db)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("username", "api-user")
	c.Set("token_name", "real-api-token")

	relayInfo := &relaycommon.RelayInfo{
		UserId:          42,
		UserQuota:       1000,
		TokenId:         77,
		TokenKey:        "sk-real-token",
		OriginModelName: "gpt-4o-mini",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: 88,
		},
		PriceData: types.PriceData{
			ModelRatio:             1,
			CompletionRatio:        2,
			CacheRatio:             1,
			SystemGlobalModelRatio: 1,
			UserGlobalModelRatio:   1,
			ChannelModelRatio:      1,
			GlobalModelRatio:       1,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
	}
	usage := &dto.Usage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
	}

	service.PostTextConsumeQuota(c, relayInfo, usage, nil)

	var log model.Log
	if err := db.Order("id desc").First(&log).Error; err != nil {
		t.Fatalf("failed to read consume log: %v", err)
	}
	if log.Quota != 20 {
		t.Fatalf("expected non-mock quota 20, got %d", log.Quota)
	}
	if log.TokenName != "real-api-token" {
		t.Fatalf("expected real token name, got %q", log.TokenName)
	}
	var user model.User
	if err := db.First(&user, 42).Error; err != nil {
		t.Fatalf("failed to read user: %v", err)
	}
	if user.Quota != 980 || user.UsedQuota != 20 || user.RequestCount != 1 {
		t.Fatalf("expected non-mock user quota counters to change, got quota=%d used=%d requests=%d", user.Quota, user.UsedQuota, user.RequestCount)
	}
	var channel model.Channel
	if err := db.First(&channel, 88).Error; err != nil {
		t.Fatalf("failed to read channel: %v", err)
	}
	if channel.UsedQuota != 20 {
		t.Fatalf("expected non-mock channel used quota 20, got %d", channel.UsedQuota)
	}
	var token model.Token
	if err := db.First(&token, 77).Error; err != nil {
		t.Fatalf("failed to read token: %v", err)
	}
	if token.RemainQuota != 980 || token.UsedQuota != 20 {
		t.Fatalf("expected non-mock token quota counters to change, got remain=%d used=%d", token.RemainQuota, token.UsedQuota)
	}
}
