package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestResponsesMultiAgentRoundTrip(t *testing.T) {
	for _, raw := range []string{`{"model":"m","multi_agent":{"enabled":false,"max_concurrent_subagents":0},"input":[]}`, `{"model":"m","multi_agent":{"enabled":true,"future":9007199254740993},"input":[]}`} {
		var req OpenAIResponsesRequest
		require.NoError(t, common.UnmarshalJsonStr(raw, &req))
		out, err := common.Marshal(req)
		require.NoError(t, err)
		require.JSONEq(t, raw, string(out))
	}
	for _, kind := range []string{"multi_agent_call", "multi_agent_call_output", "agent_message"} {
		req := OpenAIResponsesRequest{Input: []byte(`[{"type":"` + kind + `"}]`)}
		require.Error(t, req.ValidateStatelessConversion())
	}
	req := OpenAIResponsesRequest{Input: []byte(`[{"type":"function_call","agent":{"agent_name":"/root/worker"}}]`)}
	require.Error(t, req.ValidateStatelessConversion())
	req.Input = []byte(`[{"type":"function_call_output","output":{"agent":"ordinary tool data"}}]`)
	require.NoError(t, req.ValidateStatelessConversion())
}
