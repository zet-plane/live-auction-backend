package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flamego/flamego"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
)

type fakeBidRateLimiter struct {
	allowed bool
	itemID  string
	userID  string
	rate    float64
	burst   int
	nowMS   int64
}

func (f *fakeBidRateLimiter) AllowBidRate(_ context.Context, itemID, userID string, refillRatePerSecond float64, burst int, nowUnixMS int64) (bool, error) {
	f.itemID = itemID
	f.userID = userID
	f.rate = refillRatePerSecond
	f.burst = burst
	f.nowMS = nowUnixMS
	return f.allowed, nil
}

func TestBidRateLimitRejectsBeforeHandler(t *testing.T) {
	limiter := &fakeBidRateLimiter{allowed: false}
	called := false
	f := flamego.New()
	f.Use(flamego.Renderer())
	f.Post("/api/v1/items/{item_id}/bids",
		func(c flamego.Context) {
			c.Map(&usermodel.User{ID: "user_1"})
			c.Next()
		},
		BidRateLimit(limiter, BidRateLimitOptions{
			Enabled:             true,
			RefillRatePerSecond: 2,
			Burst:               4,
		}),
		func(r flamego.Render) {
			called = true
			response.OK(r, nil)
		},
	)

	rec := httptest.NewRecorder()
	f.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/items/item_1/bids", nil))

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body=%s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("expected rate limit middleware to stop before handler")
	}
	if limiter.itemID != "item_1" || limiter.userID != "user_1" {
		t.Fatalf("limiter key = item %q user %q, want item_1/user_1", limiter.itemID, limiter.userID)
	}
	if limiter.rate != 2 || limiter.burst != 4 {
		t.Fatalf("limiter args = rate %v burst %d, want 2/s burst 4", limiter.rate, limiter.burst)
	}
	if limiter.nowMS == 0 {
		t.Fatal("expected non-zero rate limit timestamp")
	}
}
