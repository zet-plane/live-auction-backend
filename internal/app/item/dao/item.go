package dao

import (
	"errors"
	"strings"

	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/item/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/page"
	"gorm.io/gorm"
)

type Store interface {
	AutoMigrate() error
	CreateItemWithRule(item *model.AuctionItem, rule *model.AuctionRule) error
	FindItemWithRule(itemID string) (*model.AuctionItem, *model.AuctionRule, error)
	UpdateItemWithRule(item *model.AuctionItem, rule *model.AuctionRule) error
	DeleteItem(itemID string) error
	ListItems(query dto.ListItemsInput) ([]model.ItemWithRule, int64, error)
	AutoMigrateBidLog() error
	CreateBidLog(log *model.BidLog) error
	ListBidRanking(itemID string, limit int) ([]dto.BidderPrice, error)
}

type GormStore struct {
	db *gorm.DB
}

func NewGormStore(db *gorm.DB) *GormStore {
	return &GormStore{db: db}
}

func (s *GormStore) AutoMigrate() error {
	return s.db.AutoMigrate(&model.AuctionItem{}, &model.AuctionRule{})
}

func (s *GormStore) CreateItemWithRule(item *model.AuctionItem, rule *model.AuctionRule) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(item).Error; err != nil {
			return err
		}
		return tx.Create(rule).Error
	})
}

func (s *GormStore) FindItemWithRule(itemID string) (*model.AuctionItem, *model.AuctionRule, error) {
	var item model.AuctionItem
	if err := s.db.First(&item, "id = ?", itemID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, errorx.ErrNotFound
		}
		return nil, nil, err
	}
	var rule model.AuctionRule
	if err := s.db.First(&rule, "id = ?", item.RuleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, errorx.ErrNotFound
		}
		return nil, nil, err
	}
	return &item, &rule, nil
}

func (s *GormStore) UpdateItemWithRule(item *model.AuctionItem, rule *model.AuctionRule) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(item).Error; err != nil {
			return err
		}
		return tx.Save(rule).Error
	})
}

func (s *GormStore) DeleteItem(itemID string) error {
	result := s.db.Delete(&model.AuctionItem{}, "id = ?", itemID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errorx.ErrNotFound
	}
	return nil
}

func (s *GormStore) ListItems(query dto.ListItemsInput) ([]model.ItemWithRule, int64, error) {
	db := s.db.Model(&model.AuctionItem{})
	if query.MerchantID != "" {
		db = db.Where("merchant_id = ?", query.MerchantID)
	}
	if query.Status != "" {
		db = db.Where("status = ?", query.Status)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		db = db.Where("title LIKE ? OR description LIKE ?", like, like)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var items []model.AuctionItem
	if err := db.Order("created_at DESC").Scopes(page.Paginate(query.Page, query.PageSize)).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	if len(items) == 0 {
		return []model.ItemWithRule{}, total, nil
	}

	ruleIDs := make([]string, 0, len(items))
	for _, item := range items {
		ruleIDs = append(ruleIDs, item.RuleID)
	}
	var rules []model.AuctionRule
	if err := s.db.Where("id IN ?", ruleIDs).Find(&rules).Error; err != nil {
		return nil, 0, err
	}
	ruleByID := make(map[string]*model.AuctionRule, len(rules))
	for i := range rules {
		ruleByID[rules[i].ID] = &rules[i]
	}

	list := make([]model.ItemWithRule, 0, len(items))
	for i := range items {
		rule := ruleByID[items[i].RuleID]
		if rule == nil {
			continue
		}
		list = append(list, model.ItemWithRule{Item: &items[i], Rule: rule})
	}
	return list, total, nil
}
