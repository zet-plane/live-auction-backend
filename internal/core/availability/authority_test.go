package availability

import (
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestRedisSelectorChoosesLocalWhenStateSaysLocal(t *testing.T) {
	cloud := redis.NewClient(&redis.Options{Addr: "cloud:6379"})
	local := redis.NewClient(&redis.Options{Addr: "local:6379"})
	selector := NewRedisSelector(cloud, local)

	got, ok := selector.Client(Snapshot{Valid: true, State: State{ActiveRedis: RedisLocal}})
	if !ok {
		t.Fatal("expected client")
	}
	if got != local {
		t.Fatal("expected local redis")
	}
}

func TestRedisSelectorFailsClosedOnInvalidSnapshot(t *testing.T) {
	selector := NewRedisSelector(redis.NewClient(&redis.Options{Addr: "cloud:6379"}), redis.NewClient(&redis.Options{Addr: "local:6379"}))

	got, ok := selector.Client(Snapshot{Valid: false, Error: "stale"})
	if ok || got != nil {
		t.Fatalf("Client() = (%v, %v), want nil false", got, ok)
	}
}
