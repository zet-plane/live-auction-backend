package service

import (
	"context"
	"errors"
	"time"

	depositservice "github.com/zet-plane/live-auction-backend/internal/app/deposit/service"
	"github.com/zet-plane/live-auction-backend/internal/app/order/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/order/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/order/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

type DepositSettler interface {
	RefundWinner(ctx context.Context, itemID, userID string) (depositservice.SettlementSummary, error)
	ForfeitWinner(ctx context.Context, itemID, userID string) (depositservice.SettlementSummary, error)
}

type Service struct {
	store          dao.Store
	paymentTimeout time.Duration
	now            func() time.Time
	depositSettler DepositSettler
}

func NewService(store dao.Store, paymentTimeout time.Duration) *Service {
	return &Service{
		store:          store,
		paymentTimeout: paymentTimeout,
		now:            time.Now,
	}
}

func (s *Service) SetDepositSettler(settler DepositSettler) {
	s.depositSettler = settler
}

func (s *Service) CreateOrder(ctx context.Context, itemID, userID string, price int64) (result *model.Order, err error) {
	var orderID string
	orderResult := "success"
	finish := observability.Track(ctx, "order.create_from_auction", "item_id", itemID, "user_id", userID, "price", price)
	defer func() {
		if err != nil {
			orderResult = "error"
		}
		finish(&err, "order_id", orderID, "result", orderResult)
	}()

	existing, err := s.store.FindOrderByItemID(itemID)
	if err == nil {
		orderID = existing.ID
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
	orderID = order.ID
	return order, nil
}

func (s *Service) Pay(ctx context.Context, current *usermodel.User, orderID string) (err error) {
	defer observability.Track(ctx, "order.pay", "user_id", currentID(current), "order_id", orderID)(&err)

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
	if s.depositSettler != nil {
		if _, settleErr := s.depositSettler.RefundWinner(ctx, order.ItemID, order.UserID); settleErr != nil {
			logx.Warnw("order.Pay refund winner deposit failed", "order_id", order.ID, "item_id", order.ItemID, "user_id", order.UserID, "err", settleErr)
		}
	}
	return nil
}

func (s *Service) Cancel(ctx context.Context, current *usermodel.User, orderID string) (err error) {
	defer observability.Track(ctx, "order.cancel", "user_id", currentID(current), "order_id", orderID)(&err)

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
	if s.depositSettler != nil {
		if _, settleErr := s.depositSettler.ForfeitWinner(ctx, order.ItemID, order.UserID); settleErr != nil {
			logx.Warnw("order.Cancel forfeit winner deposit failed", "order_id", order.ID, "item_id", order.ItemID, "user_id", order.UserID, "err", settleErr)
		}
	}
	return nil
}

func (s *Service) ListOrders(ctx context.Context, current *usermodel.User, input dto.ListOrdersInput) (result *dto.ListOrdersResult, err error) {
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
	defer observability.Track(ctx, "order.list",
		"user_id", currentID(current),
		"identity", current.Identity,
		"status", input.Status,
		"page", input.Page,
		"page_size", input.PageSize,
	)(&err)

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

func (s *Service) GetOrder(ctx context.Context, current *usermodel.User, orderID string) (result *dto.OrderDetail, err error) {
	defer observability.Track(ctx, "order.get", "user_id", currentID(current), "order_id", orderID)(&err)

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

func currentID(current *usermodel.User) string {
	if current == nil {
		return ""
	}
	return current.ID
}
