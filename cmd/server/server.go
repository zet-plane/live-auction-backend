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

	"github.com/flamego/cors"
	"github.com/flamego/flamego"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"
	"github.com/zet-plane/live-auction-backend/config"
	"github.com/zet-plane/live-auction-backend/internal/app/appInitialize"
	appCron "github.com/zet-plane/live-auction-backend/internal/cron"
	"github.com/zet-plane/live-auction-backend/internal/core/cache"
	"github.com/zet-plane/live-auction-backend/internal/core/database"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
	"github.com/zet-plane/live-auction-backend/internal/middleware/gw"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"gorm.io/gorm"
)

var configPath string

var StartCmd = &cobra.Command{
	Use:     "server",
	Short:   "Start the live-auction API server",
	Example: "live-auction server -c config.yaml",
	PreRun: func(cmd *cobra.Command, args []string) {
		config.LoadConfig(configPath)
	},
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.GetConfig()

		db, err := database.Open(database.Config{
			Driver:          cfg.Database.Driver,
			DSN:             cfg.Database.DSN,
			MaxIdleConns:    cfg.Database.MaxIdleConns,
			MaxOpenConns:    cfg.Database.MaxOpenConns,
			ConnMaxLifetime: cfg.DatabaseConnMaxLifetime(),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to connect database: %v\n", err)
			os.Exit(1)
		}

		rdb, err := cache.Open(cache.Config{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to connect redis: %v\n", err)
			os.Exit(1)
		}

		engine, err := buildEngine(cfg, db, rdb)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to initialize engine: %v\n", err)
			os.Exit(1)
		}
		if err := run(engine); err != nil {
			fmt.Fprintf(os.Stderr, "server stopped with error: %v\n", err)
			os.Exit(1)
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
	f.Use(
		flamego.Recovery(),
		gw.RequestLog(),
		flamego.Renderer(),
		cors.CORS(cors.Options{
			AllowCredentials: true,
			AllowDomain:      []string{"!*"},
			Methods: []string{
				http.MethodGet,
				http.MethodPost,
				http.MethodPut,
				http.MethodPatch,
				http.MethodDelete,
				http.MethodOptions,
			},
		}),
	)

	f.Get("/api/v1/health", func(r flamego.Render) {
		response.OK(r, map[string]string{
			"name":    cfg.App.Name,
			"version": cfg.App.Version,
			"status":  "ok",
		})
	})

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
		fmt.Printf("server listening on http://%s\n", cfg.Address())
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
		fmt.Printf("received signal %s, shutting down\n", sig)
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

	return srv.Shutdown(stopCtx)
}
