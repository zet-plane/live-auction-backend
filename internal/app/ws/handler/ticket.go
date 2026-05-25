package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/flamego/flamego"
	"github.com/redis/go-redis/v9"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

const ticketTTL = 45 * time.Second

var redisClient *redis.Client

func InitTicket(r *redis.Client) {
	redisClient = r
}

func IssueTicket(r flamego.Render, current *usermodel.User) {
	ticket, err := generateTicket()
	if err != nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	key := fmt.Sprintf("ws:ticket:%s", ticket)
	if err := redisClient.Set(context.Background(), key, current.ID, ticketTTL).Err(); err != nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	response.OK(r, map[string]string{"ticket": ticket})
}

func generateTicket() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
