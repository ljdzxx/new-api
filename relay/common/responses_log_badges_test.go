package common

import (
	"errors"
	"net/http/httptest"
	"testing"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMarkResponsesEncryptedFailure(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		status      int
		want        []string
	}{
		{"reasoning 500", `[{"type":"reasoning","encrypted_content":"opaque"}]`, 500, []string{"E1"}},
		{"compaction 503", `[{"type":"compaction","encrypted_content":"opaque"}]`, 503, []string{"E2"}},
		{"both 599", `[{"type":"reasoning","encrypted_content":"r"},{"type":"compaction","encrypted_content":"c"},{"type":"reasoning","encrypted_content":"r2"}]`, 599, []string{"E1", "E2"}},
		{"no ciphertext", `[{"type":"reasoning","summary":[]},{"type":"compaction","encrypted_content":""}]`, 502, nil},
		{"ignore nested tool data", `[{"type":"function_call_output","output":{"type":"reasoning","encrypted_content":"opaque"}}]`, 502, nil},
		{"ignore metadata", `[{"type":"message","encrypted_content":"metadata"}]`, 502, nil},
		{"string input", `"hello"`, 502, nil},
		{"400", `[{"type":"reasoning","encrypted_content":"opaque"}]`, 400, nil},
		{"499", `[{"type":"reasoning","encrypted_content":"opaque"}]`, 499, nil},
		{"success", `[{"type":"reasoning","encrypted_content":"opaque"}]`, 200, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			apiErr := types.NewOpenAIError(errors.New("generic upstream failure"), types.ErrorCodeBadResponseStatusCode, tc.status)
			MarkResponsesEncryptedFailure(c, []byte(tc.input), apiErr)
			MarkResponsesEncryptedFailure(c, []byte(tc.input), apiErr)
			require.Equal(t, tc.want, basecommon.GetContextKeyStringSlice(c, constant.ContextKeyResponsesLogBadges))
		})
	}
}

func TestMarkResponsesEncryptedFailurePreservesRecoveryBadge(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	basecommon.SetContextKey(c, constant.ContextKeyResponsesLogBadges, []string{"R1"})
	MarkResponsesEncryptedFailure(c, []byte(`[{"type":"reasoning","encrypted_content":"opaque"}]`), types.NewOpenAIError(errors.New("failed"), types.ErrorCodeBadResponse, 502))
	require.Equal(t, []string{"R1", "E1"}, basecommon.GetContextKeyStringSlice(c, constant.ContextKeyResponsesLogBadges))
}
