package router

import (
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/base/handler"
)

func RegisterRoutes(f *flamego.Flame) {
	f.Get("/health", handler.Health)
}
