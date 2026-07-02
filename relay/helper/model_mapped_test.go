package helper

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type modelMappedTestRequest struct {
	model string
	meta  *types.TokenCountMeta
}

func (r *modelMappedTestRequest) GetTokenCountMeta() *types.TokenCountMeta {
	return r.meta
}

func (r *modelMappedTestRequest) IsStream(c *gin.Context) bool {
	return false
}

func (r *modelMappedTestRequest) SetModelName(modelName string) {
	r.model = modelName
}

func newModelMappedTestContext(modelMapping string) *gin.Context {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("model_mapping", modelMapping)
	return c
}

func newModelMappedTestInfo(model string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: model,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: model,
		},
	}
}

func imageInputMeta() *types.TokenCountMeta {
	return &types.TokenCountMeta{
		Files: []*types.FileMeta{
			{FileType: types.FileTypeImage},
		},
	}
}

func TestModelMappedHelperNormalMappingIgnoresImageGuard(t *testing.T) {
	c := newModelMappedTestContext(`{"gpt-5.4":"gpt-5.3-codex-spark"}`)
	info := newModelMappedTestInfo("gpt-5.4")
	request := &modelMappedTestRequest{model: "gpt-5.4", meta: imageInputMeta()}

	err := ModelMappedHelper(c, info, request)

	require.NoError(t, err)
	require.True(t, info.IsModelMapped)
	require.Equal(t, "gpt-5.3-codex-spark", info.UpstreamModelName)
	require.Equal(t, "gpt-5.3-codex-spark", request.model)
}

func TestModelMappedHelperConditionalMappingWithoutImageInput(t *testing.T) {
	c := newModelMappedTestContext(`{"!gpt-5.4":"gpt-5.3-codex-spark"}`)
	info := newModelMappedTestInfo("gpt-5.4")
	request := &modelMappedTestRequest{
		model: "gpt-5.4",
		meta:  &types.TokenCountMeta{},
	}

	err := ModelMappedHelper(c, info, request)

	require.NoError(t, err)
	require.True(t, info.IsModelMapped)
	require.Equal(t, "gpt-5.3-codex-spark", info.UpstreamModelName)
	require.Equal(t, "gpt-5.3-codex-spark", request.model)
}

func TestModelMappedHelperConditionalMappingSkipsImageInput(t *testing.T) {
	c := newModelMappedTestContext(`{"!gpt-5.4":"gpt-5.3-codex-spark"}`)
	info := newModelMappedTestInfo("gpt-5.4")
	request := &modelMappedTestRequest{model: "gpt-5.4", meta: imageInputMeta()}

	err := ModelMappedHelper(c, info, request)

	require.NoError(t, err)
	require.False(t, info.IsModelMapped)
	require.Equal(t, "gpt-5.4", info.UpstreamModelName)
	require.Equal(t, "gpt-5.4", request.model)
}

func TestModelMappedHelperStopsAfterSingleMapping(t *testing.T) {
	c := newModelMappedTestContext(`{"gpt-5.5":"gpt-5.4","!gpt-5.4":"gpt-5.3-codex-spark"}`)
	info := newModelMappedTestInfo("gpt-5.5")
	request := &modelMappedTestRequest{model: "gpt-5.5", meta: imageInputMeta()}

	err := ModelMappedHelper(c, info, request)

	require.NoError(t, err)
	require.True(t, info.IsModelMapped)
	require.Equal(t, "gpt-5.4", info.UpstreamModelName)
	require.Equal(t, "gpt-5.4", request.model)
}

func TestModelMappedHelperConditionalMappingConflict(t *testing.T) {
	c := newModelMappedTestContext(`{"gpt-5.4":"A","!gpt-5.4":"B"}`)
	info := newModelMappedTestInfo("gpt-5.4")
	request := &modelMappedTestRequest{
		model: "gpt-5.4",
		meta:  &types.TokenCountMeta{},
	}

	err := ModelMappedHelper(c, info, request)

	require.Error(t, err)
	require.Equal(t, "model_mapping_contains_conditional_conflict", err.Error())
	require.Equal(t, "gpt-5.4", info.UpstreamModelName)
	require.Equal(t, "gpt-5.4", request.model)
}
