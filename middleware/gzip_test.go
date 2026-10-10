package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

type trackedDecompressionBody struct {
	io.Reader
	closed bool
}

func (b *trackedDecompressionBody) Close() error {
	b.closed = true
	return nil
}

func compressTestRequestBody(t *testing.T, encoding string, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	var writer io.WriteCloser
	switch encoding {
	case "gzip":
		writer = gzip.NewWriter(&buf)
	case "br":
		writer = brotli.NewWriter(&buf)
	case "zstd":
		var err error
		writer, err = zstd.NewWriter(&buf)
		require.NoError(t, err)
	default:
		return payload
	}
	_, err := writer.Write(payload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buf.Bytes()
}

func TestDecompressRequestMiddlewareJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := []byte(`{"model":"test-model","input":"hello","stream":true}`)
	for _, encoding := range []string{"", "identity", "gzip", "br", "zstd"} {
		t.Run(encoding, func(t *testing.T) {
			body := &trackedDecompressionBody{Reader: bytes.NewReader(compressTestRequestBody(t, encoding, payload))}
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", body)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Content-Encoding", encoding)
			w := httptest.NewRecorder()
			router := gin.New()
			router.Use(DecompressRequestMiddleware())
			router.POST("/v1/responses", func(c *gin.Context) {
				defer common.CleanupBodyStorage(c)
				var request map[string]any
				require.NoError(t, common.UnmarshalBodyReusable(c, &request))
				require.Equal(t, "test-model", request["model"])
				require.Equal(t, "hello", request["input"])
				require.Equal(t, true, request["stream"])
				if encoding != "identity" {
					require.Empty(t, c.GetHeader("Content-Encoding"))
				}
				c.Status(http.StatusNoContent)
			})

			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusNoContent, w.Code)
			require.True(t, body.closed)
		})
	}
}

func TestDecompressRequestMiddlewareSizeLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalLimit := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() { constant.MaxRequestBodyMB = originalLimit })
	const limit = 1 << 20

	for _, encoding := range []string{"identity", "gzip", "br", "zstd"} {
		t.Run(encoding, func(t *testing.T) {
			for _, oversized := range []bool{false, true} {
				name, size := "at_limit", limit
				if oversized {
					name, size = "over_limit", limit+1
				}
				t.Run(name, func(t *testing.T) {
					payload := bytes.Repeat([]byte("a"), size)
					body := &trackedDecompressionBody{Reader: bytes.NewReader(compressTestRequestBody(t, encoding, payload))}
					req := httptest.NewRequest(http.MethodPost, "/v1/responses", body)
					req.Header.Set("Content-Encoding", encoding)
					w := httptest.NewRecorder()
					router := gin.New()
					router.Use(DecompressRequestMiddleware())
					router.POST("/v1/responses", func(c *gin.Context) {
						defer c.Request.Body.Close()
						decoded, err := io.ReadAll(c.Request.Body)
						if oversized {
							var sizeError *http.MaxBytesError
							require.ErrorAs(t, err, &sizeError)
							require.EqualValues(t, limit, sizeError.Limit)
							require.Len(t, decoded, limit)
						} else {
							require.NoError(t, err)
							require.Equal(t, payload, decoded)
						}
					})

					router.ServeHTTP(w, req)
					require.True(t, body.closed)
				})
			}
		})
	}
}

func TestDecompressRequestMiddlewareInvalidZstd(t *testing.T) {
	gin.SetMode(gin.TestMode)
	valid := compressTestRequestBody(t, "zstd", []byte(`{"model":"test-model","input":"hello"}`))
	for name, payload := range map[string][]byte{
		"invalid":   []byte("not zstd"),
		"truncated": valid[:len(valid)-1],
	} {
		t.Run(name, func(t *testing.T) {
			body := &trackedDecompressionBody{Reader: bytes.NewReader(payload)}
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", body)
			req.Header.Set("Content-Encoding", "zstd")
			w := httptest.NewRecorder()
			router := gin.New()
			router.Use(DecompressRequestMiddleware())
			router.POST("/v1/responses", func(c *gin.Context) {
				defer common.CleanupBodyStorage(c)
				_, err := common.GetRequestBody(c)
				require.Error(t, err)
				c.AbortWithStatus(http.StatusBadRequest)
			})

			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusBadRequest, w.Code)
			require.True(t, body.closed)
		})
	}
}
