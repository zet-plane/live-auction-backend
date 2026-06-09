package availability

import "github.com/redis/go-redis/v9"

type RedisSelector struct {
	cloud *redis.Client
	local *redis.Client
}

func NewRedisSelector(cloud, local *redis.Client) *RedisSelector {
	return &RedisSelector{cloud: cloud, local: local}
}

func (s *RedisSelector) Client(snapshot Snapshot) (*redis.Client, bool) {
	if !snapshot.Valid {
		return nil, false
	}
	switch snapshot.State.ActiveRedis {
	case RedisCloud:
		return s.cloud, s.cloud != nil
	case RedisLocal:
		return s.local, s.local != nil
	default:
		return nil, false
	}
}

type Runtime struct {
	Watcher  *Watcher
	Selector *RedisSelector
}

func NewRuntime(watcher *Watcher, selector *RedisSelector) *Runtime {
	return &Runtime{Watcher: watcher, Selector: selector}
}

func (r *Runtime) Snapshot() Snapshot {
	if r == nil || r.Watcher == nil {
		return Snapshot{Valid: false, Error: "availability runtime unconfigured"}
	}
	return r.Watcher.Snapshot()
}

func (r *Runtime) ActiveRedis() (*redis.Client, Snapshot, bool) {
	snapshot := r.Snapshot()
	if r == nil || r.Selector == nil {
		return nil, snapshot, false
	}
	client, ok := r.Selector.Client(snapshot)
	return client, snapshot, ok
}
