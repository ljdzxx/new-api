package gemini

import (
	"strings"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// geminiGroundingCounter 在流式帧和候选结果之间去重查询词。
type geminiGroundingCounter struct {
	queries map[string]struct{}
}

// observe 收集 Google Search 的网页与图片查询，忽略空白项。
func (g *geminiGroundingCounter) observe(response *dto.GeminiChatResponse) {
	if response == nil {
		return
	}
	for _, candidate := range response.Candidates {
		if candidate.GroundingMetadata == nil {
			continue
		}
		metadata := candidate.GroundingMetadata
		for _, query := range append(append([]string(nil), metadata.WebSearchQueries...), metadata.ImageSearchQueries...) {
			query = strings.TrimSpace(query)
			if query == "" || len(g.queries) >= relaycommon.MaxGeminiSearchCount {
				continue
			}
			if g.queries == nil {
				g.queries = make(map[string]struct{})
			}
			g.queries[query] = struct{}{}
		}
	}
}

// commit 同时记录两种计量单位；最终价格表只启用其中一项。
func (g *geminiGroundingCounter) commit(info *relaycommon.RelayInfo) {
	if info == nil || len(g.queries) == 0 {
		return
	}
	info.SetBillableToolCount(relaycommon.GeminiGoogleSearchQueryTool, len(g.queries))
	info.SetBillableToolCount(relaycommon.GeminiGoogleSearchGroundedPromptTool, 1)
}
