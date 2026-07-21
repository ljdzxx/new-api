package common

import (
	"testing"

	appcommon "github.com/QuantumNous/new-api/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestShouldStripImageGeneration(t *testing.T) {
	original := appcommon.DisableImageGenerationOnNonImageEndpoints
	t.Cleanup(func() { appcommon.DisableImageGenerationOnNonImageEndpoints = original })

	appcommon.DisableImageGenerationOnNonImageEndpoints = true
	require.True(t, ShouldStripImageGeneration(relayconstant.RelayModeResponses))
	require.True(t, ShouldStripImageGeneration(relayconstant.RelayModeChatCompletions))
	require.False(t, ShouldStripImageGeneration(relayconstant.RelayModeImagesGenerations))
	require.False(t, ShouldStripImageGeneration(relayconstant.RelayModeImagesEdits))

	appcommon.DisableImageGenerationOnNonImageEndpoints = false
	require.False(t, ShouldStripImageGeneration(relayconstant.RelayModeResponses))
}

func TestStripImageGenerationTool(t *testing.T) {
	input := []byte(`{"tools":[{"type":"function","name":"lookup"},{"type":"image_generation","output_format":"png"},{"type":"web_search"}],"tool_choice":{"type":"image_generation"}}`)

	output, removed, err := StripImageGenerationTool(input)
	require.NoError(t, err)
	require.Equal(t, 2, removed)
	require.Equal(t, 2, len(gjson.GetBytes(output, "tools").Array()))
	require.Equal(t, "function", gjson.GetBytes(output, "tools.0.type").String())
	require.Equal(t, "web_search", gjson.GetBytes(output, "tools.1.type").String())
	require.False(t, gjson.GetBytes(output, "tool_choice").Exists())
}

func TestStripImageGenerationToolRemovesNamedToolChoice(t *testing.T) {
	input := []byte(`{"tools":[{"type":"image_generation"}],"tool_choice":{"type":"tool","name":"image_generation"}}`)

	output, removed, err := StripImageGenerationTool(input)
	require.NoError(t, err)
	require.Equal(t, 2, removed)
	require.Empty(t, gjson.GetBytes(output, "tools").Array())
	require.False(t, gjson.GetBytes(output, "tool_choice").Exists())
}

func TestStripImageGenerationToolKeepsUnrelatedChoice(t *testing.T) {
	input := []byte(`{"tools":[{"type":"function","name":"lookup"}],"tool_choice":"auto"}`)

	output, removed, err := StripImageGenerationTool(input)
	require.NoError(t, err)
	require.Zero(t, removed)
	require.JSONEq(t, string(input), string(output))
}
