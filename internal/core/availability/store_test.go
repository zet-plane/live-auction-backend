package availability

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStoreWriteAndReadState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	store := NewFileStore(path)
	state := State{Version: 1, Mode: ModeNormalCloud, Epoch: 1, ActiveRedis: RedisCloud, MySQLState: MySQLHealthy, UpdatedAtUnixMS: time.Now().UnixMilli(), Reason: "test"}

	if err := store.Write(state); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	parsed, err := ParseState(raw, ParseOptions{Now: time.Now, LastEpoch: 0, StaleAfter: time.Minute})
	if err != nil {
		t.Fatalf("ParseState() error = %v", err)
	}
	if parsed.Epoch != 1 {
		t.Fatalf("epoch = %d, want 1", parsed.Epoch)
	}
}
