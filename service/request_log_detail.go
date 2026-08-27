package service

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const requestLogDetailRedactedValue = "[REDACTED]"

func sanitizeRequestLogHeaders(headers http.Header) map[string][]string {
	result := make(map[string][]string, len(headers))
	for name, values := range headers {
		if isSensitiveRequestLogHeader(name) {
			result[name] = []string{requestLogDetailRedactedValue}
			continue
		}
		result[name] = append([]string(nil), values...)
	}
	return result
}

func isSensitiveRequestLogHeader(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "authorization" || normalized == "proxy-authorization" || normalized == "cookie" || normalized == "set-cookie" {
		return true
	}
	return strings.Contains(normalized, "api-key") || strings.Contains(normalized, "apikey") || strings.Contains(normalized, "token") || strings.Contains(normalized, "secret")
}

func truncateRequestLogBody(body []byte, limit int) ([]byte, bool) {
	if limit <= 0 || len(body) <= limit {
		return append([]byte(nil), body...), false
	}
	return append([]byte(nil), body[:limit]...), true
}

func requestLogDetailBodyLimit() int {
	return common.GetEnvOrDefault("REQUEST_LOG_DETAIL_MAX_BODY_BYTES", 64*1024)
}

type RequestLogDetailWriter struct {
	gin.ResponseWriter
	body      bytes.Buffer
	bodyBytes int64
}

func (writer *RequestLogDetailWriter) Write(data []byte) (int, error) {
	writer.recordResponse(data)
	return writer.ResponseWriter.Write(data)
}

func (writer *RequestLogDetailWriter) WriteString(data string) (int, error) {
	writer.recordResponse([]byte(data))
	return writer.ResponseWriter.WriteString(data)
}

func (writer *RequestLogDetailWriter) recordResponse(data []byte) {
	writer.bodyBytes += int64(len(data))
	remaining := requestLogDetailBodyLimit() - writer.body.Len()
	if remaining <= 0 {
		return
	}
	if len(data) > remaining {
		data = data[:remaining]
	}
	_, _ = writer.body.Write(data)
}

func SaveCapturedRequestLogDetail(c *gin.Context, writer *RequestLogDetailWriter) {
	if c == nil || writer == nil || !c.GetBool("request_log_recorded") {
		return
	}
	detailId := c.GetString("request_log_detail_id")
	if detailId == "" {
		return
	}
	requestBody := []byte{}
	if storage, err := common.GetBodyStorage(c); err == nil {
		if _, err = storage.Seek(0, io.SeekStart); err == nil {
			requestBody, _ = io.ReadAll(storage)
		}
	}
	requestSize := int64(len(requestBody))
	requestBody, requestTruncated := truncateRequestLogBody(requestBody, requestLogDetailBodyLimit())
	responseBody, responseTruncated := truncateRequestLogBody(writer.body.Bytes(), requestLogDetailBodyLimit())
	requestHeaders, _ := common.Marshal(sanitizeRequestLogHeaders(c.Request.Header))
	responseHeaders, _ := common.Marshal(sanitizeRequestLogHeaders(writer.Header()))
	createdAt := common.GetTimestamp()
	startTime, _ := c.Get("request_log_detail_start_time")
	durationMs := int64(0)
	if started, ok := startTime.(time.Time); ok {
		durationMs = time.Since(started).Milliseconds()
	}
	detail := &model.RequestLogDetail{
		Id:                  detailId,
		LogId:               c.GetInt("request_log_id"),
		UserId:              c.GetInt("id"),
		RequestId:           c.GetString(common.RequestIdKey),
		UpstreamRequestId:   c.GetString(common.UpstreamRequestIdKey),
		RequestMethod:       c.Request.Method,
		RequestPath:         c.Request.URL.Path,
		RequestContentType:  c.Request.Header.Get("Content-Type"),
		ResponseContentType: writer.Header().Get("Content-Type"),
		RequestHeaders:      string(requestHeaders),
		ResponseHeaders:     string(responseHeaders),
		RequestBody:         string(requestBody),
		ResponseBody:        string(responseBody),
		RequestSize:         requestSize,
		ResponseSize:        writer.bodyBytes,
		StatusCode:          writer.Status(),
		DurationMs:          durationMs,
		IsStream:            c.GetBool("is_stream"),
		RequestTruncated:    requestTruncated,
		ResponseTruncated:   responseTruncated || writer.bodyBytes > int64(len(responseBody)),
		CreatedAt:           createdAt,
		UpdatedAt:           createdAt,
	}
	if writer.Status() >= http.StatusBadRequest {
		detail.ErrorBody = detail.ResponseBody
	}
	if err := model.SaveRequestLogDetail(detail); err != nil {
		common.SysError("failed to save request log detail: " + err.Error())
	}
}
