package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDeleteHistoryLogsStreamsProgress(t *testing.T) {
	originalLogDB := model.LOG_DB
	t.Cleanup(func() { model.LOG_DB = originalLogDB })

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	model.LOG_DB = db
	require.NoError(t, db.Create(&[]model.Log{
		{CreatedAt: 10},
		{CreatedAt: 20},
		{CreatedAt: 30},
		{CreatedAt: 40},
	}).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/log/?target_timestamp=35&batch_size=2&stream=true", nil)

	DeleteHistoryLogs(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	require.Contains(t, recorder.Body.String(), `"type":"progress","deleted":2`)
	require.Contains(t, recorder.Body.String(), `"type":"progress","deleted":3`)
	require.Contains(t, recorder.Body.String(), `"type":"complete","deleted":3`)
	require.Contains(t, recorder.Body.String(), "data: [DONE]\n\n")

	var remaining int64
	require.NoError(t, db.Model(&model.Log{}).Count(&remaining).Error)
	require.Equal(t, int64(1), remaining)
}
