package router

import (
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit/handler"
	userhandler "github.com/zet-plane/live-auction-backend/internal/app/user/handler"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
)

func RegisterRoutes(f *flamego.Flame) {
	auth := web.Authorization(userhandler.AuthenticateToken)
	f.Group("/api/v1", func() {
		f.Post("/items/{item_id}/deposit/pay", handler.PayDeposit)
		f.Get("/items/{item_id}/deposit", handler.GetMyDeposit)
	}, auth)
}
