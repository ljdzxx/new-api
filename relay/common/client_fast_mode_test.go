package common

import (
	"net/http/httptest"
	"testing"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClientRequestedFastMode(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request dto.Request
		want    bool
	}{
		{"chat fast", &dto.GeneralOpenAIRequest{ServiceTier: []byte(`"fast"`)}, true},
		{"chat priority", &dto.GeneralOpenAIRequest{ServiceTier: []byte(`"priority"`)}, true},
		{"chat default", &dto.GeneralOpenAIRequest{ServiceTier: []byte(`"default"`)}, false},
		{"chat flex", &dto.GeneralOpenAIRequest{ServiceTier: []byte(`"flex"`)}, false},
		{"chat auto", &dto.GeneralOpenAIRequest{ServiceTier: []byte(`"auto"`)}, false},
		{"chat absent", &dto.GeneralOpenAIRequest{}, false},
		{"chat null", &dto.GeneralOpenAIRequest{ServiceTier: []byte(`null`)}, false},
		{"chat invalid type", &dto.GeneralOpenAIRequest{ServiceTier: []byte(`{"mode":"fast"}`)}, false},
		{"responses fast", &dto.OpenAIResponsesRequest{ServiceTier: "fast"}, true},
		{"responses priority", &dto.OpenAIResponsesRequest{ServiceTier: "priority"}, true},
		{"responses default", &dto.OpenAIResponsesRequest{ServiceTier: "default"}, false},
		{"responses absent", &dto.OpenAIResponsesRequest{}, false},
		{"compaction fast", &dto.OpenAIResponsesCompactionRequest{ServiceTier: "fast"}, true},
		{"compaction priority", &dto.OpenAIResponsesCompactionRequest{ServiceTier: "priority"}, true},
		{"claude fast", &dto.ClaudeRequest{Speed: []byte(`"fast"`)}, true},
		{"claude standard", &dto.ClaudeRequest{Speed: []byte(`"standard"`)}, false},
		{"claude absent", &dto.ClaudeRequest{}, false},
		{"claude priority is not fast", &dto.ClaudeRequest{ServiceTier: "priority"}, false},
		{"audio speed is not fast mode", &dto.AudioRequest{Speed: basecommon.GetPointer(2.0)}, false},
		{"missing request", nil, false},
		{"typed nil", (*dto.OpenAIResponsesRequest)(nil), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, clientRequestedFastMode(tc.request))
		})
	}
}

func TestGenRelayInfoCapturesClientFastModeBeforeRequestChanges(t *testing.T) {
	for _, fast := range []bool{false, true} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		// A copied context can contain the source request's mode, e.g. prechecks.
		basecommon.SetContextKey(c, constant.ContextKeyClientFastMode, !fast)
		request := &dto.OpenAIResponsesRequest{ServiceTier: "default"}
		if fast {
			request.ServiceTier = "priority"
		}
		GenRelayInfoResponses(c, request)
		request.ServiceTier = "fast"
		if fast {
			request.ServiceTier = "default"
		}
		require.Equal(t, fast, basecommon.GetContextKeyBool(c, constant.ContextKeyClientFastMode))
	}
}
