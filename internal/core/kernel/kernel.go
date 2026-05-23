package kernel

import (
	"context"

	"github.com/flamego/flamego"
	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
	"github.com/zet-plane/live-auction-backend/config"
	"gorm.io/gorm"
)

type Engine struct {
	Context context.Context
	Cancel  context.CancelFunc
	Flame   *flamego.Flame
	DB      *gorm.DB
	Cache   *redis.Client
	Config  *config.Config
	Cron    *cron.Cron
}
