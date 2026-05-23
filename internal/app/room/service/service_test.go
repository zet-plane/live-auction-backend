package service

import (
	"context"
	"errors"
	"testing"

	roomcache "github.com/zet-plane/live-auction-backend/internal/app/room/cache"
	"github.com/zet-plane/live-auction-backend/internal/app/room/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/room/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

// ─── fakeStore ────────────────────────────────────────────────────────────────

type fakeStore struct {
	rooms      map[string]*model.LiveRoom
	byMerchant map[string]*model.LiveRoom
	createErr  error
	updateErr  error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		rooms:      map[string]*model.LiveRoom{},
		byMerchant: map[string]*model.LiveRoom{},
	}
}

func (s *fakeStore) AutoMigrate() error { return nil }

func (s *fakeStore) CreateRoom(room *model.LiveRoom) error {
	if s.createErr != nil {
		return s.createErr
	}
	cp := *room
	s.rooms[room.ID] = &cp
	s.byMerchant[room.MerchantID] = &cp
	return nil
}

func (s *fakeStore) FindRoomByID(roomID string) (*model.LiveRoom, error) {
	r, ok := s.rooms[roomID]
	if !ok {
		return nil, errorx.ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (s *fakeStore) FindRoomByMerchantID(merchantID string) (*model.LiveRoom, error) {
	r, ok := s.byMerchant[merchantID]
	if !ok {
		return nil, errorx.ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (s *fakeStore) UpdateRoom(room *model.LiveRoom) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	cp := *room
	s.rooms[room.ID] = &cp
	s.byMerchant[room.MerchantID] = &cp
	return nil
}

func (s *fakeStore) ListRooms(status model.RoomStatus) ([]*model.LiveRoom, error) {
	var result []*model.LiveRoom
	for _, r := range s.rooms {
		if status == "" || r.Status == status {
			cp := *r
			result = append(result, &cp)
		}
	}
	return result, nil
}

// ─── fakeCache ────────────────────────────────────────────────────────────────

type fakeCache struct {
	states    map[string]*roomcache.RoomState
	queues    map[string][]string
	initErr   error
	updateErr error
}

func newFakeCache() *fakeCache {
	return &fakeCache{
		states: map[string]*roomcache.RoomState{},
		queues: map[string][]string{},
	}
}

func (c *fakeCache) InitRoomState(_ context.Context, roomID string, state roomcache.RoomState) error {
	if c.initErr != nil {
		return c.initErr
	}
	cp := state
	c.states[roomID] = &cp
	return nil
}

func (c *fakeCache) GetRoomState(_ context.Context, roomID string) (*roomcache.RoomState, bool, error) {
	s, ok := c.states[roomID]
	if !ok {
		return nil, false, nil
	}
	cp := *s
	return &cp, true, nil
}

func (c *fakeCache) UpdateRoomStatus(_ context.Context, roomID, status string) error {
	if c.updateErr != nil {
		return c.updateErr
	}
	if s, ok := c.states[roomID]; ok {
		s.Status = status
	} else {
		c.states[roomID] = &roomcache.RoomState{Status: status}
	}
	return nil
}

func (c *fakeCache) GetItemQueue(_ context.Context, roomID string) ([]string, error) {
	q := c.queues[roomID]
	if q == nil {
		return []string{}, nil
	}
	return q, nil
}

// ─── tests ────────────────────────────────────────────────────────────────────

func merchant() *usermodel.User {
	return &usermodel.User{ID: "merch_1", Identity: usermodel.IdentityMerchant}
}

func TestActivateRoomRequiresMerchant(t *testing.T) {
	svc := NewService(newFakeStore(), newFakeCache())
	user := &usermodel.User{ID: "user_1", Identity: usermodel.IdentityUser}
	_, err := svc.ActivateRoom(user, dto.CreateRoomInput{Title: "My Room"})
	if !errors.Is(err, errorx.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestActivateRoomIsIdempotent(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, newFakeCache())
	m := merchant()

	r1, err := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	r2, err := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "Different Title"})
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if r1.ID != r2.ID {
		t.Fatalf("expected same room_id, got %q and %q", r1.ID, r2.ID)
	}
	if len(store.rooms) != 1 {
		t.Fatalf("expected 1 room in store, got %d", len(store.rooms))
	}
}

func TestStartRoomTransitionsToLive(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, newFakeCache())
	m := merchant()

	r, _ := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})
	if err := svc.StartRoom(m, r.ID); err != nil {
		t.Fatalf("StartRoom: %v", err)
	}

	room, _ := store.FindRoomByID(r.ID)
	if room.Status != model.RoomLive {
		t.Fatalf("expected live, got %q", room.Status)
	}
}

func TestStartRoomRejectsNonIdle(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, newFakeCache())
	m := merchant()

	r, _ := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})
	_ = svc.StartRoom(m, r.ID)

	if err := svc.StartRoom(m, r.ID); !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestEndRoomTransitionsToIdle(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, newFakeCache())
	m := merchant()

	r, _ := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})
	_ = svc.StartRoom(m, r.ID)

	if err := svc.EndRoom(m, r.ID); err != nil {
		t.Fatalf("EndRoom: %v", err)
	}

	room, _ := store.FindRoomByID(r.ID)
	if room.Status != model.RoomIdle {
		t.Fatalf("expected idle, got %q", room.Status)
	}
}

func TestEndRoomRejectsNonLive(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, newFakeCache())
	m := merchant()

	r, _ := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})

	if err := svc.EndRoom(m, r.ID); !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestStartRoomInitializesRedisState(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, fc)
	m := merchant()

	r, _ := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})
	_ = svc.StartRoom(m, r.ID)

	state, ok := fc.states[r.ID]
	if !ok {
		t.Fatal("expected Redis state to be initialized after StartRoom")
	}
	if state.Status != "live" {
		t.Fatalf("expected status live, got %q", state.Status)
	}
}

func TestEndRoomUpdatesRedisStatus(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, fc)
	m := merchant()

	r, _ := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})
	_ = svc.StartRoom(m, r.ID)
	_ = svc.EndRoom(m, r.ID)

	state, ok := fc.states[r.ID]
	if !ok {
		t.Fatal("expected Redis state after EndRoom")
	}
	if state.Status != "idle" {
		t.Fatalf("expected status idle, got %q", state.Status)
	}
}

func TestGetRoomEnrichesOnlineCountFromCache(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, fc)
	m := merchant()

	r, _ := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})
	fc.states[r.ID] = &roomcache.RoomState{Status: "live", OnlineCount: 42}

	result, err := svc.GetRoom(r.ID)
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if result.OnlineCount != 42 {
		t.Fatalf("expected online_count 42, got %d", result.OnlineCount)
	}
}

func TestGetRoomReturnsItemQueue(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, fc)
	m := merchant()

	r, _ := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})
	fc.queues[r.ID] = []string{"item_1", "item_2"}

	result, err := svc.GetRoom(r.ID)
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if len(result.ItemQueue) != 2 || result.ItemQueue[0] != "item_1" {
		t.Fatalf("expected item_queue [item_1 item_2], got %v", result.ItemQueue)
	}
}

func TestGetRoomFallsBackWhenCacheMiss(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, newFakeCache())
	m := merchant()

	r, _ := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})

	result, err := svc.GetRoom(r.ID)
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if result.OnlineCount != 0 {
		t.Fatalf("expected online_count 0, got %d", result.OnlineCount)
	}
	if len(result.ItemQueue) != 0 {
		t.Fatalf("expected empty item_queue, got %v", result.ItemQueue)
	}
}
