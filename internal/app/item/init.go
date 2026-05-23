package item

import (
	"context"
	"errors"
	"sync"

	"github.com/zet-plane/live-auction-backend/internal/app"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/item/handler"
	"github.com/zet-plane/live-auction-backend/internal/app/item/router"
	"github.com/zet-plane/live-auction-backend/internal/app/item/service"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
)

var ErrEmptyDatabase = errors.New("database pointer is nil")

type Item struct {
	Name string

	app.UnimplementedModule
}

func (i *Item) Info() string {
	return i.Name
}

func (i *Item) PreInit(engine *kernel.Engine) error {
	if engine.DB == nil {
		return ErrEmptyDatabase
	}
	store := dao.NewGormStore(engine.DB)
	return store.AutoMigrate()
}

func (i *Item) Load(engine *kernel.Engine) error {
	store := dao.NewGormStore(engine.DB)
	policy := dto.DefaultAuctionPolicy()
	if engine.Config.Auction.ExtendTriggerSec > 0 {
		policy.ExtendTriggerSec = engine.Config.Auction.ExtendTriggerSec
	}
	if engine.Config.Auction.AutoExtendSec > 0 {
		policy.AutoExtendSec = engine.Config.Auction.AutoExtendSec
	}
	if engine.Config.Auction.MaxExtendCount > 0 {
		policy.MaxExtendCount = engine.Config.Auction.MaxExtendCount
	}
	if engine.Config.Auction.MaxTotalExtendSec > 0 {
		policy.MaxTotalExtendSec = engine.Config.Auction.MaxTotalExtendSec
	}

	svc := service.NewService(store, policy, nil)
	handler.Init(svc)
	router.RegisterRoutes(engine.Flame)
	return nil
}

func (i *Item) Stop(wg *sync.WaitGroup, _ context.Context) error {
	defer wg.Done()
	return nil
}
