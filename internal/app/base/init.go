package base

import (
	"github.com/zet-plane/live-auction-backend/internal/app"
	"github.com/zet-plane/live-auction-backend/internal/app/base/handler"
	"github.com/zet-plane/live-auction-backend/internal/app/base/router"
	baseservice "github.com/zet-plane/live-auction-backend/internal/app/base/service"
	"github.com/zet-plane/live-auction-backend/internal/app/base/storage"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
)

type Base struct {
	Name string
	app.UnimplementedModule
}

func (b *Base) Info() string { return b.Name }

func (b *Base) Load(engine *kernel.Engine) error {
	var signer baseservice.Signer
	tosSigner, err := storage.NewTOSPostSigner(engine.Config.Storage.TOS)
	if err == nil {
		signer = tosSigner
	} else {
		logx.Warnw("TOS upload signer disabled", "err", err)
	}
	uploadSvc := baseservice.NewUploadService(signer, baseservice.Options{
		PublicBaseURL: engine.Config.Storage.TOS.PublicBaseURL,
		MaxSizeBytes:  engine.Config.StorageTOSImageMaxSizeBytes(),
		Expires:       engine.Config.StorageTOSUploadExpires(),
	})
	handler.Init(engine.DB, engine.Cache, uploadSvc)
	router.RegisterRoutes(engine.Flame)
	return nil
}
