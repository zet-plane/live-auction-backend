package handler

import (
	"context"
	"net/http"

	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	orderdto "github.com/zet-plane/live-auction-backend/internal/app/order/dto"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
)

type OrderOperator interface {
	Pay(ctx context.Context, current *usermodel.User, orderID string) error
	Cancel(ctx context.Context, current *usermodel.User, orderID string) error
}

var orderSvc OrderOperator

func Init(s OrderOperator) {
	orderSvc = s
}

// Pay marks an order as paid.
//
// @Summary 支付订单
// @Tags payments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param order_id path string true "订单 ID"
// @Param body body orderdto.PayOrderRequest true "支付结果请求"
// @Success 200 {object} response.Body
// @Failure 400 {object} response.Body
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/orders/{order_id}/pay [post]
func Pay(r flamego.Render, req *http.Request, c flamego.Context, current *usermodel.User, body orderdto.PayOrderRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	if body.Result != "success" {
		logx.Warnw("Pay rejected: bad result field", "user_id", current.ID, "order_id", c.Param("order_id"), "result", body.Result)
		response.Error(r, errorx.ErrInvalidRequest)
		return
	}
	if orderSvc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := orderSvc.Pay(req.Context(), current, c.Param("order_id")); err != nil {
		logx.Warnw("Pay failed", "user_id", current.ID, "order_id", c.Param("order_id"), "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}

// Cancel cancels an order.
//
// @Summary 取消订单
// @Tags payments
// @Produce json
// @Security BearerAuth
// @Param order_id path string true "订单 ID"
// @Success 200 {object} response.Body
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/orders/{order_id}/cancel [post]
func Cancel(r flamego.Render, req *http.Request, c flamego.Context, current *usermodel.User) {
	if orderSvc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := orderSvc.Cancel(req.Context(), current, c.Param("order_id")); err != nil {
		logx.Warnw("Cancel failed", "user_id", current.ID, "order_id", c.Param("order_id"), "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}
