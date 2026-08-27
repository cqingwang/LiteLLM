package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRecordRelayRequestErrorCreatesZeroQuotaLog(t *testing.T) {
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousLogDatabaseType := common.LogDatabaseType()
	previousRedisEnabled := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	model.DB, model.LOG_DB = db, db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetLogDatabaseType(previousLogDatabaseType)
		common.RedisEnabled = previousRedisEnabled
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "request-error-test")
	c.Set("id", 42)
	c.Set("original_model", "qwen3.8-flash-next")

	recordRelayRequestError(c, types.NewErrorWithStatusCode(
		errors.New("upstream rejected reasoning effort"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadRequest,
		types.ErrOptionWithNoRecordErrorLog(),
	))

	var logEntry model.Log
	require.NoError(t, db.Where("request_id = ?", "request-error-test").First(&logEntry).Error)
	require.Equal(t, model.LogTypeError, logEntry.Type)
	require.Zero(t, logEntry.Quota)
	require.Zero(t, logEntry.PromptTokens)
	require.Zero(t, logEntry.CompletionTokens)
	require.Contains(t, logEntry.Content, "upstream rejected reasoning effort")
	require.Contains(t, logEntry.Other, "400")
}
