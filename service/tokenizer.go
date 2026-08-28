package service

import (
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/tiktoken-go/tokenizer"
	"github.com/tiktoken-go/tokenizer/codec"
)

// tokenEncoderMap won't grow after initialization
var defaultTokenEncoder tokenizer.Codec

// tokenEncoderMap is used to store token encoders for different models
var tokenEncoderMap = make(map[string]tokenizer.Codec)

// tokenEncoderMutex protects tokenEncoderMap for concurrent access
var tokenEncoderMutex sync.RWMutex

func InitTokenEncoders() {
	common.SysLog("initializing token encoders")
	defaultTokenEncoder = codec.NewCl100kBase()
	common.SysLog("token encoders initialized")
}

func getTokenEncoder(model string) tokenizer.Codec {
	// The upstream tokenizer package intentionally keeps its model table
	// conservative.  GPT-5.6's public aliases are semantically GPT-5 models,
	// so resolve them to the package's official GPT-5 entry before calling
	// ForModel.  This makes the mapping explicit and prevents these aliases
	// from falling through to the cl100k default.
	modelForTokenizer := canonicalTokenizerModel(model)
	// First, try to get the encoder from cache with read lock
	tokenEncoderMutex.RLock()
	if encoder, exists := tokenEncoderMap[model]; exists {
		tokenEncoderMutex.RUnlock()
		return encoder
	}
	tokenEncoderMutex.RUnlock()

	// If not in cache, create new encoder with write lock
	tokenEncoderMutex.Lock()
	defer tokenEncoderMutex.Unlock()

	// Double-check if another goroutine already created the encoder
	if encoder, exists := tokenEncoderMap[model]; exists {
		return encoder
	}

	// Create new encoder
	modelCodec, err := tokenizer.ForModel(tokenizer.Model(modelForTokenizer))
	if err != nil {
		// Keep the historical fallback for genuinely unknown models. The known
		// GPT-5.6 aliases above always resolve through ForModel("gpt-5") and
		// therefore never take this branch in normal operation.
		// Cache the default encoder for this model to avoid repeated failures
		tokenEncoderMap[model] = defaultTokenEncoder
		return defaultTokenEncoder
	}

	// Cache the new encoder
	tokenEncoderMap[model] = modelCodec
	return modelCodec
}

func canonicalTokenizerModel(model string) string {
	lowerModel := strings.ToLower(strings.TrimSpace(model))
	switch lowerModel {
	case "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna",
		"gpt-5.6-sol-openai-compact", "gpt-5.6-terra-openai-compact", "gpt-5.6-luna-openai-compact":
		return "gpt-5"
	default:
		return model
	}
}

func getTokenNum(tokenEncoder tokenizer.Codec, text string) int {
	if text == "" {
		return 0
	}
	tkm, _ := tokenEncoder.Count(text)
	return tkm
}
