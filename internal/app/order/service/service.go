package service

import (
	"errors"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/order/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/order/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/order/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

type Service struct {
	store          dao.Store
	paymentTimeout time.Duration
	now            func() time.Time
}

func NewService(store dao.Store, paymentTimeout time.Duration) *Service {
	return &Service{
		store:          store,
		paymentTimeout: paymentTimeout,
		now:            time.Now,
	}
}

func (s *Service) CreateOrder(itemID, userID string, price int64) (*model.Order, error) {
	existing, err := s.store.FindOrderByItemID(itemID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, errorx.ErrNotFound) {
		return nil, err
	}
	order := &model.Order{
		ID:        "order_" + snowflake.MakeUUID(),
		ItemID:    itemID,
		UserID:    userID,
		Price:     price,
		Status:    model.OrderPending,
		ExpiredAt: s.now().Add(s.paymentTimeout),
	}
	if err := s.store.CreateOrder(order); err != nil {
		return nil, err
	}
	return order, nil
}

func (s *Service) Pay(current *usermodel.User, orderID string) error {
	order, err := s.store.FindOrder(orderID)
	if err != nil {
		return err
	}
	if order.UserID != current.ID {
		return errorx.ErrUnauthorized
	}
	// reject payment if order is past its expiry
	if s.now().After(order.ExpiredAt) {
		return errorx.ErrInvalidRequest
	}
	ok, err := s.store.UpdateOrderStatus(orderID, model.OrderPending, model.OrderPaid)
	if err != nil {
		return err
	}
	if !ok {
		refetched, err2 := s.store.FindOrder(orderID)
		if err2 != nil {
			return err2
		}
		if refetched.Status == model.OrderPaid {
			return nil
		}
		return errorx.ErrInvalidRequest
	}
	return nil
}

func (s *Service) Cancel(current *usermodel.User, orderID string) error {
	order, err := s.store.FindOrder(orderID)
	if err != nil {
		return err
	}
	if order.UserID != current.ID {
		return errorx.ErrUnauthorized
	}
	ok, err := s.store.UpdateOrderStatus(orderID, model.OrderPending, model.OrderCancelled)
	if err != nil {
		return err
	}
	if !ok {
		return errorx.ErrInvalidRequest
	}
	return nil
}

func (s *Service) ListOrders(current *usermodel.User, input dto.ListOrdersInput) (*dto.ListOrdersResult, error) {
	if input.Page <= 0 {
		input.Page = 1
	}
	if input.PageSize <= 0 || input.PageSize > 100 {
		input.PageSize = 20
	}
	if current.Identity == usermodel.IdentityMerchant {
		input.MerchantID = current.ID
	} else {
		input.UserID = current.ID
	}
	list, total, err := s.store.ListOrders(input)
	if err != nil {
		return nil, err
	}
	return &dto.ListOrdersResult{
		List:     list,
		Page:     input.Page,
		PageSize: input.PageSize,
		Total:    total,
	}, nil
}

func (s *Service) GetOrder(current *usermodel.User, orderID string) (*dto.OrderDetail, error) {
	detail, err := s.store.FindOrderDetail(orderID)
	if err != nil {
		return nil, err
	}
	if current.Identity == usermodel.IdentityMerchant {
		if detail.ItemMerchantID != current.ID {
			return nil, errorx.ErrUnauthorized
		}
	} else {
		if detail.UserID != current.ID {
			return nil, errorx.ErrUnauthorized
		}
	}
	return detail, nil
}
