package controller

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestValidateVertexAPIVersion(t *testing.T) {
	for _, version := range []string{"", "v1", "v1beta1"} {
		channel := &model.Channel{Type: constant.ChannelTypeVertexAi, Other: `{"default":"global"}`, OtherSettings: `{"vertex_api_version":"` + version + `"}`}
		require.NoError(t, validateChannel(channel, false))
	}
	channel := &model.Channel{Type: constant.ChannelTypeVertexAi, OtherSettings: `{"vertex_api_version":"v2"}`}
	require.ErrorContains(t, validateChannel(channel, false), "Vertex API version")
}
