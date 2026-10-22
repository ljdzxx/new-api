package service

import (
	"image"
	"math"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGeminiImageEstimates(t *testing.T) {
	for _, tc := range []struct {
		model, detail       string
		width, height, want int
	}{
		{"gemini-2.5-flash", "", 384, 384, 258},
		{"gemini-2.5-pro", "", 1024, 1024, 1290},
		{"gemini-2.5-flash", "", 3840, 2160, 3354},
		{"gemini-2.5-flash", "", 0, 0, 1290},
		{"gemini-2.5-flash", "", math.MaxInt, math.MaxInt, 3354},
		{"gemini-3-flash-preview", "MEDIA_RESOLUTION_LOW", 1, 1, 280},
		{"gemini-3-pro-preview", "MEDIA_RESOLUTION_MEDIUM", 4096, 4096, 560},
		{"gemini-3-pro-preview", "MEDIA_RESOLUTION_HIGH", 1, 1, 1120},
		{"gemini-4-argon", "MEDIA_RESOLUTION_ULTRA_HIGH", 1, 1, 2240},
		{"gemini-flash-latest", "", 0, 0, 1120},
	} {
		require.Equal(t, tc.want, geminiImageTokens(tc.model, tc.detail, tc.width, tc.height), "%+v", tc)
	}
}

func TestGeminiRequestEstimateUsesMetadataAndMediaFlags(t *testing.T) {
	oldCount, oldMedia, oldNonstream := constant.CountToken, constant.GetMediaToken, constant.GetMediaTokenNotStream
	constant.CountToken, constant.GetMediaToken, constant.GetMediaTokenNotStream = true, true, true
	t.Cleanup(func() {
		constant.CountToken, constant.GetMediaToken, constant.GetMediaTokenNotStream = oldCount, oldMedia, oldNonstream
	})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1beta/models/gemini-2.5-flash:generateContent", nil)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gemini-2.5-flash")
	var req dto.GeminiChatRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"systemInstruction":{"parts":[{"text":"system rules"}]},"contents":[{"parts":[{"functionCall":{"name":"lookup","args":{"x":3}}}]}]}`, &req))
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatGemini, Request: &req}
	tokens, err := EstimateRequestToken(c, &types.TokenCountMeta{}, info)
	require.NoError(t, err)
	require.Equal(t, EstimateToken(Gemini, req.GetTokenCountMeta().CombineText), tokens)
	require.Positive(t, tokens)

	info.Request = nil
	source := types.NewFileSourceFromData("AAAA", "image/png")
	source.SetCache(&types.CachedFileData{ImageConfig: &image.Config{Width: 256, Height: 256}})
	meta := &types.TokenCountMeta{Files: []*types.FileMeta{{FileType: types.FileTypeImage, Source: source}}}
	tokens, err = EstimateRequestToken(c, meta, info)
	require.NoError(t, err)
	require.Equal(t, 258, tokens)
	constant.GetMediaToken = false
	tokens, err = EstimateRequestToken(c, meta, info)
	require.NoError(t, err)
	require.Equal(t, 1290, tokens)
	constant.GetMediaToken = true
	meta.Files[0].Source = types.NewFileSourceFromData("AAAA", "image/avif")
	tokens, err = EstimateRequestToken(c, meta, info)
	require.NoError(t, err, "unknown inline dimensions must use the fallback")
	require.Equal(t, 1290, tokens)
	meta.Files[0].Source = types.NewFileSourceFromData("https://127.0.0.1:1/private.png", "image/png")
	tokens, err = EstimateRequestToken(c, meta, info)
	require.NoError(t, err, "native fileData must not be fetched")
	require.Equal(t, 1290, tokens)
}

func TestGeminiTextEstimateAndSaturationAudit(t *testing.T) {
	// Match upstream's floating accumulation followed by ceil (93 + epsilon).
	require.Equal(t, 94, EstimateToken(Gemini, strings.Repeat("中", 100)))
	// Force an oversized estimate through the same checked path without
	// allocating gigabytes of request data.
	multipliersLock.Lock()
	old := multipliersMap[Gemini]
	overflow := old
	overflow.Word = math.MaxFloat64
	multipliersMap[Gemini] = overflow
	multipliersLock.Unlock()
	t.Cleanup(func() { multipliersLock.Lock(); multipliersMap[Gemini] = old; multipliersLock.Unlock() })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{}
	tokens, err := estimateGeminiRequestToken(c, &types.TokenCountMeta{CombineText: "large request"}, info, "gemini-2.5-flash")
	require.NoError(t, err)
	require.Equal(t, common.MaxQuota, tokens)
	require.NotNil(t, info.QuotaClamp)
	require.Equal(t, common.QuotaClampOverflow, info.QuotaClamp.Kind)
	apiErr := PreConsumeBilling(c, tokens, info)
	require.NotNil(t, apiErr)
	require.Same(t, info.QuotaClamp, apiErr.Err)
	require.Nil(t, info.Billing, "oversized estimate must fail before any deduction")
}
