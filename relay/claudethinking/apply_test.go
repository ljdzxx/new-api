package claudethinking_test

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/claudethinking"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNativeClaudeControlsArePreserved(t *testing.T) {
	for _, fields := range []string{
		``,
		`,"thinking":{"type":"enabled","budget_tokens":8192}`,
		`,"thinking":{"type":"disabled"}`,
		`,"thinking":{"type":"adaptive","display":"omitted","block_binding":{"prefix_mismatch_behavior":"drop_block"},"future_field":false}`,
		`,"output_config":{"effort":"xhigh","format":{"type":"json_schema"}}`,
		`,"thinking":{"type":"vendor_mode"},"output_config":{"effort":"vendor_effort"}`,
	} {
		t.Run(fields, func(t *testing.T) {
			body := `{"model":"claude-opus-5.5","max_tokens":8192` + fields + `}`
			var original dto.ClaudeRequest
			require.NoError(t, common.UnmarshalJsonStr(body, &original))
			req, err := common.DeepCopy(&original)
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{OriginModelName: req.Model, ChannelMeta: &relaycommon.ChannelMeta{}}
			require.NoError(t, claudethinking.ApplyNative(nil, req, info))
			encoded, err := common.Marshal(req)
			require.NoError(t, err)
			for _, path := range []string{"model", "thinking", "output_config"} {
				require.Equal(t, gjson.Get(body, path).Exists(), gjson.GetBytes(encoded, path).Exists())
				if gjson.Get(body, path).Exists() {
					require.JSONEq(t, gjson.Get(body, path).Raw, gjson.GetBytes(encoded, path).Raw)
				}
			}
		})
	}
}

func TestNativeSuffixRendersAdaptiveAndPreservesOutputConfig(t *testing.T) {
	var req dto.ClaudeRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"claude-opus-5.5-medium","max_tokens":8192,"temperature":0,"top_p":0.9,"top_k":40,"thinking":{"type":"enabled","budget_tokens":32768,"block_binding":{"prefix_mismatch_behavior":"drop_block"}},"output_config":{"effort":"max","format":{"type":"json_schema"}}}`, &req))
	info := &relaycommon.RelayInfo{OriginModelName: req.Model, ChannelMeta: &relaycommon.ChannelMeta{}}
	require.NoError(t, claudethinking.ApplyNative(nil, &req, info))
	require.Equal(t, "claude-opus-5.5", req.Model)
	require.Equal(t, "adaptive", req.Thinking.Type)
	require.Nil(t, req.Thinking.BudgetTokens)
	require.Equal(t, "summarized", req.Thinking.Display)
	require.JSONEq(t, `{"prefix_mismatch_behavior":"drop_block"}`, string(req.Thinking.BlockBinding))
	require.JSONEq(t, `{"effort":"medium","format":{"type":"json_schema"}}`, string(req.OutputConfig))
	require.Equal(t, "medium", info.ReasoningEffort)
	require.Nil(t, req.Temperature)
	require.Nil(t, req.TopP)
	require.Nil(t, req.TopK)
	require.NotEmpty(t, info.ConversionDiagnostics())
}

func TestPassthroughSkipsThinkingButKeepsLocalEffortMapping(t *testing.T) {
	for _, global := range []bool{false, true} {
		t.Run(map[bool]string{false: "channel", true: "global"}[global], func(t *testing.T) {
			settings := model_setting.GetGlobalSettings()
			before := settings.PassThroughRequestEnabled
			settings.PassThroughRequestEnabled = global
			t.Cleanup(func() { settings.PassThroughRequestEnabled = before })
			body := []byte(`{"model":"claude-opus-5.5-thinking","thinking":{"type":"enabled","budget_tokens":8192},"extension":false}`)
			var req dto.ClaudeRequest
			require.NoError(t, common.Unmarshal(body, &req))
			info := &relaycommon.RelayInfo{OriginModelName: req.Model, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: req.Model}}
			info.ChannelSetting.PassThroughBodyEnabled = !global
			require.NoError(t, claudethinking.ApplyNative(nil, &req, info))
			require.Equal(t, "claude-opus-5.5-thinking", req.Model)
			require.Equal(t, "enabled", req.Thinking.Type)
			unchanged, err := helper.ApplyModelMappingToPassthroughBody(body, info, &req)
			require.NoError(t, err)
			require.Equal(t, body, unchanged)
			info.IsModelMapped = true
			info.ReasoningEffort = "medium"
			req.OutputConfig = []byte(`{"effort":"medium"}`)
			patched, err := helper.ApplyModelMappingToPassthroughBody(body, info, &req)
			require.NoError(t, err)
			require.Equal(t, "medium", gjson.GetBytes(patched, "output_config.effort").String())
			require.Equal(t, "enabled", gjson.GetBytes(patched, "thinking.type").String())
			require.Equal(t, "false", gjson.GetBytes(patched, "extension").Raw)
		})
	}
}

func TestClaudeThinkingSuffixControls(t *testing.T) {
	for _, tc := range []struct{ model, wantModel, kind, effort string }{
		{"claude-opus-5-5-thinking", "claude-opus-5-5", "adaptive", "medium"},
		{"claude-opus-5.5-thinking", "claude-opus-5.5", "adaptive", "medium"},
		{"claude-opus-5-5-nothinking", "claude-opus-5-5", "adaptive", "low"},
		{"claude-sonnet-5-5-none", "claude-sonnet-5-5", "between_tools", ""},
		{"claude-opus-5-5-thinking-8192", "claude-opus-5-5", "adaptive", "medium"},
		{"claude-opus-5-5@thinking:adaptive@effort:xhigh", "claude-opus-5-5", "adaptive", "xhigh"},
		{"claude-opus-4-6-xhigh", "claude-opus-4-6", "adaptive", "max"},
		{"claude-opus-4-8-minimal", "claude-opus-4-8", "adaptive", "low"},
	} {
		t.Run(tc.model, func(t *testing.T) {
			req := &dto.ClaudeRequest{Model: tc.model, MaxTokens: common.GetPointer(uint(8192))}
			require.NoError(t, claudethinking.ApplyNative(nil, req, nil))
			require.Equal(t, tc.wantModel, req.Model)
			require.NotNil(t, req.Thinking)
			require.Equal(t, tc.kind, req.Thinking.Type)
			require.Equal(t, tc.effort, req.GetEfforts())
		})
	}
}

func TestClaudeThinkingSuffixSettings(t *testing.T) {
	settings := model_setting.GetClaudeSettings()
	before := settings.ThinkingAdapterEnabled
	settings.ThinkingAdapterEnabled = false
	t.Cleanup(func() { settings.ThinkingAdapterEnabled = before })
	req := &dto.ClaudeRequest{Model: "claude-opus-5-5-medium"}
	require.NoError(t, claudethinking.ApplyNative(nil, req, nil))
	require.Equal(t, "claude-opus-5-5-medium", req.Model)
	require.Nil(t, req.Thinking)
	global := model_setting.GetGlobalSettings()
	blacklist := global.ThinkingModelBlacklist
	global.ThinkingModelBlacklist = append(append([]string(nil), blacklist...), "claude-opus-5-5@effort:medium")
	t.Cleanup(func() { global.ThinkingModelBlacklist = blacklist })
	req.Model = "claude-opus-5-5@effort:medium"
	require.NoError(t, claudethinking.ApplyNative(nil, req, nil))
	require.Equal(t, "claude-opus-5-5@effort:medium", req.Model)
	require.Nil(t, req.Thinking)
}

func TestManualThinkingBudgetsStayWithinMaxTokens(t *testing.T) {
	for _, budget := range []int{1, 1024, 8192, math.MaxInt} {
		maxTokens := uint(4096)
		out, err := claudethinking.RenderClaude("claude-3-7-sonnet", claudethinking.Intent{
			Mode: claudethinking.ModeEnabled, BudgetTokens: &budget,
		}, &maxTokens, 0.8)
		require.NoError(t, err)
		require.Equal(t, "enabled", out.Thinking.Type)
		require.GreaterOrEqual(t, *out.Thinking.BudgetTokens, 1024)
		require.Less(t, uint(*out.Thinking.BudgetTokens), maxTokens)
	}
	maxTokens := uint(math.MaxInt)
	out, err := claudethinking.RenderClaude("claude-3-7-sonnet", claudethinking.Intent{Mode: claudethinking.ModeEnabled}, &maxTokens, 0.8)
	require.NoError(t, err)
	require.Positive(t, *out.Thinking.BudgetTokens)
	require.Less(t, uint(*out.Thinking.BudgetTokens), maxTokens)
}
