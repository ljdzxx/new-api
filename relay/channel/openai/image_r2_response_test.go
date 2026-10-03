package openai

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/image_storage_setting"

	"github.com/stretchr/testify/require"
)

func TestOpenaiImageR2AllResponseModes(t *testing.T) {
	setting := image_storage_setting.GetImageStorageSetting()
	previous := *setting
	t.Cleanup(func() { *setting = previous })
	uploads := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		uploads++
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	*setting = image_storage_setting.ImageStorageSetting{
		R2Enabled: true, R2Bucket: "images", R2Endpoint: server.URL,
		R2AccessKeyID: "test-key", R2SecretAccessKey: "test-secret",
	}
	workerCalls := 0
	worker := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload map[string]string
		require.NoError(t, common.DecodeJson(request.Body, &payload))
		require.Equal(t, "https://source.invalid/image.png", payload["source_url"])
		workerCalls++
		body, err := common.Marshal(map[string]string{"object_key": payload["object_key"]})
		require.NoError(t, err)
		_, _ = writer.Write(body)
	}))
	t.Cleanup(worker.Close)
	transport := http.DefaultTransport
	http.DefaultTransport = worker.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = transport })
	setting.R2WorkerURL, setting.R2WorkerSecret = worker.URL+"/import", strings.Repeat("a", 32)
	image := base64.StdEncoding.EncodeToString([]byte{137, 80, 78, 71, 13, 10, 26, 10, 0})
	for _, path := range []string{"/v1/images/generations", "/v1/images/edits"} {
		for _, mode := range []string{"json", "sse", "json-as-sse"} {
			for _, format := range []string{"base64", "url"} {
				t.Run(path+"/"+mode+"/"+format, func(t *testing.T) {
					imageField := fmt.Sprintf(`"b64_json":"%s"`, image)
					if format == "url" {
						imageField = `"url":"https://source.invalid/image.png"`
					}
					body := fmt.Sprintf(`{"data":[{%s}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}`, imageField)
					contentType := "application/json"
					if mode == "sse" {
						contentType = "text/event-stream"
						body = fmt.Sprintf("data: {\"type\":\"image_generation.completed\",%s,\"usage\":{\"input_tokens\":3,\"output_tokens\":4,\"total_tokens\":7}}\n\ndata: [DONE]\n\n", imageField)
					}
					context, recorder, response, info := newImageTestContext(t, body, contentType, mode != "json")
					context.Request.URL.Path = path
					original := context.Writer
					writer := service.NewImageStorageResponseWriter(context, info)
					context.Writer = writer
					if mode == "json" {
						usage, relayError := OpenaiImageHandler(context, info, response)
						require.Nil(t, relayError)
						require.Equal(t, 7, usage.TotalTokens)
					} else {
						usage, relayError := OpenaiImageStreamHandler(context, info, response)
						require.Nil(t, relayError)
						require.Equal(t, 7, usage.TotalTokens)
					}
					context.Writer = original
					require.NoError(t, writer.Finish())
					require.NotContains(t, recorder.Body.String(), "b64_json")
					require.NotContains(t, recorder.Body.String(), image)
					require.NotContains(t, recorder.Body.String(), "source.invalid")
					require.Contains(t, recorder.Body.String(), server.URL+"/images/generated-images/")
					if mode != "json" {
						require.True(t, strings.HasSuffix(recorder.Body.String(), "data: [DONE]\n\n"))
					}
				})
			}
		}
	}
	require.Equal(t, 6, uploads)
	require.Equal(t, 6, workerCalls)
}
