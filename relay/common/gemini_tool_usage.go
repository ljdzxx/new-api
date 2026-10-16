package common

import "github.com/QuantumNous/new-api/common"

const (
	GeminiGoogleSearchQueryTool          = "google_search"
	GeminiGoogleSearchGroundedPromptTool = "google_search_grounded_prompt"
	MaxGeminiSearchCount                 = 10_000
)

// SetBillableToolCount 写入上游响应确认的工具调用次数，重复快照采用覆盖语义。
func (info *RelayInfo) SetBillableToolCount(name string, count int) {
	if info == nil || name == "" || count < 0 {
		return
	}
	if count > MaxGeminiSearchCount {
		common.SysError("Gemini search count exceeded billing bound")
		count = MaxGeminiSearchCount
	}
	if info.ResponsesUsageInfo == nil {
		info.ResponsesUsageInfo = &ResponsesUsageInfo{}
	}
	if info.ResponsesUsageInfo.BuiltInTools == nil {
		info.ResponsesUsageInfo.BuiltInTools = make(map[string]*BuildInToolInfo)
	}
	info.ResponsesUsageInfo.BuiltInTools[name] = &BuildInToolInfo{ToolName: name, CallCount: count}
}
