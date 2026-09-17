package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/gin-gonic/gin"
)

func TestGetUserAPIInfoWithDashboardDisabled(t *testing.T) {
	settings := console_setting.GetConsoleSetting()
	original := *settings
	t.Cleanup(func() { *settings = original })
	settings.ApiInfoEnabled = false
	settings.ApiInfo = `[{"id":1,"route":"Primary","url":"https://api.example.com","description":"Primary API","color":"blue"},{"id":2,"route":"Backup","url":"https://backup.example.com","description":"Backup API","color":"green"}]`

	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	GetUserAPIInfo(ctx)
	// Decode only the fields relevant to the endpoint contract.
	var payload struct {
		Success bool `json:"success"`
		Data    []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := common.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !payload.Success || len(payload.Data) != 2 {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
	if payload.Data[0].URL != "https://api.example.com" || payload.Data[1].URL != "https://backup.example.com" {
		t.Fatalf("configured endpoints were not preserved: %+v", payload.Data)
	}
}
