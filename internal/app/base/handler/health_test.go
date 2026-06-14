package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/base/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/base/service"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	wshub "github.com/zet-plane/live-auction-backend/internal/app/ws/hub"
	"github.com/zet-plane/live-auction-backend/internal/core/availability"
)

func TestLivezAlwaysReturnsOK(t *testing.T) {
	f := flamego.New()
	f.Use(flamego.Renderer())
	f.Get("/livez", Livez)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	f.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestReadyzWithoutDBReturnsServiceUnavailable(t *testing.T) {
	prevDB, prevCache, prevUploadSvc, prevAvailability := db, cache, uploadSvc, availabilityRuntime
	t.Cleanup(func() { Init(prevDB, prevCache, prevUploadSvc) })
	t.Cleanup(func() { availabilityRuntime = prevAvailability })
	Init(nil, nil, nil)
	availabilityRuntime = nil
	f := flamego.New()
	f.Use(flamego.Renderer())
	f.Get("/readyz", Readyz)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	f.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestReadyzReportsLocalRedisActiveAsDegradedOK(t *testing.T) {
	prevAvailability := availabilityRuntime
	t.Cleanup(func() { availabilityRuntime = prevAvailability })
	InitAvailabilityForTest(availability.Snapshot{
		Valid:       true,
		Mode:        availability.ModeLocalRedisActive,
		ActiveRedis: availability.RedisLocal,
		CloudRedis:  availability.DependencyStatus{Healthy: false, Error: "cloud down"},
		LocalRedis:  availability.DependencyStatus{Healthy: true},
		MySQL:       availability.DependencyStatus{Healthy: true},
		MySQLState:  availability.MySQLHealthy,
		Reason:      "local_sticky",
		UpdatedAt:   time.Now(),
	})

	status, body := requestReadyzForTest()
	if status != http.StatusOK {
		t.Fatalf("readyz status = %d, want 200; body=%s", status, body)
	}
	if !strings.Contains(body, `"status":"degraded"`) {
		t.Fatalf("readyz body missing degraded status: %s", body)
	}
	if !strings.Contains(body, `"active_redis":{"status":"ok","value":"local"}`) {
		t.Fatalf("readyz body missing active redis local value: %s", body)
	}
	if !strings.Contains(body, `"message":"service is using backup redis"`) {
		t.Fatalf("readyz body missing failover message: %s", body)
	}
	if !strings.Contains(body, `"readiness":{"status":"ok","value":"ready"}`) {
		t.Fatalf("readyz body missing explicit readiness component: %s", body)
	}
}

func TestReadyzReportsSwitchingToLocalRedisAsReady(t *testing.T) {
	prevAvailability := availabilityRuntime
	t.Cleanup(func() { availabilityRuntime = prevAvailability })
	InitAvailabilityForTest(availability.Snapshot{
		Valid:       true,
		Mode:        availability.ModeLocalRedisSwitching,
		ActiveRedis: availability.RedisLocal,
		CloudRedis:  availability.DependencyStatus{Healthy: false, Error: "cloud down"},
		LocalRedis:  availability.DependencyStatus{Healthy: true},
		MySQL:       availability.DependencyStatus{Healthy: true},
		MySQLState:  availability.MySQLHealthy,
		Reason:      "cloud_redis_failover",
		UpdatedAt:   time.Now(),
	})

	status, body := requestReadyzForTest()
	if status != http.StatusOK {
		t.Fatalf("readyz status = %d, want 200; body=%s", status, body)
	}
	if !strings.Contains(body, `"active_redis":{"status":"ok","value":"local"}`) {
		t.Fatalf("readyz body missing active redis local value: %s", body)
	}
	if !strings.Contains(body, `"message":"service is switching to backup redis"`) {
		t.Fatalf("readyz body missing switching message: %s", body)
	}
}

func TestReadyzReportsOKWhenMySQLExpiredButRedisHealthy(t *testing.T) {
	prevAvailability := availabilityRuntime
	t.Cleanup(func() { availabilityRuntime = prevAvailability })
	InitAvailabilityForTest(availability.Snapshot{
		Valid:       true,
		Mode:        availability.ModeAuctionProtected,
		ActiveRedis: availability.RedisNone,
		CloudRedis:  availability.DependencyStatus{Healthy: true},
		LocalRedis:  availability.DependencyStatus{Healthy: false, Error: "local down"},
		MySQL:       availability.DependencyStatus{Healthy: false, Error: "dial tcp mysql:3306: i/o timeout"},
		MySQLState:  availability.MySQLBuffering,
		Reason:      "mysql_buffering_expired",
		UpdatedAt:   time.Now(),
	})

	status, body := requestReadyzForTest()
	if status != http.StatusOK {
		t.Fatalf("readyz status = %d, want 200; body=%s", status, body)
	}
	if !strings.Contains(body, `"status":"degraded"`) {
		t.Fatalf("readyz body missing degraded status: %s", body)
	}
	if !strings.Contains(body, `"mysql":{"status":"error"`) {
		t.Fatalf("readyz body missing mysql error: %s", body)
	}
}

func TestReadyzReturns503WhenAuctionProtected(t *testing.T) {
	prevAvailability := availabilityRuntime
	t.Cleanup(func() { availabilityRuntime = prevAvailability })
	InitAvailabilityForTest(availability.Snapshot{
		Valid:       true,
		Mode:        availability.ModeAuctionProtected,
		ActiveRedis: availability.RedisNone,
		CloudRedis:  availability.DependencyStatus{Healthy: false, Error: "cloud down"},
		LocalRedis:  availability.DependencyStatus{Healthy: false, Error: "local down"},
		MySQL:       availability.DependencyStatus{Healthy: true},
		MySQLState:  availability.MySQLHealthy,
		Reason:      "redis_down",
		UpdatedAt:   time.Now(),
	})

	status, body := requestReadyzForTest()
	if status != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want 503; body=%s", status, body)
	}
	if !strings.Contains(body, `"status":"degraded"`) {
		t.Fatalf("readyz body missing degraded status: %s", body)
	}
}

func TestReadyzReportsWaitingForBackupRedisAsReady(t *testing.T) {
	prevAvailability := availabilityRuntime
	t.Cleanup(func() { availabilityRuntime = prevAvailability })
	InitAvailabilityForTest(availability.Snapshot{
		Valid:       true,
		Mode:        availability.ModeAuctionProtected,
		ActiveRedis: availability.RedisNone,
		CloudRedis:  availability.DependencyStatus{Healthy: false, Error: "cloud down"},
		LocalRedis:  availability.DependencyStatus{Healthy: true},
		MySQL:       availability.DependencyStatus{Healthy: true},
		MySQLState:  availability.MySQLHealthy,
		Reason:      "cloud_redis_failover_threshold",
		UpdatedAt:   time.Now(),
	})

	status, body := requestReadyzForTest()
	if status != http.StatusOK {
		t.Fatalf("readyz status = %d, want 200; body=%s", status, body)
	}
	if !strings.Contains(body, `"message":"service is waiting to switch to backup redis"`) {
		t.Fatalf("readyz body missing wait-for-backup message: %s", body)
	}
	if !strings.Contains(body, `"local_redis":{"status":"ok"`) {
		t.Fatalf("readyz body missing healthy local redis: %s", body)
	}
	if !strings.Contains(body, `"readiness":{"status":"ok","value":"ready"}`) {
		t.Fatalf("readyz body missing ready component: %s", body)
	}
}

func TestHealthIncludesPresenceDegraded(t *testing.T) {
	prevAvailability := availabilityRuntime
	t.Cleanup(func() {
		availabilityRuntime = prevAvailability
		wshub.SetPresenceStatusForTest("ok")
	})
	InitAvailabilityForTest(availability.Snapshot{
		Valid:       true,
		Mode:        availability.ModeLocalRedisActive,
		ActiveRedis: availability.RedisLocal,
		CloudRedis:  availability.DependencyStatus{Healthy: false, Error: "cloud down"},
		LocalRedis:  availability.DependencyStatus{Healthy: true},
		MySQL:       availability.DependencyStatus{Healthy: true},
		MySQLState:  availability.MySQLHealthy,
		Reason:      "local_mode",
		UpdatedAt:   time.Now(),
	})
	SetPresenceStatusForTest("degraded")
	f := flamego.New()
	f.Use(flamego.Renderer())
	f.Get("/health", Health)

	rec := httptest.NewRecorder()
	f.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "presence") || !strings.Contains(rec.Body.String(), "degraded") {
		t.Fatalf("health body missing presence degraded: %s", rec.Body.String())
	}
}

func requestReadyzForTest() (int, string) {
	f := flamego.New()
	f.Use(flamego.Renderer())
	f.Get("/readyz", Readyz)
	rec := httptest.NewRecorder()
	f.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	return rec.Code, rec.Body.String()
}

func TestSignImageUploadWithoutServiceReturnsInternal(t *testing.T) {
	prevDB, prevCache, prevUploadSvc := db, cache, uploadSvc
	t.Cleanup(func() { Init(prevDB, prevCache, prevUploadSvc) })
	Init(nil, nil, nil)
	f := flamego.New()
	f.Use(flamego.Renderer())
	f.Use(func(c flamego.Context) {
		c.Map(&usermodel.User{ID: "user_123"})
		c.Next()
	})
	f.Post("/api/v1/base/uploads/images/sign", binding.JSON(dto.SignImageUploadRequest{}), SignImageUpload)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/base/uploads/images/sign", strings.NewReader(`{"filename":"a.png","content_type":"image/png","size":1,"usage":"item"}`))
	req.Header.Set("Content-Type", "application/json")
	f.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
}

func TestSignImageUploadWithoutSignerReturnsServiceUnavailable(t *testing.T) {
	prevDB, prevCache, prevUploadSvc := db, cache, uploadSvc
	t.Cleanup(func() { Init(prevDB, prevCache, prevUploadSvc) })
	Init(nil, nil, service.NewUploadService(nil, service.Options{}))
	f := flamego.New()
	f.Use(flamego.Renderer())
	f.Use(func(c flamego.Context) {
		c.Map(&usermodel.User{ID: "user_123"})
		c.Next()
	})
	f.Post("/api/v1/base/uploads/images/sign", binding.JSON(dto.SignImageUploadRequest{}), SignImageUpload)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/base/uploads/images/sign", strings.NewReader(`{"filename":"a.png","content_type":"image/png","size":1,"usage":"item"}`))
	req.Header.Set("Content-Type", "application/json")
	f.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestSignImageUploadBindingErrorReturnsBadRequest(t *testing.T) {
	f := flamego.New()
	f.Use(flamego.Renderer())
	f.Use(func(c flamego.Context) {
		c.Map(&usermodel.User{ID: "user_123"})
		c.Next()
	})
	f.Post("/api/v1/base/uploads/images/sign", binding.JSON(dto.SignImageUploadRequest{}), SignImageUpload)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/base/uploads/images/sign", strings.NewReader(`{`))
	req.Header.Set("Content-Type", "application/json")
	f.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}
