package helper

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const responsesStreamStateKey = "responses_stream_state"

type responsesStreamState struct {
	Started   bool
	Ended     bool
	ID        string
	Model     string
	CreatedAt int64
	Sequence  int64
}

func getResponsesStreamState(c *gin.Context) *responsesStreamState {
	if value, ok := c.Get(responsesStreamStateKey); ok {
		return value.(*responsesStreamState)
	}
	state := &responsesStreamState{Sequence: -1}
	c.Set(responsesStreamStateKey, state)
	return state
}

// ResponsesStreamStarted excludes keepalives: only real events lock the attempt.
// Writers must be serialized, and readers run after stream scanner cleanup.
func ResponsesStreamStarted(c *gin.Context) bool {
	if c == nil {
		return false
	}
	value, ok := c.Get(responsesStreamStateKey)
	return ok && value.(*responsesStreamState).Started
}

func isResponsesEvent(c *gin.Context, eventType string) bool {
	return c != nil && c.Request != nil && c.Request.URL != nil &&
		strings.HasSuffix(c.Request.URL.Path, "/responses") &&
		eventType != "response.keepalive" && eventType != "ping"
}

func recordResponsesEvent(c *gin.Context, eventType, data string) {
	if !isResponsesEvent(c, eventType) {
		return
	}
	state := getResponsesStreamState(c)
	// Mark before writing: even a partial write must never allow replay.
	state.Started = true
	var event struct {
		Sequence *int64 `json:"sequence_number"`
		Response *struct {
			ID        string `json:"id"`
			Model     string `json:"model"`
			CreatedAt int64  `json:"created_at"`
		} `json:"response"`
	}
	if common.UnmarshalJsonStr(data, &event) == nil {
		if event.Sequence != nil && *event.Sequence > state.Sequence {
			state.Sequence = *event.Sequence
		}
		if event.Response != nil {
			if event.Response.ID != "" {
				state.ID = event.Response.ID
			}
			if event.Response.Model != "" {
				state.Model = event.Response.Model
			}
			if event.Response.CreatedAt != 0 {
				state.CreatedAt = event.Response.CreatedAt
			}
		}
	}
}

// WriteResponsesStreamFailure ends a committed SSE response instead of appending
// JSON to it. The caller has already stopped all stream and keepalive writers.
func WriteResponsesStreamFailure(c *gin.Context, apiErr *types.NewAPIError) error {
	if requestContextDone(c) {
		return c.Request.Context().Err()
	}
	state := getResponsesStreamState(c)
	if state.Ended {
		return nil
	}
	state.Ended = true
	if state.ID == "" {
		state.ID = "resp_" + common.GetUUID()
	}
	if state.CreatedAt == 0 {
		state.CreatedAt = time.Now().Unix()
	}
	sequence := state.Sequence
	if sequence < math.MaxInt64 {
		sequence++
	}
	// Codex treats arbitrary response.failed codes (including server_error) as
	// retryable. invalid_prompt maps to InvalidRequest and stops the current turn.
	// Keep the actual cause in the message; this is a replay-safety rejection,
	// not an assertion that the original prompt was malformed.
	message := fmt.Sprintf("The response could not be completed and this request must not be replayed automatically. %s", apiErr.ToOpenAIError().Message)
	data, err := common.Marshal(gin.H{
		"type": "response.failed", "sequence_number": sequence,
		"response": gin.H{
			"id": state.ID, "object": "response", "created_at": state.CreatedAt,
			"model": state.Model, "status": "failed", "output": []any{}, "usage": nil,
			"error": gin.H{"type": "invalid_request_error", "code": "invalid_prompt", "message": message},
		},
	})
	if err != nil {
		return err
	}
	ExtendWriteDeadline(c)
	if _, err = fmt.Fprintf(c.Writer, "event: response.failed\ndata: %s\n\n", data); err != nil {
		return err
	}
	return FlushWriter(c)
}
