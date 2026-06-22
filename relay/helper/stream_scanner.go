package helper

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"

	"github.com/gin-gonic/gin"
)

const (
	InitialScannerBufferSize    = 64 << 10  // 64KB (64*1024)
	DefaultMaxScannerBufferSize = 128 << 20 // 64MB (64*1024*1024) default SSE buffer size
	DefaultPingInterval         = 10 * time.Second
)

type StreamScannerFinishReason string

const (
	StreamScannerFinished           StreamScannerFinishReason = "finished"
	StreamScannerInvalidInput       StreamScannerFinishReason = "invalid_input"
	StreamScannerTimeout            StreamScannerFinishReason = "timeout"
	StreamScannerClientDisconnected StreamScannerFinishReason = "client_disconnected"
	StreamScannerHandlerStopped     StreamScannerFinishReason = "handler_stopped"
	StreamScannerScannerError       StreamScannerFinishReason = "scanner_error"
)

type StreamScannerResult struct {
	Reason StreamScannerFinishReason
	Err    error
}

type StreamScannerOptions struct {
	PingDataFunc func(c *gin.Context) error
}

func getScannerBufferSize() int {
	if constant.StreamScannerMaxBufferMB > 0 {
		return constant.StreamScannerMaxBufferMB << 20
	}
	return DefaultMaxScannerBufferSize
}

func NewStreamScanner(reader io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, InitialScannerBufferSize), getScannerBufferSize())
	return scanner
}

func StreamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler any) {
	_ = StreamScannerHandlerWithOptions(c, resp, info, dataHandler, StreamScannerOptions{})
}

func StreamScannerHandlerWithOptions(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler any, options StreamScannerOptions) StreamScannerResult {
	handler, ok := normalizeStreamDataHandler(dataHandler)
	if resp == nil || handler == nil || !ok {
		return StreamScannerResult{Reason: StreamScannerInvalidInput}
	}
	if info == nil {
		info = &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	}

	// Always create a fresh StreamStatus for each scanner run.
	info.StreamStatus = relaycommon.NewStreamStatus()

	defer func() {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	streamingTimeout := time.Duration(constant.StreamingTimeout) * time.Second
	if streamingTimeout <= 0 {
		streamingTimeout = 30 * time.Second
	}

	var (
		stopChan   = make(chan bool, 3)
		scanner    = NewStreamScanner(resp.Body)
		ticker     = time.NewTicker(streamingTimeout)
		pingTicker *time.Ticker
		writeMutex sync.Mutex
		wg         sync.WaitGroup
	)

	generalSettings := operation_setting.GetGeneralSetting()
	pingEnabled := generalSettings.PingIntervalEnabled && !info.DisablePing
	pingInterval := time.Duration(generalSettings.PingIntervalSeconds) * time.Second
	if pingInterval <= 0 {
		pingInterval = DefaultPingInterval
	}

	if pingEnabled {
		pingTicker = time.NewTicker(pingInterval)
	}
	pingDataFunc := options.PingDataFunc
	if pingDataFunc == nil {
		pingDataFunc = PingData
	}

	logger.LogDebug(c, "relay timeout seconds: %d", common.RelayTimeout)
	logger.LogDebug(c, "relay max idle conns: %d", common.RelayMaxIdleConns)
	logger.LogDebug(c, "relay max idle conns per host: %d", common.RelayMaxIdleConnsPerHost)
	logger.LogDebug(c, "streaming timeout seconds: %d", int64(streamingTimeout.Seconds()))
	logger.LogDebug(c, "ping interval seconds: %d", int64(pingInterval.Seconds()))

	defer func() {
		common.SafeSendBool(stopChan, true)
		if resp.Body != nil {
			_ = resp.Body.Close()
		}

		ticker.Stop()
		if pingTicker != nil {
			pingTicker.Stop()
		}

		done := make(chan struct{})
		gopool.Go(func() {
			wg.Wait()
			close(done)
		})

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			logger.LogError(c, "timeout waiting for goroutines to exit")
		}

		close(stopChan)
	}()

	scanner.Split(bufio.ScanLines)
	SetEventStreamHeaders(c)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctx = context.WithValue(ctx, "stop_chan", stopChan)

	if pingEnabled && pingTicker != nil {
		wg.Add(1)
		gopool.Go(func() {
			defer func() {
				wg.Done()
				if r := recover(); r != nil {
					logger.LogError(c, fmt.Sprintf("ping goroutine panic: %v", r))
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("ping panic: %v", r))
					common.SafeSendBool(stopChan, true)
				}
				logger.LogDebug(c, "ping goroutine exited")
			}()

			maxPingDuration := 30 * time.Minute
			pingTimeout := time.NewTimer(maxPingDuration)
			defer pingTimeout.Stop()

			for {
				select {
				case <-pingTicker.C:
					done := make(chan error, 1)
					gopool.Go(func() {
						writeMutex.Lock()
						defer writeMutex.Unlock()
						done <- pingDataFunc(c)
					})

					select {
					case err := <-done:
						if err != nil {
							logger.LogError(c, "ping data error: "+err.Error())
							info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPingFail, err)
							return
						}
						logger.LogDebug(c, "ping data sent")
					case <-time.After(10 * time.Second):
						logger.LogError(c, "ping data send timeout")
						info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPingFail, fmt.Errorf("ping send timeout"))
						return
					case <-ctx.Done():
						return
					case <-stopChan:
						return
					}
				case <-ctx.Done():
					return
				case <-stopChan:
					return
				case <-c.Request.Context().Done():
					return
				case <-pingTimeout.C:
					logger.LogError(c, "ping goroutine max duration reached")
					return
				}
			}
		})
	}

	dataChan := make(chan string, 10)

	wg.Add(1)
	gopool.Go(func() {
		defer func() {
			wg.Done()
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("data handler goroutine panic: %v", r))
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("handler panic: %v", r))
			}
			common.SafeSendBool(stopChan, true)
		}()
		sr := newStreamResult(info.StreamStatus)
		for data := range dataChan {
			sr.reset()
			writeMutex.Lock()
			handler(data, sr)
			writeMutex.Unlock()
			if sr.IsStopped() {
				return
			}
		}
	})

	wg.Add(1)
	common.RelayCtxGo(ctx, func() {
		defer func() {
			close(dataChan)
			wg.Done()
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("scanner goroutine panic: %v", r))
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("scanner panic: %v", r))
			}
			common.SafeSendBool(stopChan, true)
			logger.LogDebug(c, "scanner goroutine exited")
		}()

		for scanner.Scan() {
			select {
			case <-stopChan:
				return
			case <-ctx.Done():
				return
			case <-c.Request.Context().Done():
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, c.Request.Context().Err())
				return
			default:
			}

			ticker.Reset(streamingTimeout)
			data := scanner.Text()
			logger.LogDebug(c, "stream scanner data: %s", data)

			if len(data) < 6 {
				continue
			}
			if data[:5] != "data:" && data[:6] != "[DONE]" {
				continue
			}
			data = data[5:]
			data = strings.TrimSpace(data)
			if data == "" {
				continue
			}
			if shouldLogXiaomiClaudeStreamData(c, info) {
				logger.LogInfo(c, fmt.Sprintf("[xiaomi claude] raw upstream sse data bytes=%d:\n%s", len(data), data))
			}
			if !strings.HasPrefix(data, "[DONE]") {
				info.SetFirstResponseTime()
				info.ReceivedResponseCount++

				select {
				case dataChan <- data:
				case <-ctx.Done():
					return
				case <-stopChan:
					return
				}
			} else {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				logger.LogDebug(c, "received [DONE], stopping scanner")
				return
			}
		}

		if err := scanner.Err(); err != nil {
			if err != io.EOF {
				logger.LogError(c, "scanner error: "+err.Error())
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
			}
		}
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
	})

	select {
	case <-ticker.C:
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)
	case <-stopChan:
	case <-c.Request.Context().Done():
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, c.Request.Context().Err())
	}

	if info.StreamStatus.IsNormalEnd() && !info.StreamStatus.HasErrors() {
		logger.LogInfo(c, fmt.Sprintf("stream ended: %s", info.StreamStatus.Summary()))
	} else {
		logger.LogError(c, fmt.Sprintf("stream ended: %s, received=%d", info.StreamStatus.Summary(), info.ReceivedResponseCount))
	}
	return streamScannerResultFromStatus(info.StreamStatus)
}

func normalizeStreamDataHandler(dataHandler any) (func(data string, sr *StreamResult), bool) {
	switch handler := dataHandler.(type) {
	case func(data string, sr *StreamResult):
		return handler, true
	case func(data string) bool:
		return func(data string, sr *StreamResult) {
			if !handler(data) {
				sr.Stop(nil)
			}
		}, true
	default:
		return nil, false
	}
}

func streamScannerResultFromStatus(status *relaycommon.StreamStatus) StreamScannerResult {
	if status == nil {
		return StreamScannerResult{Reason: StreamScannerFinished}
	}
	switch status.EndReason {
	case relaycommon.StreamEndReasonTimeout:
		return StreamScannerResult{Reason: StreamScannerTimeout, Err: status.EndError}
	case relaycommon.StreamEndReasonClientGone:
		return StreamScannerResult{Reason: StreamScannerClientDisconnected, Err: status.EndError}
	case relaycommon.StreamEndReasonScannerErr, relaycommon.StreamEndReasonPanic, relaycommon.StreamEndReasonPingFail:
		return StreamScannerResult{Reason: StreamScannerScannerError, Err: status.EndError}
	case relaycommon.StreamEndReasonHandlerStop:
		return StreamScannerResult{Reason: StreamScannerHandlerStopped, Err: status.EndError}
	default:
		return StreamScannerResult{Reason: StreamScannerFinished, Err: status.EndError}
	}
}

func shouldLogXiaomiClaudeStreamData(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if !common.DebugEnabled && !common.DebugTraceEnabledForContext(c) {
		return false
	}
	if c != nil && common.GetContextKeyBool(c, constant.ContextKeyXiaomiClaudeDebug) {
		return true
	}
	return info != nil &&
		info.ChannelType == constant.ChannelTypeXiaomi &&
		info.GetFinalRequestRelayFormat() == types.RelayFormatClaude
}
