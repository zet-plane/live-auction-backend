package service

import (
	"context"
	"errors"
	"strings"

	itemdto "github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	roomcache "github.com/zet-plane/live-auction-backend/internal/app/room/cache"
	"github.com/zet-plane/live-auction-backend/internal/app/room/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/room/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/room/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

type Service struct {
	store      dao.Store
	cache      roomcache.Cache
	itemReader ItemReader
}

type ItemReader interface {
	ListItemsByIDs(ctx context.Context, itemIDs []string) ([]itemdto.ItemListDTO, error)
}

func NewService(store dao.Store, cache roomcache.Cache, readers ...ItemReader) *Service {
	var reader ItemReader
	if len(readers) > 0 {
		reader = readers[0]
	}
	return &Service{store: store, cache: cache, itemReader: reader}
}

func (s *Service) ActivateRoom(ctx context.Context, current *usermodel.User, input dto.CreateRoomInput) (result *dto.MerchantRoomDTO, err error) {
	var roomID string
	finish := observability.Track(ctx, "room.activate", "merchant_id", userID(current))
	defer func() {
		finish(&err, "room_id", roomID)
	}()

	if !isMerchant(current) {
		return nil, errorx.ErrUnauthorized
	}
	existing, err := s.store.FindRoomByMerchantID(current.ID)
	if err == nil {
		roomID = existing.ID
		onlineCount := 0
		if state, ok, _ := s.cache.GetRoomState(ctx, existing.ID); ok {
			onlineCount = state.OnlineCount
		}
		itemQueue, _ := s.cache.GetItemQueue(ctx, existing.ID)
		detail := dto.NewMerchantRoomDTO(existing, onlineCount, len(itemQueue))
		return &detail, nil
	}
	if !errors.Is(err, errorx.ErrNotFound) {
		return nil, err
	}
	room := &model.LiveRoom{
		ID:         "room_" + snowflake.MakeUUID(),
		MerchantID: current.ID,
		Title:      strings.TrimSpace(input.Title),
		Status:     model.RoomIdle,
	}
	if err := s.store.CreateRoom(room); err != nil {
		return nil, err
	}
	roomID = room.ID
	detail := dto.NewMerchantRoomDTO(room, 0, 0)
	return &detail, nil
}

func (s *Service) GetMerchantRoom(ctx context.Context, current *usermodel.User) (result *dto.MerchantRoomDTO, err error) {
	var roomID string
	finish := observability.Track(ctx, "room.get_merchant_room", "merchant_id", userID(current))
	defer func() {
		finish(&err, "room_id", roomID)
	}()

	if !isMerchant(current) {
		return nil, errorx.ErrUnauthorized
	}
	room, err := s.store.FindRoomByMerchantID(current.ID)
	if err != nil {
		return nil, err
	}
	roomID = room.ID
	onlineCount := 0
	if state, ok, _ := s.cache.GetRoomState(ctx, room.ID); ok {
		onlineCount = state.OnlineCount
	}
	itemQueue, _ := s.cache.GetItemQueue(ctx, room.ID)
	detail := dto.NewMerchantRoomDTO(room, onlineCount, len(itemQueue))
	return &detail, nil
}

func (s *Service) StartRoom(ctx context.Context, current *usermodel.User, roomID string) (err error) {
	defer observability.Track(ctx, "room.start", "merchant_id", userID(current), "room_id", strings.TrimSpace(roomID))(&err)

	room, err := s.findMerchantRoom(current, roomID)
	if err != nil {
		return err
	}
	if room.Status != model.RoomIdle {
		return errorx.ErrInvalidRequest
	}
	room.Status = model.RoomLive
	if err := s.store.UpdateRoom(room); err != nil {
		return err
	}
	if initErr := s.cache.InitRoomState(ctx, room.ID, roomcache.RoomState{
		MerchantID: room.MerchantID,
		Status:     "live",
	}); initErr != nil {
		_ = initErr // soft fail
	}
	return nil
}

func (s *Service) EndRoom(ctx context.Context, current *usermodel.User, roomID string) (err error) {
	defer observability.Track(ctx, "room.end", "merchant_id", userID(current), "room_id", strings.TrimSpace(roomID))(&err)

	room, err := s.findMerchantRoom(current, roomID)
	if err != nil {
		return err
	}
	if room.Status != model.RoomLive {
		return errorx.ErrInvalidRequest
	}
	room.Status = model.RoomIdle
	room.CurrentItemID = ""
	if err := s.store.UpdateRoom(room); err != nil {
		return err
	}
	if updateErr := s.cache.UpdateRoomStatus(ctx, room.ID, "idle"); updateErr != nil {
		_ = updateErr // soft fail
	}
	if clearErr := s.cache.ClearRoomCurrentItem(ctx, room.ID); clearErr != nil {
		_ = clearErr // soft fail
	}
	return nil
}

func (s *Service) GetRoom(ctx context.Context, roomID string) (result *dto.RoomDetailDTO, err error) {
	roomID = strings.TrimSpace(roomID)
	defer observability.Track(ctx, "room.get", "room_id", roomID)(&err)

	room, err := s.store.FindRoomByID(roomID)
	if err != nil {
		return s.getRoomFromCache(ctx, roomID, err)
	}
	onlineCount := 0
	if state, ok, _ := s.cache.GetRoomState(ctx, room.ID); ok {
		onlineCount = state.OnlineCount
	}
	itemQueue, _ := s.cache.GetItemQueue(ctx, room.ID)
	itemQueue, items := s.roomItems(ctx, room.CurrentItemID, itemQueue)
	detail := dto.NewRoomDetailDTO(room, onlineCount, itemQueue, items)
	return &detail, nil
}

func (s *Service) getRoomFromCache(ctx context.Context, roomID string, sourceErr error) (*dto.RoomDetailDTO, error) {
	if s.cache == nil {
		return nil, sourceErr
	}
	state, ok, err := s.cache.GetRoomState(ctx, roomID)
	if err != nil || !ok {
		return nil, sourceErr
	}
	itemQueue, _ := s.cache.GetItemQueue(ctx, roomID)
	itemQueue, items := s.roomItems(ctx, state.CurrentItemID, itemQueue)
	room := &model.LiveRoom{
		ID:            roomID,
		MerchantID:    state.MerchantID,
		Status:        model.RoomStatus(state.Status),
		CurrentItemID: state.CurrentItemID,
	}
	detail := dto.NewRoomDetailDTO(room, state.OnlineCount, itemQueue, items)
	return &detail, nil
}

func (s *Service) ListRooms(ctx context.Context, statusFilter model.RoomStatus) (result []*dto.RoomDetailDTO, err error) {
	if statusFilter == "" {
		statusFilter = model.RoomLive
	}
	defer observability.Track(ctx, "room.list", "status", statusFilter)(&err)

	rooms, err := s.store.ListRooms(statusFilter)
	if err != nil {
		return nil, err
	}
	result = make([]*dto.RoomDetailDTO, 0, len(rooms))
	for _, room := range rooms {
		onlineCount := 0
		if state, ok, _ := s.cache.GetRoomState(ctx, room.ID); ok {
			onlineCount = state.OnlineCount
		}
		itemQueue, _ := s.cache.GetItemQueue(ctx, room.ID)
		itemQueue = effectiveItemQueue(room.CurrentItemID, itemQueue)
		d := dto.NewRoomDetailDTO(room, onlineCount, itemQueue, nil)
		result = append(result, &d)
	}
	return result, nil
}

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
		itemQueue, items := s.roomItems(ctx, room.CurrentItemID, itemQueue)
		d := dto.NewRoomDetailDTO(room, onlineCount, itemQueue, items)
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

func (s *Service) roomItems(ctx context.Context, currentItemID string, itemQueue []string) ([]string, []itemdto.ItemListDTO) {
	itemQueue = effectiveItemQueue(currentItemID, itemQueue)
	if s.itemReader == nil || len(itemQueue) == 0 {
		return itemQueue, []itemdto.ItemListDTO{}
	}
	items, err := s.itemReader.ListItemsByIDs(ctx, itemQueue)
	if err != nil {
		return itemQueue, []itemdto.ItemListDTO{}
	}
	return itemQueue, items
}

func effectiveItemQueue(currentItemID string, itemQueue []string) []string {
	currentItemID = strings.TrimSpace(currentItemID)
	if len(itemQueue) == 0 {
		if currentItemID == "" {
			return []string{}
		}
		return []string{currentItemID}
	}
	result := make([]string, 0, len(itemQueue)+1)
	if currentItemID != "" {
		result = append(result, currentItemID)
	}
	for _, itemID := range itemQueue {
		itemID = strings.TrimSpace(itemID)
		if itemID == "" || itemID == currentItemID {
			continue
		}
		result = append(result, itemID)
	}
	return result
}

func (s *Service) findMerchantRoom(current *usermodel.User, roomID string) (*model.LiveRoom, error) {
	if !isMerchant(current) {
		return nil, errorx.ErrUnauthorized
	}
	room, err := s.store.FindRoomByID(strings.TrimSpace(roomID))
	if err != nil {
		return nil, err
	}
	if room.MerchantID != current.ID {
		return nil, errorx.ErrNotFound
	}
	return room, nil
}

func isMerchant(current *usermodel.User) bool {
	return current != nil && current.Identity == usermodel.IdentityMerchant
}

func userID(current *usermodel.User) string {
	if current == nil {
		return ""
	}
	return current.ID
}
