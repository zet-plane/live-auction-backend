package availability

import "time"

type Mode string
type RedisAuthority string
type MySQLStatus string

const (
	ModeNormalCloud         Mode = "normal_cloud"
	ModeLocalRedisSwitching Mode = "local_redis_switching"
	ModeLocalRedisActive    Mode = "local_redis_active"
	ModeMySQLBuffering      Mode = "mysql_buffering"
	ModeAuctionProtected    Mode = "auction_protected"

	RedisCloud RedisAuthority = "cloud"
	RedisLocal RedisAuthority = "local"
	RedisNone  RedisAuthority = "none"

	MySQLHealthy   MySQLStatus = "healthy"
	MySQLDown      MySQLStatus = "down"
	MySQLBuffering MySQLStatus = "buffering"
)

type DependencyStatus struct {
	Healthy bool
	Latency time.Duration
	Error   string
}

type Snapshot struct {
	Valid                       bool
	Mode                        Mode
	ActiveRedis                 RedisAuthority
	CloudRedis                  DependencyStatus
	LocalRedis                  DependencyStatus
	MySQL                       DependencyStatus
	MySQLState                  MySQLStatus
	MySQLBufferingStartedAt     time.Time
	MySQLBufferingStartedUnixMS int64
	UpdatedAt                   time.Time
	Reason                      string
	Error                       string
}

func (s Snapshot) RedisWritable() bool {
	return s.Valid && (s.ActiveRedis == RedisCloud || s.ActiveRedis == RedisLocal) && s.Mode != ModeAuctionProtected
}

func (s Snapshot) MySQLBufferingExpired(now time.Time, window time.Duration) bool {
	if s.Mode != ModeMySQLBuffering || s.MySQLBufferingStartedAt.IsZero() || window <= 0 {
		return false
	}
	return now.Sub(s.MySQLBufferingStartedAt) > window
}
