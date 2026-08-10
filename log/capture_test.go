package log_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/arandu-io/hesape/log"
)

// TestCaptureRecordsTheLine is what a test asserts against instead of parsing
// stdout: asserting on formatted text pins the assertion to the handler rather
// than to the behaviour.
func TestCaptureRecordsTheLine(t *testing.T) {
	logger, records := log.Capture()

	logger.Warn("password reset blocked", "reason", "too_many_attempts", "retry_after", 60)

	if records.Len() != 1 {
		t.Fatalf("captured %d lines, want 1", records.Len())
	}
	line := records.All()[0]
	if line.Message != "password reset blocked" {
		t.Errorf("message = %q", line.Message)
	}
	if line.Level != log.LevelWarning {
		t.Errorf("level = %v, want warning", line.Level)
	}
	if line.Attrs["reason"] != "too_many_attempts" {
		t.Errorf("attrs = %v", line.Attrs)
	}
	if line.Attrs["retry_after"] != int64(60) {
		t.Errorf("retry_after = %#v, want the value slog stored", line.Attrs["retry_after"])
	}
	if line.Time.IsZero() {
		t.Error("the line carries no time")
	}
}

// TestCaptureKeepsEveryLevel: a test that has to lower the level before it can
// assert is a test that fails for the wrong reason.
func TestCaptureKeepsEveryLevel(t *testing.T) {
	logger, records := log.Capture()
	ctx := context.Background()

	for _, level := range []slog.Level{
		log.LevelDebug, log.LevelInfo, log.LevelNotice, log.LevelWarning,
		log.LevelError, log.LevelCritical, log.LevelAlert, log.LevelEmergency,
	} {
		logger.Log(ctx, level, "line")
	}

	if records.Len() != 8 {
		t.Fatalf("captured %d lines, want all eight levels", records.Len())
	}
}

func TestCaptureFlattensGroups(t *testing.T) {
	logger, records := log.Capture()

	logger.WithGroup("user").With("id", "u-9").Info("signed in", slog.Group("device", "os", "linux"))

	attrs := records.All()[0].Attrs
	if attrs["user.id"] != "u-9" {
		t.Errorf("attrs = %v, want the grouped attribute under a dotted key", attrs)
	}
	if attrs["user.device.os"] != "linux" {
		t.Errorf("attrs = %v, want the nested group flattened too", attrs)
	}
}

// TestTwoLoggersFromOneParentDoNotShareFields is the aliasing bug this handler
// is written to avoid: with append onto a shared backing array, one request ends
// up logging another request's tenant.
func TestTwoLoggersFromOneParentDoNotShareFields(t *testing.T) {
	logger, records := log.Capture()
	parent := logger.With("component", "worker")

	parent.With("tenant", "t-1").Info("left")
	parent.With("tenant", "t-2").Info("right")

	all := records.All()
	if len(all) != 2 {
		t.Fatalf("captured %d lines, want 2", len(all))
	}
	if all[0].Attrs["tenant"] != "t-1" || all[1].Attrs["tenant"] != "t-2" {
		t.Errorf("lines = %v, %v", all[0].Attrs, all[1].Attrs)
	}
	for _, line := range all {
		if line.Attrs["component"] != "worker" {
			t.Errorf("the parent field was lost: %v", line.Attrs)
		}
	}
}

// TestCaptureIsSafeUnderConcurrentWriters: a worker or a relay under test logs
// from several goroutines, and the assertion runs while they are still going.
func TestCaptureIsSafeUnderConcurrentWriters(t *testing.T) {
	logger, records := log.Capture()

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			logger.Info("line", "worker", i)
		}(i)
	}
	for range 50 {
		_ = records.All()
		_ = records.Len()
	}
	wg.Wait()

	if records.Len() != 8 {
		t.Fatalf("captured %d lines, want 8", records.Len())
	}
}
