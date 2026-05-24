package dao

import (
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/item/model"
)

func (s *GormStore) AutoMigrateBidLog() error {
	return s.db.AutoMigrate(&model.BidLog{})
}

func (s *GormStore) CreateBidLog(log *model.BidLog) error {
	return s.db.Create(log).Error
}

func (s *GormStore) ListBidRanking(itemID string, limit int) ([]dto.BidderPrice, error) {
	var rows []struct {
		UserID   string
		Price    int64
		UserName string
	}
	err := s.db.Table("bid_logs b").
		Select("b.user_id, MAX(b.price) as price, u.name as user_name").
		Joins("LEFT JOIN users u ON b.user_id = u.id").
		Where("b.item_id = ?", itemID).
		Group("b.user_id, u.name").
		Order("price DESC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	entries := make([]dto.BidderPrice, len(rows))
	for i, r := range rows {
		entries[i] = dto.BidderPrice{
			UserID:   r.UserID,
			UserName: r.UserName,
			Price:    r.Price,
		}
	}
	return entries, nil
}
