package room

import (
	"context"
	"errors"
	"sync"

	"github.com/zet-plane/live-auction-backend/internal/app"
	itemapp "github.com/zet-plane/live-auction-backend/internal/app/item"
	"github.com/zet-plane/live-auction-backend/internal/app/room/cache"
	"github.com/zet-plane/live-auction-backend/internal/app/room/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/room/handler"
	"github.com/zet-plane/live-auction-backend/internal/app/room/router"
	"github.com/zet-plane/live-auction-backend/internal/app/room/service"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
)

var ErrEmptyDatabase = errors.New("database pointer is nil")

type Room struct {
	Name string
	app.UnimplementedModule
}

func (r *Room) Info() string { return r.Name }

func (r *Room) PreInit(engine *kernel.Engine) error {
	if engine.DB == nil {
		return ErrEmptyDatabase
	}
	store := dao.NewGormStore(engine.DB)
	return store.AutoMigrate()
}

func (r *Room) Load(engine *kernel.Engine) error {
	store := dao.NewGormStore(engine.DB)
	c := cache.NewRedisCache(engine.Cache)
	svc := service.NewService(store, c, itemapp.ItemReader)
	handler.Init(svc)
	router.RegisterRoutes(engine.Flame)
	return nil
}

func (r *Room) Stop(wg *sync.WaitGroup, _ context.Context) error {
	defer wg.Done()
	return nil
}
