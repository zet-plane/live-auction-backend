package router

import (
	"github.com/flamego/flamego"
	userhandler "github.com/zet-plane/live-auction-backend/internal/app/user/handler"
	"github.com/zet-plane/live-auction-backend/internal/app/ws/handler"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
)

func RegisterRoutes(f *flamego.Flame) {
	auth := web.Authorization(userhandler.AuthenticateToken)

	f.Group("/api/v1", func() {
		f.Post("/ws-ticket", auth, handler.IssueTicket)
	})
	f.Get("/ws/v1/rooms/{room_id}", handler.ServeWS)
}
