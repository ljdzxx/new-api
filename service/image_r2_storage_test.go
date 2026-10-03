package service

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/image_storage_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func imageStorageTestConfig(t *testing.T) (*image_storage_setting.ImageStorageSetting, *int) {
	t.Helper()
	setting := image_storage_setting.GetImageStorageSetting()
	previous := *setting
	t.Cleanup(func() { *setting = previous })
	uploads := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPut, request.Method)
		require.Equal(t, "image/png", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.Equal(t, imageStorageTestPNG(), body)
		uploads++
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	*setting = image_storage_setting.ImageStorageSetting{
		R2Enabled: true, R2Bucket: "images", R2Endpoint: server.URL,
		R2AccessKeyID: "test-key", R2SecretAccessKey: "test-secret",
		R2ObjectPrefix: "generated-images/", R2URLExpireHours: 24,
	}
	return setting, &uploads
}

func imageStorageTestPNG() []byte {
	return []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 0}
}

func imageStorageTestContext(path string) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, path, nil)
	return context, recorder, &relaycommon.RelayInfo{RequestId: "request-test", UserId: 1}
}

func TestImageR2Base64AndDataURL(t *testing.T) {
	setting, uploads := imageStorageTestConfig(t)
	base64Image := base64.StdEncoding.EncodeToString(imageStorageTestPNG())
	for _, path := range []string{"/v1/images/generations", "/v1/images/edits"} {
		for _, image := range []map[string]string{{"b64_json": base64Image}, {"url": "data:image/png;base64," + base64Image}} {
			context, _, info := imageStorageTestContext(path)
			body, err := common.Marshal(map[string]any{"created": 123, "data": []any{image}, "usage": map[string]int{"output_tokens": 7}})
			require.NoError(t, err)
			rewritten, err := StoreImageResultsToR2(context, info, body)
			require.NoError(t, err)
			require.NotContains(t, string(rewritten), "b64_json")
			require.NotContains(t, string(rewritten), base64Image)
			require.Contains(t, string(rewritten), setting.R2Endpoint+"/images/generated-images/")
			require.Contains(t, string(rewritten), `"output_tokens":7`)
		}
	}
	require.Equal(t, 4, *uploads)
}

func TestImageR2WorkerImportsURLWithoutDownloadingImage(t *testing.T) {
	setting, uploads := imageStorageTestConfig(t)
	workerCalls := 0
	worker := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer "+setting.R2WorkerSecret, request.Header.Get("Authorization"))
		var payload map[string]string
		require.NoError(t, common.DecodeJson(request.Body, &payload))
		require.Equal(t, "https://source.invalid/image.jpg", payload["source_url"])
		require.Equal(t, "images", payload["bucket"])
		require.True(t, strings.HasPrefix(payload["object_key"], "generated-images/"))
		workerCalls++
		body, err := common.Marshal(map[string]string{"object_key": payload["object_key"]})
		require.NoError(t, err)
		_, _ = writer.Write(body)
	}))
	t.Cleanup(worker.Close)
	transport := http.DefaultTransport
	http.DefaultTransport = worker.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = transport })
	setting.R2WorkerURL = worker.URL + "/import"
	setting.R2WorkerSecret = strings.Repeat("a", 32)
	context, _, info := imageStorageTestContext("/v1/images/edits")
	rewritten, err := StoreImageResultsToR2(context, info, []byte(`{"data":[{"url":"https://source.invalid/image.jpg","revised_prompt":"cat"}]}`))
	require.NoError(t, err)
	require.Equal(t, 1, workerCalls)
	require.Equal(t, 0, *uploads)
	require.NotContains(t, string(rewritten), "source.invalid")
	require.Contains(t, string(rewritten), `"revised_prompt":"cat"`)
	require.Contains(t, string(rewritten), "X-Amz-Signature")
}

func TestImageR2WorkerFailuresReturnOriginalURL(t *testing.T) {
	setting, _ := imageStorageTestConfig(t)
	context, _, info := imageStorageTestContext("/v1/images/generations")
	body := []byte(`{"data":[{"url":"http://source.invalid/image.png","revised_prompt":"cat"}],"usage":{"output_tokens":7}}`)
	rewritten, err := StoreImageResultsToR2(context, info, body)
	require.NoError(t, err)
	require.Equal(t, body, rewritten)
	for _, status := range []int{http.StatusForbidden, http.StatusBadGateway, http.StatusOK} {
		worker := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(status)
			_, _ = writer.Write([]byte(`{"object_key":"wrong-key"}`))
		}))
		setting.R2WorkerURL, setting.R2WorkerSecret = worker.URL, strings.Repeat("b", 32)
		transport := http.DefaultTransport
		http.DefaultTransport = worker.Client().Transport
		rewritten, err := StoreImageResultsToR2(context, info, body)
		http.DefaultTransport = transport
		worker.Close()
		require.NoError(t, err)
		require.Equal(t, body, rewritten)
	}
}

func TestImageR2URLFallbackDoesNotBlockOtherImages(t *testing.T) {
	setting, uploads := imageStorageTestConfig(t)
	context, _, info := imageStorageTestContext("/v1/images/generations")
	body, err := common.Marshal(map[string]any{"data": []any{
		map[string]string{"url": "http://source.invalid/image.png", "revised_prompt": "original"},
		map[string]string{"b64_json": base64.StdEncoding.EncodeToString(imageStorageTestPNG())},
	}})
	require.NoError(t, err)
	rewritten, err := StoreImageResultsToR2(context, info, body)
	require.NoError(t, err)
	var result struct {
		Data []map[string]string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(rewritten, &result))
	require.Len(t, result.Data, 2)
	require.Equal(t, "http://source.invalid/image.png", result.Data[0]["url"])
	require.Equal(t, "original", result.Data[0]["revised_prompt"])
	require.True(t, strings.HasPrefix(result.Data[1]["url"], setting.R2Endpoint+"/images/"))
	require.NotContains(t, result.Data[1], "b64_json")
	require.Equal(t, 1, *uploads)
}

func TestImageStorageResponseJSONAndDirectWrites(t *testing.T) {
	_, uploads := imageStorageTestConfig(t)
	base64Image := base64.StdEncoding.EncodeToString(imageStorageTestPNG())
	for _, path := range []string{"/v1/images/generations", "/v1/images/edits"} {
		for _, useCopyHelper := range []bool{true, false} {
			context, recorder, info := imageStorageTestContext(path)
			original := context.Writer
			writer := NewImageStorageResponseWriter(context, info)
			context.Writer = writer
			body := []byte(fmt.Sprintf(`{"data":[{"b64_json":"%s"}],"usage":{"total_tokens":7}}`, base64Image))
			if useCopyHelper {
				IOCopyBytesGracefully(context, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}}, body)
			} else {
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeaderNow()
				_, _ = writer.Write(body[:20])
				_, _ = writer.WriteString(string(body[20:]))
			}
			require.Empty(t, recorder.Body.String())
			context.Writer = original
			require.NoError(t, writer.Finish())
			require.NotContains(t, recorder.Body.String(), "b64_json")
			require.Contains(t, recorder.Body.String(), `"total_tokens":7`)
			require.Equal(t, fmt.Sprint(recorder.Body.Len()), recorder.Header().Get("Content-Length"))
		}
	}
	require.Equal(t, 4, *uploads)
}

func TestImageStorageResponseSSEPartialCompletedAndSplitFrames(t *testing.T) {
	_, uploads := imageStorageTestConfig(t)
	base64Image := base64.StdEncoding.EncodeToString(imageStorageTestPNG())
	for _, path := range []string{"/v1/images/generations", "/v1/images/edits"} {
		context, recorder, info := imageStorageTestContext(path)
		original := context.Writer
		writer := NewImageStorageResponseWriter(context, info)
		context.Writer = writer
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.Header().Set("Content-Length", "999")
		writer.WriteHeader(-1)
		for _, eventType := range []string{"image_generation.partial_image", "image_edit.completed"} {
			frame := fmt.Sprintf("event: %s\r\ndata: {\"type\":\"%s\",\"b64_json\":\"%s\",\"partial_image_index\":0,\"usage\":{\"output_tokens\":7}}\r\n\r\n", eventType, eventType, base64Image)
			for offset := 0; offset < len(frame); offset += 7 {
				end := offset + 7
				if end > len(frame) {
					end = len(frame)
				}
				_, _ = writer.WriteString(frame[offset:end])
			}
			writer.Flush()
		}
		_, _ = writer.WriteString("data: [DONE]\n\n")
		context.Writer = original
		require.NoError(t, writer.Finish())
		require.NotContains(t, recorder.Body.String(), "b64_json")
		require.Contains(t, recorder.Body.String(), "event: image_generation.partial_image")
		require.Contains(t, recorder.Body.String(), "event: image_edit.completed")
		require.Contains(t, recorder.Body.String(), `"output_tokens":7`)
		require.Contains(t, recorder.Body.String(), "data: [DONE]")
		require.Empty(t, recorder.Header().Get("Content-Length"))
		require.Len(t, writer.completed, 1)
	}
	require.Equal(t, 4, *uploads)
}

func TestImageStorageResponseBase64FailsClosed(t *testing.T) {
	imageStorageTestConfig(t)
	for _, streaming := range []bool{false, true} {
		context, recorder, info := imageStorageTestContext("/v1/images/generations")
		original := context.Writer
		writer := NewImageStorageResponseWriter(context, info)
		context.Writer = writer
		if streaming {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = writer.WriteString("data: {\"type\":\"image_generation.completed\",\"b64_json\":\"invalid-upstream-image\"}\n\ndata: [DONE]\n\n")
		} else {
			_, _ = writer.WriteString(`{"data":[{"b64_json":"invalid-upstream-image"}]}`)
		}
		context.Writer = original
		require.Error(t, writer.Finish())
		require.Contains(t, recorder.Body.String(), "image_storage_error")
		require.NotContains(t, recorder.Body.String(), "invalid-upstream-image")
		if !streaming {
			require.Equal(t, http.StatusBadGateway, recorder.Code)
		}
	}
}

func TestImageStorageResponseDisabledAndOtherPaths(t *testing.T) {
	setting, _ := imageStorageTestConfig(t)
	context, _, info := imageStorageTestContext("/v1/images/generations")
	setting.R2Enabled = false
	require.Nil(t, NewImageStorageResponseWriter(context, info))
	setting.R2Enabled = true
	context.Request.URL.Path = "/v1/chat/completions"
	require.Nil(t, NewImageStorageResponseWriter(context, info))
}
