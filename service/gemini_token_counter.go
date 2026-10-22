package service

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func estimateGeminiRequestToken(c *gin.Context, meta *types.TokenCountMeta, info *relaycommon.RelayInfo, model string) (int, error) {
	tokens, clamp := estimateTokenChecked(Gemini, meta.CombineText)
	if meta.TokenType == types.TokenTypeTextNumber {
		tokens, clamp = common.QuotaFromFloatChecked(float64(utf8.RuneCountInString(meta.CombineText)))
	}
	if clamp != nil && info.QuotaClamp == nil {
		info.QuotaClamp = clamp
	}
	total := float64(tokens)
	for _, file := range meta.Files {
		if file == nil || file.Source == nil {
			continue
		}
		switch file.FileType {
		case types.FileTypeImage:
			width, height := 0, 0
			readSize := geminiImageNeedsSize(model) && constant.GetMediaToken && (constant.GetMediaTokenNotStream || info.IsStream)
			// Preserve native fileData behavior: provider-owned URLs (including
			// gs://) are not downloaded by the gateway for estimation.
			if info.RelayFormat == types.RelayFormatGemini && (file.Source.IsURL() || strings.HasPrefix(file.Source.GetRawData(), "gs://")) {
				readSize = false
			}
			if readSize {
				config, _, err := GetImageConfig(c, file.Source)
				if err != nil && file.Source.IsURL() {
					return 0, fmt.Errorf("count Gemini image tokens: %w", err)
				}
				// Gemini accepts formats that the local image decoder may not
				// understand (e.g. AVIF). Unknown inline dimensions use the same
				// fallback as disabled media inspection; URL security errors fail.
				if err == nil {
					width, height = config.Width, config.Height
				}
			}
			total += float64(geminiImageTokens(model, file.Detail, width, height))
		case types.FileTypeAudio:
			total += 256
		case types.FileTypeVideo:
			total += 8192
		default:
			total += 4096
		}
	}
	tokens, clamp = common.QuotaFromFloatChecked(total)
	if clamp != nil && info.QuotaClamp == nil {
		info.QuotaClamp = clamp
	}
	common.SetContextKey(c, constant.ContextKeyPromptTokens, tokens)
	return tokens, nil
}

// Gemini 3+ and unversioned aliases use media resolution. Older versions use
// image dimensions. These are upstream tokenkit's Gemini rules (bb28f2ccad).
func geminiImageNeedsSize(model string) bool {
	_, version, ok := strings.Cut(strings.ToLower(model), "gemini-")
	if !ok {
		return false
	}
	end := 0
	for end < len(version) && version[end] >= '0' && version[end] <= '9' {
		end++
	}
	major, err := strconv.Atoi(version[:end])
	return err == nil && major < 3
}

func geminiImageTokens(model, detail string, width, height int) int {
	if !geminiImageNeedsSize(model) {
		switch strings.ToUpper(detail) {
		case "MEDIA_RESOLUTION_LOW":
			return 280
		case "MEDIA_RESOLUTION_MEDIUM":
			return 560
		case "MEDIA_RESOLUTION_ULTRA_HIGH":
			return 2240
		default:
			return 1120
		}
	}
	// Unknown dimensions use upstream's 1024x1024 fallback (1290 tokens).
	if width <= 0 || height <= 0 {
		width, height = 1024, 1024
	}
	if width <= 384 && height <= 384 {
		return 258
	}
	// Floating intermediates avoid multiplication overflow from image headers.
	w, h := float64(width), float64(height)
	if longest := max(w, h); longest > 65535 {
		w, h = max(1, math.Floor(w*65535/longest)), max(1, math.Floor(h*65535/longest))
	}
	tile := min(max(math.Floor(min(w, h)*2/3), 256), 768)
	tiles := min(math.Ceil(w/tile)*math.Ceil(h/tile), 12)
	return common.QuotaFromFloat((tiles + 1) * 258)
}
