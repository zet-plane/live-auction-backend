package availability

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type ProbeFunc func(context.Context) DependencyStatus

type Probe struct {
	CloudRedis ProbeFunc
	LocalRedis ProbeFunc
	MySQL      ProbeFunc
}

type Options struct {
	Now                  func() time.Time
	ProbeInterval        time.Duration
	FailoverAfter        time.Duration
	MySQLBufferingWindow time.Duration
	Probe                Probe
}

type Runtime struct {
	cloudRedis *redis.Client
	localRedis *redis.Client
	db         *gorm.DB
	opts       Options
	v          atomic.Value
	mu         sync.Mutex

	cloudDownSince          time.Time
	mysqlDownSince          time.Time
	localAuthorityActivated bool
}

func NewRuntime(cloudRedis, localRedis *redis.Client, db *gorm.DB, opts Options) *Runtime {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.ProbeInterval <= 0 {
		opts.ProbeInterval = time.Second
	}
	if opts.FailoverAfter <= 0 {
		opts.FailoverAfter = 3 * time.Second
	}
	if opts.MySQLBufferingWindow <= 0 {
		opts.MySQLBufferingWindow = 10 * time.Second
	}
	rt := &Runtime{cloudRedis: cloudRedis, localRedis: localRedis, db: db, opts: opts}
	rt.v.Store(Snapshot{Valid: false, Mode: ModeAuctionProtected, ActiveRedis: RedisNone, Reason: "not_probed", Error: "not probed"})
	return rt
}

func (r *Runtime) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{Valid: false, Mode: ModeAuctionProtected, ActiveRedis: RedisNone, Reason: "runtime_nil", Error: "availability runtime unconfigured"}
	}
	return r.v.Load().(Snapshot)
}

func (r *Runtime) ActiveRedis() (*redis.Client, Snapshot, bool) {
	snapshot := r.Snapshot()
	if !snapshot.RedisWritable() {
		return nil, snapshot, false
	}
	switch snapshot.ActiveRedis {
	case RedisCloud:
		return r.cloudRedis, snapshot, r.cloudRedis != nil
	case RedisLocal:
		return r.localRedis, snapshot, r.localRedis != nil
	default:
		return nil, snapshot, false
	}
}

func (r *Runtime) Run(ctx context.Context) {
	r.Refresh(ctx)
	ticker := time.NewTicker(r.opts.ProbeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Refresh(ctx)
		}
	}
}

func (r *Runtime) Refresh(ctx context.Context) {
	if r == nil {
		return
	}
	now := r.opts.Now()
	cloud := r.probeCloud(ctx)
	local := r.probeLocal(ctx)
	mysql := r.probeMySQL(ctx)

	r.mu.Lock()
	defer r.mu.Unlock()

	if cloud.Healthy {
		r.cloudDownSince = time.Time{}
	} else if r.cloudDownSince.IsZero() {
		r.cloudDownSince = now
	}

	if mysql.Healthy {
		r.mysqlDownSince = time.Time{}
	} else if r.mysqlDownSince.IsZero() {
		r.mysqlDownSince = now
	}

	mode := ModeAuctionProtected
	active := RedisNone
	mysqlState := MySQLHealthy
	reason := "protected"

	if !mysql.Healthy {
		mysqlState = MySQLBuffering
	}

	if r.localAuthorityActivated {
		if local.Healthy {
			mode = ModeLocalRedisActive
			active = RedisLocal
			reason = "local_sticky"
		} else {
			mode = ModeAuctionProtected
			active = RedisNone
			reason = "local_redis_down"
		}
	} else if cloud.Healthy {
		mode = ModeNormalCloud
		active = RedisCloud
		reason = "cloud_redis_ok"
	} else if local.Healthy && !r.cloudDownSince.IsZero() && r.cloudFailoverReady(now) {
		mode = ModeLocalRedisSwitching
		active = RedisLocal
		reason = "cloud_redis_failover"
		r.localAuthorityActivated = true
	} else {
		mode = ModeAuctionProtected
		active = RedisNone
		reason = "cloud_redis_failover_threshold"
	}

	var mysqlStarted time.Time
	if !mysql.Healthy {
		mysqlStarted = r.mysqlDownSince
		if !mysqlStarted.IsZero() && now.Sub(mysqlStarted) <= r.opts.MySQLBufferingWindow && active != RedisNone {
			mode = ModeMySQLBuffering
			mysqlState = MySQLBuffering
			reason = "mysql_buffering"
		} else {
			mode = ModeAuctionProtected
			active = RedisNone
			reason = "mysql_buffering_expired"
		}
	}

	r.v.Store(Snapshot{
		Valid:                       true,
		Mode:                        mode,
		ActiveRedis:                 active,
		CloudRedis:                  cloud,
		LocalRedis:                  local,
		MySQL:                       mysql,
		MySQLState:                  mysqlState,
		MySQLBufferingStartedAt:     mysqlStarted,
		MySQLBufferingStartedUnixMS: unixMilliOrZero(mysqlStarted),
		UpdatedAt:                   now,
		Reason:                      reason,
	})
}

func (r *Runtime) probeCloud(ctx context.Context) DependencyStatus {
	if r.opts.Probe.CloudRedis != nil {
		return r.opts.Probe.CloudRedis(ctx)
	}
	return probeRedis(ctx, r.cloudRedis)
}

func (r *Runtime) cloudFailoverReady(now time.Time) bool {
	if r.opts.FailoverAfter <= time.Nanosecond {
		return true
	}
	return now.Sub(r.cloudDownSince) >= r.opts.FailoverAfter
}

func (r *Runtime) probeLocal(ctx context.Context) DependencyStatus {
	if r.opts.Probe.LocalRedis != nil {
		return r.opts.Probe.LocalRedis(ctx)
	}
	return probeRedis(ctx, r.localRedis)
}

func (r *Runtime) probeMySQL(ctx context.Context) DependencyStatus {
	if r.opts.Probe.MySQL != nil {
		return r.opts.Probe.MySQL(ctx)
	}
	return probeDB(ctx, r.db)
}

func probeRedis(ctx context.Context, client *redis.Client) DependencyStatus {
	if client == nil {
		return DependencyStatus{Healthy: false, Error: "not initialized"}
	}
	start := time.Now()
	err := client.Ping(ctx).Err()
	return statusFromError(time.Since(start), err)
}

func probeDB(ctx context.Context, db *gorm.DB) DependencyStatus {
	if db == nil {
		return DependencyStatus{Healthy: false, Error: "not initialized"}
	}
	sqlDB, err := db.DB()
	if err != nil {
		return DependencyStatus{Healthy: false, Error: err.Error()}
	}
	start := time.Now()
	err = sqlDB.PingContext(ctx)
	return statusFromError(time.Since(start), err)
}

func statusFromError(latency time.Duration, err error) DependencyStatus {
	if err != nil {
		return DependencyStatus{Healthy: false, Latency: latency, Error: err.Error()}
	}
	return DependencyStatus{Healthy: true, Latency: latency}
}

func unixMilliOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
