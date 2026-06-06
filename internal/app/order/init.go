package order

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app"
	"github.com/zet-plane/live-auction-backend/internal/app/order/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/order/handler"
	"github.com/zet-plane/live-auction-backend/internal/app/order/model"
	"github.com/zet-plane/live-auction-backend/internal/app/order/router"
	"github.com/zet-plane/live-auction-backend/internal/app/order/service"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/core/cronlease"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
	"github.com/zet-plane/live-auction-backend/internal/core/observability"
)

// Service is the package-level contract exported for cross-module calls.
type Service interface {
	CreateOrder(ctx context.Context, itemID, userID string, price int64) (*model.Order, error)
	Pay(ctx context.Context, current *usermodel.User, orderID string) error
	Cancel(ctx context.Context, current *usermodel.User, orderID string) error
	SetDepositSettler(settler service.DepositSettler)
}

var Svc Service

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
	svc := service.NewService(store, 30*time.Minute)
	Svc = svc
	handler.Init(svc)
	router.RegisterRoutes(engine.Flame)

	storeLease := cronlease.RedisStore{Client: engine.Cache}
	owner := leaseOwner(os.Hostname)
	engine.Cron.AddFunc("@every 5m", observability.WrapCron("order.scan_expired_orders",
		cronlease.Wrap("order.scan_expired_orders", owner, 2*time.Minute, storeLease, svc.ScanExpiredOrders)))
	engine.Cron.AddFunc("@every 10m", observability.WrapCron("order.scan_compensation",
		cronlease.Wrap("order.scan_compensation", owner, 2*time.Minute, storeLease, svc.ScanCompensation)))
	return nil
}

func leaseOwner(hostname func() (string, error)) string {
	name, err := hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "backend-unknown"
	}
	return "backend-" + strings.TrimSpace(name)
}

func (o *Order) Stop(wg *sync.WaitGroup, _ context.Context) error {
	defer wg.Done()
	return nil
}
