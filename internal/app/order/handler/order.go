package handler

import (
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/order/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/order/model"
	"github.com/zet-plane/live-auction-backend/internal/app/order/service"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
)

var svc *service.Service

func Init(s *service.Service) {
	svc = s
}

// ListOrders lists orders for the current user or merchant.
//
// @Summary 订单列表
// @Tags orders
// @Produce json
// @Security BearerAuth
// @Param status query string false "订单状态"
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Success 200 {object} response.Body{data=dto.ListOrdersResult}
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/orders [get]
func ListOrders(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	input := dto.ListOrdersInput{
		Status:   model.OrderStatus(c.Query("status")),
		Page:     c.QueryInt("page"),
		PageSize: c.QueryInt("page_size"),
	}
	result, err := svc.ListOrders(current, input)
	if err != nil {
		logx.Warnw("ListOrders failed", "user_id", current.ID, "status", input.Status, "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

// GetOrder returns an order detail.
//
// @Summary 订单详情
// @Tags orders
// @Produce json
// @Security BearerAuth
// @Param order_id path string true "订单 ID"
// @Success 200 {object} response.Body{data=dto.OrderDetail}
// @Failure 401 {object} response.Body
// @Failure 404 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/orders/{order_id} [get]
func GetOrder(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	detail, err := svc.GetOrder(current, c.Param("order_id"))
	if err != nil {
		logx.Warnw("GetOrder failed", "user_id", current.ID, "order_id", c.Param("order_id"), "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, detail)
}
