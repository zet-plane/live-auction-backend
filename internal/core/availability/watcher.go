package availability

import (
	"context"
	"sync/atomic"
	"time"
)

type Snapshot struct {
	State State
	Valid bool
	Error string
}

type WatcherOptions struct {
	Now        func() time.Time
	StaleAfter time.Duration
	LastEpoch  int64
}

type Watcher struct {
	path string
	opts WatcherOptions
	v    atomic.Value
}

func NewWatcher(path string, opts WatcherOptions) *Watcher {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	w := &Watcher{path: path, opts: opts}
	w.v.Store(Snapshot{Valid: false, Error: "not loaded"})
	return w
}

func (w *Watcher) Refresh() error {
	current := w.Snapshot()
	state, err := NewFileStore(w.path).Read(ParseOptions{
		Now:        w.opts.Now,
		LastEpoch:  maxInt64(w.opts.LastEpoch, current.State.Epoch),
		StaleAfter: w.opts.StaleAfter,
	})
	if err != nil {
		w.v.Store(Snapshot{State: current.State, Valid: false, Error: err.Error()})
		return err
	}
	w.v.Store(Snapshot{State: state, Valid: true})
	return nil
}

func (w *Watcher) Snapshot() Snapshot {
	return w.v.Load().(Snapshot)
}

func (w *Watcher) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = w.Refresh()
		}
	}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
