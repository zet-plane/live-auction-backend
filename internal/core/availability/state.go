package availability

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

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

	MySQLHealthy    MySQLStatus = "healthy"
	MySQLDown       MySQLStatus = "down"
	MySQLBuffering  MySQLStatus = "buffering"
	MySQLRecovering MySQLStatus = "recovering"
)

var (
	ErrInvalidState = errors.New("availability state invalid")
	ErrStaleState   = errors.New("availability state stale")
)

type State struct {
	Version                       int            `json:"version"`
	Mode                          Mode           `json:"mode"`
	Epoch                         int64          `json:"epoch"`
	ActiveRedis                   RedisAuthority `json:"active_redis"`
	MySQLState                    MySQLStatus    `json:"mysql_state"`
	MySQLBufferingStartedAtUnixMS int64          `json:"mysql_buffering_started_at_unix_ms,omitempty"`
	UpdatedAtUnixMS               int64          `json:"updated_at_unix_ms"`
	Reason                        string         `json:"reason"`
}

type ParseOptions struct {
	Now        func() time.Time
	LastEpoch  int64
	StaleAfter time.Duration
}

func ParseState(raw []byte, opts ParseOptions) (State, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}

	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		return State{}, fmt.Errorf("%w: decode: %v", ErrInvalidState, err)
	}
	if err := state.Validate(opts.Now(), opts.LastEpoch, opts.StaleAfter); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s State) Validate(now time.Time, lastEpoch int64, staleAfter time.Duration) error {
	if s.Version != 1 {
		return fmt.Errorf("%w: unsupported version %d", ErrInvalidState, s.Version)
	}
	if s.Epoch < lastEpoch {
		return fmt.Errorf("%w: epoch regressed from %d to %d", ErrInvalidState, lastEpoch, s.Epoch)
	}
	if !validMode(s.Mode) || !validRedis(s.ActiveRedis) || !validMySQL(s.MySQLState) {
		return fmt.Errorf("%w: mode=%q redis=%q mysql=%q", ErrInvalidState, s.Mode, s.ActiveRedis, s.MySQLState)
	}
	if s.UpdatedAtUnixMS <= 0 {
		return fmt.Errorf("%w: updated_at_unix_ms is required", ErrInvalidState)
	}
	if updatedAt := time.UnixMilli(s.UpdatedAtUnixMS); updatedAt.After(now) {
		return fmt.Errorf("%w: updated_at_unix_ms=%d is in the future", ErrInvalidState, s.UpdatedAtUnixMS)
	}
	if staleAfter > 0 && now.Sub(time.UnixMilli(s.UpdatedAtUnixMS)) > staleAfter {
		return fmt.Errorf("%w: updated_at_unix_ms=%d", ErrStaleState, s.UpdatedAtUnixMS)
	}
	return nil
}

func (s State) ValidForWrites(now time.Time, staleAfter time.Duration) bool {
	return s.Validate(now, s.Epoch, staleAfter) == nil
}

func (s State) MySQLBufferingExpired(now time.Time, window time.Duration) bool {
	if s.Mode != ModeMySQLBuffering || s.MySQLBufferingStartedAtUnixMS <= 0 {
		return false
	}
	return now.Sub(time.UnixMilli(s.MySQLBufferingStartedAtUnixMS)) > window
}

func validMode(mode Mode) bool {
	switch mode {
	case ModeNormalCloud, ModeLocalRedisSwitching, ModeLocalRedisActive, ModeMySQLBuffering, ModeAuctionProtected:
		return true
	default:
		return false
	}
}

func validRedis(redis RedisAuthority) bool {
	return redis == RedisCloud || redis == RedisLocal
}

func validMySQL(mysql MySQLStatus) bool {
	switch mysql {
	case MySQLHealthy, MySQLDown, MySQLBuffering, MySQLRecovering:
		return true
	default:
		return false
	}
}
