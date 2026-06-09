package handler

import (
	"context"
	"net/http"

	"github.com/flamego/flamego"
	"github.com/gorilla/websocket"
	wshub "github.com/zet-plane/live-auction-backend/internal/app/ws/hub"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

var (
	hub      *wshub.Hub
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return web.NewOriginPolicy("debug", nil).Allows(r.Header.Get("Origin")) },
	}
)

func Init(h *wshub.Hub) {
	hub = h
}

func ConfigureOriginChecker(policy web.OriginPolicy) {
	upgrader.CheckOrigin = func(r *http.Request) bool {
		return policy.Allows(r.Header.Get("Origin"))
	}
}

// ServeWS upgrades an authenticated live room WebSocket connection.
//
// @Summary 连接直播间 WebSocket
// @Tags websocket
// @Param room_id path string true "直播间 ID"
// @Param ticket query string true "通过 /api/v1/ws-ticket 签发的 ticket"
// @Success 101 {string} string "Switching Protocols"
// @Failure 400 {string} string "missing ticket or room_id"
// @Failure 401 {string} string "invalid or expired ticket"
// @Router /ws/v1/rooms/{room_id} [get]
func ServeWS(c flamego.Context, w http.ResponseWriter, r *http.Request) {
	roomID := c.Param("room_id")
	ticket := r.URL.Query().Get("ticket")
	if ticket == "" || roomID == "" {
		http.Error(w, "missing ticket or room_id", http.StatusBadRequest)
		return
	}

	userID, err := consumeTicket(context.Background(), ticket)
	if err == ErrTicketAuthorityUnavailable {
		http.Error(w, "ticket authority unavailable", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		http.Error(w, "invalid or expired ticket", http.StatusUnauthorized)
		return
	}

	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	stream := wshub.ParseConnStream(r.URL.Query().Get("stream"))
	conn := wshub.NewConnWithStream("conn_"+snowflake.MakeUUID(), userID, roomID, wsConn, hub, stream)
	hub.Register(conn)
	go conn.StartReadLoop()
	go conn.StartWriteLoop()
}
