package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/flamego/flamego"
	"github.com/redis/go-redis/v9"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/core/availability"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

const ticketTTL = 45 * time.Second

type redisStringSetter interface {
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
}

type redisStringGetDeleter interface {
	GetDel(ctx context.Context, key string) *redis.StringCmd
}

type activeRedis interface {
	ActiveRedis() (*redis.Client, availability.Snapshot, bool)
}

type activeTicketStore struct {
	runtime  activeRedis
	snapshot availability.Snapshot
	cloud    redisStringSetter
	local    redisStringSetter
	cloudGet redisStringGetDeleter
	localGet redisStringGetDeleter
}

var ticketAuthority activeTicketStore

var ErrTicketAuthorityUnavailable = errorx.New(http.StatusServiceUnavailable, 50303, "websocket ticket authority temporarily unavailable")

func InitTicketAuthority(rt activeRedis) {
	ticketAuthority = activeTicketStore{runtime: rt}
}

func InitTicket(r *redis.Client) {
	ticketAuthority = activeTicketStore{
		snapshot: availability.Snapshot{Valid: true, State: availability.State{Epoch: 0, ActiveRedis: availability.RedisCloud}},
		cloud:    r,
		cloudGet: r,
	}
}

func InitTicketStoreForTest(store activeTicketStore) {
	ticketAuthority = store
}

// IssueTicket issues a short-lived WebSocket ticket.
//
// @Summary 签发 WebSocket ticket
// @Tags websocket
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Body
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/ws-ticket [post]
func IssueTicket(r flamego.Render, current *usermodel.User) {
	ticket, err := generateTicket()
	if err != nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := issueTicketForUser(context.Background(), ticket, current.ID, ticketTTL); err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, map[string]string{"ticket": ticket})
}

func ticketKey(epoch int64, ticket string) string {
	return fmt.Sprintf("ws:ticket:%d:%s", epoch, ticket)
}

func issueTicketForUser(ctx context.Context, ticket, userID string, ttl time.Duration) error {
	client, snapshot, ok := ticketAuthority.activeSetter()
	if !ok || !snapshot.Valid {
		return ErrTicketAuthorityUnavailable
	}
	return client.Set(ctx, ticketKey(snapshot.State.Epoch, ticket), userID, ttl).Err()
}

func consumeTicket(ctx context.Context, ticket string) (string, error) {
	client, snapshot, ok := ticketAuthority.activeGetter()
	if !ok || !snapshot.Valid {
		return "", ErrTicketAuthorityUnavailable
	}
	return client.GetDel(ctx, ticketKey(snapshot.State.Epoch, ticket)).Result()
}

func (s activeTicketStore) activeSetter() (redisStringSetter, availability.Snapshot, bool) {
	if s.runtime != nil {
		client, snapshot, ok := s.runtime.ActiveRedis()
		return client, snapshot, ok
	}
	if s.snapshot.State.ActiveRedis == availability.RedisLocal && s.local != nil {
		return s.local, s.snapshot, true
	}
	if s.cloud != nil {
		return s.cloud, s.snapshot, true
	}
	return nil, s.snapshot, false
}

func (s activeTicketStore) activeGetter() (redisStringGetDeleter, availability.Snapshot, bool) {
	if s.runtime != nil {
		client, snapshot, ok := s.runtime.ActiveRedis()
		return client, snapshot, ok
	}
	if s.snapshot.State.ActiveRedis == availability.RedisLocal && s.localGet != nil {
		return s.localGet, s.snapshot, true
	}
	if s.snapshot.State.ActiveRedis == availability.RedisLocal && s.local != nil {
		if getter, ok := s.local.(redisStringGetDeleter); ok {
			return getter, s.snapshot, true
		}
	}
	if s.cloudGet != nil {
		return s.cloudGet, s.snapshot, true
	}
	if getter, ok := s.cloud.(redisStringGetDeleter); ok {
		return getter, s.snapshot, true
	}
	return nil, s.snapshot, false
}

func generateTicket() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
