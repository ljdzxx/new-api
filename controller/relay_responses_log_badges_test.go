package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResponsesEncryptedFailureBadgesIncludePassthrough(t *testing.T) {
	for _, mode := range []string{"normal", "channel_passthrough", "global_passthrough"} {
		for _, tc := range []struct {
			name, input, mapping, expected string
			status, calls                  int
			recoverFirst, success          bool
		}{
			{"reasoning 500", `{"type":"reasoning","encrypted_content":"gAAAAAB_encrypted_reasoning_1234567890"}`, "", `["E1"]`, 500, 1, false, false},
			{"reasoning 502", `{"type":"reasoning","encrypted_content":"gAAAAAB_encrypted_reasoning_1234567890"}`, "", `["R1","E1"]`, 502, 2, false, false},
			{"reasoning 503", `{"type":"reasoning","encrypted_content":"gAAAAAB_encrypted_reasoning_1234567890"}`, "", `["R1","E1"]`, 503, 2, false, false},
			{"compaction 503", `{"type":"compaction","encrypted_content":"gAAAAAB_encrypted_compaction_1234567890"}`, "", `["E2"]`, 503, 1, false, false},
			{"both", `{"type":"reasoning","encrypted_content":"gAAAAAB_encrypted_reasoning_1234567890"},{"type":"compaction","encrypted_content":"gAAAAAB_encrypted_compaction_1234567890"}`, "", `["E1","E2"]`, 502, 1, false, false},
			{"recovery then 502", `{"type":"reasoning","encrypted_content":"gAAAAAB_encrypted_reasoning_1234567890"}`, "", `["R1","E1"]`, 502, 2, true, false},
			{"400 unmapped", `{"type":"reasoning","encrypted_content":"gAAAAAB_encrypted_reasoning_1234567890"}`, "", "", 400, 1, false, false},
			{"400 mapped to 502", `{"type":"compaction","encrypted_content":"gAAAAAB_encrypted_compaction_1234567890"}`, `{"400":"502"}`, `["E2"]`, 400, 1, false, false},
			{"reasoning 400 mapped to 502", `{"type":"reasoning","encrypted_content":"gAAAAAB_encrypted_reasoning_1234567890"}`, `{"400":"502"}`, `["E1"]`, 400, 1, false, false},
			{"502 mapped to 400", `{"type":"reasoning","encrypted_content":"gAAAAAB_encrypted_reasoning_1234567890"}`, `{"502":"400"}`, `["R1"]`, 502, 2, false, false},
			{"no ciphertext", `{"role":"user","content":"hello"}`, "", "", 502, 1, false, false},
			{"success clears old attempt flags", `{"type":"reasoning","encrypted_content":"gAAAAAB_encrypted_reasoning_1234567890"}`, "", "", 200, 1, false, true},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				db := setupResponsesRelayBillingTest(t)
				settings := model_setting.GetGlobalSettings()
				oldPassthrough, oldLogs := settings.PassThroughRequestEnabled, constant.ErrorLogEnabled
				settings.PassThroughRequestEnabled, constant.ErrorLogEnabled = mode == "global_passthrough", true
				t.Cleanup(func() { settings.PassThroughRequestEnabled, constant.ErrorLogEnabled = oldPassthrough, oldLogs })
				attempt := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					attempt++
					w.Header().Set("Content-Type", "application/json")
					if tc.success {
						fmt.Fprint(w, `{"id":"resp_ok","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
						return
					}
					if tc.recoverFirst && attempt == 1 {
						w.WriteHeader(400)
						fmt.Fprint(w, `{"error":{"type":"invalid_request_error","code":"invalid_encrypted_content","message":"rejected"}}`)
						return
					}
					w.WriteHeader(tc.status)
					fmt.Fprint(w, `{"error":{"type":"server_error","code":"server_error","message":"generic upstream failure"}}`)
				}))
				defer server.Close()
				service.InitHttpClient()
				body := `{"model":"gpt-4o-mini","input":[` + tc.input + `,{"role":"user","content":"continue"}]}`
				var request dto.OpenAIResponsesRequest
				require.NoError(t, common.Unmarshal([]byte(body), &request))
				c, _, _ := newRelayMockContext(false)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
				common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: mode == "channel_passthrough"})
				common.SetContextKey(c, constant.ContextKeyResponsesLogBadges, []string{"E1", "E2"})
				c.Set("status_code_mapping", tc.mapping)
				info := &relaycommon.RelayInfo{UserId: 42, TokenId: 77, OriginModelName: "gpt-4o-mini", RelayMode: relayconstant.RelayModeResponses, Request: &request, DisablePing: true}
				apiErr := relay.ResponsesHelper(c, info)
				require.Equal(t, tc.calls, attempt)
				if tc.success {
					require.Nil(t, apiErr)
					require.Empty(t, common.GetContextKeyStringSlice(c, constant.ContextKeyResponsesLogBadges))
					return
				}
				require.NotNil(t, apiErr)
				processChannelError(c, *types.NewChannelError(88, constant.ChannelTypeOpenAI, "mock-channel", false, "sk-upstream", false), apiErr)
				var logs []model.Log
				require.NoError(t, db.Where("type = ?", model.LogTypeError).Find(&logs).Error)
				require.Len(t, logs, 1)
				require.Equal(t, tc.expected, gjson.Get(logs[0].Other, "responses_badges").Raw, logs[0].Other)
			})
		}
	}
}
