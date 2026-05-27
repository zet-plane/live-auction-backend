package handler

import (
	"context"
	"time"

	"github.com/flamego/flamego"
	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"gorm.io/gorm"
)

var (
	db    *gorm.DB
	cache *redis.Client
)

func Init(d *gorm.DB, c *redis.Client) {
	db = d
	cache = c
}

type componentStatus struct {
	Status  string `json:"status"`
	Latency string `json:"latency,omitempty"`
	Error   string `json:"error,omitempty"`
}

type healthData struct {
	Status string                     `json:"status"`
	Components map[string]componentStatus `json:"components"`
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
