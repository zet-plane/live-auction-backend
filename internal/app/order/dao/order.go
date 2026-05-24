package dao

import (
	"errors"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/order/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/order/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/page"
	"gorm.io/gorm"
)

type Store interface {
	AutoMigrate() error
	CreateOrder(order *model.Order) error
	FindOrder(orderID string) (*model.Order, error)
	FindOrderByItemID(itemID string) (*model.Order, error)
	FindOrderDetail(orderID string) (*dto.OrderDetail, error)
	UpdateOrderStatus(orderID string, from, to model.OrderStatus) (bool, error)
	ListOrders(input dto.ListOrdersInput) ([]dto.OrderWithTitle, int64, error)
	ListExpiredPendingOrders(before time.Time, limit int) ([]model.Order, error)
	ListEndedItemsWithoutOrder(limit int) ([]dto.EndedItemSummary, error)
}

type GormStore struct {
	db *gorm.DB
}

func NewGormStore(db *gorm.DB) *GormStore {
	return &GormStore{db: db}
}

func (s *GormStore) AutoMigrate() error {
	return s.db.AutoMigrate(&model.Order{})
}

func (s *GormStore) CreateOrder(order *model.Order) error {
	return s.db.Create(order).Error
}

func (s *GormStore) FindOrder(orderID string) (*model.Order, error) {
	var o model.Order
	if err := s.db.First(&o, "id = ?", orderID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errorx.ErrNotFound
		}
		return nil, err
	}
	return &o, nil
}

func (s *GormStore) FindOrderByItemID(itemID string) (*model.Order, error) {
	var o model.Order
	if err := s.db.First(&o, "item_id = ?", itemID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errorx.ErrNotFound
		}
		return nil, err
	}
	return &o, nil
}

func (s *GormStore) FindOrderDetail(orderID string) (*dto.OrderDetail, error) {
	var result struct {
		model.Order
		ItemTitle      string `gorm:"column:item_title"`
		ItemMerchantID string `gorm:"column:item_merchant_id"`
	}
	err := s.db.Table("orders").
		Select("orders.*, auction_items.title as item_title, auction_items.merchant_id as item_merchant_id").
		Joins("JOIN auction_items ON auction_items.id = orders.item_id AND auction_items.deleted_at IS NULL").
		Where("orders.id = ?", orderID).
		Scan(&result).Error
	if err != nil {
		return nil, err
	}
	if result.ID == "" {
		return nil, errorx.ErrNotFound
	}
	return &dto.OrderDetail{
		ID:             result.ID,
		ItemID:         result.ItemID,
		ItemTitle:      result.ItemTitle,
		ItemMerchantID: result.ItemMerchantID,
		UserID:         result.UserID,
		Price:          result.Price,
		Status:         result.Status,
		ExpiredAt:      result.ExpiredAt.Format(time.RFC3339),
		CreatedAt:      result.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      result.UpdatedAt.Format(time.RFC3339),
	}, nil
}

func (s *GormStore) UpdateOrderStatus(orderID string, from, to model.OrderStatus) (bool, error) {
	result := s.db.Model(&model.Order{}).
		Where("id = ? AND status = ?", orderID, from).
		Update("status", to)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (s *GormStore) ListOrders(input dto.ListOrdersInput) ([]dto.OrderWithTitle, int64, error) {
	db := s.db.Table("orders").
		Select("orders.*, auction_items.title as item_title").
		Joins("JOIN auction_items ON auction_items.id = orders.item_id AND auction_items.deleted_at IS NULL")

	if input.UserID != "" {
		db = db.Where("orders.user_id = ?", input.UserID)
	}
	if input.MerchantID != "" {
		db = db.Where("auction_items.merchant_id = ?", input.MerchantID)
	}
	if input.Status != "" {
		db = db.Where("orders.status = ?", input.Status)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []struct {
		model.Order
		ItemTitle string `gorm:"column:item_title"`
	}
	if err := db.Order("orders.created_at DESC").
		Scopes(page.Paginate(input.Page, input.PageSize)).
		Scan(&rows).Error; err != nil {
		return nil, 0, err
	}

	list := make([]dto.OrderWithTitle, len(rows))
	for i, r := range rows {
		list[i] = dto.OrderWithTitle{
			ID:        r.ID,
			ItemID:    r.ItemID,
			ItemTitle: r.ItemTitle,
			UserID:    r.UserID,
			Price:     r.Price,
			Status:    r.Status,
			ExpiredAt: r.ExpiredAt.Format(time.RFC3339),
			CreatedAt: r.CreatedAt.Format(time.RFC3339),
		}
	}
	return list, total, nil
}

func (s *GormStore) ListExpiredPendingOrders(before time.Time, limit int) ([]model.Order, error) {
	var orders []model.Order
	err := s.db.Where("status = ? AND expired_at < ?", model.OrderPending, before).
		Limit(limit).Find(&orders).Error
	return orders, err
}

func (s *GormStore) ListEndedItemsWithoutOrder(limit int) ([]dto.EndedItemSummary, error) {
	var results []dto.EndedItemSummary
	err := s.db.Raw(`
		SELECT ai.id as item_id, ai.winner_id, ai.deal_price
		FROM auction_items ai
		LEFT JOIN orders o ON o.item_id = ai.id
		WHERE ai.status = 'ended'
		  AND ai.winner_id != ''
		  AND ai.deleted_at IS NULL
		  AND o.id IS NULL
		LIMIT ?
	`, limit).Scan(&results).Error
	return results, err
}
