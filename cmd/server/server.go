package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/flamego/flamego"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"
	"github.com/zet-plane/live-auction-backend/config"
	"github.com/zet-plane/live-auction-backend/internal/app/appInitialize"
	"github.com/zet-plane/live-auction-backend/internal/core/cache"
	"github.com/zet-plane/live-auction-backend/internal/core/database"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	appCron "github.com/zet-plane/live-auction-backend/internal/cron"
	"github.com/zet-plane/live-auction-backend/internal/middleware/gw"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var configPath string

var StartCmd = &cobra.Command{
	Use:     "server",
	Short:   "Start the live-auction API server",
	Example: "live-auction server -c config.yaml",
	PreRun: func(cmd *cobra.Command, args []string) {
		config.LoadConfig(configPath)
		cfg := config.GetConfig()
		if cfg.Observability.Logs.Format == "json" {
			logx.SetUp(logx.WithZapConfig(logx.JSONConfig()))
			return
		}
		logx.SetUp()
	},
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.GetConfig()

		shutdown, err := observability.Setup(context.Background(), cfg.Observability)
		if err != nil {
			logx.Errorf("observability setup failed: %v", err)
			shutdown = func(context.Context) error { return nil }
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := shutdown(ctx); err != nil {
				logx.Errorf("observability shutdown failed: %v", err)
			}
		}()

		rec, err := observability.NewRecorder()
		if err != nil {
			logx.Errorf("observability recorder setup failed: %v", err)
			observability.SetDefaultRecorder(nil)
		} else {
			observability.SetDefaultRecorder(rec)
		}

		db, err := database.Open(database.Config{
			Driver:          cfg.Database.Driver,
			DSN:             cfg.Database.DSN,
			MaxIdleConns:    cfg.Database.MaxIdleConns,
			MaxOpenConns:    cfg.Database.MaxOpenConns,
			ConnMaxLifetime: cfg.DatabaseConnMaxLifetime(),
		})
		if err != nil {
			logx.Fatalf("failed to connect database: %v", err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			logx.Fatalf("failed to access database pool: %v", err)
		}
		cleanupRuntimeMetrics, err := observability.RegisterRuntimeMetrics(otel.GetMeterProvider(), observability.SQLDBStatsProvider{DB: sqlDB})
		if err != nil {
			logx.Errorf("runtime metrics setup failed: %v", err)
		} else {
			defer func() {
				if err := cleanupRuntimeMetrics(); err != nil {
					logx.Errorf("runtime metrics cleanup failed: %v", err)
				}
			}()
		}

		rdb, err := cache.Open(cache.Config{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
		if err != nil {
			logx.Fatalf("failed to connect redis: %v", err)
		}

		engine, err := buildEngine(cfg, db, rdb)
		if err != nil {
			logx.Fatalf("failed to initialize engine: %v", err)
		}
		if err := run(engine); err != nil {
			logx.Fatalf("server stopped with error: %v", err)
		}
	},
}

func init() {
	StartCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "config.yaml", "path to config file")
}

func buildEngine(cfg *config.Config, db *gorm.DB, rdb *redis.Client) (*kernel.Engine, error) {
	ctx, cancel := context.WithCancel(context.Background())

	flamego.SetEnv(flamego.EnvType(cfg.Mode))
	f := flamego.New()
	originPolicy := web.NewOriginPolicy(cfg.Mode, cfg.Security.AllowedOrigins)
	f.Use(
		flamego.Recovery(),
		observability.HTTPMiddleware(observability.DefaultRecorder()),
		gw.RequestLog(),
		flamego.Renderer(),
		web.CORSMiddleware(originPolicy),
	)

	f.Get("/api/v1/health", func(r flamego.Render) {
		response.OK(r, map[string]string{
			"name":    cfg.App.Name,
			"version": cfg.App.Version,
			"status":  "ok",
		})
	})
	registerSwaggerRoutes(f)

	c := appCron.New()

	engine := &kernel.Engine{
		Context: ctx,
		Cancel:  cancel,
		Flame:   f,
		DB:      db,
		Cache:   rdb,
		Config:  cfg,
		Cron:    c,
	}

	apps := appInitialize.GetApps()
	for _, mod := range apps {
		if err := mod.PreInit(engine); err != nil {
			cancel()
			return nil, fmt.Errorf("module %s PreInit: %w", mod.Info(), err)
		}
	}
	for _, mod := range apps {
		if err := mod.Load(engine); err != nil {
			cancel()
			return nil, fmt.Errorf("module %s Load: %w", mod.Info(), err)
		}
	}

	c.Start()
	appCron.PrintEntries(c)

	return engine, nil
}

func run(engine *kernel.Engine) error {
	cfg := config.GetConfig()
	srv := &http.Server{
		Addr:              cfg.Address(),
		Handler:           engine.Flame,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logx.Infof("server listening on http://%s", cfg.Address())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logx.Infof("received signal %s, shutting down", sig)
	case err := <-errCh:
		return err
	}

	engine.Cancel()
	engine.Cron.Stop()

	var wg sync.WaitGroup
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	for _, mod := range appInitialize.GetApps() {
		wg.Add(1)
		go func() { _ = mod.Stop(&wg, stopCtx) }()
	}
	wg.Wait()

	err := srv.Shutdown(stopCtx)
	logx.Stop()
	return err
}
