package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	itemdto "github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	itemmodel "github.com/zet-plane/live-auction-backend/internal/app/item/model"
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

func (s *fakeStore) ListLiveRoomsByCursor(cursor *dto.RoomFeedCursor, limit int) ([]*model.LiveRoom, error) {
	var result []*model.LiveRoom
	for _, r := range s.rooms {
		if r.Status != model.RoomLive {
			continue
		}
		if cursor != nil {
			if r.CreatedAt.After(cursor.CreatedAt) {
				continue
			}
			if r.CreatedAt.Equal(cursor.CreatedAt) && r.ID >= cursor.ID {
				continue
			}
		}
		cp := *r
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID > result[j].ID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

// ─── fakeCache ────────────────────────────────────────────────────────────────

type fakeCache struct {
	states       map[string]*roomcache.RoomState
	queues       map[string][]string
	initErr      error
	updateErr    error
	stateErr     error
	itemQueueErr error
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
	if c.stateErr != nil {
		return nil, false, c.stateErr
	}
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

func (c *fakeCache) ClearRoomCurrentItem(_ context.Context, roomID string) error {
	if s, ok := c.states[roomID]; ok {
		s.CurrentItemID = ""
	}
	return nil
}

func (c *fakeCache) GetItemQueue(_ context.Context, roomID string) ([]string, error) {
	if c.itemQueueErr != nil {
		return nil, c.itemQueueErr
	}
	q := c.queues[roomID]
	if q == nil {
		return []string{}, nil
	}
	return q, nil
}

type fakeItemReader struct {
	items map[string]itemdto.ItemListDTO
}

func (r *fakeItemReader) ListItemsByIDs(_ context.Context, itemIDs []string) ([]itemdto.ItemListDTO, error) {
	result := make([]itemdto.ItemListDTO, 0, len(itemIDs))
	for _, itemID := range itemIDs {
		if item, ok := r.items[itemID]; ok {
			result = append(result, item)
		}
	}
	return result, nil
}

// ─── tests ────────────────────────────────────────────────────────────────────

func merchant() *usermodel.User {
	return &usermodel.User{ID: "merch_1", Identity: usermodel.IdentityMerchant}
}

func TestActivateRoomRequiresMerchant(t *testing.T) {
	svc := NewService(newFakeStore(), newFakeCache())
	user := &usermodel.User{ID: "user_1", Identity: usermodel.IdentityUser}
	_, err := svc.ActivateRoom(context.Background(), user, dto.CreateRoomInput{Title: "My Room"})
	if !errors.Is(err, errorx.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestActivateRoomIsIdempotent(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, newFakeCache())
	m := merchant()

	r1, err := svc.ActivateRoom(context.Background(), m, dto.CreateRoomInput{Title: "My Room"})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	r2, err := svc.ActivateRoom(context.Background(), m, dto.CreateRoomInput{Title: "Different Title"})
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

	r, _ := svc.ActivateRoom(context.Background(), m, dto.CreateRoomInput{Title: "My Room"})
	if err := svc.StartRoom(context.Background(), m, r.ID); err != nil {
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

	r, _ := svc.ActivateRoom(context.Background(), m, dto.CreateRoomInput{Title: "My Room"})
	_ = svc.StartRoom(context.Background(), m, r.ID)

	if err := svc.StartRoom(context.Background(), m, r.ID); !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestEndRoomTransitionsToIdle(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, newFakeCache())
	m := merchant()

	r, _ := svc.ActivateRoom(context.Background(), m, dto.CreateRoomInput{Title: "My Room"})
	_ = svc.StartRoom(context.Background(), m, r.ID)

	if err := svc.EndRoom(context.Background(), m, r.ID); err != nil {
		t.Fatalf("EndRoom: %v", err)
	}

	room, _ := store.FindRoomByID(r.ID)
	if room.Status != model.RoomIdle {
		t.Fatalf("expected idle, got %q", room.Status)
	}
}

func TestEndRoomClearsCurrentItemID(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, fc)
	m := merchant()

	r, _ := svc.ActivateRoom(context.Background(), m, dto.CreateRoomInput{Title: "My Room"})
	_ = svc.StartRoom(context.Background(), m, r.ID)
	room, _ := store.FindRoomByID(r.ID)
	room.CurrentItemID = "item_123"
	_ = store.UpdateRoom(room)
	fc.states[r.ID].CurrentItemID = "item_123"

	if err := svc.EndRoom(context.Background(), m, r.ID); err != nil {
		t.Fatalf("EndRoom: %v", err)
	}

	room, _ = store.FindRoomByID(r.ID)
	if room.CurrentItemID != "" {
		t.Fatalf("expected MySQL current_item_id cleared, got %q", room.CurrentItemID)
	}
	if fc.states[r.ID].CurrentItemID != "" {
		t.Fatalf("expected Redis current_item_id cleared, got %q", fc.states[r.ID].CurrentItemID)
	}
}

func TestEndRoomRejectsNonLive(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, newFakeCache())
	m := merchant()

	r, _ := svc.ActivateRoom(context.Background(), m, dto.CreateRoomInput{Title: "My Room"})

	if err := svc.EndRoom(context.Background(), m, r.ID); !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestStartRoomInitializesRedisState(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, fc)
	m := merchant()

	r, _ := svc.ActivateRoom(context.Background(), m, dto.CreateRoomInput{Title: "My Room"})
	_ = svc.StartRoom(context.Background(), m, r.ID)

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

	r, _ := svc.ActivateRoom(context.Background(), m, dto.CreateRoomInput{Title: "My Room"})
	_ = svc.StartRoom(context.Background(), m, r.ID)
	_ = svc.EndRoom(context.Background(), m, r.ID)

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

	r, _ := svc.ActivateRoom(context.Background(), m, dto.CreateRoomInput{Title: "My Room"})
	fc.states[r.ID] = &roomcache.RoomState{Status: "live", OnlineCount: 42}

	result, err := svc.GetRoom(context.Background(), r.ID)
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

	r, _ := svc.ActivateRoom(context.Background(), m, dto.CreateRoomInput{Title: "My Room"})
	fc.queues[r.ID] = []string{"item_1", "item_2"}

	result, err := svc.GetRoom(context.Background(), r.ID)
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if len(result.ItemQueue) != 2 || result.ItemQueue[0] != "item_1" {
		t.Fatalf("expected item_queue [item_1 item_2], got %v", result.ItemQueue)
	}
}

func TestGetRoomReturnsFullItemsInQueueOrder(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	reader := &fakeItemReader{items: map[string]itemdto.ItemListDTO{
		"item_1": {ID: "item_1", RoomID: "room_1", Title: "First", Status: itemmodel.ItemPublished, CurrentPrice: 1000},
		"item_2": {ID: "item_2", RoomID: "room_1", Title: "Second", Status: itemmodel.ItemOngoing, CurrentPrice: 1500},
	}}
	svc := NewService(store, fc, reader)
	m := merchant()

	r, _ := svc.ActivateRoom(context.Background(), m, dto.CreateRoomInput{Title: "My Room"})
	fc.queues[r.ID] = []string{"item_2", "item_1"}
	reader.items["item_1"] = itemdto.ItemListDTO{ID: "item_1", RoomID: r.ID, Title: "First", Status: itemmodel.ItemPublished, CurrentPrice: 1000, EndTime: time.Now().Add(time.Minute)}
	reader.items["item_2"] = itemdto.ItemListDTO{ID: "item_2", RoomID: r.ID, Title: "Second", Status: itemmodel.ItemOngoing, CurrentPrice: 1500, EndTime: time.Now().Add(time.Minute)}

	result, err := svc.GetRoom(context.Background(), r.ID)
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if len(result.Item) != 2 {
		t.Fatalf("expected 2 full items, got %d", len(result.Item))
	}
	if result.Item[0].ID != "item_2" || result.Item[1].ID != "item_1" {
		t.Fatalf("expected item order [item_2 item_1], got [%s %s]", result.Item[0].ID, result.Item[1].ID)
	}
	if result.Item[0].Title != "Second" || result.Item[0].CurrentPrice != 1500 {
		t.Fatalf("expected full item detail for item_2, got %+v", result.Item[0])
	}
}

func TestGetRoomFallsBackWhenCacheMiss(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, newFakeCache())
	m := merchant()

	r, _ := svc.ActivateRoom(context.Background(), m, dto.CreateRoomInput{Title: "My Room"})

	result, err := svc.GetRoom(context.Background(), r.ID)
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

func addRoom(store *fakeStore, id string, status model.RoomStatus, createdAt time.Time) {
	room := &model.LiveRoom{
		ID:         id,
		MerchantID: "merchant_" + id,
		Title:      "Room " + id,
		Status:     status,
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
	}
	store.rooms[id] = room
	store.byMerchant[room.MerchantID] = room
}

func TestListRoomFeedReturnsOnlyLiveRoomsInStableOrder(t *testing.T) {
	store := newFakeStore()
	base := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)
	addRoom(store, "room_a", model.RoomLive, base.Add(time.Minute))
	addRoom(store, "room_c", model.RoomLive, base)
	addRoom(store, "room_b", model.RoomLive, base)
	addRoom(store, "room_idle", model.RoomIdle, base.Add(2*time.Minute))

	result, err := NewService(store, newFakeCache()).ListRoomFeed(context.Background(), dto.RoomFeedInput{Limit: 10})
	if err != nil {
		t.Fatalf("ListRoomFeed: %v", err)
	}
	if len(result.List) != 3 {
		t.Fatalf("expected 3 live rooms, got %d", len(result.List))
	}
	got := []string{result.List[0].ID, result.List[1].ID, result.List[2].ID}
	want := []string{"room_a", "room_c", "room_b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected order %v, got %v", want, got)
	}
	if result.HasMore {
		t.Fatal("expected has_more false")
	}
	if result.NextCursor != "" {
		t.Fatalf("expected empty next_cursor, got %q", result.NextCursor)
	}
}

func TestListRoomFeedReturnsFullItemsInQueueOrder(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	reader := &fakeItemReader{items: map[string]itemdto.ItemListDTO{
		"item_1": {ID: "item_1", RoomID: "room_a", Title: "First", Status: itemmodel.ItemPublished, CurrentPrice: 1000},
		"item_2": {ID: "item_2", RoomID: "room_a", Title: "Second", Status: itemmodel.ItemOngoing, CurrentPrice: 1500},
	}}
	base := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)
	addRoom(store, "room_a", model.RoomLive, base)
	fc.queues["room_a"] = []string{"item_2", "item_1"}

	result, err := NewService(store, fc, reader).ListRoomFeed(context.Background(), dto.RoomFeedInput{Limit: 10})
	if err != nil {
		t.Fatalf("ListRoomFeed: %v", err)
	}
	if len(result.List) != 1 {
		t.Fatalf("expected 1 live room, got %d", len(result.List))
	}
	if len(result.List[0].Item) != 2 {
		t.Fatalf("expected 2 full items, got %d", len(result.List[0].Item))
	}
	if result.List[0].Item[0].ID != "item_2" || result.List[0].Item[1].ID != "item_1" {
		t.Fatalf("expected item order [item_2 item_1], got [%s %s]", result.List[0].Item[0].ID, result.List[0].Item[1].ID)
	}
}

func TestListRoomFeedReturnsNextBatchFromCursor(t *testing.T) {
	store := newFakeStore()
	base := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)
	addRoom(store, "room_4", model.RoomLive, base.Add(4*time.Minute))
	addRoom(store, "room_3", model.RoomLive, base.Add(3*time.Minute))
	addRoom(store, "room_2", model.RoomLive, base.Add(2*time.Minute))
	addRoom(store, "room_1", model.RoomLive, base.Add(time.Minute))

	svc := NewService(store, newFakeCache())
	first, err := svc.ListRoomFeed(context.Background(), dto.RoomFeedInput{Limit: 2})
	if err != nil {
		t.Fatalf("first ListRoomFeed: %v", err)
	}
	if !first.HasMore || first.NextCursor == "" {
		t.Fatalf("expected has_more with next cursor, got %+v", first)
	}
	if got := []string{first.List[0].ID, first.List[1].ID}; !reflect.DeepEqual(got, []string{"room_4", "room_3"}) {
		t.Fatalf("unexpected first batch %v", got)
	}

	second, err := svc.ListRoomFeed(context.Background(), dto.RoomFeedInput{Cursor: first.NextCursor, Limit: 2})
	if err != nil {
		t.Fatalf("second ListRoomFeed: %v", err)
	}
	if got := []string{second.List[0].ID, second.List[1].ID}; !reflect.DeepEqual(got, []string{"room_2", "room_1"}) {
		t.Fatalf("unexpected second batch %v", got)
	}
	if second.HasMore || second.NextCursor != "" {
		t.Fatalf("expected no more results, got %+v", second)
	}
}

func TestListRoomFeedRejectsInvalidCursor(t *testing.T) {
	_, err := NewService(newFakeStore(), newFakeCache()).ListRoomFeed(context.Background(), dto.RoomFeedInput{Cursor: "bad"})
	if !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestListRoomFeedNormalizesLimitAndSoftFailsCache(t *testing.T) {
	store := newFakeStore()
	base := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)
	for i := 0; i < dto.RoomFeedMaxLimit+5; i++ {
		addRoom(store, fmt.Sprintf("room_%02d", i), model.RoomLive, base.Add(time.Duration(i)*time.Minute))
	}
	fc := newFakeCache()
	fc.stateErr = errors.New("state unavailable")
	fc.itemQueueErr = errors.New("queue unavailable")

	result, err := NewService(store, fc).ListRoomFeed(context.Background(), dto.RoomFeedInput{Limit: dto.RoomFeedMaxLimit + 10})
	if err != nil {
		t.Fatalf("ListRoomFeed: %v", err)
	}
	if len(result.List) != dto.RoomFeedMaxLimit {
		t.Fatalf("expected max limit %d, got %d", dto.RoomFeedMaxLimit, len(result.List))
	}
	if !result.HasMore || result.NextCursor == "" {
		t.Fatalf("expected next cursor with more results, got %+v", result)
	}
	if result.List[0].OnlineCount != 0 {
		t.Fatalf("expected online_count fallback 0, got %d", result.List[0].OnlineCount)
	}
	if len(result.List[0].ItemQueue) != 0 {
		t.Fatalf("expected empty item_queue fallback, got %v", result.List[0].ItemQueue)
	}
}
