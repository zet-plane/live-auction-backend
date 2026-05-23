package app

import (
	"context"
	"sync"

	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
)

type Module interface {
	Info() string
	PreInit(*kernel.Engine) error
	Load(*kernel.Engine) error
	Stop(wg *sync.WaitGroup, ctx context.Context) error

	mustEmbedUnimplementedModule()
}

type UnimplementedModule struct{}

func (*UnimplementedModule) Info() string                        { return "unimplemented" }
func (*UnimplementedModule) PreInit(*kernel.Engine) error        { return nil }
func (*UnimplementedModule) Load(*kernel.Engine) error           { return nil }
func (*UnimplementedModule) Stop(wg *sync.WaitGroup, _ context.Context) error {
	defer wg.Done()
	return nil
}
func (*UnimplementedModule) mustEmbedUnimplementedModule() {}
