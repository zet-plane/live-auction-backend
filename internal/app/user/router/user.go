package router

import (
	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/user/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/user/handler"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
)

func RegisterRoutes(f *flamego.Flame) {
	auth := web.Authorization(handler.AuthenticateToken)

	f.Post("/api/v1/auth/register", binding.JSON(dto.RegisterRequest{}), handler.Register)
	f.Post("/api/v1/auth/login", binding.JSON(dto.LoginRequest{}), handler.Login)
	f.Group("/api/v1/users/me", func() {
		f.Get("", handler.Me)
		f.Put("", binding.JSON(dto.UpdateProfileRequest{}), handler.UpdateMe)
		f.Delete("", handler.DeleteMe)
	}, auth)
}
