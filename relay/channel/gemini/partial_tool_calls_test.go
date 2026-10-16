package gemini

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestGeminiPartialFunctionCallReconstruction(t *testing.T) {
	state := &geminiPartialStreamState{partialToolByCandidate: make(map[int64]*geminiPartialToolCall)}
	id := "call_partial"
	continued := true
	first := &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{Index: 0, Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{FunctionCall: &dto.FunctionCall{ID: &id, FunctionName: "lookup", PartialArgs: []dto.GeminiPartialArg{{JSONPath: "$.city", StringValue: common.GetPointer("Tok")}}, WillContinue: &continued}}}}}}}
	prepared, err := state.preparePartialFunctionCalls(first)
	require.NoError(t, err)
	require.Empty(t, prepared.Candidates[0].Content.Parts)

	continued = false
	number := 2.0
	second := &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{Index: 0, Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{FunctionCall: &dto.FunctionCall{PartialArgs: []dto.GeminiPartialArg{{JSONPath: "$.city", StringValue: common.GetPointer("yo")}, {JSONPath: "$.units[0]", NumberValue: &number}}, WillContinue: &continued}}}}}}}
	prepared, err = state.preparePartialFunctionCalls(second)
	require.NoError(t, err)
	require.Len(t, prepared.Candidates[0].Content.Parts, 1)
	call := prepared.Candidates[0].Content.Parts[0].FunctionCall
	require.Equal(t, "call_partial", *call.ID)
	require.Equal(t, "lookup", call.FunctionName)
	require.Equal(t, "Tokyo", call.Arguments.(map[string]any)["city"])
	require.Equal(t, float64(2), call.Arguments.(map[string]any)["units"].([]any)[0])
}

func TestGeminiPartialFunctionCallRejectsHugeArrayIndex(t *testing.T) {
	state := &geminiPartialStreamState{partialToolByCandidate: make(map[int64]*geminiPartialToolCall)}
	value := 1.0
	continued := false
	response := &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{Index: 0, Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{FunctionCall: &dto.FunctionCall{FunctionName: "lookup", PartialArgs: []dto.GeminiPartialArg{{JSONPath: "$.items[4096]", NumberValue: &value}}, WillContinue: &continued}}}}}}}
	_, err := state.preparePartialFunctionCalls(response)
	require.Error(t, err)
}

func TestGeminiPartialFunctionCallPreservesArgsSnapshot(t *testing.T) {
	state := &geminiPartialStreamState{partialToolByCandidate: make(map[int64]*geminiPartialToolCall)}
	_, ready, err := state.appendPartialFunctionCall(0, &dto.FunctionCall{FunctionName: "lookup", Arguments: map[string]any{"enabled": false}, WillContinue: common.GetPointer(true)})
	require.NoError(t, err)
	require.False(t, ready)
	call, ready, err := state.appendPartialFunctionCall(0, &dto.FunctionCall{Arguments: map[string]any{"count": float64(0)}, WillContinue: common.GetPointer(false)})
	require.NoError(t, err)
	require.True(t, ready)
	require.Equal(t, map[string]any{"enabled": false, "count": float64(0)}, call.Arguments)
}
