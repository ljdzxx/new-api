package controller

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/dop251/goja"
	"github.com/gin-gonic/gin"
)

const (
	mockJSTimeout        = 100 * time.Millisecond
	mockJSMaxScriptBytes = 64 * 1024
)

func getMockJSHandlerScript(c *gin.Context, settings dto.ChannelSettings) string {
	if c == nil || c.Request == nil || c.Request.URL == nil || len(settings.MockJSHandlers) == 0 {
		return ""
	}
	path := strings.TrimSpace(c.Request.URL.Path)
	if path == "" {
		return ""
	}
	if script := strings.TrimSpace(settings.MockJSHandlers[path]); script != "" {
		return script
	}
	return ""
}

func getMockJSBodyText(c *gin.Context) (string, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return "", nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return "", err
	}
	body, err := storage.Bytes()
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func getMockRequestBodyBytes(c *gin.Context) (int64, error) {
	if c == nil {
		return 0, nil
	}
	if storage, exists := c.Get(common.KeyBodyStorage); exists && storage != nil {
		if bs, ok := storage.(common.BodyStorage); ok {
			return bs.Size(), nil
		}
	}
	if cached, exists := c.Get(common.KeyRequestBody); exists && cached != nil {
		if body, ok := cached.([]byte); ok {
			return int64(len(body)), nil
		}
	}
	if c.Request == nil || c.Request.Body == nil {
		return 0, nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return 0, err
	}
	return storage.Size(), nil
}

func runMockJSProcess(script string, body string) (string, error) {
	script = strings.TrimSpace(script)
	if script == "" {
		return "", fmt.Errorf("mock js handler is empty")
	}
	if len(script) > mockJSMaxScriptBytes {
		return "", fmt.Errorf("mock js handler exceeds %d bytes", mockJSMaxScriptBytes)
	}

	vm := goja.New()
	if err := vm.Set("body", body); err != nil {
		return "", err
	}

	timer := time.AfterFunc(mockJSTimeout, func() {
		vm.Interrupt("mock js execution timeout")
	})
	defer timer.Stop()

	wrapped := fmt.Sprintf(`%s
if (typeof process !== "function") {
  throw new Error("mock js handler must define function process(body)");
}
process(body);`, script)
	value, err := vm.RunString(wrapped)
	if err != nil {
		return "", err
	}
	return value.String(), nil
}
