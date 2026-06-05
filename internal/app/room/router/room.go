package router

import (
	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/room/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/room/handler"
	userhandler "github.com/zet-plane/live-auction-backend/internal/app/user/handler"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
)

func RegisterRoutes(f *flamego.Flame) {
	auth := web.Authorization(userhandler.AuthenticateToken)

	f.Get("/api/v1/rooms", handler.ListRooms)
	f.Get("/api/v1/rooms/feed", handler.ListRoomFeed)
	f.Get("/api/v1/rooms/{room_id}", handler.GetRoom)
	f.Group("/api/v1", func() {
		f.Post("/merchant/room", binding.JSON(dto.CreateRoomRequest{}), handler.ActivateRoom)
		f.Get("/merchant/room", handler.GetMerchantRoom)
		f.Post("/rooms/{room_id}/start", handler.StartRoom)
		f.Post("/rooms/{room_id}/end", handler.EndRoom)
	}, auth)
}
