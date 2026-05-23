package user

import (
	"context"
	"errors"
	"sync"

	"github.com/zet-plane/live-auction-backend/internal/app"
	"github.com/zet-plane/live-auction-backend/internal/app/user/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/user/handler"
	"github.com/zet-plane/live-auction-backend/internal/app/user/router"
	"github.com/zet-plane/live-auction-backend/internal/app/user/service"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
)

var ErrEmptyDatabase = errors.New("database pointer is nil")

type User struct {
	Name string

	app.UnimplementedModule
}

func (u *User) Info() string {
	return u.Name
}

func (u *User) PreInit(engine *kernel.Engine) error {
	if engine.DB == nil {
		return ErrEmptyDatabase
	}
	store := dao.NewGormStore(engine.DB)
	return store.AutoMigrate()
}

func (u *User) Load(engine *kernel.Engine) error {
	store := dao.NewGormStore(engine.DB)

	opts := service.Options{
		TokenSecret: engine.Config.Auth.TokenSecret,
		TokenTTL:    engine.Config.AuthTokenTTL(),
	}

	svc := service.NewService(store, opts)
	handler.Init(svc)
	router.RegisterRoutes(engine.Flame)
	return nil
}

func (u *User) Stop(wg *sync.WaitGroup, _ context.Context) error {
	defer wg.Done()
	return nil
}
