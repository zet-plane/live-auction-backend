package ws

import (
	"context"
	"sync"

	"github.com/zet-plane/live-auction-backend/internal/app"
	"github.com/zet-plane/live-auction-backend/internal/app/ws/handler"
	wshub "github.com/zet-plane/live-auction-backend/internal/app/ws/hub"
	"github.com/zet-plane/live-auction-backend/internal/app/ws/router"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

// Hub is the package-level singleton implementing wsevent.Broadcaster.
var Hub wsevent.Broadcaster

type WS struct {
	Name string
	app.UnimplementedModule
}

func (w *WS) Info() string { return w.Name }

func (w *WS) Load(engine *kernel.Engine) error {
	h := wshub.NewHub(engine.Cache)
	Hub = h
	handler.Init(h)
	handler.InitTicket(engine.Cache)
	router.RegisterRoutes(engine.Flame)
	return nil
}

func (w *WS) Stop(wg *sync.WaitGroup, _ context.Context) error {
	defer wg.Done()
	return nil
}
