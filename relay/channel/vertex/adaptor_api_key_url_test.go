package vertex

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURLWithAPIKey(t *testing.T) {
	tests := []struct {
		name      string
		projectID string
		region    string
		stream    bool
		want      string
	}{
		{
			name:      "project ID keeps the global region",
			projectID: "my-project",
			region:    `{"default":"global"}`,
			want:      "https://aiplatform.googleapis.com/v1/projects/my-project/locations/global/publishers/google/models/gemini-4-argon:generateContent?key=test-key",
		},
		{
			name:      "project ID keeps a regional endpoint when streaming",
			projectID: "my-project",
			region:    `{"default":"us-central1"}`,
			stream:    true,
			want:      "https://us-central1-aiplatform.googleapis.com/v1/projects/my-project/locations/us-central1/publishers/google/models/gemini-4-argon:streamGenerateContent?alt=sse&key=test-key",
		},
		{
			name:   "no project ID keeps the express-mode URL",
			region: `{"default":"global"}`,
			want:   "https://aiplatform.googleapis.com/v1/publishers/google/models/gemini-4-argon:generateContent?key=test-key",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				IsStream:        tt.stream,
				OriginModelName: "gemini-4-argon",
				ChannelMeta: &relaycommon.ChannelMeta{
					ApiVersion:        tt.region,
					ApiKey:            "test-key",
					UpstreamModelName: "gemini-4-argon",
					ChannelOtherSettings: dto.ChannelOtherSettings{
						VertexKeyType:   dto.VertexKeyTypeAPIKey,
						VertexProjectID: tt.projectID,
					},
				},
			}
			adaptor := &Adaptor{}
			adaptor.Init(info)

			got, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestVertexProjectURLModes(t *testing.T) {
	for _, tc := range []struct {
		name, model, baseURL, key, project, path string
		keyType                                  dto.VertexKeyType
		stream                                   bool
	}{
		{"claude stream", "claude-opus-4-6", "", "test-key", " project-123 ", "https://aiplatform.googleapis.com/v1/projects/project-123/locations/global/publishers/anthropic/models/claude-opus-4-6:streamRawPredict?alt=sse&key=test-key", dto.VertexKeyTypeAPIKey, true},
		{"gateway", "gemini-2.5-flash", "https://gateway.example/vertex/v1/", "test-key", "project-123", "https://gateway.example/vertex/v1/projects/project-123/locations/global/publishers/google/models/gemini-2.5-flash:generateContent?key=test-key", dto.VertexKeyTypeAPIKey, false},
		{"blank express", "gemini-2.5-flash", "", "test-key", "  ", "https://aiplatform.googleapis.com/v1/publishers/google/models/gemini-2.5-flash:generateContent?key=test-key", dto.VertexKeyTypeAPIKey, false},
		{"service account owns project", "gemini-2.5-flash", "", `{"project_id":"service-project"}`, "ignored-project", "https://aiplatform.googleapis.com/v1/projects/service-project/locations/global/publishers/google/models/gemini-2.5-flash:generateContent", dto.VertexKeyTypeJSON, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{OriginModelName: tc.model, IsStream: tc.stream,
				ChannelMeta: &relaycommon.ChannelMeta{
					ApiVersion: `{"default":"global"}`, ApiKey: tc.key,
					UpstreamModelName: tc.model, ChannelBaseUrl: tc.baseURL,
					ChannelOtherSettings: dto.ChannelOtherSettings{VertexKeyType: tc.keyType, VertexProjectID: tc.project},
				}}
			adaptor := &Adaptor{}
			adaptor.Init(info)
			got, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			require.Equal(t, tc.path, got)
		})
	}
}
