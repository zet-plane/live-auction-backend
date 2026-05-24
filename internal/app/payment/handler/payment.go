package handler

import (
	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/order/dto"
	orderservice "github.com/zet-plane/live-auction-backend/internal/app/order/service"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

var orderSvc *orderservice.Service

func Init(s *orderservice.Service) {
	orderSvc = s
}

func Pay(r flamego.Render, c flamego.Context, current *usermodel.User, body dto.PayOrderRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	if orderSvc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := orderSvc.Pay(current, c.Param("order_id")); err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}

func Cancel(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if orderSvc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := orderSvc.Cancel(current, c.Param("order_id")); err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}
