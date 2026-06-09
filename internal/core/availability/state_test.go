package availability

import (
	"testing"
	"time"
)

func TestSnapshotHelpers(t *testing.T) {
	now := time.UnixMilli(1710000000000)
	s := Snapshot{
		Valid:       true,
		Mode:        ModeLocalRedisActive,
		ActiveRedis: RedisLocal,
		CloudRedis:  DependencyStatus{Healthy: false, Error: "timeout"},
		LocalRedis:  DependencyStatus{Healthy: true, Latency: 2 * time.Millisecond},
		MySQL:       DependencyStatus{Healthy: true, Latency: time.Millisecond},
		UpdatedAt:   now,
	}

	if !s.RedisWritable() {
		t.Fatal("expected redis writable")
	}
	if s.MySQLBufferingExpired(now.Add(11*time.Second), 10*time.Second) {
		t.Fatal("local redis active without buffering start must not expire")
	}
}

func TestMySQLBufferingExpired(t *testing.T) {
	start := time.UnixMilli(1710000000000)
	s := Snapshot{
		Valid:                       true,
		Mode:                        ModeMySQLBuffering,
		ActiveRedis:                 RedisCloud,
		MySQLBufferingStartedAt:     start,
		MySQLBufferingStartedUnixMS: start.UnixMilli(),
	}

	if s.MySQLBufferingExpired(start.Add(9*time.Second), 10*time.Second) {
		t.Fatal("buffering expired too early")
	}
	if !s.MySQLBufferingExpired(start.Add(11*time.Second), 10*time.Second) {
		t.Fatal("buffering should expire after window")
	}
}

func TestProtectedSnapshotIsNotWritable(t *testing.T) {
	s := Snapshot{Valid: true, Mode: ModeAuctionProtected, ActiveRedis: RedisNone}
	if s.RedisWritable() {
		t.Fatal("protected snapshot must not be writable")
	}
}
