package router

import (
	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/item/handler"
	userhandler "github.com/zet-plane/live-auction-backend/internal/app/user/handler"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
)

func RegisterRoutes(f *flamego.Flame) {
	auth := web.Authorization(userhandler.AuthenticateToken)

	f.Get("/api/v1/items", handler.ListItems)
	f.Get("/api/v1/items/{item_id}", handler.GetItem)
	f.Group("/api/v1", func() {
		f.Post("/items", binding.JSON(dto.CreateItemRequest{}), handler.CreateItem)
		f.Get("/merchant/items", handler.ListMerchantItems)
		f.Put("/items/{item_id}", binding.JSON(dto.CreateItemRequest{}), handler.UpdateItem)
		f.Delete("/items/{item_id}", handler.DeleteItem)
		f.Post("/items/{item_id}/publish", handler.PublishItem)
		f.Post("/items/{item_id}/start", handler.StartItem)
		f.Post("/items/{item_id}/cancel", handler.CancelItem)
	}, auth)
}
