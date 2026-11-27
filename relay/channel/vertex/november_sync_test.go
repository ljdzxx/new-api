package vertex

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestVertexChannelAPIVersion(t *testing.T) {
	for _, keyType := range []dto.VertexKeyType{dto.VertexKeyTypeJSON, dto.VertexKeyTypeAPIKey} {
		for _, version := range []string{"", "v1", "v1beta1"} {
			for _, stream := range []bool{false, true} {
				for _, base := range []string{"https://vertex.example", "https://vertex.example/v1"} {
					info := &relaycommon.RelayInfo{IsStream: stream, OriginModelName: "gemini-4-argon", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-4-argon", ChannelBaseUrl: base, ApiVersion: "us-central1", ApiKey: `{"project_id":"project"}`, ChannelOtherSettings: dto.ChannelOtherSettings{VertexKeyType: keyType, VertexProjectID: "project", VertexAPIVersion: version}}}
					a := &Adaptor{}
					a.Init(info)
					got, err := a.GetRequestURL(info)
					require.NoError(t, err)
					wantVersion := version
					if wantVersion == "" {
						wantVersion = "v1"
					}
					require.Contains(t, got, "https://vertex.example/"+wantVersion+"/projects/project/locations/us-central1/publishers/google/models/gemini-4-argon:")
					if stream {
						require.Contains(t, got, "streamGenerateContent?alt=sse")
					} else {
						require.Contains(t, got, "generateContent")
					}
					// This switch applies to Google models, not Anthropic paths.
					a.RequestMode = RequestModeClaude
					got, err = a.getRequestUrl(info, "claude-opus-4-6", "rawPredict")
					require.NoError(t, err)
					require.Contains(t, got, "/v1/projects/")
				}
			}
		}
	}
}

func TestVertexTokenShortLifetimeAndReplacedKeyMaterial(t *testing.T) {
	calls := 0
	lifetime := time.Minute
	stubAccessTokenFetch(t, func(Credentials, string) (string, time.Duration, error) { calls++; return "token", lifetime, nil })
	info := newAccessTokenTestInfo(31)
	a := newAccessTokenTestAdaptor("")
	for i := 0; i < 2; i++ {
		_, err := getAccessToken(a, info)
		require.NoError(t, err)
	}
	require.Equal(t, 2, calls)
	lifetime = time.Hour
	_, err := getAccessToken(a, info)
	require.NoError(t, err)
	a.AccountCredentials.PrivateKey = "replaced key with no key ID"
	_, err = getAccessToken(a, info)
	require.NoError(t, err)
	require.Equal(t, 4, calls)
}
