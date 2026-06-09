package dao

import (
	"database/sql"
	"errors"

	"github.com/go-sql-driver/mysql"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/item/model"
	"gorm.io/gorm/clause"
)

func (s *GormStore) AutoMigrateBidLog() error {
	if err := s.db.AutoMigrate(&model.BidLog{}); err != nil {
		return err
	}
	if err := s.backfillLegacyBidLogVersions(); err != nil {
		return err
	}
	if err := s.db.Exec("CREATE UNIQUE INDEX idx_bid_logs_item_epoch_version_unique ON bid_logs (item_id, authority_epoch, auction_version)").Error; err != nil && !isDuplicateIndexError(err) {
		return err
	}
	return nil
}

func (s *GormStore) CreateBidLog(log *model.BidLog) error {
	return s.db.Create(log).Error
}

func isDuplicateIndexError(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1061
}

func (s *GormStore) backfillLegacyBidLogVersions() error {
	return s.db.Exec(`
WITH ranked AS (
	SELECT
		id,
		ROW_NUMBER() OVER (PARTITION BY item_id ORDER BY created_at ASC, id ASC) AS seq
	FROM bid_logs
	WHERE authority_epoch = 0 AND auction_version = 0
)
UPDATE bid_logs b
JOIN ranked r ON r.id = b.id
SET b.auction_version = r.seq
WHERE b.authority_epoch = 0 AND b.auction_version = 0
`).Error
}

func (s *GormStore) CreateBidLogs(logs []*model.BidLog) error {
	if len(logs) == 0 {
		return nil
	}
	return s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&logs).Error
}

func (s *GormStore) ListBidLogsForItemEpoch(itemID string, authorityEpoch int64) ([]*model.BidLog, error) {
	var logs []*model.BidLog
	err := s.db.Where("item_id = ? AND authority_epoch = ?", itemID, authorityEpoch).
		Order("auction_version ASC").
		Find(&logs).Error
	return logs, err
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

func (s *GormStore) GetUserRanking(itemID, userID string) (*dto.CurrentUserRanking, error) {
	var best sql.NullInt64
	if err := s.db.Model(&model.BidLog{}).
		Select("MAX(price)").
		Where("item_id = ? AND user_id = ?", itemID, userID).
		Scan(&best).Error; err != nil {
		return nil, err
	}
	if !best.Valid {
		return nil, nil
	}

	var higher int64
	subquery := s.db.Model(&model.BidLog{}).
		Select("user_id, MAX(price) as price").
		Where("item_id = ?", itemID).
		Group("user_id")
	if err := s.db.Table("(?) as bidder_best", subquery).
		Where("price > ?", best.Int64).
		Count(&higher).Error; err != nil {
		return nil, err
	}

	rank := int(higher) + 1
	return &dto.CurrentUserRanking{
		UserID:   userID,
		Rank:     rank,
		Price:    best.Int64,
		IsLeader: rank == 1,
		HasBid:   true,
	}, nil
}
