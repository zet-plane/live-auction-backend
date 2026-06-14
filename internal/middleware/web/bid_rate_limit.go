package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/flamego/flamego"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
)

var ErrBidRateLimited = errorx.New(http.StatusTooManyRequests, 42901, "too many bid requests")

type BidRateLimitOptions struct {
	Enabled             bool
	RefillRatePerSecond float64
	Burst               int
}

type BidRateLimiter interface {
	AllowBidRate(ctx context.Context, itemID, userID string, refillRatePerSecond float64, burst int, nowUnixMS int64) (bool, error)
}

func BidRateLimit(limiter BidRateLimiter, opts BidRateLimitOptions) flamego.Handler {
	opts = normalizeBidRateLimit(opts)
	if !opts.Enabled {
		return func(c flamego.Context) {
			c.Next()
		}
	}
	return func(c flamego.Context, r flamego.Render, req *http.Request, current *usermodel.User) {
		itemID := strings.TrimSpace(c.Param("item_id"))
		if limiter == nil || current == nil || strings.TrimSpace(current.ID) == "" || itemID == "" {
			response.Error(r, errorx.ErrInternal)
			return
		}
		allowed, err := limiter.AllowBidRate(req.Context(), itemID, current.ID, opts.RefillRatePerSecond, opts.Burst, time.Now().UnixMilli())
		if err != nil {
			logx.Warnw("BidRateLimit failed", "user_id", current.ID, "item_id", itemID, "err", err)
			response.Error(r, errorx.ErrServiceUnavailable)
			return
		}
		if !allowed {
			response.Error(r, ErrBidRateLimited)
			return
		}
		c.Next()
	}
}

func normalizeBidRateLimit(opts BidRateLimitOptions) BidRateLimitOptions {
	if !opts.Enabled || opts.RefillRatePerSecond <= 0 || opts.Burst <= 0 {
		return BidRateLimitOptions{}
	}
	return opts
}
