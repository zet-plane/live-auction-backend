package service

import (
	"errors"
	"strings"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/deposit/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

type Service struct {
	store dao.Store
	now   func() time.Time
}

func NewService(store dao.Store) *Service {
	return &Service{store: store, now: time.Now}
}

func (s *Service) PayDeposit(current *usermodel.User, itemID string) (*dto.DepositDetail, error) {
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
	return dto.NewDepositDetail(deposit), nil
}

func (s *Service) GetMyDeposit(current *usermodel.User, itemID string) (*dto.DepositDetail, error) {
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
	return dto.NewDepositDetail(deposit), nil
}

func (s *Service) HasPaidDeposit(itemID, userID string, requiredAmount int64) (bool, error) {
	if requiredAmount <= 0 {
		return true, nil
	}
	itemID = strings.TrimSpace(itemID)
	userID = strings.TrimSpace(userID)
	if itemID == "" || userID == "" {
		return false, nil
	}
	deposit, err := s.store.FindDeposit(itemID, userID)
	if errors.Is(err, errorx.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return deposit.Status == model.DepositPaid && deposit.Amount >= requiredAmount, nil
}
