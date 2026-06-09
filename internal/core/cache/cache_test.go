package cache

import "testing"

func TestOpenDoesNotPingWhenDisabled(t *testing.T) {
	client, err := Open(Config{Addr: "127.0.0.1:1", DisablePing: true})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if client == nil {
		t.Fatal("expected redis client")
	}
	_ = client.Close()
}
