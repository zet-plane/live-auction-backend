package dao

import (
	"errors"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/deposit/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"gorm.io/gorm"
)

type Store interface {
	AutoMigrate() error
	FindRequiredAmount(itemID string) (int64, error)
	FindDeposit(itemID, userID string) (*model.Deposit, error)
	CreateDeposit(deposit *model.Deposit) error
	UpdateDeposit(deposit *model.Deposit) error
	ListPaidDepositsByItem(itemID string) ([]model.Deposit, error)
	TransitionDepositStatus(itemID, userID string, from, to model.DepositStatus, terminalAt *time.Time) (bool, error)
}

type GormStore struct {
	db *gorm.DB
}

func NewGormStore(db *gorm.DB) *GormStore {
	return &GormStore{db: db}
}

func (s *GormStore) AutoMigrate() error {
	return s.db.AutoMigrate(&model.Deposit{})
}

func (s *GormStore) FindRequiredAmount(itemID string) (int64, error) {
	var row struct {
		DepositAmount int64 `gorm:"column:deposit_amount"`
	}
	err := s.db.Table("auction_rules").
		Select("auction_rules.deposit_amount").
		Joins("JOIN auction_items ON auction_items.rule_id = auction_rules.id AND auction_items.deleted_at IS NULL").
		Where("auction_items.id = ?", itemID).
		Scan(&row).Error
	if err != nil {
		return 0, err
	}
	var count int64
	if err := s.db.Table("auction_items").Where("id = ? AND deleted_at IS NULL", itemID).Count(&count).Error; err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, errorx.ErrNotFound
	}
	return row.DepositAmount, nil
}

func (s *GormStore) FindDeposit(itemID, userID string) (*model.Deposit, error) {
	var d model.Deposit
	if err := s.db.First(&d, "item_id = ? AND user_id = ?", itemID, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errorx.ErrNotFound
		}
		return nil, err
	}
	return &d, nil
}

func (s *GormStore) CreateDeposit(deposit *model.Deposit) error {
	return s.db.Create(deposit).Error
}

func (s *GormStore) UpdateDeposit(deposit *model.Deposit) error {
	return s.db.Save(deposit).Error
}

func (s *GormStore) ListPaidDepositsByItem(itemID string) ([]model.Deposit, error) {
	var deposits []model.Deposit
	err := s.db.Where("item_id = ? AND status = ?", itemID, model.DepositPaid).Find(&deposits).Error
	return deposits, err
}

func (s *GormStore) TransitionDepositStatus(itemID, userID string, from, to model.DepositStatus, terminalAt *time.Time) (bool, error) {
	updates := map[string]any{"status": to}
	if terminalAt != nil {
		updates["refunded_at"] = terminalAt
	}
	result := s.db.Model(&model.Deposit{}).
		Where("item_id = ? AND user_id = ? AND status = ?", itemID, userID, from).
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}
