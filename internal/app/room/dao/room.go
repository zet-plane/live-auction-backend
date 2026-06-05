package dao

import (
	"errors"

	"github.com/zet-plane/live-auction-backend/internal/app/room/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/room/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"gorm.io/gorm"
)

type Store interface {
	AutoMigrate() error
	CreateRoom(room *model.LiveRoom) error
	FindRoomByID(roomID string) (*model.LiveRoom, error)
	FindRoomByMerchantID(merchantID string) (*model.LiveRoom, error)
	UpdateRoom(room *model.LiveRoom) error
	ListRooms(status model.RoomStatus) ([]*model.LiveRoom, error)
	ListLiveRoomsByCursor(cursor *dto.RoomFeedCursor, limit int) ([]*model.LiveRoom, error)
}

type GormStore struct {
	db *gorm.DB
}

func NewGormStore(db *gorm.DB) *GormStore {
	return &GormStore{db: db}
}

func (s *GormStore) AutoMigrate() error {
	return s.db.AutoMigrate(&model.LiveRoom{})
}

func (s *GormStore) CreateRoom(room *model.LiveRoom) error {
	return s.db.Create(room).Error
}

func (s *GormStore) FindRoomByID(roomID string) (*model.LiveRoom, error) {
	var room model.LiveRoom
	if err := s.db.First(&room, "id = ?", roomID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errorx.ErrNotFound
		}
		return nil, err
	}
	return &room, nil
}

func (s *GormStore) FindRoomByMerchantID(merchantID string) (*model.LiveRoom, error) {
	var room model.LiveRoom
	if err := s.db.First(&room, "merchant_id = ?", merchantID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errorx.ErrNotFound
		}
		return nil, err
	}
	return &room, nil
}

func (s *GormStore) UpdateRoom(room *model.LiveRoom) error {
	return s.db.Save(room).Error
}

func (s *GormStore) ListRooms(status model.RoomStatus) ([]*model.LiveRoom, error) {
	var rooms []*model.LiveRoom
	db := s.db.Model(&model.LiveRoom{})
	if status != "" {
		db = db.Where("status = ?", status)
	}
	if err := db.Order("created_at DESC").Find(&rooms).Error; err != nil {
		return nil, err
	}
	return rooms, nil
}

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
