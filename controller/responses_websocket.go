package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const responsesWSWriteTimeout = 30 * time.Second

type responsesWSContextKey struct{}
type responsesWSCall struct {
	session                     *responsesWSSession
	eventID, streamID, generate string
	settled                     chan struct{}
}
type responsesWSSession struct {
	ctx            context.Context
	cancel         context.CancelFunc
	client         *websocket.Conn
	request        *http.Request
	mu             sync.Mutex
	clientMu       sync.Mutex
	targetMu       sync.Mutex
	target         *websocket.Conn
	pipe           *io.PipeWriter
	binding        *service.ResponsesBinding
	busy           bool
	settled        chan struct{}
	responseID     string
	lastResponseID string
	targetDead     bool
	workers        sync.WaitGroup
}

// Each create runs fresh authentication, rate limits, distribution and the
// ordinary Relay billing lifecycle. No pooled Gin context crosses requests.
var responsesWSRequestEngine = sync.OnceValue(func() *gin.Engine {
	engine := gin.New()
	engine.ForwardedByClientIP = false
	_ = engine.SetTrustedProxies(nil)
	engine.POST("/v1/responses", func(c *gin.Context) {
		call := c.Request.Context().Value(responsesWSContextKey{}).(*responsesWSCall)
		c.Set(common.RequestIdKey, common.GetUUID())
		common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
		c.Set("responses_websocket_request", true)
		c.Set(relaychannel.ResponsesTransportContextKey, call)
		call.session.mu.Lock()
		binding := call.session.binding
		call.session.mu.Unlock()
		if binding != nil {
			c.Set(service.ResponsesBindingContextKey, binding)
		}
		c.Next()
	}, middleware.BodyStorageCleanup(), middleware.SystemPerformanceCheck(), middleware.TokenAuth(), middleware.ModelRequestRateLimit(), middleware.Distribute(), func(c *gin.Context) { Relay(c, types.RelayFormatOpenAIResponses) })
	return engine
})

func ResponsesWebSocket(c *gin.Context) {
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(c.Request.Context())
	request := c.Request.Clone(ctx)
	request.Header = c.Request.Header.Clone()
	request.RemoteAddr = net.JoinHostPort(c.ClientIP(), "0")
	for _, name := range []string{"Connection", "Upgrade", "Sec-WebSocket-Key", "Sec-WebSocket-Version", "Sec-WebSocket-Extensions", "Sec-WebSocket-Protocol", "Content-Length", "Content-Encoding"} {
		request.Header.Del(name)
	}
	session := &responsesWSSession{ctx: ctx, cancel: cancel, client: ws, request: request}
	defer func() { session.close(); session.workers.Wait() }()
	limit := int64(constant.MaxRequestBodyMB)
	if limit <= 0 {
		limit = 128
	}
	ws.SetReadLimit(limit << 20)
	for {
		kind, body, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if kind != websocket.TextMessage || !gjson.ValidBytes(body) {
			session.sendError("", 400, "invalid JSON websocket event")
			continue
		}
		event := gjson.ParseBytes(body)
		eventID := event.Get("event_id").String()
		switch event.Get("type").String() {
		case "response.create":
			requestBody, err := responsesWSRequestBody(body)
			if err != nil {
				session.sendError(eventID, 400, err.Error())
				continue
			}
			session.mu.Lock()
			if session.busy {
				session.mu.Unlock()
				session.sendError(eventID, 409, "another response.create is active")
				continue
			}
			session.busy = true
			session.responseID = ""
			settled := make(chan struct{})
			session.settled = settled
			session.mu.Unlock()
			call := &responsesWSCall{session: session, settled: settled, eventID: eventID, streamID: event.Get("stream_id").Raw, generate: event.Get("generate").Raw}
			session.workers.Add(1)
			go func() { defer session.workers.Done(); session.run(call, requestBody) }()
		case "response.inject", "response.cancel":
			if err := session.control(body); err != nil {
				session.sendError(eventID, 400, err.Error())
			}
		default:
			session.sendError(eventID, 400, "unsupported websocket event")
		}
	}
}

func responsesWSRequestBody(body []byte) ([]byte, error) {
	event := gjson.ParseBytes(body)
	if response := event.Get("response"); response.Exists() {
		if !response.IsObject() {
			return nil, errors.New("response must be an object")
		}
		body = []byte(response.Raw)
	} else {
		var err error
		for _, key := range []string{"type", "event_id", "stream_id", "generate"} {
			body, err = sjson.DeleteBytes(body, key)
			if err != nil {
				return nil, err
			}
		}
	}
	// The shared SSE handler handles transport-independent usage and settlement.
	return sjson.SetBytes(body, "stream", true)
}
func (s *responsesWSSession) run(call *responsesWSCall, body []byte) {
	writer := &responsesWSWriter{session: s, header: make(http.Header)}
	defer func() {
		defer close(call.settled)
		if recovered := recover(); recovered != nil {
			common.SysError(fmt.Sprintf("responses websocket panic: %v\n%s", recovered, debug.Stack()))
			s.sendError(call.eventID, 500, "responses websocket request failed")
			s.close()
		}
		s.mu.Lock()
		// If the SSE consumer stopped before an upstream terminal (for example
		// on idle timeout), do not reuse a connection with unfinished output.
		if s.pipe != nil {
			s.targetDead = true
		}
		s.busy = false
		s.mu.Unlock()
		if len(writer.terminal) > 0 {
			_ = s.writeClient(writer.terminal)
		}
		s.mu.Lock()
		dead := s.targetDead
		s.mu.Unlock()
		if dead {
			s.close()
		}
	}()
	req := s.request.Clone(context.WithValue(s.ctx, responsesWSContextKey{}, call))
	req.Method = http.MethodPost
	req.URL.Path = "/v1/responses"
	req.URL.RawPath = ""
	req.URL.RawQuery = ""
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", "application/json")
	responsesWSRequestEngine().ServeHTTP(writer, req)
	if writer.status >= 400 && len(writer.terminal) == 0 {
		var envelope map[string]any
		if common.Unmarshal(writer.buffer.Bytes(), &envelope) != nil {
			envelope = map[string]any{"error": map[string]string{"message": http.StatusText(writer.status), "type": "invalid_request_error"}}
		}
		envelope["type"] = "error"
		envelope["status"] = writer.status
		if call.eventID != "" {
			envelope["event_id"] = call.eventID
		}
		writer.terminal, _ = common.Marshal(envelope)
	}
}

func (call *responsesWSCall) RoundTrip(c *gin.Context, info *relaycommon.RelayInfo, req *http.Request) (*http.Response, error) {
	s := call.session
	if !info.ChannelSetting.ResponsesWebSocketEnabled {
		return nil, types.NewErrorWithStatusCode(errors.New("responses_websocket_enabled is required on the channel"), types.ErrorCodeInvalidRequest, 400, types.ErrOptionWithSkipRetry())
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	for _, field := range []string{"stream", "background"} {
		body, err = sjson.DeleteBytes(body, field)
		if err != nil {
			return nil, err
		}
	}
	body, err = sjson.SetBytes(body, "type", "response.create")
	if err != nil {
		return nil, err
	}
	if call.eventID != "" {
		body, err = sjson.SetBytes(body, "event_id", call.eventID)
		if err != nil {
			return nil, err
		}
	}
	if call.streamID != "" {
		body, err = sjson.SetRawBytes(body, "stream_id", []byte(call.streamID))
		if err != nil {
			return nil, err
		}
	}
	if call.generate != "" {
		body, err = sjson.SetRawBytes(body, "generate", []byte(call.generate))
		if err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	target := s.target
	s.mu.Unlock()
	if target == nil {
		targetURL := *req.URL
		switch targetURL.Scheme {
		case "http":
			targetURL.Scheme = "ws"
		case "https":
			targetURL.Scheme = "wss"
		default:
			return nil, errors.New("unsupported upstream websocket scheme")
		}
		client, err := service.GetRelayHttpClient(info.ChannelSetting.Proxy, true)
		if err != nil {
			return nil, err
		}
		dialer := *websocket.DefaultDialer
		dialer.HandshakeTimeout = responsesWSWriteTimeout
		if client != nil {
			if transport, ok := client.Transport.(*http.Transport); ok {
				dialer.Proxy = transport.Proxy
				dialer.NetDialContext = transport.DialContext
				dialer.TLSClientConfig = transport.TLSClientConfig
			}
		}
		headers := req.Header.Clone()
		headers.Del("Content-Type")
		headers.Del("Accept")
		headers.Del("Content-Length")
		var response *http.Response
		target, response, err = dialer.DialContext(s.ctx, targetURL.String(), headers)
		if err != nil {
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			return nil, err
		}
		limit := int64(constant.StreamScannerMaxBufferMB)
		if limit <= 0 {
			limit = 64
		}
		target.SetReadLimit(limit << 20)
		binding := service.ResponsesBindingFromInfo(c, info)
		s.mu.Lock()
		if s.ctx.Err() != nil {
			s.mu.Unlock()
			_ = target.Close()
			return nil, s.ctx.Err()
		}
		s.target = target
		s.binding = &binding
		s.mu.Unlock()
		s.workers.Add(1)
		go func() { defer s.workers.Done(); s.readTarget(target) }()
	}
	reader, writer := io.Pipe()
	s.mu.Lock()
	if s.ctx.Err() != nil || s.targetDead {
		s.mu.Unlock()
		_ = reader.Close()
		_ = writer.Close()
		return nil, errors.New("upstream websocket connection is closed")
	}
	s.pipe = writer
	s.mu.Unlock()
	if err := s.writeTarget(body); err != nil {
		_ = reader.Close()
		_ = writer.CloseWithError(err)
		s.close()
		return nil, err
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader, Request: req}, nil
}
func responsesWSTerminal(kind string) bool {
	switch kind {
	case "response.completed", "response.failed", "response.incomplete", "response.cancelled", "response.canceled", "response.done":
		return true
	}
	return false
}
func (s *responsesWSSession) readTarget(target *websocket.Conn) {
	for {
		timeout := time.Duration(constant.StreamingTimeout) * time.Second
		if timeout <= 0 {
			timeout = 300 * time.Second
		}
		_ = target.SetReadDeadline(time.Now().Add(timeout))
		_, body, err := target.ReadMessage()
		if err != nil {
			s.mu.Lock()
			pipe := s.pipe
			active := s.busy
			s.targetDead = true
			s.pipe = nil
			s.mu.Unlock()
			if pipe != nil {
				_ = pipe.CloseWithError(err)
			}
			// Let the active relay settle and send its terminal before closing
			// the client. An upstream failure is not a downstream cancellation.
			if active {
				return
			}
			s.close()
			return
		}
		event := gjson.ParseBytes(body)
		kind := event.Get("type").String()
		id := event.Get("response.id").String()
		s.mu.Lock()
		pipe := s.pipe
		settled := s.settled
		stale := id != "" && id == s.lastResponseID && !strings.HasPrefix(kind, "response.inject.")
		if kind == "response.created" && !stale {
			s.responseID = id
		}
		terminal := responsesWSTerminal(kind) || kind == "error"
		if terminal && pipe != nil && !stale {
			s.pipe = nil
			if id != "" {
				s.lastResponseID = id
			}
		}
		s.mu.Unlock()
		if stale {
			continue
		}
		// Injection acknowledgements can arrive after the response terminal; never
		// feed them into a subsequent request's usage/accounting stream.
		if pipe == nil || strings.HasPrefix(kind, "response.inject.") {
			if s.writeClient(body) != nil {
				s.close()
				return
			}
			continue
		}
		// SSE data must occupy one line, even when upstream frames are pretty JSON.
		var eventJSON map[string]json.RawMessage
		if common.Unmarshal(body, &eventJSON) != nil {
			_ = pipe.CloseWithError(errors.New("invalid upstream websocket event"))
			s.close()
			return
		}
		data, err := common.Marshal(eventJSON)
		if err != nil {
			_ = pipe.CloseWithError(err)
			s.close()
			return
		}
		if _, err = fmt.Fprintf(pipe, "data: %s\n\n", data); err != nil {
			s.close()
			return
		}
		if terminal {
			_ = pipe.Close()
			// Preserve upstream event order while the ordinary relay settles.
			// Late injection acknowledgements follow the terminal on the wire.
			select {
			case <-settled:
			case <-s.ctx.Done():
				return
			}
		}
	}
}

func (s *responsesWSSession) control(body []byte) error {
	event := gjson.ParseBytes(body)
	s.mu.Lock()
	ready := s.target != nil
	active := s.busy
	id := s.responseID
	last := s.lastResponseID
	s.mu.Unlock()
	if !ready {
		return errors.New("no upstream response connection")
	}
	if event.Get("type").String() == "response.inject" {
		responseID := event.Get("response_id")
		if responseID.Type != gjson.String || responseID.String() == "" || responseID.String() != id && responseID.String() != last {
			return errors.New("response.inject requires a response_id from this connection")
		}
		input := event.Get("input")
		if !input.IsArray() || len(input.Array()) == 0 {
			return errors.New("response.inject input must contain tool outputs")
		}
		for _, item := range input.Array() {
			kind := item.Get("type").String()
			if (kind != "function_call_output" && kind != "custom_tool_call_output") || item.Get("call_id").String() == "" || !item.Get("output").Exists() {
				return errors.New("response.inject only accepts tool outputs with call_id and output")
			}
		}
	} else {
		if !active || id == "" {
			return errors.New("no accepted active response to cancel")
		}
		if targetID := event.Get("response_id"); targetID.Exists() && targetID.String() != id {
			return errors.New("response.cancel must target the active response on this connection")
		}
	}
	return s.writeTarget(body)
}
func (s *responsesWSSession) writeTarget(body []byte) error {
	s.targetMu.Lock()
	defer s.targetMu.Unlock()
	s.mu.Lock()
	target := s.target
	s.mu.Unlock()
	if target == nil {
		return errors.New("no upstream connection")
	}
	_ = target.SetWriteDeadline(time.Now().Add(responsesWSWriteTimeout))
	return target.WriteMessage(websocket.TextMessage, body)
}
func (s *responsesWSSession) writeClient(body []byte) error {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	_ = s.client.SetWriteDeadline(time.Now().Add(responsesWSWriteTimeout))
	return s.client.WriteMessage(websocket.TextMessage, body)
}
func (s *responsesWSSession) sendError(eventID string, status int, message string) {
	data, _ := common.Marshal(map[string]any{"type": "error", "status": status, "event_id": eventID, "error": map[string]string{"type": "invalid_request_error", "message": message}})
	_ = s.writeClient(data)
}
func (s *responsesWSSession) close() {
	s.cancel()
	_ = s.client.Close()
	s.mu.Lock()
	target, pipe := s.target, s.pipe
	s.pipe = nil
	s.mu.Unlock()
	if target != nil {
		_ = target.Close()
	}
	if pipe != nil {
		_ = pipe.CloseWithError(context.Canceled)
	}
}

type responsesWSWriter struct {
	session  *responsesWSSession
	header   http.Header
	status   int
	buffer   bytes.Buffer
	terminal []byte
}

func (w *responsesWSWriter) Header() http.Header { return w.header }
func (w *responsesWSWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *responsesWSWriter) Flush() {}
func (w *responsesWSWriter) Write(data []byte) (int, error) {
	w.WriteHeader(200)
	if w.status >= 400 {
		return w.buffer.Write(data)
	}
	w.buffer.Write(data)
	for {
		raw := w.buffer.Bytes()
		end := bytes.Index(raw, []byte("\n\n"))
		if end < 0 {
			break
		}
		block := append([]byte(nil), raw[:end]...)
		w.buffer.Next(end + 2)
		var values [][]byte
		for _, line := range bytes.Split(block, []byte("\n")) {
			if bytes.HasPrefix(line, []byte("data:")) {
				values = append(values, bytes.TrimSpace(line[5:]))
			}
		}
		event := bytes.Join(values, []byte("\n"))
		if len(event) == 0 || bytes.Equal(event, []byte("[DONE]")) {
			continue
		}
		kind := gjson.GetBytes(event, "type").String()
		if kind == "response.keepalive" {
			continue
		}
		if responsesWSTerminal(kind) || kind == "error" {
			if len(w.terminal) == 0 {
				w.terminal = append([]byte(nil), event...)
			}
			continue
		}
		if err := w.session.writeClient(event); err != nil {
			return 0, err
		}
	}
	return len(data), nil
}
