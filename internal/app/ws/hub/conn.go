package hub

import (
	"github.com/gorilla/websocket"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

type Conn struct {
	id     string
	userID string
	roomID string
	ws     *websocket.Conn
	send   chan wsevent.Event
	hub    *Hub
}
