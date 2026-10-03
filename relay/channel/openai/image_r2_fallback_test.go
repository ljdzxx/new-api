package openai

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/image_storage_setting"

	"github.com/stretchr/testify/require"
)

func TestOpenaiImageR2URLFallbackAllResponseModes(t *testing.T) {
	setting := image_storage_setting.GetImageStorageSetting()
	previous := *setting
	t.Cleanup(func() { *setting = previous })
	*setting = image_storage_setting.ImageStorageSetting{R2Enabled: true}
	for _, path := range []string{"/v1/images/generations", "/v1/images/edits"} {
		for _, mode := range []string{"json", "sse", "json-as-sse"} {
			for _, scheme := range []string{"http", "https"} {
				t.Run(path+"/"+mode+"/"+scheme, func(t *testing.T) {
					sourceURL := scheme + "://source.invalid/image.png"
					body := fmt.Sprintf(`{"data":[{"url":"%s","revised_prompt":"cat"}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}`, sourceURL)
					contentType := "application/json"
					if mode == "sse" {
						contentType = "text/event-stream"
						body = fmt.Sprintf("data: {\"type\":\"image_edit.completed\",\"url\":\"%s\",\"revised_prompt\":\"cat\",\"usage\":{\"input_tokens\":3,\"output_tokens\":4,\"total_tokens\":7}}\n\ndata: [DONE]\n\n", sourceURL)
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
					require.Equal(t, 200, recorder.Code)
					require.Contains(t, recorder.Body.String(), sourceURL)
					require.Contains(t, recorder.Body.String(), `"revised_prompt":"cat"`)
					require.NotContains(t, recorder.Body.String(), "image_storage_error")
					if mode != "json" {
						require.True(t, strings.HasSuffix(recorder.Body.String(), "data: [DONE]\n\n"))
					}
				})
			}
		}
	}
}
