package router

import (
	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/order/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/payment/handler"
	userhandler "github.com/zet-plane/live-auction-backend/internal/app/user/handler"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
)

func RegisterRoutes(f *flamego.Flame) {
	auth := web.Authorization(userhandler.AuthenticateToken)
	f.Group("/api/v1", func() {
		f.Post("/orders/{order_id}/pay", binding.JSON(dto.PayOrderRequest{}), handler.Pay)
		f.Post("/orders/{order_id}/cancel", handler.Cancel)
	}, auth)
}
