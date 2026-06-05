# Room Feed Cursor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a public cursor-based live-room feed endpoint for short-video-style browsing.

**Architecture:** Keep the existing `GET /api/v1/rooms` list untouched and add a dedicated `GET /api/v1/rooms/feed` endpoint. Cursor encoding lives in room DTOs, durable ordering lives in room DAO, and service code owns limit normalization, Redis enrichment, and response shaping.

**Tech Stack:** Go, Flamego handlers/routes, GORM, room fake store/cache unit tests, `pkg/errorx`, `internal/core/observability`.

---

## File Structure

- Modify `internal/app/room/dto/room.go`: add feed input/result types, cursor type, cursor encode/decode helpers, and limit normalization constants.
- Modify `internal/app/room/dto/room_test.go`: add cursor helper tests.
- Modify `internal/app/room/dao/room.go`: add `ListLiveRoomsByCursor` to `Store` and implement the GORM query.
- Modify `internal/app/room/service/service.go`: add `ListRoomFeed` and reuse existing Redis enrichment behavior.
- Modify `internal/app/room/service/service_test.go`: update `fakeStore`, add feed service tests, and add cache-error test coverage.
- Modify `internal/app/room/handler/room.go`: add `ListRoomFeed` handler and Swagger comments.
- Modify `internal/app/room/router/room.go`: register `/api/v1/rooms/feed` before `/api/v1/rooms/{room_id}`.

---

### Task 1: DTO Cursor Helpers

**Files:**
- Modify: `internal/app/room/dto/room.go`
- Test: `internal/app/room/dto/room_test.go`

- [ ] **Step 1: Write failing DTO tests**

Append these tests to `internal/app/room/dto/room_test.go`:

```go
func TestRoomFeedCursorRoundTrip(t *testing.T) {
	createdAt := time.Date(2026, 6, 5, 10, 30, 45, 123456000, time.UTC)
	cursor := RoomFeedCursor{CreatedAt: createdAt, ID: "room_abc"}

	encoded, err := EncodeRoomFeedCursor(cursor)
	if err != nil {
		t.Fatalf("EncodeRoomFeedCursor: %v", err)
	}
	if encoded == "" {
		t.Fatal("expected non-empty cursor")
	}

	decoded, err := DecodeRoomFeedCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeRoomFeedCursor: %v", err)
	}
	if !decoded.CreatedAt.Equal(createdAt) || decoded.ID != cursor.ID {
		t.Fatalf("decoded cursor mismatch: %+v", decoded)
	}
}

func TestDecodeRoomFeedCursorRejectsInvalidValue(t *testing.T) {
	if _, err := DecodeRoomFeedCursor("not-base64"); !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestDecodeRoomFeedCursorRejectsMissingFields(t *testing.T) {
	raw := `{"created_at":"2026-06-05T10:30:45Z","id":""}`
	encoded := base64.RawURLEncoding.EncodeToString([]byte(raw))

	if _, err := DecodeRoomFeedCursor(encoded); !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestNormalizeRoomFeedInput(t *testing.T) {
	input := NormalizeRoomFeedInput(RoomFeedInput{Limit: 0})
	if input.Limit != RoomFeedDefaultLimit {
		t.Fatalf("expected default limit %d, got %d", RoomFeedDefaultLimit, input.Limit)
	}

	input = NormalizeRoomFeedInput(RoomFeedInput{Limit: RoomFeedMaxLimit + 1})
	if input.Limit != RoomFeedMaxLimit {
		t.Fatalf("expected max limit %d, got %d", RoomFeedMaxLimit, input.Limit)
	}

	input = NormalizeRoomFeedInput(RoomFeedInput{Cursor: "  abc  ", Limit: 3})
	if input.Cursor != "abc" || input.Limit != 3 {
		t.Fatalf("expected trimmed cursor and preserved limit, got %+v", input)
	}
}
```

Update the imports in `internal/app/room/dto/room_test.go` to include:

```go
import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)
```

- [ ] **Step 2: Run DTO tests and verify they fail**

Run:

```bash
rtk go test ./internal/app/room/dto
```

Expected: FAIL because `RoomFeedCursor`, `EncodeRoomFeedCursor`, `DecodeRoomFeedCursor`, `NormalizeRoomFeedInput`, `RoomFeedDefaultLimit`, and `RoomFeedMaxLimit` do not exist yet.

- [ ] **Step 3: Implement DTO types and helpers**

In `internal/app/room/dto/room.go`, add these imports:

```go
import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	itemdto "github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/room/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)
```

Add these declarations after `CreateRoomRequest.Input()`:

```go
const (
	RoomFeedDefaultLimit = 10
	RoomFeedMaxLimit     = 50
)

type RoomFeedInput struct {
	Cursor string
	Limit  int
}

type RoomFeedCursor struct {
	CreatedAt time.Time
	ID        string
}

type roomFeedCursorPayload struct {
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
}

type RoomFeedResult struct {
	List       []RoomDetailDTO `json:"list"`
	NextCursor string          `json:"next_cursor"`
	HasMore    bool            `json:"has_more"`
}

func NormalizeRoomFeedInput(input RoomFeedInput) RoomFeedInput {
	input.Cursor = strings.TrimSpace(input.Cursor)
	switch {
	case input.Limit > RoomFeedMaxLimit:
		input.Limit = RoomFeedMaxLimit
	case input.Limit <= 0:
		input.Limit = RoomFeedDefaultLimit
	}
	return input
}

func EncodeRoomFeedCursor(cursor RoomFeedCursor) (string, error) {
	if cursor.CreatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return "", errorx.ErrInvalidRequest
	}
	payload := roomFeedCursorPayload{
		CreatedAt: cursor.CreatedAt.UTC().Format(time.RFC3339Nano),
		ID:        strings.TrimSpace(cursor.ID),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func DecodeRoomFeedCursor(value string) (*RoomFeedCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, errorx.ErrInvalidRequest
	}
	var payload roomFeedCursorPayload
	if err := json.Unmarshal(b, &payload); err != nil {
		return nil, errorx.ErrInvalidRequest
	}
	id := strings.TrimSpace(payload.ID)
	if id == "" || strings.TrimSpace(payload.CreatedAt) == "" {
		return nil, errorx.ErrInvalidRequest
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil {
		return nil, errorx.ErrInvalidRequest
	}
	return &RoomFeedCursor{CreatedAt: createdAt.UTC(), ID: id}, nil
}
```

- [ ] **Step 4: Run DTO tests and verify they pass**

Run:

```bash
rtk go test ./internal/app/room/dto
```

Expected: PASS.

- [ ] **Step 5: Commit Task 1**

Run:

```bash
rtk git add internal/app/room/dto/room.go internal/app/room/dto/room_test.go
rtk git commit -m "feat: add room feed cursor dto"
```

Expected: commit succeeds with only DTO files staged.

---

### Task 2: DAO Cursor Query

**Files:**
- Modify: `internal/app/room/dao/room.go`
- Test support: `internal/app/room/service/service_test.go`

- [ ] **Step 1: Update the store interface and fake store signature**

In `internal/app/room/dao/room.go`, add the DTO import:

```go
import (
	"errors"

	"github.com/zet-plane/live-auction-backend/internal/app/room/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/room/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"gorm.io/gorm"
)
```

Add this method to `Store`:

```go
ListLiveRoomsByCursor(cursor *dto.RoomFeedCursor, limit int) ([]*model.LiveRoom, error)
```

In `internal/app/room/service/service_test.go`, add a compiling fake method after `ListRooms`:

```go
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
```

Update `internal/app/room/service/service_test.go` imports to include:

```go
import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"
)
```

- [ ] **Step 2: Run room package tests and verify compile status**

Run:

```bash
rtk go test ./internal/app/room/...
```

Expected: FAIL because room module wiring passes `*dao.GormStore` into `service.NewService`, and `GormStore` does not implement the expanded `Store` interface until `ListLiveRoomsByCursor` is added.

- [ ] **Step 3: Implement GORM cursor query**

Add this method to `internal/app/room/dao/room.go` after `ListRooms`:

```go
func (s *GormStore) ListLiveRoomsByCursor(cursor *dto.RoomFeedCursor, limit int) ([]*model.LiveRoom, error) {
	var rooms []*model.LiveRoom
	db := s.db.Model(&model.LiveRoom{}).Where("status = ?", model.RoomLive)
	if cursor != nil {
		db = db.Where(
			"created_at < ? OR (created_at = ? AND id < ?)",
			cursor.CreatedAt,
			cursor.CreatedAt,
			cursor.ID,
		)
	}
	if limit > 0 {
		db = db.Limit(limit)
	}
	if err := db.Order("created_at DESC, id DESC").Find(&rooms).Error; err != nil {
		return nil, err
	}
	return rooms, nil
}
```

- [ ] **Step 4: Run room package tests and verify they pass**

Run:

```bash
rtk go test ./internal/app/room/...
```

Expected: PASS.

- [ ] **Step 5: Commit Task 2**

Run:

```bash
rtk git add internal/app/room/dao/room.go internal/app/room/service/service_test.go
rtk git commit -m "feat: add live room cursor store query"
```

Expected: commit succeeds with DAO and test support files staged.

---

### Task 3: Room Feed Service

**Files:**
- Modify: `internal/app/room/service/service.go`
- Modify: `internal/app/room/service/service_test.go`

- [ ] **Step 1: Add fake cache error switches**

In `internal/app/room/service/service_test.go`, extend `fakeCache`:

```go
type fakeCache struct {
	states      map[string]*roomcache.RoomState
	queues      map[string][]string
	initErr     error
	updateErr   error
	stateErr    error
	itemQueueErr error
}
```

Update `GetRoomState`:

```go
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
```

Update `GetItemQueue`:

```go
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
```

- [ ] **Step 2: Write failing service feed tests**

Append these helpers and tests to `internal/app/room/service/service_test.go`:

```go
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
```

Update imports in `internal/app/room/service/service_test.go` to include:

```go
import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"
)
```

- [ ] **Step 3: Run service tests and verify they fail**

Run:

```bash
rtk go test ./internal/app/room/service
```

Expected: FAIL because `Service.ListRoomFeed` does not exist.

- [ ] **Step 4: Implement `ListRoomFeed`**

Add this method to `internal/app/room/service/service.go` after `ListRooms`:

```go
func (s *Service) ListRoomFeed(ctx context.Context, input dto.RoomFeedInput) (result *dto.RoomFeedResult, err error) {
	input = dto.NormalizeRoomFeedInput(input)
	hasMore := false
	finish := observability.Track(ctx, "room.feed", "limit", input.Limit)
	defer func() {
		finish(&err, "has_more", hasMore)
	}()

	cursor, err := dto.DecodeRoomFeedCursor(input.Cursor)
	if err != nil {
		return nil, err
	}

	rooms, err := s.store.ListLiveRoomsByCursor(cursor, input.Limit+1)
	if err != nil {
		return nil, err
	}
	if len(rooms) > input.Limit {
		hasMore = true
		rooms = rooms[:input.Limit]
	}

	list := make([]dto.RoomDetailDTO, 0, len(rooms))
	for _, room := range rooms {
		onlineCount := 0
		if state, ok, _ := s.cache.GetRoomState(ctx, room.ID); ok {
			onlineCount = state.OnlineCount
		}
		itemQueue, _ := s.cache.GetItemQueue(ctx, room.ID)
		d := dto.NewRoomDetailDTO(room, onlineCount, itemQueue, nil)
		list = append(list, d)
	}

	nextCursor := ""
	if hasMore && len(rooms) > 0 {
		last := rooms[len(rooms)-1]
		nextCursor, err = dto.EncodeRoomFeedCursor(dto.RoomFeedCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return nil, err
		}
	}

	return &dto.RoomFeedResult{
		List:       list,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}
```

- [ ] **Step 5: Run service tests and verify they pass**

Run:

```bash
rtk go test ./internal/app/room/service
```

Expected: PASS.

- [ ] **Step 6: Commit Task 3**

Run:

```bash
rtk git add internal/app/room/service/service.go internal/app/room/service/service_test.go
rtk git commit -m "feat: add room feed service"
```

Expected: commit succeeds with service files staged.

---

### Task 4: Handler, Route, and Package Verification

**Files:**
- Modify: `internal/app/room/handler/room.go`
- Modify: `internal/app/room/router/room.go`

- [ ] **Step 1: Write handler code**

In `internal/app/room/handler/room.go`, add this function after `ListRooms`:

```go
// ListRoomFeed lists live rooms for short-video-style feed browsing.
//
// @Summary 直播间 Feed
// @Tags rooms
// @Produce json
// @Param cursor query string false "游标"
// @Param limit query int false "每批数量"
// @Success 200 {object} response.Body{data=dto.RoomFeedResult}
// @Failure 400 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/rooms/feed [get]
func ListRoomFeed(r flamego.Render, req *http.Request, c flamego.Context) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	input := dto.RoomFeedInput{
		Cursor: c.Query("cursor"),
		Limit:  c.QueryInt("limit"),
	}
	result, err := svc.ListRoomFeed(req.Context(), input)
	if err != nil {
		logx.Warnw("ListRoomFeed failed", "limit", input.Limit, "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}
```

- [ ] **Step 2: Register route before the room-id route**

In `internal/app/room/router/room.go`, update the public route block to:

```go
f.Get("/api/v1/rooms", handler.ListRooms)
f.Get("/api/v1/rooms/feed", handler.ListRoomFeed)
f.Get("/api/v1/rooms/{room_id}", handler.GetRoom)
```

- [ ] **Step 3: Run focused room tests**

Run:

```bash
rtk go test ./internal/app/room/...
```

Expected: PASS.

- [ ] **Step 4: Run broader build verification**

Run:

```bash
rtk go test ./internal/app/room/... ./internal/app/item/... ./internal/app/order/...
```

Expected: PASS. This checks room changes plus nearby modules that share DTOs, item queue DTOs, and service wiring expectations.

- [ ] **Step 5: Commit Task 4**

Run:

```bash
rtk git add internal/app/room/handler/room.go internal/app/room/router/room.go
rtk git commit -m "feat: expose room feed endpoint"
```

Expected: commit succeeds with handler and router files staged.

---

### Task 5: Final Verification

**Files:**
- Verify only; no file edits expected.

- [ ] **Step 1: Run all room-related tests**

Run:

```bash
rtk go test ./internal/app/room/...
```

Expected: PASS.

- [ ] **Step 2: Run full Go test suite if local dependencies allow it**

Run:

```bash
rtk go test ./...
```

Expected: PASS when the local environment has all required test dependencies available. If a test outside the changed room/feed surface fails due to existing environment constraints, capture the exact package and error before deciding whether a narrower verification is acceptable.

- [ ] **Step 3: Inspect git status**

Run:

```bash
rtk git status --short
```

Expected: only unrelated pre-existing workspace changes remain. No uncommitted files from the room feed implementation should be left.

- [ ] **Step 4: Final response**

Report:

```text
Implemented GET /api/v1/rooms/feed for live-only cursor browsing.
Verified with: rtk go test ./internal/app/room/...
Full suite: <result from rtk go test ./...>
```

If commits were created, include the commit hashes from:

```bash
rtk git log --oneline -4
```
