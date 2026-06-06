package cronlease

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeStore struct {
	acquired bool
	err      error
}

func (s fakeStore) Acquire(context.Context, string, string, time.Duration) (bool, error) {
	return s.acquired, s.err
}

func TestWrapRunsFunctionWhenLeaseAcquired(t *testing.T) {
	called := false
	fn := Wrap("job_a", "pod_a", time.Second, fakeStore{acquired: true}, func(context.Context) {
		called = true
	})

	fn(context.Background())

	if !called {
		t.Fatal("expected wrapped function to run")
	}
}

func TestWrapSkipsFunctionWhenLeaseNotAcquired(t *testing.T) {
	called := false
	fn := Wrap("job_a", "pod_a", time.Second, fakeStore{acquired: false}, func(context.Context) {
		called = true
	})

	fn(context.Background())

	if called {
		t.Fatal("expected wrapped function to be skipped")
	}
}

func TestWrapSkipsFunctionWhenAcquireErrors(t *testing.T) {
	called := false
	fn := Wrap("job_a", "pod_a", time.Second, fakeStore{err: errors.New("redis down")}, func(context.Context) {
		called = true
	})

	fn(context.Background())

	if called {
		t.Fatal("expected wrapped function to be skipped after acquire error")
	}
}
