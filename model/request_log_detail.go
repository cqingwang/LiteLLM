package model

import (
	"context"

	"github.com/QuantumNous/new-api/common"
)

type RequestLogDetail struct {
	Id                  string `json:"id" gorm:"type:varchar(64);primaryKey"`
	LogId               int    `json:"log_id" gorm:"index"`
	UserId              int    `json:"user_id" gorm:"index"`
	RequestId           string `json:"request_id" gorm:"type:varchar(64);index"`
	UpstreamRequestId   string `json:"upstream_request_id" gorm:"type:varchar(128);index"`
	RequestMethod       string `json:"request_method"`
	RequestPath         string `json:"request_path"`
	RequestContentType  string `json:"request_content_type"`
	ResponseContentType string `json:"response_content_type"`
	RequestHeaders      string `json:"request_headers"`
	ResponseHeaders     string `json:"response_headers"`
	RequestBody         string `json:"request_body" gorm:"type:text"`
	ResponseBody        string `json:"response_body" gorm:"type:text"`
	ErrorBody           string `json:"error_body" gorm:"type:text"`
	RequestSize         int64  `json:"request_size"`
	ResponseSize        int64  `json:"response_size"`
	StatusCode          int    `json:"status_code"`
	DurationMs          int64  `json:"duration_ms"`
	IsStream            bool   `json:"is_stream"`
	RequestTruncated    bool   `json:"request_truncated"`
	ResponseTruncated   bool   `json:"response_truncated"`
	CreatedAt           int64  `json:"created_at" gorm:"index"`
	UpdatedAt           int64  `json:"updated_at"`
}

func NewRequestLogDetailId() string {
	return common.NewRequestId()
}

func GetRequestLogDetailForUser(detailId string, userId int, isAdmin bool) (*RequestLogDetail, error) {
	query := DB.Where("id = ?", detailId)
	if !isAdmin {
		query = query.Where("user_id = ?", userId)
	}
	var detail RequestLogDetail
	if err := query.First(&detail).Error; err != nil {
		return nil, err
	}
	return &detail, nil
}

func SaveRequestLogDetail(detail *RequestLogDetail) error {
	if detail.CreatedAt == 0 {
		detail.CreatedAt = common.GetTimestamp()
	}
	return DB.Save(detail).Error
}

func DeleteOldRequestLogDetails(ctx context.Context, targetTimestamp int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return DB.WithContext(ctx).
		Where("created_at < ?", targetTimestamp).
		Delete(&RequestLogDetail{}).Error
}
