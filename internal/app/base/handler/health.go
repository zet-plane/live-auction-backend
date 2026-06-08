package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/internal/app/base/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/base/service"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
	"gorm.io/gorm"
)

var (
	db        *gorm.DB
	cache     *redis.Client
	uploadSvc *service.UploadService
)

func Init(d *gorm.DB, c *redis.Client, u *service.UploadService) {
	db = d
	cache = c
	uploadSvc = u
}

type componentStatus struct {
	Status  string `json:"status"`
	Latency string `json:"latency,omitempty"`
	Error   string `json:"error,omitempty"`
}

type healthData struct {
	Status     string                     `json:"status"`
	Components map[string]componentStatus `json:"components"`
}

func Livez(r flamego.Render) {
	response.OK(r, map[string]string{"status": "ok"})
}

// SignImageUpload returns a browser POST upload signature for an image.
//
// @Summary 获取图片直传签名
// @Tags base
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.SignImageUploadRequest true "图片上传签名请求"
// @Success 200 {object} response.Body{data=dto.SignImageUploadResult}
// @Failure 400 {object} response.Body
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/base/uploads/images/sign [post]
func SignImageUpload(r flamego.Render, req *http.Request, current *usermodel.User, body dto.SignImageUploadRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	if uploadSvc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := uploadSvc.SignImageUpload(req.Context(), current, body.Input())
	if err != nil {
		logx.Warnw("SignImageUpload failed", "user_id", current.ID, "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

func Readyz(r flamego.Render) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	components := make(map[string]componentStatus)
	overall := "ok"

	if db == nil {
		components["mysql"] = componentStatus{Status: "error", Error: "not initialized"}
		overall = "degraded"
	} else {
		start := time.Now()
		sqlDB, err := db.DB()
		if err == nil {
			err = sqlDB.PingContext(ctx)
		}
		elapsed := time.Since(start)
		if err != nil {
			components["mysql"] = componentStatus{Status: "error", Error: err.Error()}
			overall = "degraded"
		} else {
			components["mysql"] = componentStatus{Status: "ok", Latency: elapsed.String()}
		}
	}

	if cache == nil {
		components["redis"] = componentStatus{Status: "error", Error: "not initialized"}
		overall = "degraded"
	} else {
		start := time.Now()
		err := cache.Ping(ctx).Err()
		elapsed := time.Since(start)
		if err != nil {
			components["redis"] = componentStatus{Status: "error", Error: err.Error()}
			overall = "degraded"
		} else {
			components["redis"] = componentStatus{Status: "ok", Latency: elapsed.String()}
		}
	}

	data := healthData{Status: overall, Components: components}
	if overall != "ok" {
		response.Success(r, 503, overall, data)
		return
	}
	response.OK(r, data)
}

// Health checks MySQL and Redis connectivity.
//
// @Summary 健康检查
// @Tags system
// @Produce json
// @Success 200 {object} response.Body{data=healthData}
// @Success 503 {object} response.Body{data=healthData}
// @Router /health [get]
func Health(r flamego.Render) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	components := make(map[string]componentStatus)
	overall := "ok"

	// MySQL
	if db != nil {
		start := time.Now()
		sqlDB, err := db.DB()
		if err == nil {
			err = sqlDB.PingContext(ctx)
		}
		elapsed := time.Since(start)
		if err != nil {
			components["mysql"] = componentStatus{Status: "error", Error: err.Error()}
			overall = "degraded"
		} else {
			components["mysql"] = componentStatus{Status: "ok", Latency: elapsed.String()}
		}
	} else {
		components["mysql"] = componentStatus{Status: "error", Error: "not initialized"}
		overall = "degraded"
	}

	// Redis
	if cache != nil {
		start := time.Now()
		err := cache.Ping(ctx).Err()
		elapsed := time.Since(start)
		if err != nil {
			components["redis"] = componentStatus{Status: "error", Error: err.Error()}
			overall = "degraded"
		} else {
			components["redis"] = componentStatus{Status: "ok", Latency: elapsed.String()}
		}
	} else {
		components["redis"] = componentStatus{Status: "error", Error: "not initialized"}
		overall = "degraded"
	}

	data := healthData{Status: overall, Components: components}
	if overall != "ok" {
		response.Success(r, 503, overall, data)
		return
	}
	response.OK(r, data)
}
