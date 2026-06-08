package router

import (
	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/base/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/base/handler"
	userhandler "github.com/zet-plane/live-auction-backend/internal/app/user/handler"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
)

func RegisterRoutes(f *flamego.Flame) {
	auth := web.Authorization(userhandler.AuthenticateToken)

	f.Get("/livez", handler.Livez)
	f.Get("/readyz", handler.Readyz)
	f.Get("/health", handler.Health)
	f.Post("/api/v1/base/uploads/images/sign", auth, binding.JSON(dto.SignImageUploadRequest{}), handler.SignImageUpload)
}
