package ws

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/zet-plane/live-auction-backend/internal/app"
	"github.com/zet-plane/live-auction-backend/internal/app/ws/bus"
	"github.com/zet-plane/live-auction-backend/internal/app/ws/handler"
	wshub "github.com/zet-plane/live-auction-backend/internal/app/ws/hub"
	"github.com/zet-plane/live-auction-backend/internal/app/ws/router"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

// Hub is the package-level singleton implementing wsevent.Broadcaster.
var Hub wsevent.Broadcaster

var localSnapshotTarget interface{ SetSnapshotProvider(wshub.SnapshotProvider) }

type WS struct {
	Name   string
	cancel context.CancelFunc
	app.UnimplementedModule
}

func (w *WS) Info() string { return w.Name }

func (w *WS) Load(engine *kernel.Engine) error {
	localHub := wshub.NewHub(engine.Cache)
	localSnapshotTarget = localHub
	Hub = localHub
	if engine.Cache != nil {
		Hub = bus.NewBroadcaster(bus.NewRedisPublisher(engine.Cache), bus.Options{PodID: podID()})
		subCtx, cancel := context.WithCancel(engine.Context)
		w.cancel = cancel
		go bus.NewSubscriber(localHub).Run(subCtx, engine.Cache)
	}
	handler.Init(localHub)
	handler.ConfigureOriginChecker(web.NewOriginPolicy(engine.Config.Mode, engine.Config.Security.AllowedOrigins))
	handler.InitTicket(engine.Cache)
	router.RegisterRoutes(engine.Flame)
	return nil
}

func SetSnapshotProvider(provider wshub.SnapshotProvider) {
	if localSnapshotTarget != nil {
		localSnapshotTarget.SetSnapshotProvider(provider)
	}
}

func (w *WS) Stop(wg *sync.WaitGroup, _ context.Context) error {
	defer wg.Done()
	if w.cancel != nil {
		w.cancel()
	}
	return nil
}

func podID() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "backend-unknown"
	}
	return "backend-" + strings.TrimSpace(name)
}
