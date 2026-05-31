package hub

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

const (
	readDeadline  = 60 * time.Second
	writeDeadline = 10 * time.Second
	sendBufSize   = 64
)

type Conn struct {
	id        string
	userID    string
	roomID    string
	ws        *websocket.Conn
	send      chan wsevent.Event
	hub       *Hub
	closeMu   sync.RWMutex
	closeOnce sync.Once
	closed    bool
}

type clientMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

func NewConn(id, userID, roomID string, ws *websocket.Conn, hub *Hub) *Conn {
	return &Conn{
		id:     id,
		userID: userID,
		roomID: roomID,
		ws:     ws,
		send:   make(chan wsevent.Event, sendBufSize),
		hub:    hub,
	}
}

func (c *Conn) StartReadLoop() {
	defer func() {
		c.hub.closeConn(c)
	}()

	c.ws.SetReadDeadline(time.Now().Add(readDeadline))

	for {
		_, msg, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		c.ws.SetReadDeadline(time.Now().Add(readDeadline))

		var cm clientMessage
		if err := json.Unmarshal(msg, &cm); err != nil {
			continue
		}

		switch cm.Type {
		case "ping":
			c.enqueue(wsevent.Event{Type: "pong"})
		case "leave_room":
			return
		}
	}
}

func (c *Conn) StartWriteLoop() {
	defer c.hub.closeConn(c)

	for event := range c.send {
		c.ws.SetWriteDeadline(time.Now().Add(writeDeadline))
		if err := c.ws.WriteJSON(event); err != nil {
			return
		}
	}
}

func (c *Conn) enqueue(event wsevent.Event) bool {
	c.closeMu.RLock()
	defer c.closeMu.RUnlock()
	if c.closed {
		return false
	}
	select {
	case c.send <- event:
		return true
	default:
		return false
	}
}

func (c *Conn) close() {
	c.closeOnce.Do(func() {
		c.closeMu.Lock()
		c.closed = true
		close(c.send)
		c.closeMu.Unlock()
		if c.ws != nil {
			_ = c.ws.Close()
		}
	})
}
