package service

import (
	"context"
	"errors"
	"strings"

	roomcache "github.com/zet-plane/live-auction-backend/internal/app/room/cache"
	"github.com/zet-plane/live-auction-backend/internal/app/room/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/room/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/room/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

type Service struct {
	store dao.Store
	cache roomcache.Cache
}

func NewService(store dao.Store, cache roomcache.Cache) *Service {
	return &Service{store: store, cache: cache}
}

func (s *Service) ActivateRoom(current *usermodel.User, input dto.CreateRoomInput) (*dto.MerchantRoomDTO, error) {
	if !isMerchant(current) {
		return nil, errorx.ErrUnauthorized
	}
	existing, err := s.store.FindRoomByMerchantID(current.ID)
	if err == nil {
		onlineCount := 0
		if state, ok, _ := s.cache.GetRoomState(context.Background(), existing.ID); ok {
			onlineCount = state.OnlineCount
		}
		itemQueue, _ := s.cache.GetItemQueue(context.Background(), existing.ID)
		result := dto.NewMerchantRoomDTO(existing, onlineCount, len(itemQueue))
		return &result, nil
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
	result := dto.NewMerchantRoomDTO(room, 0, 0)
	return &result, nil
}

func (s *Service) GetMerchantRoom(current *usermodel.User) (*dto.MerchantRoomDTO, error) {
	if !isMerchant(current) {
		return nil, errorx.ErrUnauthorized
	}
	room, err := s.store.FindRoomByMerchantID(current.ID)
	if err != nil {
		return nil, err
	}
	onlineCount := 0
	if state, ok, _ := s.cache.GetRoomState(context.Background(), room.ID); ok {
		onlineCount = state.OnlineCount
	}
	itemQueue, _ := s.cache.GetItemQueue(context.Background(), room.ID)
	result := dto.NewMerchantRoomDTO(room, onlineCount, len(itemQueue))
	return &result, nil
}

func (s *Service) StartRoom(current *usermodel.User, roomID string) error {
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
	if initErr := s.cache.InitRoomState(context.Background(), room.ID, roomcache.RoomState{
		MerchantID: room.MerchantID,
		Status:     "live",
	}); initErr != nil {
		_ = initErr // soft fail
	}
	return nil
}

func (s *Service) EndRoom(current *usermodel.User, roomID string) error {
	room, err := s.findMerchantRoom(current, roomID)
	if err != nil {
		return err
	}
	if room.Status != model.RoomLive {
		return errorx.ErrInvalidRequest
	}
	room.Status = model.RoomIdle
	if err := s.store.UpdateRoom(room); err != nil {
		return err
	}
	if updateErr := s.cache.UpdateRoomStatus(context.Background(), room.ID, "idle"); updateErr != nil {
		_ = updateErr // soft fail
	}
	return nil
}

func (s *Service) GetRoom(roomID string) (*dto.RoomDetailDTO, error) {
	room, err := s.store.FindRoomByID(strings.TrimSpace(roomID))
	if err != nil {
		return nil, err
	}
	onlineCount := 0
	if state, ok, _ := s.cache.GetRoomState(context.Background(), room.ID); ok {
		onlineCount = state.OnlineCount
	}
	itemQueue, _ := s.cache.GetItemQueue(context.Background(), room.ID)
	result := dto.NewRoomDetailDTO(room, onlineCount, itemQueue)
	return &result, nil
}

func (s *Service) ListRooms(statusFilter model.RoomStatus) ([]*dto.RoomDetailDTO, error) {
	if statusFilter == "" {
		statusFilter = model.RoomLive
	}
	rooms, err := s.store.ListRooms(statusFilter)
	if err != nil {
		return nil, err
	}
	result := make([]*dto.RoomDetailDTO, 0, len(rooms))
	for _, room := range rooms {
		onlineCount := 0
		if state, ok, _ := s.cache.GetRoomState(context.Background(), room.ID); ok {
			onlineCount = state.OnlineCount
		}
		itemQueue, _ := s.cache.GetItemQueue(context.Background(), room.ID)
		d := dto.NewRoomDetailDTO(room, onlineCount, itemQueue)
		result = append(result, &d)
	}
	return result, nil
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
