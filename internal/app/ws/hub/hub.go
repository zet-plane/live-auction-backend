package hub

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/internal/core/availability"
	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

type SnapshotProvider interface {
	SnapshotForRoom(ctx context.Context, roomID string) (*wsevent.Event, bool, error)
}

type presenceStore interface {
	JoinRoom(ctx context.Context, roomID, userID string) error
	LeaveRoom(ctx context.Context, roomID, userID string) error
}

type activeRedisProvider interface {
	ActiveRedis() (*redis.Client, availability.Snapshot, bool)
}

var errActiveRedisUnavailable = errors.New("active redis unavailable")

var presenceStatus atomic.Value

func init() {
	presenceStatus.Store("ok")
}

func PresenceStatus() string {
	return presenceStatus.Load().(string)
}

func SetPresenceStatusForTest(status string) {
	presenceStatus.Store(status)
}

func markPresenceDegraded() {
	presenceStatus.Store("degraded")
}

type Hub struct {
	mu               sync.RWMutex
	rooms            map[string]map[string]*Conn // roomID → connID → Conn
	users            map[string][]*Conn          // userID → []*Conn
	redis            *redis.Client
	presence         presenceStore
	snapshotProvider SnapshotProvider
}

func NewHub(redisClient *redis.Client) *Hub {
	h := &Hub{
		rooms: make(map[string]map[string]*Conn),
		users: make(map[string][]*Conn),
		redis: redisClient,
	}
	if redisClient != nil {
		h.presence = redisPresenceStore{client: redisClient}
	}
	return h
}

func (h *Hub) SetSnapshotProvider(provider SnapshotProvider) {
	h.mu.Lock()
	h.snapshotProvider = provider
	h.mu.Unlock()
}

func (h *Hub) SetPresenceStore(store presenceStore) {
	h.mu.Lock()
	h.presence = store
	h.mu.Unlock()
}

func (h *Hub) Register(c *Conn) {
	h.mu.Lock()
	if h.rooms[c.roomID] == nil {
		h.rooms[c.roomID] = make(map[string]*Conn)
	}
	var replaced []*Conn
	for connID, existing := range h.rooms[c.roomID] {
		if existing.userID != c.userID || existing.stream != c.stream || existing.id == c.id {
			continue
		}
		delete(h.rooms[c.roomID], connID)
		h.removeUserConnLocked(existing)
		replaced = append(replaced, existing)
	}
	h.rooms[c.roomID][c.id] = c
	h.users[c.userID] = append(h.users[c.userID], c)
	h.mu.Unlock()

	observability.DefaultRecorder().WSConnection(context.Background(), observability.WSConnectionMetric{
		Action:      "register",
		Result:      "success",
		ActiveDelta: 1,
	})
	recordConnectionLifecycle(c, "accepted", "")
	for range replaced {
		observability.DefaultRecorder().WSConnection(context.Background(), observability.WSConnectionMetric{
			Action:      "replace",
			Result:      "success",
			ActiveDelta: -1,
		})
	}

	for _, old := range replaced {
		recordConnectionLifecycle(old, "closed", "replaced")
		old.close()
	}

	if h.presence != nil {
		go h.syncPresenceOnJoin(c.roomID, c.userID)
	}

	if !streamAccepts(c.stream, streamControl) {
		return
	}

	h.mu.RLock()
	provider := h.snapshotProvider
	h.mu.RUnlock()
	if provider == nil {
		return
	}
	event, ok, err := provider.SnapshotForRoom(context.Background(), c.roomID)
	if err != nil || !ok || event == nil {
		return
	}
	if !streamAccepts(c.stream, classifyEventStream(event.Type)) {
		return
	}
	h.deliver(c, *event)
}

func (h *Hub) Remove(c *Conn) {
	removed := false
	userRoomStillActive := false
	h.mu.Lock()
	if room, ok := h.rooms[c.roomID]; ok {
		if current, ok := room[c.id]; ok && current == c {
			delete(room, c.id)
			removed = true
			for _, existing := range room {
				if existing.userID == c.userID {
					userRoomStillActive = true
					break
				}
			}
			if len(room) == 0 {
				delete(h.rooms, c.roomID)
			}
		}
	}
	h.removeUserConnLocked(c)
	h.mu.Unlock()

	if removed && !userRoomStillActive && h.presence != nil {
		go h.syncPresenceOnLeave(c.roomID, c.userID)
	}
	if removed {
		observability.DefaultRecorder().WSConnection(context.Background(), observability.WSConnectionMetric{
			Action:      "remove",
			Result:      "success",
			ActiveDelta: -1,
		})
		recordConnectionLifecycle(c, "closed", "normal")
	}
}

func (h *Hub) removeUserConnLocked(c *Conn) {
	conns := h.users[c.userID]
	filtered := conns[:0]
	for _, uc := range conns {
		if uc.id != c.id {
			filtered = append(filtered, uc)
		}
	}
	if len(filtered) == 0 {
		delete(h.users, c.userID)
	} else {
		h.users[c.userID] = filtered
	}
}

func (h *Hub) Fanout(topic string, event wsevent.Event) error {
	roomID := strings.TrimPrefix(topic, "room:")
	h.SendToRoom(roomID, event)
	return nil
}

func (h *Hub) SendToRoom(roomID string, event wsevent.Event) {
	start := time.Now()
	h.mu.RLock()
	room := h.rooms[roomID]
	conns := make([]*Conn, 0, len(room))
	for _, c := range room {
		conns = append(conns, c)
	}
	h.mu.RUnlock()

	var delivered int64
	var dropped int64
	eventStream := classifyEventStream(event.Type)
	for _, c := range conns {
		if !streamAccepts(c.stream, eventStream) {
			continue
		}
		if h.deliver(c, event) {
			delivered++
		} else {
			dropped++
		}
	}
	result := "success"
	if dropped > 0 {
		result = "dropped"
	}
	observability.DefaultRecorder().WSBroadcast(context.Background(), observability.WSBroadcastMetric{
		Mode:       "fanout",
		Result:     result,
		EventType:  event.Type,
		Recipients: delivered,
		Duration:   time.Since(start),
	})
}

func (h *Hub) Unicast(addr string, event wsevent.Event) error {
	userID := strings.TrimPrefix(addr, "user:")
	h.SendToUser(userID, event)
	return nil
}

func (h *Hub) SendToUser(userID string, event wsevent.Event) {
	start := time.Now()
	h.mu.RLock()
	conns := append([]*Conn(nil), h.users[userID]...)
	h.mu.RUnlock()

	var delivered int64
	var dropped int64
	eventStream := classifyEventStream(event.Type)
	for _, c := range conns {
		if !streamAccepts(c.stream, eventStream) {
			continue
		}
		if h.deliver(c, event) {
			delivered++
		} else {
			dropped++
		}
	}
	result := "success"
	if dropped > 0 {
		result = "dropped"
	}
	observability.DefaultRecorder().WSBroadcast(context.Background(), observability.WSBroadcastMetric{
		Mode:       "unicast",
		Result:     result,
		EventType:  event.Type,
		Recipients: delivered,
		Duration:   time.Since(start),
	})
}

func (h *Hub) deliver(c *Conn, event wsevent.Event) bool {
	start := time.Now()
	switch classifyEventLane(event.Type) {
	case laneHigh:
		queueLen := int64(len(c.high))
		queueCap := int64(cap(c.high))
		if !c.enqueueHigh(event) {
			reason := "high_queue_full"
			if c.isClosed() {
				reason = "closed"
			}
			h.closeConnWithReason(c, reason)
			recordDelivery(event.Type, "dropped", reason, queueLen, queueCap, time.Since(start))
			return false
		}
		recordDelivery(event.Type, "success", string(laneHigh), int64(len(c.high)), queueCap, time.Since(start))
		return true
	case laneLatest:
		_, ok := c.enqueueTimeSync(event)
		if !ok {
			reason := "latest_queue_full"
			if c.isClosed() {
				reason = "closed"
			}
			h.closeConnWithReason(c, reason)
			recordDelivery(event.Type, "dropped", reason, 0, 1, time.Since(start))
			return false
		}
		recordDelivery(event.Type, "success", string(laneLatest), 1, 1, time.Since(start))
		return true
	default:
		queueLen := int64(len(c.send))
		queueCap := int64(cap(c.send))
		if !c.enqueueNormal(event) {
			reason := "send_queue_full"
			if c.isClosed() {
				reason = "closed"
			}
			h.closeConnWithReason(c, reason)
			recordDelivery(event.Type, "dropped", reason, queueLen, queueCap, time.Since(start))
			return false
		}
		recordDelivery(event.Type, "success", string(laneNormal), int64(len(c.send)), queueCap, time.Since(start))
		return true
	}
}

func recordDelivery(eventType, result, reason string, queueLen, queueCap int64, duration time.Duration) {
	observability.DefaultRecorder().WSDelivery(context.Background(), observability.WSDeliveryMetric{
		Result:    result,
		Reason:    reason,
		EventType: eventType,
		QueueLen:  queueLen,
		QueueCap:  queueCap,
		Duration:  duration,
	})
}

func recordConnectionLifecycle(c *Conn, result, reason string) {
	if c == nil {
		return
	}
	observability.DefaultRecorder().WSConnectionLifecycle(context.Background(), observability.WSConnectionLifecycleMetric{
		Stream: string(c.stream),
		Result: result,
		Reason: reason,
	})
}

func (h *Hub) closeConn(c *Conn) {
	h.closeConnWithReason(c, "unspecified")
}

func (h *Hub) closeConnWithReason(c *Conn, reason string) {
	if c.isClosed() {
		h.Remove(c)
		observability.DefaultRecorder().WSConnection(context.Background(), observability.WSConnectionMetric{
			Action: "close",
			Result: "already_closed",
			Reason: reason,
		})
		return
	}
	c.closeWith(func() {
		h.Remove(c)
	})
	observability.DefaultRecorder().WSConnection(context.Background(), observability.WSConnectionMetric{
		Action: "close",
		Result: "success",
		Reason: reason,
	})
}

func (h *Hub) syncPresenceOnJoin(roomID, userID string) {
	if err := h.presence.JoinRoom(context.Background(), roomID, userID); err != nil {
		markPresenceDegraded()
		logx.Warnw("ws.hub sync presence join failed", "room_id", roomID, "user_id", userID, "err", err)
	}
}

func (h *Hub) syncPresenceOnLeave(roomID, userID string) {
	if err := h.presence.LeaveRoom(context.Background(), roomID, userID); err != nil {
		markPresenceDegraded()
		logx.Warnw("ws.hub sync presence leave failed", "room_id", roomID, "user_id", userID, "err", err)
	}
}

type redisPresenceStore struct {
	client *redis.Client
}

func NewRedisPresenceStore(client *redis.Client) presenceStore {
	return redisPresenceStore{client: client}
}

type activePresenceStore struct {
	provider activeRedisProvider
}

func NewActivePresenceStore(provider activeRedisProvider) presenceStore {
	return activePresenceStore{provider: provider}
}

func (s activePresenceStore) JoinRoom(ctx context.Context, roomID, userID string) error {
	client, _, ok := s.provider.ActiveRedis()
	if !ok {
		return errActiveRedisUnavailable
	}
	return redisPresenceStore{client: client}.JoinRoom(ctx, roomID, userID)
}

func (s activePresenceStore) LeaveRoom(ctx context.Context, roomID, userID string) error {
	client, _, ok := s.provider.ActiveRedis()
	if !ok {
		return errActiveRedisUnavailable
	}
	return redisPresenceStore{client: client}.LeaveRoom(ctx, roomID, userID)
}

func (s redisPresenceStore) JoinRoom(ctx context.Context, roomID, userID string) error {
	stateKey := "auction:room:" + roomID + ":state"
	onlineKey := "auction:room:" + roomID + ":online_users"
	if err := s.client.SAdd(ctx, onlineKey, userID).Err(); err != nil {
		return err
	}
	count, err := s.client.SCard(ctx, onlineKey).Result()
	if err != nil {
		return err
	}
	return s.client.HSet(ctx, stateKey, "online_count", count).Err()
}

func (s redisPresenceStore) LeaveRoom(ctx context.Context, roomID, userID string) error {
	stateKey := "auction:room:" + roomID + ":state"
	onlineKey := "auction:room:" + roomID + ":online_users"
	if err := s.client.SRem(ctx, onlineKey, userID).Err(); err != nil {
		return err
	}
	count, err := s.client.SCard(ctx, onlineKey).Result()
	if err != nil {
		return err
	}
	return s.client.HSet(ctx, stateKey, "online_count", count).Err()
}
