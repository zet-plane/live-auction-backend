package database

import (
	"fmt"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/middleware/gormv2"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type Config struct {
	Driver          string
	DSN             string
	MaxIdleConns    int
	MaxOpenConns    int
	ConnMaxLifetime time.Duration
}

func Open(cfg Config) (*gorm.DB, error) {
	if cfg.Driver == "" {
		cfg.Driver = "mysql"
	}
	if cfg.Driver != "mysql" {
		return nil, fmt.Errorf("unsupported database driver %q", cfg.Driver)
	}
	if cfg.DSN == "" {
		return nil, fmt.Errorf("database dsn is required")
	}

	db, err := gorm.Open(mysql.Open(cfg.DSN), &gorm.Config{
		Logger: gormv2.NewLogger(),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	return db, nil
}
