package base

import (
	"github.com/zet-plane/live-auction-backend/internal/app"
	"github.com/zet-plane/live-auction-backend/internal/app/base/handler"
	"github.com/zet-plane/live-auction-backend/internal/app/base/router"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
)

type Base struct {
	Name string
	app.UnimplementedModule
}

func (b *Base) Info() string { return b.Name }

func (b *Base) Load(engine *kernel.Engine) error {
	handler.Init(engine.DB, engine.Cache)
	router.RegisterRoutes(engine.Flame)
	return nil
}
