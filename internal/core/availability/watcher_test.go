package availability

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherKeepsLastValidSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewFileStore(path)
	now := time.UnixMilli(1710000000000)
	initial := State{Version: 1, Mode: ModeNormalCloud, Epoch: 1, ActiveRedis: RedisCloud, MySQLState: MySQLHealthy, UpdatedAtUnixMS: now.UnixMilli(), Reason: "ok"}
	if err := store.Write(initial); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	w := NewWatcher(path, WatcherOptions{Now: func() time.Time { return now }, StaleAfter: time.Minute})
	if err := w.Refresh(); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	got := w.Snapshot()
	if got.State.Epoch != 1 || !got.Valid {
		t.Fatalf("snapshot = %+v", got)
	}
}

func TestWatcherRunRefreshesStateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewFileStore(path)
	initial := State{
		Version:         1,
		Mode:            ModeNormalCloud,
		Epoch:           1,
		ActiveRedis:     RedisCloud,
		MySQLState:      MySQLHealthy,
		UpdatedAtUnixMS: time.Now().UnixMilli(),
		Reason:          "boot",
	}
	if err := store.Write(initial); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	w := NewWatcher(path, WatcherOptions{StaleAfter: time.Minute})
	if err := w.Refresh(); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, 10*time.Millisecond)

	updated := State{
		Version:         1,
		Mode:            ModeLocalRedisActive,
		Epoch:           2,
		ActiveRedis:     RedisLocal,
		MySQLState:      MySQLHealthy,
		UpdatedAtUnixMS: time.Now().UnixMilli(),
		Reason:          "switch",
	}
	if err := store.Write(updated); err != nil {
		t.Fatalf("Write() updated error = %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if snap := w.Snapshot(); snap.Valid && snap.State.Epoch == 2 && snap.State.ActiveRedis == RedisLocal {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("watcher snapshot did not refresh: %+v", w.Snapshot())
}
