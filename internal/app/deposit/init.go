package deposit

import (
	"context"
	"errors"
	"sync"

	"github.com/zet-plane/live-auction-backend/internal/app"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit/handler"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit/router"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit/service"
	orderapp "github.com/zet-plane/live-auction-backend/internal/app/order"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
)

type Service interface {
	HasPaidDeposit(ctx context.Context, itemID, userID string, requiredAmount int64) (bool, error)
	RefundNonWinners(ctx context.Context, itemID, winnerUserID string) (service.SettlementSummary, error)
	RefundWinner(ctx context.Context, itemID, userID string) (service.SettlementSummary, error)
	ForfeitWinner(ctx context.Context, itemID, userID string) (service.SettlementSummary, error)
}

var Svc Service

var errNilDB = errors.New("database pointer is nil")

type Deposit struct {
	Name string
	app.UnimplementedModule
}

func (d *Deposit) Info() string { return d.Name }

func (d *Deposit) PreInit(engine *kernel.Engine) error {
	if engine.DB == nil {
		return errNilDB
	}
	return dao.NewGormStore(engine.DB).AutoMigrate()
}

func (d *Deposit) Load(engine *kernel.Engine) error {
	store := dao.NewGormStore(engine.DB)
	svc := service.NewService(store)
	Svc = svc
	if orderapp.Svc != nil {
		orderapp.Svc.SetDepositSettler(svc)
	}
	handler.Init(svc)
	router.RegisterRoutes(engine.Flame)
	return nil
}

func (d *Deposit) Stop(wg *sync.WaitGroup, _ context.Context) error {
	defer wg.Done()
	return nil
}
