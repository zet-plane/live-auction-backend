package item

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app"
	depositapp "github.com/zet-plane/live-auction-backend/internal/app/deposit"
	"github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/item/handler"
	"github.com/zet-plane/live-auction-backend/internal/app/item/router"
	"github.com/zet-plane/live-auction-backend/internal/app/item/service"
	orderapp "github.com/zet-plane/live-auction-backend/internal/app/order"
	wsapp "github.com/zet-plane/live-auction-backend/internal/app/ws"
	"github.com/zet-plane/live-auction-backend/internal/core/cronlease"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
)

var ErrEmptyDatabase = errors.New("database pointer is nil")

type Reader interface {
	ListItemsByIDs(ctx context.Context, itemIDs []string) ([]dto.ItemListDTO, error)
}

// ItemReader is the package-level contract exported for room feed enrichment.
var ItemReader Reader

type Item struct {
	Name string
	svc  *service.Service

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
	if err := store.AutoMigrate(); err != nil {
		return err
	}
	return store.AutoMigrateBidLog()
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

	var c cache.Cache
	if engine.Availability != nil {
		c = cache.NewActiveRedisCache(engine.Availability)
	} else if engine.Cache != nil {
		c = cache.NewRedisCache(engine.Cache)
	}
	svc := service.NewService(store, policy, c, orderapp.Svc, depositapp.Svc, wsapp.Hub)
	svc.SetAvailability(engine.Availability, engine.Config.MySQLBufferingWindow())
	if engine.CloudRedis != nil || engine.LocalRedis != nil {
		var cloudCache cache.Cache
		var localCache cache.Cache
		if engine.CloudRedis != nil {
			cloudCache = cache.NewRedisCache(engine.CloudRedis)
		}
		if engine.LocalRedis != nil {
			localCache = cache.NewRedisCache(engine.LocalRedis)
		}
		svc.SetRedisAuthorities(cloudCache, localCache)
	}
	leaseStore := cronlease.NewRedisStore(engine.Cache)
	leaseOwner := bidLogConsumerName(os.Hostname)
	svc.SetRankingRebuildOwner(leaseOwner)
	i.svc = svc
	ItemReader = svc
	handler.Init(svc)
	router.RegisterRoutes(engine.Flame)
	wsapp.SetSnapshotProvider(svc)
	engine.Cron.AddFunc("@every 1s",
		cronlease.WrapCron("item.start_due_auctions", leaseOwner, 2*time.Second, leaseStore, svc.StartDueAuctions))
	engine.Cron.AddFunc("@every 1s",
		cronlease.WrapCron("item.settle_due_auctions", leaseOwner, 2*time.Second, leaseStore, svc.SettleDueAuctions))
	engine.Cron.AddFunc("@every 1s",
		cronlease.WrapCron("item.broadcast_time_sync", leaseOwner, time.Second, leaseStore, svc.BroadcastTimeSync))
	engine.Cron.AddFunc("@every 1m",
		cronlease.WrapCron("item.end_expired_auctions_fallback", leaseOwner, 30*time.Second, leaseStore, svc.EndExpiredAuctions))
	if engine.Cache != nil {
		reader := cache.NewBidLogStreamReader(engine.Cache, leaseOwner)
		if err := reader.EnsureGroup(engine.Context); err != nil {
			return err
		}
		svc.StartBidLogWorker(engine.Context, reader)
	}
	return nil
}

func bidLogConsumerName(hostname func() (string, error)) string {
	name, err := hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "backend-1"
	}
	return "backend-" + strings.TrimSpace(name)
}

func (i *Item) Stop(wg *sync.WaitGroup, _ context.Context) error {
	defer wg.Done()
	if i.svc != nil {
		i.svc.StopBidLogWorker()
	}
	return nil
}
