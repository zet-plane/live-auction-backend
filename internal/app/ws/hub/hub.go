package hub

import (
	"context"
	"strings"
	"sync"

	"github.com/redis/go-redis/v9"
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

func (h *Hub) Register(c *Conn) {
	h.mu.Lock()
	if h.rooms[c.roomID] == nil {
		h.rooms[c.roomID] = make(map[string]*Conn)
	}
	var replaced []*Conn
	for connID, existing := range h.rooms[c.roomID] {
		if existing.userID != c.userID || existing.id == c.id {
			continue
		}
		delete(h.rooms[c.roomID], connID)
		h.removeUserConnLocked(existing)
		replaced = append(replaced, existing)
	}
	h.rooms[c.roomID][c.id] = c
	h.users[c.userID] = append(h.users[c.userID], c)
	h.mu.Unlock()

	for _, old := range replaced {
		old.close()
	}

	if h.presence != nil {
		go h.syncPresenceOnJoin(c.roomID, c.userID)
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
	h.deliver(c, *event)
}

func (h *Hub) Remove(c *Conn) {
	removed := false
	h.mu.Lock()
	if room, ok := h.rooms[c.roomID]; ok {
		if current, ok := room[c.id]; ok && current == c {
			delete(room, c.id)
			removed = true
			if len(room) == 0 {
				delete(h.rooms, c.roomID)
			}
		}
	}
	h.removeUserConnLocked(c)
	h.mu.Unlock()

	if removed && h.presence != nil {
		go h.syncPresenceOnLeave(c.roomID, c.userID)
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
	h.mu.RLock()
	room := h.rooms[roomID]
	conns := make([]*Conn, 0, len(room))
	for _, c := range room {
		conns = append(conns, c)
	}
	h.mu.RUnlock()

	for _, c := range conns {
		h.deliver(c, event)
	}
}

func (h *Hub) Unicast(addr string, event wsevent.Event) error {
	userID := strings.TrimPrefix(addr, "user:")
	h.mu.RLock()
	conns := append([]*Conn(nil), h.users[userID]...)
	h.mu.RUnlock()

	for _, c := range conns {
		h.deliver(c, event)
	}
	return nil
}

func (h *Hub) deliver(c *Conn, event wsevent.Event) {
	if !c.enqueue(event) {
		h.closeConn(c)
	}
}

func (h *Hub) closeConn(c *Conn) {
	if c.isClosed() {
		h.Remove(c)
		return
	}
	c.closeWith(func() {
		h.Remove(c)
	})
}

func (h *Hub) syncPresenceOnJoin(roomID, userID string) {
	if err := h.presence.JoinRoom(context.Background(), roomID, userID); err != nil {
		logx.Warnw("ws.hub sync presence join failed", "room_id", roomID, "user_id", userID, "err", err)
	}
}

func (h *Hub) syncPresenceOnLeave(roomID, userID string) {
	if err := h.presence.LeaveRoom(context.Background(), roomID, userID); err != nil {
		logx.Warnw("ws.hub sync presence leave failed", "room_id", roomID, "user_id", userID, "err", err)
	}
}

type redisPresenceStore struct {
	client *redis.Client
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
