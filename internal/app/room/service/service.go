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
		return nil, err
	}
	onlineCount := 0
	if state, ok, _ := s.cache.GetRoomState(ctx, room.ID); ok {
		onlineCount = state.OnlineCount
	}
	itemQueue, _ := s.cache.GetItemQueue(ctx, room.ID)
	items := s.roomItems(ctx, itemQueue)
	detail := dto.NewRoomDetailDTO(room, onlineCount, itemQueue, items)
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
		d := dto.NewRoomDetailDTO(room, onlineCount, itemQueue, nil)
		result = append(result, &d)
	}
	return result, nil
}

func (s *Service) roomItems(ctx context.Context, itemQueue []string) []itemdto.ItemListDTO {
	if s.itemReader == nil || len(itemQueue) == 0 {
		return []itemdto.ItemListDTO{}
	}
	items, err := s.itemReader.ListItemsByIDs(ctx, itemQueue)
	if err != nil {
		return []itemdto.ItemListDTO{}
	}
	return items
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
