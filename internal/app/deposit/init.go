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
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
)

type DepositChecker interface {
	HasPaidDeposit(ctx context.Context, itemID, userID string, requiredAmount int64) (bool, error)
}

var Svc DepositChecker

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
	handler.Init(svc)
	router.RegisterRoutes(engine.Flame)
	return nil
}

func (d *Deposit) Stop(wg *sync.WaitGroup, _ context.Context) error {
	defer wg.Done()
	return nil
}
