package hub

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

const (
	readDeadline  = 60 * time.Second
	writeDeadline = 10 * time.Second
	highBufSize   = 16
	sendBufSize   = 64
)

var pingInterval = 25 * time.Second

type Conn struct {
	id     string
	userID string
	roomID string
	ws     socket
	high   chan wsevent.Event
	send   chan wsevent.Event
	hub    *Hub

	timeSyncMu      sync.Mutex
	latestTimeSync  *wsevent.Event
	timeSyncUpdated time.Time
	timeSyncNotify  chan struct{}

	closeMu   sync.RWMutex
	closeOnce sync.Once
	closed    bool
}

type socket interface {
	SetReadDeadline(t time.Time) error
	SetPongHandler(h func(appData string) error)
	ReadMessage() (messageType int, p []byte, err error)
	SetWriteDeadline(t time.Time) error
	WriteJSON(v any) error
	WriteControl(messageType int, data []byte, deadline time.Time) error
	Close() error
}

type clientMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

func NewConn(id, userID, roomID string, ws socket, hub *Hub) *Conn {
	return &Conn{
		id:     id,
		userID: userID,
		roomID: roomID,
		ws:     ws,
		high:   make(chan wsevent.Event, highBufSize),
		send:   make(chan wsevent.Event, sendBufSize),
		hub:    hub,

		timeSyncNotify: make(chan struct{}, 1),
	}
}

func (c *Conn) StartReadLoop() {
	defer func() {
		c.hub.closeConnWithReason(c, "read_loop_exit")
	}()

	c.ws.SetReadDeadline(time.Now().Add(readDeadline))
	c.ws.SetPongHandler(func(string) error {
		return c.ws.SetReadDeadline(time.Now().Add(readDeadline))
	})

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
	closeReason := "write_loop_exit"
	defer func() {
		c.hub.closeConnWithReason(c, closeReason)
	}()

	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	var deferredNormal *wsevent.Event
	for {
		if deferredNormal == nil {
			if event, ok := c.popHigh(); ok {
				if reason, ok := c.writeTracked(event, laneHigh); !ok {
					closeReason = "write_json_" + reason
					return
				}
				continue
			}
			if event, lag, ok := c.popTimeSync(); ok {
				if reason, ok := c.writeTracked(event, laneLatest); !ok {
					recordTimeSyncWrite("failed", lag)
					closeReason = "write_json_" + reason
					return
				}
				recordTimeSyncWrite("success", lag)
				continue
			}
		}
		if deferredNormal != nil {
			if event, ok := c.popHigh(); ok {
				if reason, ok := c.writeTracked(event, laneHigh); !ok {
					closeReason = "write_json_" + reason
					return
				}
				continue
			}
			if event, lag, ok := c.popTimeSync(); ok {
				if reason, ok := c.writeTracked(event, laneLatest); !ok {
					recordTimeSyncWrite("failed", lag)
					closeReason = "write_json_" + reason
					return
				}
				recordTimeSyncWrite("success", lag)
				continue
			}
			event := *deferredNormal
			deferredNormal = nil
			if reason, ok := c.writeTracked(event, laneNormal); !ok {
				closeReason = "write_json_" + reason
				return
			}
			continue
		}

		select {
		case event, ok := <-c.high:
			if !ok {
				closeReason = "high_closed"
				return
			}
			if reason, ok := c.writeTracked(event, laneHigh); !ok {
				closeReason = "write_json_" + reason
				return
			}
		case <-c.timeSyncNotify:
			continue
		case event, ok := <-c.send:
			if !ok {
				closeReason = "send_closed"
				return
			}
			if high, ok := c.popHigh(); ok {
				deferredNormal = &event
				if reason, ok := c.writeTracked(high, laneHigh); !ok {
					closeReason = "write_json_" + reason
					return
				}
				continue
			}
			if latest, lag, ok := c.popTimeSync(); ok {
				deferredNormal = &event
				if reason, ok := c.writeTracked(latest, laneLatest); !ok {
					recordTimeSyncWrite("failed", lag)
					closeReason = "write_json_" + reason
					return
				}
				recordTimeSyncWrite("success", lag)
				continue
			}
			if reason, ok := c.writeTracked(event, laneNormal); !ok {
				closeReason = "write_json_" + reason
				return
			}
		case <-ticker.C:
			deadline := time.Now().Add(writeDeadline)
			if err := c.ws.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
				closeReason = "ping_" + classifySocketWriteReason(err)
				return
			}
		}
	}
}

func (c *Conn) popHigh() (wsevent.Event, bool) {
	select {
	case event, ok := <-c.high:
		return event, ok
	default:
		return wsevent.Event{}, false
	}
}

func (c *Conn) popTimeSync() (wsevent.Event, time.Duration, bool) {
	c.timeSyncMu.Lock()
	defer c.timeSyncMu.Unlock()
	if c.latestTimeSync == nil {
		return wsevent.Event{}, 0, false
	}
	event := *c.latestTimeSync
	lag := time.Since(c.timeSyncUpdated)
	c.latestTimeSync = nil
	return event, lag, true
}

func (c *Conn) writeTracked(event wsevent.Event, lane eventLane) (string, bool) {
	queueLen, queueCap := c.queueStats(lane)
	c.ws.SetWriteDeadline(time.Now().Add(writeDeadline))
	start := time.Now()
	if err := c.ws.WriteJSON(event); err != nil {
		reason := classifySocketWriteReason(err)
		observability.DefaultRecorder().WSWrite(context.Background(), observability.WSWriteMetric{
			Result:    "failed",
			Reason:    reason,
			EventType: event.Type,
			QueueLen:  queueLen,
			QueueCap:  queueCap,
			Duration:  time.Since(start),
		})
		return reason, false
	}
	observability.DefaultRecorder().WSWrite(context.Background(), observability.WSWriteMetric{
		Result:    "success",
		EventType: event.Type,
		QueueLen:  queueLen,
		QueueCap:  queueCap,
		Duration:  time.Since(start),
	})
	return "none", true
}

func recordTimeSyncWrite(result string, lag time.Duration) {
	observability.DefaultRecorder().WSTimeSync(context.Background(), observability.WSTimeSyncMetric{
		Action:   "write",
		Result:   result,
		WriteLag: lag,
	})
}

func (c *Conn) queueStats(lane eventLane) (int64, int64) {
	switch lane {
	case laneHigh:
		return int64(len(c.high)), int64(cap(c.high))
	case laneLatest:
		return 0, 1
	default:
		return int64(len(c.send)), int64(cap(c.send))
	}
}

func classifySocketWriteReason(err error) string {
	if err == nil {
		return "none"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return "timeout"
	case strings.Contains(msg, "reset"):
		return "connection_reset"
	case strings.Contains(msg, "broken pipe"):
		return "broken_pipe"
	case strings.Contains(msg, "closed"):
		return "closed"
	default:
		return "write_error"
	}
}

func (c *Conn) enqueue(event wsevent.Event) bool {
	switch classifyEventLane(event.Type) {
	case laneHigh:
		return c.enqueueHigh(event)
	case laneLatest:
		_, ok := c.enqueueTimeSync(event)
		return ok
	default:
		return c.enqueueNormal(event)
	}
}

func (c *Conn) enqueueHigh(event wsevent.Event) bool {
	c.closeMu.RLock()
	defer c.closeMu.RUnlock()
	if c.closed {
		return false
	}
	select {
	case c.high <- event:
		return true
	default:
		return false
	}
}

func (c *Conn) enqueueNormal(event wsevent.Event) bool {
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

func (c *Conn) enqueueTimeSync(event wsevent.Event) (bool, bool) {
	c.closeMu.RLock()
	defer c.closeMu.RUnlock()
	if c.closed {
		return false, false
	}

	c.timeSyncMu.Lock()
	overwritten := c.latestTimeSync != nil
	eventCopy := event
	c.latestTimeSync = &eventCopy
	c.timeSyncUpdated = time.Now()
	c.timeSyncMu.Unlock()
	if overwritten {
		observability.DefaultRecorder().WSTimeSync(context.Background(), observability.WSTimeSyncMetric{
			Action: "overwrite",
			Result: "success",
		})
	}

	select {
	case c.timeSyncNotify <- struct{}{}:
	default:
	}
	return overwritten, true
}

func (c *Conn) close() {
	c.closeWith(nil)
}

func (c *Conn) closeWith(beforeClose func()) {
	c.closeOnce.Do(func() {
		if beforeClose != nil {
			beforeClose()
		}
		c.closeMu.Lock()
		c.closed = true
		if c.high != nil {
			close(c.high)
		}
		if c.send != nil {
			close(c.send)
		}
		c.closeMu.Unlock()
		if c.ws != nil {
			_ = c.ws.Close()
		}
	})
}

func (c *Conn) isClosed() bool {
	c.closeMu.RLock()
	defer c.closeMu.RUnlock()
	return c.closed
}
