package hub

import (
	"context"
	"strings"
	"sync"

	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

type SnapshotProvider interface {
	SnapshotForRoom(ctx context.Context, roomID string) (*wsevent.Event, bool, error)
}

type Hub struct {
	mu               sync.RWMutex
	rooms            map[string]map[string]*Conn // roomID → connID → Conn
	users            map[string][]*Conn          // userID → []*Conn
	redis            *redis.Client
	snapshotProvider SnapshotProvider
}

func NewHub(redisClient *redis.Client) *Hub {
	return &Hub{
		rooms: make(map[string]map[string]*Conn),
		users: make(map[string][]*Conn),
		redis: redisClient,
	}
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
	h.rooms[c.roomID][c.id] = c
	h.users[c.userID] = append(h.users[c.userID], c)
	h.mu.Unlock()

	if h.redis != nil {
		go h.syncRedisOnJoin(c.roomID, c.userID)
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
	h.mu.Lock()
	if room, ok := h.rooms[c.roomID]; ok {
		delete(room, c.id)
		if len(room) == 0 {
			delete(h.rooms, c.roomID)
		}
	}
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
	h.mu.Unlock()

	if h.redis != nil {
		go h.syncRedisOnLeave(c.roomID, c.userID)
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
	conns := h.users[userID]
	h.mu.RUnlock()

	for _, c := range conns {
		h.deliver(c, event)
	}
	return nil
}

func (h *Hub) deliver(c *Conn, event wsevent.Event) {
	select {
	case c.send <- event:
	default:
		h.closeConn(c)
	}
}

func (h *Hub) closeConn(c *Conn) {
	h.Remove(c)
	if c.ws != nil {
		c.ws.Close()
	}
	close(c.send)
}

func (h *Hub) syncRedisOnJoin(roomID, userID string) {
	ctx := context.Background()
	stateKey := "auction:room:" + roomID + ":state"
	onlineKey := "auction:room:" + roomID + ":online_users"
	_ = h.redis.SAdd(ctx, onlineKey, userID).Err()
	_ = h.redis.HIncrBy(ctx, stateKey, "online_count", 1).Err()
}

func (h *Hub) syncRedisOnLeave(roomID, userID string) {
	ctx := context.Background()
	stateKey := "auction:room:" + roomID + ":state"
	onlineKey := "auction:room:" + roomID + ":online_users"
	_ = h.redis.SRem(ctx, onlineKey, userID).Err()
	_ = h.redis.HIncrBy(ctx, stateKey, "online_count", -1).Err()
}
