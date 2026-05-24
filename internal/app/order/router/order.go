package router

import (
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/order/handler"
	userhandler "github.com/zet-plane/live-auction-backend/internal/app/user/handler"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
)

func RegisterRoutes(f *flamego.Flame) {
	auth := web.Authorization(userhandler.AuthenticateToken)
	f.Group("/api/v1", func() {
		f.Get("/orders", handler.ListOrders)
		f.Get("/orders/{order_id}", handler.GetOrder)
	}, auth)
}
