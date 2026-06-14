package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/deposit/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/core/availability"
	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

type Service struct {
	store        dao.Store
	cache        PaidDepositCache
	availability AvailabilitySnapshotProvider
	now          func() time.Time
}

type PaidDepositCache interface {
	MarkPaidDeposit(ctx context.Context, itemID, userID string, amount int64) error
	HasPaidDeposit(ctx context.Context, itemID, userID string, requiredAmount int64) (bool, error)
}

type AvailabilitySnapshotProvider interface {
	Snapshot() availability.Snapshot
}

type SettlementSummary struct {
	Refunded  int
	Forfeited int
	Skipped   int
}

func NewService(store dao.Store, caches ...PaidDepositCache) *Service {
	var cache PaidDepositCache
	if len(caches) > 0 {
		cache = caches[0]
	}
	return &Service{store: store, cache: cache, now: time.Now}
}

func (s *Service) SetAvailability(provider AvailabilitySnapshotProvider) {
	s.availability = provider
}

func (s *Service) PayDeposit(ctx context.Context, current *usermodel.User, itemID string) (result *dto.DepositDetail, err error) {
	defer observability.Track(ctx, "deposit.pay", "user_id", userID(current), "item_id", strings.TrimSpace(itemID))(&err)

	if current == nil || strings.TrimSpace(current.ID) == "" {
		return nil, errorx.ErrUnauthorized
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return nil, errorx.ErrInvalidRequest
	}
	amount, err := s.store.FindRequiredAmount(itemID)
	if err != nil {
		return nil, err
	}
	if amount <= 0 {
		return nil, errorx.ErrInvalidRequest
	}

	existing, err := s.store.FindDeposit(itemID, current.ID)
	if err == nil {
		if existing.Status == model.DepositPaid && existing.Amount >= amount {
			s.cachePaidDeposit(ctx, existing.ItemID, existing.UserID, existing.Amount)
			return dto.NewDepositDetail(existing), nil
		}
		if existing.Status == model.DepositRefunded || existing.Status == model.DepositForfeited {
			return nil, errorx.ErrInvalidRequest
		}
		now := s.now()
		existing.Amount = amount
		existing.Status = model.DepositPaid
		existing.PaidAt = &now
		if err := s.store.UpdateDeposit(existing); err != nil {
			return nil, err
		}
		s.cachePaidDeposit(ctx, existing.ItemID, existing.UserID, existing.Amount)
		return dto.NewDepositDetail(existing), nil
	}
	if !errors.Is(err, errorx.ErrNotFound) {
		return nil, err
	}

	now := s.now()
	deposit := &model.Deposit{
		ID:     "deposit_" + snowflake.MakeUUID(),
		ItemID: itemID,
		UserID: current.ID,
		Amount: amount,
		Status: model.DepositPaid,
		PaidAt: &now,
	}
	if err := s.store.CreateDeposit(deposit); err != nil {
		return nil, err
	}
	s.cachePaidDeposit(ctx, deposit.ItemID, deposit.UserID, deposit.Amount)
	return dto.NewDepositDetail(deposit), nil
}

func (s *Service) GetMyDeposit(ctx context.Context, current *usermodel.User, itemID string) (result *dto.DepositDetail, err error) {
	defer observability.Track(ctx, "deposit.get_my", "user_id", userID(current), "item_id", strings.TrimSpace(itemID))(&err)

	if current == nil || strings.TrimSpace(current.ID) == "" {
		return nil, errorx.ErrUnauthorized
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return nil, errorx.ErrInvalidRequest
	}
	deposit, err := s.store.FindDeposit(itemID, current.ID)
	if err != nil {
		return nil, err
	}
	if deposit.Status == model.DepositPaid {
		s.cachePaidDeposit(ctx, deposit.ItemID, deposit.UserID, deposit.Amount)
	}
	return dto.NewDepositDetail(deposit), nil
}

func (s *Service) HasPaidDeposit(ctx context.Context, itemID, userID string, requiredAmount int64) (ok bool, err error) {
	defer observability.Track(ctx, "deposit.check",
		"item_id", strings.TrimSpace(itemID),
		"user_id", strings.TrimSpace(userID),
		"required_amount", requiredAmount,
	)(&err)

	if requiredAmount <= 0 {
		return true, nil
	}
	itemID = strings.TrimSpace(itemID)
	userID = strings.TrimSpace(userID)
	if itemID == "" || userID == "" {
		return false, nil
	}
	if s.mysqlUnavailableForDepositChecks() {
		if ok, cacheErr := s.hasCachedPaidDeposit(ctx, itemID, userID, requiredAmount); cacheErr == nil {
			return ok, nil
		} else {
			logx.Warnw("deposit cache check failed", "item_id", itemID, "user_id", userID, "err", cacheErr)
			return false, nil
		}
	}
	deposit, err := s.store.FindDeposit(itemID, userID)
	if errors.Is(err, errorx.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		if ok, cacheErr := s.hasCachedPaidDeposit(ctx, itemID, userID, requiredAmount); cacheErr == nil && ok {
			return true, nil
		} else if cacheErr != nil {
			logx.Warnw("deposit cache check failed", "item_id", itemID, "user_id", userID, "err", cacheErr)
		}
		return false, err
	}
	paid := deposit.Status == model.DepositPaid && deposit.Amount >= requiredAmount
	if paid {
		s.cachePaidDeposit(ctx, deposit.ItemID, deposit.UserID, deposit.Amount)
	}
	return paid, nil
}

func (s *Service) cachePaidDeposit(ctx context.Context, itemID, userID string, amount int64) {
	if s.cache == nil {
		return
	}
	if err := s.cache.MarkPaidDeposit(ctx, itemID, userID, amount); err != nil {
		logx.Warnw("deposit cache mark failed", "item_id", itemID, "user_id", userID, "err", err)
	}
}

func (s *Service) hasCachedPaidDeposit(ctx context.Context, itemID, userID string, requiredAmount int64) (bool, error) {
	if s.cache == nil {
		return false, nil
	}
	return s.cache.HasPaidDeposit(ctx, itemID, userID, requiredAmount)
}

func (s *Service) mysqlUnavailableForDepositChecks() bool {
	if s.availability == nil {
		return false
	}
	snapshot := s.availability.Snapshot()
	return snapshot.MySQLState == availability.MySQLBuffering ||
		snapshot.MySQLState == availability.MySQLDown ||
		(!snapshot.MySQL.Healthy && snapshot.MySQL.Error != "")
}

func (s *Service) RefundNonWinners(ctx context.Context, itemID, winnerUserID string) (summary SettlementSummary, err error) {
	itemID = strings.TrimSpace(itemID)
	winnerUserID = strings.TrimSpace(winnerUserID)
	finish := observability.Track(ctx, "deposit.refund_non_winners", "item_id", itemID, "winner_user_id", winnerUserID)
	defer func() {
		finish(&err, "refunded", summary.Refunded, "skipped", summary.Skipped)
	}()
	if itemID == "" {
		return summary, errorx.ErrInvalidRequest
	}
	deposits, err := s.store.ListPaidDepositsByItem(itemID)
	if err != nil {
		return summary, err
	}
	now := s.now()
	for _, deposit := range deposits {
		if deposit.UserID == winnerUserID {
			summary.Skipped++
			continue
		}
		ok, err := s.store.TransitionDepositStatus(itemID, deposit.UserID, model.DepositPaid, model.DepositRefunded, &now)
		if err != nil {
			return summary, err
		}
		if ok {
			summary.Refunded++
		} else {
			summary.Skipped++
		}
	}
	return summary, nil
}

func (s *Service) RefundWinner(ctx context.Context, itemID, userID string) (SettlementSummary, error) {
	return s.settleWinnerDeposit(ctx, "deposit.refund_winner", itemID, userID, model.DepositRefunded)
}

func (s *Service) ForfeitWinner(ctx context.Context, itemID, userID string) (SettlementSummary, error) {
	return s.settleWinnerDeposit(ctx, "deposit.forfeit_winner", itemID, userID, model.DepositForfeited)
}

func (s *Service) settleWinnerDeposit(ctx context.Context, operation, itemID, userID string, target model.DepositStatus) (summary SettlementSummary, err error) {
	itemID = strings.TrimSpace(itemID)
	userID = strings.TrimSpace(userID)
	finish := observability.Track(ctx, operation, "item_id", itemID, "user_id", userID, "target_status", string(target))
	defer func() {
		finish(&err, "refunded", summary.Refunded, "forfeited", summary.Forfeited, "skipped", summary.Skipped)
	}()
	if itemID == "" || userID == "" {
		return summary, errorx.ErrInvalidRequest
	}
	now := s.now()
	ok, err := s.store.TransitionDepositStatus(itemID, userID, model.DepositPaid, target, &now)
	if err != nil {
		return summary, err
	}
	if !ok {
		summary.Skipped = 1
		return summary, nil
	}
	if target == model.DepositRefunded {
		summary.Refunded = 1
	} else if target == model.DepositForfeited {
		summary.Forfeited = 1
	}
	return summary, nil
}

func userID(current *usermodel.User) string {
	if current == nil {
		return ""
	}
	return current.ID
}
