package handler

import (
	"context"
	"fmt"
	"net/http"

	"github.com/flamego/flamego"
	"github.com/gorilla/websocket"
	wshub "github.com/zet-plane/live-auction-backend/internal/app/ws/hub"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

var (
	hub      *wshub.Hub
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}
)

func Init(h *wshub.Hub) {
	hub = h
}

func ServeWS(c flamego.Context, w http.ResponseWriter, r *http.Request) {
	roomID := c.Param("room_id")
	ticket := r.URL.Query().Get("ticket")
	if ticket == "" || roomID == "" {
		http.Error(w, "missing ticket or room_id", http.StatusBadRequest)
		return
	}

	key := fmt.Sprintf("ws:ticket:%s", ticket)
	userID, err := redisClient.GetDel(context.Background(), key).Result()
	if err != nil {
		http.Error(w, "invalid or expired ticket", http.StatusUnauthorized)
		return
	}

	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	conn := wshub.NewConn("conn_"+snowflake.MakeUUID(), userID, roomID, wsConn, hub)
	hub.Register(conn)
	go conn.StartReadLoop()
	go conn.StartWriteLoop()
}
