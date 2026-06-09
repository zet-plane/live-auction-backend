package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flamego/flamego"
	"github.com/redis/go-redis/v9"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/core/availability"
)

type fakeTicketRedis struct {
	setKeys []string
}

func TestTicketStoreUsesActiveRedisAuthority(t *testing.T) {
	local := &fakeTicketRedis{}
	InitTicketStoreForTest(activeTicketStore{
		snapshot: availability.Snapshot{Valid: true, Mode: availability.ModeLocalRedisActive, ActiveRedis: availability.RedisLocal},
		local:    local,
	})

	err := issueTicketForUser(context.Background(), "ticket_1", "user_1", 45*time.Second)
	if err != nil {
		t.Fatalf("issueTicketForUser() error = %v", err)
	}
	if len(local.setKeys) != 1 || local.setKeys[0] != "ws:ticket:0:ticket_1" {
		t.Fatalf("set keys = %+v", local.setKeys)
	}
}

func TestIssueTicketReturnsServiceUnavailableWhenAuthorityUnavailable(t *testing.T) {
	InitTicketStoreForTest(activeTicketStore{
		snapshot: availability.Snapshot{Valid: false, Mode: availability.ModeAuctionProtected, ActiveRedis: availability.RedisNone, Error: "unavailable"},
	})

	f := flamego.New()
	f.Use(flamego.Renderer())
	f.Use(func(c flamego.Context) {
		c.Map(&usermodel.User{ID: "user_1"})
		c.Next()
	})
	f.Post("/ws-ticket", IssueTicket)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ws-ticket", nil)
	f.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func (f *fakeTicketRedis) Set(_ context.Context, key string, _ any, _ time.Duration) *redis.StatusCmd {
	f.setKeys = append(f.setKeys, key)
	return redis.NewStatusResult("OK", nil)
}

func (_ *fakeTicketRedis) GetDel(context.Context, string) *redis.StringCmd {
	return redis.NewStringResult("", redis.Nil)
}
