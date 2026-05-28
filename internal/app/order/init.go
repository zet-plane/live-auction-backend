package order

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app"
	"github.com/zet-plane/live-auction-backend/internal/app/order/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/order/handler"
	"github.com/zet-plane/live-auction-backend/internal/app/order/router"
	"github.com/zet-plane/live-auction-backend/internal/app/order/service"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
)

// Svc is the package-level service instance exported for use by item and payment modules.
var Svc *service.Service

var errNilDB = errors.New("database pointer is nil")

type Order struct {
	Name string
	app.UnimplementedModule
}

func (o *Order) Info() string { return o.Name }

func (o *Order) PreInit(engine *kernel.Engine) error {
	if engine.DB == nil {
		return errNilDB
	}
	return dao.NewGormStore(engine.DB).AutoMigrate()
}

func (o *Order) Load(engine *kernel.Engine) error {
	store := dao.NewGormStore(engine.DB)
	Svc = service.NewService(store, 30*time.Minute)
	handler.Init(Svc)
	router.RegisterRoutes(engine.Flame)

	engine.Cron.AddFunc("@every 5m", func() { Svc.ScanExpiredOrders(context.Background()) })
	engine.Cron.AddFunc("@every 10m", func() { Svc.ScanCompensation(context.Background()) })
	return nil
}

func (o *Order) Stop(wg *sync.WaitGroup, _ context.Context) error {
	defer wg.Done()
	return nil
}
