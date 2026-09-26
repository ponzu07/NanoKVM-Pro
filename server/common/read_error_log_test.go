package common

import (
	"testing"
	"time"
)

func TestReadErrorLogSuppressesRepeats(t *testing.T) {
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	l := newReadErrorLog("audio")
	l.now = func() time.Time { return now }

	if !l.failed(-1) {
		t.Fatal("first failure should be logged")
	}

	for i := 0; i < 100; i++ {
		now = now.Add(20 * time.Millisecond)
		if l.failed(-1) {
			t.Fatalf("repeat %d within interval should be suppressed", i)
		}
	}
	if l.suppressed != 100 {
		t.Fatalf("suppressed = %d, want 100", l.suppressed)
	}

	now = now.Add(readErrorLogInterval)
	if !l.failed(-1) {
		t.Fatal("failure after interval should be logged")
	}
	if l.suppressed != 0 {
		t.Fatalf("suppressed = %d after log, want 0", l.suppressed)
	}
}

func TestReadErrorLogLogsChangedResult(t *testing.T) {
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	l := newReadErrorLog("H.264")
	l.now = func() time.Time { return now }

	l.failed(-1)
	now = now.Add(time.Millisecond)
	if !l.failed(-2) {
		t.Fatal("a different result should be logged immediately")
	}
	now = now.Add(time.Millisecond)
	if l.failed(-2) {
		t.Fatal("repeat of the new result should be suppressed")
	}
}
