package payment

import (
	"context"
	"sync"

	"github.com/zet-plane/live-auction-backend/internal/app"
	orderapp "github.com/zet-plane/live-auction-backend/internal/app/order"
	"github.com/zet-plane/live-auction-backend/internal/app/payment/handler"
	"github.com/zet-plane/live-auction-backend/internal/app/payment/router"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
)

type Payment struct {
	Name string
	app.UnimplementedModule
}

func (p *Payment) Info() string { return p.Name }

func (p *Payment) Load(engine *kernel.Engine) error {
	handler.Init(orderapp.Svc)
	router.RegisterRoutes(engine.Flame)
	return nil
}

func (p *Payment) Stop(wg *sync.WaitGroup, _ context.Context) error {
	defer wg.Done()
	return nil
}
