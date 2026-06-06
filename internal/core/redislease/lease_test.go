package redislease

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeSetter struct {
	ok  bool
	err error
}

func (s fakeSetter) SetNX(context.Context, string, any, time.Duration) (bool, error) {
	return s.ok, s.err
}

func TestAcquireReturnsTrueWhenSetNXWins(t *testing.T) {
	store := Store{Setter: fakeSetter{ok: true}}
	ok, err := store.Acquire(context.Background(), "key", "owner", time.Second)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected acquired lease")
	}
}

func TestAcquireReturnsFalseWhenSetNXLoses(t *testing.T) {
	store := Store{Setter: fakeSetter{ok: false}}
	ok, err := store.Acquire(context.Background(), "key", "owner", time.Second)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if ok {
		t.Fatal("expected lease miss")
	}
}

func TestAcquireReturnsErrorFromStore(t *testing.T) {
	store := Store{Setter: fakeSetter{err: errors.New("redis down")}}
	ok, err := store.Acquire(context.Background(), "key", "owner", time.Second)
	if err == nil {
		t.Fatal("expected error")
	}
	if ok {
		t.Fatal("expected ok=false on error")
	}
}
