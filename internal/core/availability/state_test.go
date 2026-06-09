package availability

import (
	"testing"
	"time"
)

func TestParseStateAcceptsValidState(t *testing.T) {
	now := time.UnixMilli(1710000000000)
	raw := []byte(`{"version":1,"mode":"normal_cloud","epoch":12,"active_redis":"cloud","mysql_state":"healthy","mysql_buffering_started_at_unix_ms":0,"updated_at_unix_ms":1710000000000,"reason":"probe_ok"}`)

	state, err := ParseState(raw, ParseOptions{Now: func() time.Time { return now }, LastEpoch: 11, StaleAfter: 5 * time.Second})
	if err != nil {
		t.Fatalf("ParseState() error = %v", err)
	}
	if state.Mode != ModeNormalCloud || state.Epoch != 12 || state.ActiveRedis != RedisCloud {
		t.Fatalf("unexpected state: %+v", state)
	}
}

func TestParseStateRejectsRegressingEpoch(t *testing.T) {
	raw := []byte(`{"version":1,"mode":"normal_cloud","epoch":10,"active_redis":"cloud","mysql_state":"healthy","updated_at_unix_ms":1710000000000,"reason":"probe_ok"}`)

	_, err := ParseState(raw, ParseOptions{Now: func() time.Time { return time.UnixMilli(1710000000000) }, LastEpoch: 11, StaleAfter: 5 * time.Second})
	if err == nil {
		t.Fatal("expected regressing epoch to fail")
	}
}

func TestParseStateRejectsStaleFile(t *testing.T) {
	raw := []byte(`{"version":1,"mode":"local_redis_active","epoch":12,"active_redis":"local","mysql_state":"healthy","updated_at_unix_ms":1710000000000,"reason":"probe_ok"}`)

	_, err := ParseState(raw, ParseOptions{Now: func() time.Time { return time.UnixMilli(1710000009000) }, LastEpoch: 12, StaleAfter: 5 * time.Second})
	if err == nil {
		t.Fatal("expected stale state to fail")
	}
}

func TestParseStateRejectsFutureFile(t *testing.T) {
	raw := []byte(`{"version":1,"mode":"normal_cloud","epoch":12,"active_redis":"cloud","mysql_state":"healthy","updated_at_unix_ms":1710000001000,"reason":"probe_ok"}`)

	_, err := ParseState(raw, ParseOptions{Now: func() time.Time { return time.UnixMilli(1710000000000) }, LastEpoch: 12, StaleAfter: 5 * time.Second})
	if err == nil {
		t.Fatal("expected future state to fail")
	}
}

func TestStateProtectsWhenBufferingWindowExpired(t *testing.T) {
	state := State{
		Version:                       1,
		Mode:                          ModeMySQLBuffering,
		Epoch:                         20,
		ActiveRedis:                   RedisCloud,
		MySQLState:                    MySQLBuffering,
		MySQLBufferingStartedAtUnixMS: 1710000000000,
		UpdatedAtUnixMS:               1710000000000,
	}

	if !state.MySQLBufferingExpired(time.UnixMilli(1710000010001), 10*time.Second) {
		t.Fatal("expected buffering window to be expired")
	}
}
