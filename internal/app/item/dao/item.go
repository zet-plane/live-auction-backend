package dao

import (
	"errors"
	"strings"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/item/model"
	roommodel "github.com/zet-plane/live-auction-backend/internal/app/room/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/page"
	"gorm.io/gorm"
)

type Store interface {
	AutoMigrate() error
	CreateItemWithRule(item *model.AuctionItem, rule *model.AuctionRule) error
	FindItemWithRule(itemID string) (*model.AuctionItem, *model.AuctionRule, error)
	ListItemsByIDs(itemIDs []string) ([]model.ItemWithRule, error)
	UpdateItemWithRule(item *model.AuctionItem, rule *model.AuctionRule) error
	SetRoomCurrentItem(roomID, itemID string) error
	GetRoomCurrentItem(roomID string) (string, bool, error)
	ClearRoomCurrentItem(roomID, itemID string) error
	DeleteItem(itemID string) error
	ListItems(query dto.ListItemsInput) ([]model.ItemWithRule, int64, error)
	ListOngoingItemsPastEndTime(before time.Time, limit int) ([]model.ItemWithRule, error)
	AutoMigrateBidLog() error
	CreateBidLog(log *model.BidLog) error
	CreateBidLogs(logs []*model.BidLog) error
	ListBidRanking(itemID string, limit int) ([]dto.BidderPrice, error)
	GetUserRanking(itemID, userID string) (*dto.CurrentUserRanking, error)
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

func (s *GormStore) ListItemsByIDs(itemIDs []string) ([]model.ItemWithRule, error) {
	if len(itemIDs) == 0 {
		return []model.ItemWithRule{}, nil
	}
	var items []model.AuctionItem
	if err := s.db.Where("id IN ?", itemIDs).Find(&items).Error; err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return []model.ItemWithRule{}, nil
	}

	ruleIDs := make([]string, 0, len(items))
	for _, item := range items {
		ruleIDs = append(ruleIDs, item.RuleID)
	}
	var rules []model.AuctionRule
	if err := s.db.Where("id IN ?", ruleIDs).Find(&rules).Error; err != nil {
		return nil, err
	}
	ruleByID := make(map[string]*model.AuctionRule, len(rules))
	for i := range rules {
		ruleByID[rules[i].ID] = &rules[i]
	}
	itemByID := make(map[string]*model.AuctionItem, len(items))
	for i := range items {
		itemByID[items[i].ID] = &items[i]
	}

	result := make([]model.ItemWithRule, 0, len(items))
	for _, itemID := range itemIDs {
		item := itemByID[itemID]
		if item == nil {
			continue
		}
		rule := ruleByID[item.RuleID]
		if rule == nil {
			continue
		}
		result = append(result, model.ItemWithRule{Item: item, Rule: rule})
	}
	return result, nil
}

func (s *GormStore) UpdateItemWithRule(item *model.AuctionItem, rule *model.AuctionRule) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(item).Error; err != nil {
			return err
		}
		return tx.Save(rule).Error
	})
}

func (s *GormStore) SetRoomCurrentItem(roomID, itemID string) error {
	return s.db.Model(&roommodel.LiveRoom{}).
		Where("id = ?", roomID).
		Update("current_item_id", itemID).Error
}

func (s *GormStore) GetRoomCurrentItem(roomID string) (string, bool, error) {
	var room roommodel.LiveRoom
	if err := s.db.Select("current_item_id").First(&room, "id = ?", roomID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	if room.CurrentItemID == "" {
		return "", false, nil
	}
	return room.CurrentItemID, true, nil
}

func (s *GormStore) ClearRoomCurrentItem(roomID, itemID string) error {
	return s.db.Model(&roommodel.LiveRoom{}).
		Where("id = ? AND current_item_id = ?", roomID, itemID).
		Update("current_item_id", "").Error
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

func (s *GormStore) ListOngoingItemsPastEndTime(before time.Time, limit int) ([]model.ItemWithRule, error) {
	var items []model.AuctionItem
	if err := s.db.
		Where("status = ? AND deleted_at IS NULL", model.ItemOngoing).
		Find(&items).Error; err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	ruleIDs := make([]string, 0, len(items))
	for _, it := range items {
		ruleIDs = append(ruleIDs, it.RuleID)
	}
	var rules []model.AuctionRule
	if err := s.db.Where("id IN ? AND end_time < ?", ruleIDs, before).Find(&rules).Error; err != nil {
		return nil, err
	}
	ruleByID := make(map[string]*model.AuctionRule, len(rules))
	for i := range rules {
		ruleByID[rules[i].ID] = &rules[i]
	}
	result := make([]model.ItemWithRule, 0, len(rules))
	for i := range items {
		rule, ok := ruleByID[items[i].RuleID]
		if !ok {
			continue
		}
		result = append(result, model.ItemWithRule{Item: &items[i], Rule: rule})
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}
