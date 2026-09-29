package controller

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/groupmonitor"
	"github.com/QuantumNous/new-api/service"
	monitorconfig "github.com/QuantumNous/new-api/setting/group_monitor"
	"github.com/QuantumNous/new-api/setting/image_storage_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func installMonitorTestConfig(t *testing.T) monitorconfig.Config {
	t.Helper()
	cfg := monitorconfig.Default()
	cfg.Enabled = true
	cfg.BaseURL = "http://localhost:3000"
	cfg.Groups = map[string]monitorconfig.Group{"monitor-test": {Models: []string{"model"}, Key: "secret-never-public"}}
	raw, err := common.Marshal(cfg)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	old, exists := common.OptionMap[monitorconfig.OptionKey]
	common.OptionMap[monitorconfig.OptionKey] = string(raw)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if exists {
			common.OptionMap[monitorconfig.OptionKey] = old
		} else {
			delete(common.OptionMap, monitorconfig.OptionKey)
		}
	})
	return cfg
}

func TestMonitorNoRedisHidesAllData(t *testing.T) {
	installMonitorTestConfig(t)
	old := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = old })
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/monitor", nil)
	GetGroupMonitor(c)
	require.Contains(t, w.Body.String(), "redis_unavailable")
	require.NotContains(t, w.Body.String(), "monitor-test")
	require.NotContains(t, w.Body.String(), "secret-never-public")
}

func TestMonitorConfigReturnsPlaintextKeysWithoutChangingStoredConfig(t *testing.T) {
	installMonitorTestConfig(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	GetGroupMonitorConfig(c)
	require.Contains(t, w.Body.String(), "secret-never-public")
	require.NotContains(t, w.Body.String(), "********")
	require.Equal(t, "secret-never-public", monitorconfig.Get().Groups["monitor-test"].Key)
}

func TestMonitorPublicSnapshotUsesRedisSamples(t *testing.T) {
	installMonitorTestConfig(t)
	r := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: r.Addr()})
	defer client.Close()
	old, enabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDB, common.RedisEnabled = old, enabled })
	ttft := 12.0
	require.NoError(t, groupmonitor.Write(context.Background(), "monitor-test", "model", "requests", groupmonitor.Sample{ID: "sample", At: time.Now().UnixMilli(), Success: true, TTFT: &ttft}))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/monitor", nil)
	GetGroupMonitor(c)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"success_rate":1`)
	require.Contains(t, w.Body.String(), `"ttft":12`)
	require.NotContains(t, w.Body.String(), "secret-never-public")
	for _, key := range r.Keys() {
		require.Positive(t, r.TTL(key))
	}
}

func TestMonitorSnapshotUsesUserGroupRatiosAndIsolatesCache(t *testing.T) {
	cfg := installMonitorTestConfig(t)
	for _, group := range []string{"fallback", "free", "unpriced"} {
		cfg.Groups[group] = monitorconfig.Group{Models: []string{"model"}}
	}
	raw, err := common.Marshal(cfg)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	common.OptionMap[monitorconfig.OptionKey] = string(raw)
	common.OptionMapRWMutex.Unlock()

	r := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: r.Addr()})
	defer client.Close()
	old, enabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	baseRatios := ratio_setting.GroupRatio2JSONString()
	specialRatios := ratio_setting.GroupGroupRatio2JSONString()
	t.Cleanup(func() {
		common.RDB, common.RedisEnabled = old, enabled
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(baseRatios))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(specialRatios))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"monitor-test":2,"fallback":3,"free":1}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"vip":{"monitor-test":0.5,"free":0},"svip":{"monitor-test":0.2}}`))
	setUserGroup := func(id int, group string) {
		t.Helper()
		require.NoError(t, common.RedisHSetObj("user:"+strconv.Itoa(id), &model.UserBase{
			Id: id, Group: group, Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
		}, time.Hour))
	}
	setUserGroup(1, "vip")
	setUserGroup(2, "svip")
	setUserGroup(3, "default")

	read := func(userID int, expectedRatio, expectedFreeRatio float64) string {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/monitor", nil)
		if userID > 0 {
			c.Set("id", userID)
			c.Set("group", "stale-session-group")
		}
		GetGroupMonitor(c)
		require.Equal(t, 200, w.Code)
		require.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				Groups []struct {
					Name  string   `json:"name"`
					Ratio *float64 `json:"ratio"`
				} `json:"groups"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
		require.True(t, response.Success)
		require.Len(t, response.Data.Groups, 4)
		ratios := map[string]*float64{}
		for _, group := range response.Data.Groups {
			ratios[group.Name] = group.Ratio
		}
		for group, expected := range map[string]float64{"monitor-test": expectedRatio, "free": expectedFreeRatio, "fallback": 3} {
			require.NotNil(t, ratios[group], group)
			require.Equal(t, expected, *ratios[group], group)
		}
		require.Nil(t, ratios["unpriced"])
		return w.Body.String()
	}

	guest := read(0, 2, 1)
	vip := read(1, 0.5, 0)
	svip := read(2, 0.2, 1)
	read(3, 2, 1)
	require.JSONEq(t, guest, read(0, 2, 1))
	require.JSONEq(t, vip, read(1, 0.5, 0))
	require.JSONEq(t, svip, read(2, 0.2, 1))
	require.Equal(t, 2.0, ratio_setting.GetGroupRatio("monitor-test"))

	// Pricing changes and user group changes must bypass the previous snapshot.
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"vip":{"monitor-test":0.25,"free":0},"svip":{"monitor-test":0.2}}`))
	read(1, 0.25, 0)
	setUserGroup(1, "svip")
	read(1, 0.2, 1)
	read(0, 2, 1)
}

func TestMonitorHiddenGroupsAreNotPublic(t *testing.T) {
	cfg := installMonitorTestConfig(t)
	off := false
	g := cfg.Groups["monitor-test"]
	g.Active = &off
	cfg.Groups["monitor-test"] = g
	raw, err := common.Marshal(cfg)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	common.OptionMap[monitorconfig.OptionKey] = string(raw)
	common.OptionMapRWMutex.Unlock()
	r := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: r.Addr()})
	defer client.Close()
	old, enabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDB, common.RedisEnabled = old, enabled })
	for _, handler := range []gin.HandlerFunc{GetGroupMonitor, GetGroupMonitorTrend, GetGroupMonitorArtworks, GetGroupMonitorHistory, GetGroupMonitorRecord, GetGroupMonitorPreview} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/monitor?group=monitor-test", nil)
		handler(c)
		c.Writer.WriteHeaderNow()
		require.NotContains(t, w.Body.String(), "monitor-test")
		require.NotContains(t, w.Body.String(), "secret-never-public")
	}
	require.True(t, monitorconfig.Get().Contains("monitor-test", "model"), "hidden groups still collect samples")
}

func TestMonitorLogicUsesDedicatedModel(t *testing.T) {
	cfg := installMonitorTestConfig(t)
	on := true
	g := cfg.Groups["monitor-test"]
	g.LogicTest = &on
	g.LogicModel = "dedicated-logic"
	cfg.Groups["monitor-test"] = g
	raw, err := common.Marshal(cfg)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	common.OptionMap[monitorconfig.OptionKey] = string(raw)
	common.OptionMapRWMutex.Unlock()
	r := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: r.Addr()})
	defer client.Close()
	old, enabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDB, common.RedisEnabled = old, enabled })
	require.NoError(t, client.Set(context.Background(), groupmonitor.Key("monitor-test", "dedicated-logic", "logic:latest"), `{"ok":true,"answer":"dedicated-answer"}`, time.Hour).Err())
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/monitor", nil)
	GetGroupMonitor(c)
	require.Contains(t, w.Body.String(), `"logic_model":"dedicated-logic"`)
	require.Contains(t, w.Body.String(), `"ok":true`)
	require.NotContains(t, w.Body.String(), "dedicated-answer")
}

func TestMonitorHistoryDetailScope(t *testing.T) {
	installMonitorTestConfig(t)
	r := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: r.Addr()})
	defer client.Close()
	old, enabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDB, common.RedisEnabled = old, enabled })
	result := service.MonitorResult{ID: "test-record", Kind: "logic", Prompt: "question", Answer: "partial reply", Status: "request_failed", At: time.Now().UnixMilli()}
	raw, err := common.Marshal(result)
	require.NoError(t, err)
	require.NoError(t, client.Set(context.Background(), groupmonitor.Key("monitor-test", "test-record", "logic:record"), raw, time.Hour).Err())
	for _, tc := range []struct {
		group, kind, id string
		status          int
	}{
		{"monitor-test", "logic", "test-record", 200}, {"other", "logic", "test-record", 404},
		{"monitor-test", "svg", "test-record", 404}, {"monitor-test", "invalid", "test-record", 400},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/monitor/record?group="+tc.group+"&kind="+tc.kind+"&id="+tc.id, nil)
		GetGroupMonitorRecord(c)
		c.Writer.WriteHeaderNow()
		require.Equal(t, tc.status, w.Code)
		if tc.status == 200 {
			require.Contains(t, w.Body.String(), "partial reply")
			require.Contains(t, w.Body.String(), "question")
		} else {
			require.NotContains(t, w.Body.String(), "partial reply")
		}
		require.NotContains(t, w.Body.String(), "secret-never-public")
	}
}

func TestMonitorPageIncludesAllGroupsAndSectionsInOneResponse(t *testing.T) {
	cfg := installMonitorTestConfig(t)
	on, off := true, false
	cfg.Groups = map[string]monitorconfig.Group{
		"alpha":  {Models: []string{"fast", "slow"}, Key: "alpha-secret", SVGTest: &on, LogicTest: &on, SVGModel: "svg", LogicModel: "logic"},
		"beta":   {Models: []string{"fast"}, Key: "beta-secret"},
		"hidden": {Models: []string{"fast"}, Key: "hidden-secret", Active: &off},
	}
	raw, err := common.Marshal(cfg)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	common.OptionMap[monitorconfig.OptionKey] = string(raw)
	storage := image_storage_setting.GetImageStorageSetting()
	oldStorage := *storage
	storage.R2Endpoint, storage.R2Bucket = "https://example.r2.cloudflarestorage.com", "bucket"
	storage.R2AccessKeyID, storage.R2SecretAccessKey = "test-access", "test-secret"
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() { common.OptionMapRWMutex.Lock(); *storage = oldStorage; common.OptionMapRWMutex.Unlock() })
	r := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: r.Addr()})
	defer client.Close()
	old, enabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDB, common.RedisEnabled = old, enabled })
	ctx, now := context.Background(), time.Now()
	fast, slow := 10.0, 90.0
	for _, name := range []string{"alpha", "beta", "hidden"} {
		require.NoError(t, groupmonitor.Write(ctx, name, "fast", "requests", groupmonitor.Sample{ID: "fast", At: now.Add(-time.Minute).UnixMilli(), Success: true, TTFT: &fast}))
	}
	require.NoError(t, groupmonitor.Write(ctx, "alpha", "slow", "requests", groupmonitor.Sample{ID: "slow", At: now.Add(-time.Minute).UnixMilli(), Success: true, TTFT: &slow}))
	for _, kind := range []string{"logic", "svg"} {
		summary := service.MonitorResult{ID: kind + "-record", At: now.UnixMilli(), Model: kind, Kind: kind, OK: true, Status: "success"}
		data, err := common.Marshal(summary)
		require.NoError(t, err)
		require.NoError(t, client.ZAdd(ctx, groupmonitor.Key("alpha", "", kind+":history"), &redis.Z{Score: float64(summary.At), Member: string(data)}).Err())
		summary.Answer, summary.Prompt = "large-body-not-for-page", "full-prompt-not-for-page"
		data, err = common.Marshal(summary)
		require.NoError(t, err)
		require.NoError(t, client.Set(ctx, groupmonitor.Key("alpha", summary.ID, kind+":record"), data, time.Hour).Err())
	}
	art := service.MonitorArtwork{ID: "svg-record", At: now.UnixMilli(), Model: "svg", HTMLKey: "monitor-svg/alpha/artwork.html", PreviewVersion: 2}
	data, err := common.Marshal(art)
	require.NoError(t, err)
	require.NoError(t, client.LPush(ctx, groupmonitor.Key("alpha", "", "artworks"), string(data)).Err())
	type pageGroup struct {
		Name       string               `json:"name"`
		Metrics    groupmonitor.Metrics `json:"metrics"`
		TrendModel string               `json:"trend_model"`
		Trend      struct {
			Actual []groupmonitor.Point `json:"actual"`
			From   int64                `json:"from"`
			To     int64                `json:"to"`
		} `json:"trend"`
		History       map[string][]service.MonitorResult `json:"history"`
		Artworks      []service.MonitorArtwork           `json:"artworks"`
		ArtworksError string                             `json:"artworks_error"`
	}
	read := func(query string) ([]pageGroup, string) {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/monitor"+query, nil)
		GetGroupMonitor(c)
		require.Equal(t, 200, w.Code)
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				Groups []pageGroup `json:"groups"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
		require.True(t, response.Success)
		for _, private := range []string{"alpha-secret", "beta-secret", "hidden-secret", "test-secret", "large-body-not-for-page", "full-prompt-not-for-page", `"hidden"`, `"html_key"`} {
			require.NotContains(t, w.Body.String(), private)
		}
		return response.Data.Groups, w.Body.String()
	}
	groups, original := read("")
	require.Len(t, groups, 2)
	require.Equal(t, "alpha", groups[0].Name)
	require.Equal(t, 2, groups[0].Metrics.Total)
	require.Len(t, groups[0].Trend.Actual, 1)
	require.Equal(t, 50.0, groups[0].Trend.Actual[0].TTFT)
	require.Len(t, groups[0].History["logic"], 1)
	require.Len(t, groups[0].History["svg"], 1)
	require.Len(t, groups[0].Artworks, 1)
	require.Contains(t, groups[0].Artworks[0].HTMLURL, "X-Amz-Signature")
	require.Empty(t, groups[1].History["logic"])
	require.Empty(t, groups[1].Artworks)
	_, cached := read("")
	require.JSONEq(t, original, cached)
	groups, _ = read("?hours=6&models=" + url.QueryEscape(`{"alpha":"fast"}`))
	require.Equal(t, "fast", groups[0].TrendModel)
	require.Equal(t, 10.0, groups[0].Trend.Actual[0].TTFT)
	require.Equal(t, int64((6*time.Hour)/time.Millisecond), groups[0].Trend.To-groups[0].Trend.From)
	require.Equal(t, 2, groups[0].Metrics.Total, "trend selection must not change group metrics")
	for _, models := range []string{`{"hidden":"fast"}`, `{"alpha":"missing"}`, `{`} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/monitor?models="+url.QueryEscape(models), nil)
		GetGroupMonitor(c)
		c.Writer.WriteHeaderNow()
		require.Equal(t, 400, w.Code)
	}
	common.OptionMapRWMutex.Lock()
	storage.R2AccessKeyID = ""
	common.OptionMapRWMutex.Unlock()
	groups, _ = read("?hours=24")
	require.Equal(t, "artworks_unavailable", groups[0].ArtworksError)
	require.Len(t, groups[0].History["svg"], 1, "R2 configuration errors must not hide tests or other groups")
}
