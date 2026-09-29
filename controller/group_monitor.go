package controller

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/groupmonitor"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	monitorconfig "github.com/QuantumNous/new-api/setting/group_monitor"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

func GetGroupMonitorConfig(c *gin.Context) {
	cfg := monitorconfig.Get()
	common.ApiSuccess(c, gin.H{"config": cfg, "redis_enabled": groupmonitor.Ready(), "master": common.IsMasterNode})
}

func UpdateGroupMonitorConfig(c *gin.Context) {
	cfg := monitorconfig.Default()
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20), &cfg); err != nil {
		common.ApiErrorMsg(c, "监控配置 JSON 无效")
		return
	}
	cfg.Normalize()
	if err := monitorconfig.Validate(cfg); err != nil {
		common.ApiError(c, err)
		return
	}
	raw, err := common.Marshal(cfg)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err = model.UpdateOption(monitorconfig.OptionKey, string(raw)); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func monitorReady(c *gin.Context) (monitorconfig.Config, bool) {
	cfg := monitorconfig.Get()
	state := "ready"
	if !groupmonitor.Ready() {
		state = "redis_unavailable"
	} else if !cfg.Enabled {
		state = "disabled"
	} else {
		ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
		defer cancel()
		if common.RDB.Ping(ctx).Err() != nil {
			state = "redis_unavailable"
		}
	}
	if state != "ready" {
		common.ApiSuccess(c, gin.H{"state": state, "groups": []any{}})
		return cfg, false
	}
	return cfg, true
}

func monitorLatest(ctx context.Context, group, model, kind string) (*service.MonitorResult, error) {
	raw, err := common.RDB.Get(ctx, groupmonitor.Key(group, model, kind+":latest")).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result service.MonitorResult
	if err = common.UnmarshalJsonStr(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func GetGroupMonitor(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	cfg, ok := monitorReady(c)
	if !ok {
		return
	}
	hours, _ := strconv.Atoi(c.Query("hours"))
	if hours != 1 && hours != 6 && hours != 24 {
		hours = 1
	}
	selectedModels := map[string]string{}
	if raw := c.Query("models"); raw != "" {
		if len(raw) > 32768 || common.UnmarshalJsonStr(raw, &selectedModels) != nil || len(selectedModels) > 100 {
			c.Status(400)
			return
		}
	}
	for group, name := range selectedModels {
		g, exists := cfg.Groups[group]
		if !exists || !g.Visible() || (name != "" && !cfg.Contains(group, name)) {
			c.Status(400)
			return
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	now := time.Now()
	descs := setting.GetUserUsableGroupsCopy()
	ratios := ratio_setting.GetGroupRatioCopy()
	if userID := c.GetInt("id"); userID > 0 {
		userGroup, err := model.GetUserGroup(userID, false)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		for group := range ratios {
			ratios[group] = service.GetUserGroupRatio(userGroup, group)
		}
	}
	// Include effective ratios so cached snapshots stay specific to the user's pricing.
	cacheKey := groupmonitor.Key("snapshot", common.GetJsonString(cfg)+common.GetJsonString(descs)+common.GetJsonString(ratios)+strconv.Itoa(hours)+common.GetJsonString(selectedModels), "page-view-v2")
	if raw, err := common.RDB.Get(ctx, cacheKey).Result(); err == nil {
		var cached gin.H
		if common.UnmarshalJsonStr(raw, &cached) == nil {
			common.ApiSuccess(c, cached)
			return
		}
	}
	groups := []gin.H{}
	names := []string{}
	for name := range cfg.Groups {
		if cfg.Groups[name].Visible() {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, group := range names {
		g := cfg.Groups[group]
		all := []groupmonitor.Sample{}
		models := []gin.H{}
		for _, name := range g.Models {
			samples, err := groupmonitor.Read(ctx, group, name, "requests", now.Add(-time.Hour).UnixMilli(), now.UnixMilli())
			if err != nil {
				monitorReadError(c)
				return
			}
			all = append(all, samples...)
			probe, err := monitorLatest(ctx, group, name, "probe")
			if err != nil {
				monitorReadError(c)
				return
			}
			models = append(models, gin.H{"name": name, "probe": probe, "metrics": groupmonitor.Aggregate(samples)})
		}
		var ratio *float64
		if v, exists := ratios[group]; exists {
			ratio = &v
		}
		var svg, logic *service.MonitorResult
		var err error
		if g.TestSVG() {
			svg, err = monitorLatest(ctx, group, g.SVGModel, "svg")
		}
		if err != nil {
			monitorReadError(c)
			return
		}
		if g.TestLogic() {
			logic, err = monitorLatest(ctx, group, g.LogicModel, "logic")
		}
		if err != nil {
			monitorReadError(c)
			return
		}
		// Legacy latest entries can contain full replies. Page polling returns
		// summaries only; the selected record endpoint serves the full text.
		for _, latest := range []*service.MonitorResult{svg, logic} {
			if latest != nil {
				latest.Prompt, latest.Answer, latest.Expected = "", "", ""
			}
		}
		trendModels := g.Models
		if name := selectedModels[group]; name != "" {
			trendModels = []string{name}
		}
		trend, err := readMonitorTrend(ctx, group, trendModels, hours, now)
		if err != nil {
			monitorReadError(c)
			return
		}
		history := map[string][]service.MonitorResult{}
		for _, kind := range []string{"logic", "svg"} {
			history[kind], err = service.GetMonitorHistory(ctx, group, kind, cfg)
			if err != nil {
				monitorReadError(c)
				return
			}
		}
		artworks, err := service.GetMonitorArtworks(ctx, cfg, group)
		artworkError := ""
		if err != nil {
			artworks = []service.MonitorArtwork{}
			artworkError = "artworks_unavailable"
		}
		groups = append(groups, gin.H{"name": group, "description": descs[group], "ratio": ratio, "models": models, "metrics": groupmonitor.Aggregate(all), "svg": svg, "logic": logic, "svg_model": g.SVGModel, "logic_model": g.LogicModel, "svg_test": g.TestSVG(), "logic_test": g.TestLogic(), "protocol": g.Protocol, "trend": trend, "trend_model": selectedModels[group], "history": history, "artworks": artworks, "artworks_error": artworkError})
	}
	result := gin.H{"state": "ready", "hours": hours, "updated_at": now.UnixMilli(), "probe_minutes": cfg.ProbeMinutes, "logic_minutes": cfg.LogicMinutes, "groups": groups}
	if raw, err := common.Marshal(result); err == nil {
		_ = common.RDB.Set(ctx, cacheKey, raw, 15*time.Second).Err()
	}
	common.ApiSuccess(c, result)
}

func monitorReadError(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "监控数据暂时不可用", "data": gin.H{"state": "redis_unavailable"}})
}

func GetGroupMonitorTrend(c *gin.Context) {
	cfg, ok := monitorReady(c)
	if !ok {
		return
	}
	group := c.Query("group")
	g, ok := cfg.Groups[group]
	if !ok || !g.Visible() {
		c.Status(404)
		return
	}
	hours, _ := strconv.Atoi(c.Query("hours"))
	if hours != 1 && hours != 6 && hours != 24 {
		hours = 1
	}
	models := g.Models
	if name := c.Query("model"); name != "" {
		if !cfg.Contains(group, name) {
			c.Status(404)
			return
		}
		models = []string{name}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	now := time.Now()
	result, err := readMonitorTrend(ctx, group, models, hours, now)
	if err != nil {
		monitorReadError(c)
		return
	}
	common.ApiSuccess(c, result)
}

func readMonitorTrend(ctx context.Context, group string, models []string, hours int, now time.Time) (gin.H, error) {
	cacheKey := groupmonitor.Key(group, common.GetJsonString(models)+strconv.Itoa(hours), "trend-view")
	if raw, err := common.RDB.Get(ctx, cacheKey).Result(); err == nil {
		var cached gin.H
		if common.UnmarshalJsonStr(raw, &cached) == nil {
			return cached, nil
		}
	}
	actual, err := groupmonitor.Trend(ctx, group, models, now, hours)
	if err != nil {
		return nil, err
	}
	probes := []groupmonitor.Point{}
	for _, name := range models {
		samples, e := groupmonitor.Read(ctx, group, name, "probe", now.Add(-time.Duration(hours)*time.Hour).UnixMilli(), now.UnixMilli())
		if e != nil {
			return nil, e
		}
		for _, s := range samples {
			if s.TTFT != nil {
				probes = append(probes, groupmonitor.Point{At: s.At, TTFT: *s.TTFT, Count: 1})
			}
		}
	}
	if len(models) > 1 {
		buckets := map[int64]groupmonitor.Point{}
		for _, p := range probes {
			minute := p.At / 60000
			b := buckets[minute]
			b.At = minute * 60000
			b.TTFT += p.TTFT
			b.Count++
			buckets[minute] = b
		}
		probes = probes[:0]
		for _, p := range buckets {
			p.TTFT /= float64(p.Count)
			probes = append(probes, p)
		}
	}
	sort.Slice(probes, func(i, j int) bool { return probes[i].At < probes[j].At })
	result := gin.H{"actual": actual, "probe": probes, "from": now.Add(-time.Duration(hours) * time.Hour).UnixMilli(), "to": now.UnixMilli()}
	if raw, err := common.Marshal(result); err == nil {
		_ = common.RDB.Set(ctx, cacheKey, raw, 15*time.Second).Err()
	}
	return result, nil
}

func GetGroupMonitorArtworks(c *gin.Context) {
	cfg, ok := monitorReady(c)
	if !ok {
		return
	}
	group := c.Query("group")
	if g, exists := cfg.Groups[group]; !exists || !g.Visible() {
		c.Status(404)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	arts, err := service.GetMonitorArtworks(ctx, cfg, group)
	if err != nil {
		common.ApiErrorMsg(c, "作品暂时不可用，请检查 R2 配置")
		return
	}
	common.ApiSuccess(c, arts)
}

// Histories are scoped to visible groups; replies are loaded separately on demand.
func GetGroupMonitorHistory(c *gin.Context) {
	cfg, ok := monitorReady(c)
	if !ok {
		return
	}
	group := c.Query("group")
	if g, exists := cfg.Groups[group]; !exists || !g.Visible() {
		c.Status(404)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	history := map[string][]service.MonitorResult{}
	for _, kind := range []string{"logic", "svg"} {
		results, err := service.GetMonitorHistory(ctx, group, kind, cfg)
		if err != nil {
			monitorReadError(c)
			return
		}
		history[kind] = results
	}
	common.ApiSuccess(c, history)
}

func GetGroupMonitorRecord(c *gin.Context) {
	cfg, ok := monitorReady(c)
	if !ok {
		return
	}
	group, kind, id := c.Query("group"), c.Query("kind"), c.Query("id")
	if g, exists := cfg.Groups[group]; !exists || !g.Visible() {
		c.Status(404)
		return
	}
	if (kind != "logic" && kind != "svg") || len(id) > 64 || id == "" {
		c.Status(400)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	result, err := service.GetMonitorRecord(ctx, group, kind, id, cfg)
	if err == redis.Nil {
		c.Status(404)
		return
	}
	if err != nil {
		monitorReadError(c)
		return
	}
	common.ApiSuccess(c, result)
}

func SwitchMonitorTokenGroup(c *gin.Context) {
	var req struct {
		TokenID int    `json:"token_id"`
		Group   string `json:"group"`
	}
	if common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 4096), &req) != nil {
		c.Status(400)
		return
	}
	cfg := monitorconfig.Get()
	if g, ok := cfg.Groups[req.Group]; !ok || !g.Visible() || !cfg.Enabled {
		c.Status(404)
		return
	}
	userID := c.GetInt("id")
	userGroup, err := model.GetUserGroup(userID, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if _, ok := service.GetUserUsableGroups(userGroup)[req.Group]; !ok || !ratio_setting.ContainsGroupRatio(req.Group) {
		c.Status(403)
		return
	}
	token, err := model.GetTokenByIds(req.TokenID, userID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err = token.UpdateMonitorGroup(req.Group); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// Reuse token creation validation while enforcing the monitor's group scope.
func CreateMonitorToken(c *gin.Context) {
	var token model.Token
	if common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10), &token) != nil {
		c.Status(400)
		return
	}
	cfg := monitorconfig.Get()
	if g, ok := cfg.Groups[token.Group]; !ok || !g.Visible() || !cfg.Enabled {
		c.Status(404)
		return
	}
	group, err := model.GetUserGroup(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if _, ok := service.GetUserUsableGroups(group)[token.Group]; !ok || !ratio_setting.ContainsGroupRatio(token.Group) {
		c.Status(403)
		return
	}
	raw, err := common.Marshal(token)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	c.Request.ContentLength = int64(len(raw))
	AddToken(c)
}

func GetGroupMonitorPreview(c *gin.Context) {
	cfg, ok := monitorReady(c)
	if !ok {
		return
	}
	group, id := c.Query("group"), c.Query("id")
	if g, exists := cfg.Groups[group]; !exists || !g.Visible() {
		c.Status(404)
		return
	}
	if id == "" || len(id) > 64 {
		c.Status(400)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	document, err := service.GetMonitorLegacyPreview(ctx, cfg, group, id)
	if err == redis.Nil {
		c.Status(404)
		return
	}
	if err != nil {
		common.SysError("group monitor legacy preview: " + err.Error())
		c.Status(502)
		return
	}
	c.Header("Cache-Control", "private, max-age=300")
	c.Header("Content-Security-Policy", "sandbox allow-scripts")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(200, "text/html; charset=utf-8", []byte(document))
}
