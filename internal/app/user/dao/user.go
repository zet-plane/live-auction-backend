package dao

import (
	"fmt"
	"strings"

	"github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"gorm.io/gorm"
)

var (
	ErrNotFound = errorx.ErrNotFound
)

type Store interface {
	AutoMigrate() error
	CreateUser(user *model.User) error
	FindUserByAccount(account string) (*model.User, error)
	FindUserByID(id string) (*model.User, error)
	UpdateUser(user *model.User) error
	DeleteUser(id string) error
}

type GormStore struct {
	db *gorm.DB
}

func NewGormStore(db *gorm.DB) *GormStore {
	return &GormStore{db: db}
}

func (s *GormStore) AutoMigrate() error {
	return s.db.AutoMigrate(&model.User{})
}

func (s *GormStore) CreateUser(user *model.User) error {
	return s.db.Create(user).Error
}

func (s *GormStore) FindUserByAccount(account string) (*model.User, error) {
	var u model.User
	result := s.db.Where("account = ?", account).Limit(1).Find(&u)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return &u, nil
}

func (s *GormStore) FindUserByID(id string) (*model.User, error) {
	var u model.User
	result := s.db.Where("id = ?", id).Limit(1).Find(&u)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return &u, nil
}

func (s *GormStore) UpdateUser(user *model.User) error {
	result := s.db.Model(&model.User{}).
		Where("id = ?", user.ID).
		Updates(userProfileUpdateValues(user))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func userProfileUpdateValues(user *model.User) map[string]any {
	return map[string]any{
		"name":       user.Name,
		"avatar_url": user.AvatarURL,
		"motto":      user.Motto,
		"identity":   user.Identity,
	}
}

func (s *GormStore) DeleteUser(id string) error {
	result := s.db.Delete(&model.User{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func DefaultName(account string) string {
	account = strings.TrimSpace(account)
	if len(account) <= 4 {
		return fmt.Sprintf("User%s", account)
	}
	return fmt.Sprintf("User%s", account[len(account)-4:])
}
