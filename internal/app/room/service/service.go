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
	panic("not implemented")
}

func (s *Service) GetMerchantRoom(current *usermodel.User) (*dto.MerchantRoomDTO, error) {
	panic("not implemented")
}

func (s *Service) StartRoom(current *usermodel.User, roomID string) error {
	panic("not implemented")
}

func (s *Service) EndRoom(current *usermodel.User, roomID string) error {
	panic("not implemented")
}

func (s *Service) GetRoom(roomID string) (*dto.RoomDetailDTO, error) {
	panic("not implemented")
}

func (s *Service) ListRooms(statusFilter model.RoomStatus) ([]*dto.RoomDetailDTO, error) {
	panic("not implemented")
}

func (s *Service) findMerchantRoom(current *usermodel.User, roomID string) (*model.LiveRoom, error) {
	panic("not implemented")
}

func isMerchant(current *usermodel.User) bool {
	return current != nil && current.Identity == usermodel.IdentityMerchant
}

// keep compiler happy — used in Task 6 implementations
var _ = context.Background
var _ = errors.Is
var _ = strings.TrimSpace
var _ = snowflake.MakeUUID
var _ = errorx.ErrUnauthorized
