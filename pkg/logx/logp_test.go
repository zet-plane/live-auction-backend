package logx

import (
	"sync"
	"testing"
)

func resetLoggerForTest() {
	rootLogger = nil
	setupOnce = sync.Once{}
}

func TestInfowDoesNotPanicWhenSetupNotCalled(t *testing.T) {
	resetLoggerForTest()
	defer resetLoggerForTest()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Infow panicked without explicit setup: %v", r)
		}
	}()

	Infow("test message", "key", "value")
}

func TestTrackCompletesWithoutExplicitSetup(t *testing.T) {
	resetLoggerForTest()
	defer resetLoggerForTest()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Track panicked without explicit setup: %v", r)
		}
	}()

	var err error
	finish := Track("test.operation", "id", "123")
	finish(&err, "result", "ok")
}
