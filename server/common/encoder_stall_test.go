package common

import (
	"testing"
	"time"
)

func newTestStallGuard(now *time.Time, stable *bool) *encoderStallGuard {
	g := newEncoderStallGuard()
	g.now = func() time.Time { return *now }
	g.signalStable = func() bool { return *stable }
	return g
}

func failFor(g *encoderStallGuard, now *time.Time, d time.Duration) bool {
	end := now.Add(d)
	for now.Before(end) {
		*now = now.Add(time.Second / 120)
		if g.paused() {
			return true
		}
		g.observe(-1)
	}
	return g.paused()
}

func TestEncoderStallGuardPausesWhenStableInputYieldsNoFrames(t *testing.T) {
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	stable := true
	g := newTestStallGuard(&now, &stable)

	g.observe(3)
	if failFor(g, &now, encoderStallTimeout-encoderStallCheck) {
		t.Fatal("paused before the stall timeout")
	}
	if !failFor(g, &now, 2*encoderStallCheck+time.Second) {
		t.Fatal("did not pause after the stall timeout")
	}

	pauseStart := now
	for now.Sub(pauseStart) < encoderIdleResetPause-time.Second {
		now = now.Add(100 * time.Millisecond)
		if !g.paused() {
			t.Fatalf("pause ended after %s, want %s", now.Sub(pauseStart), encoderIdleResetPause)
		}
	}
	now = pauseStart.Add(encoderIdleResetPause + time.Millisecond)
	if g.paused() {
		t.Fatal("still paused after the reset pause")
	}

	// The re-initialized encoder gets a fresh timeout before another pause.
	if failFor(g, &now, encoderStallTimeout-encoderStallCheck) {
		t.Fatal("paused again right after the reset pause")
	}
}

func TestEncoderStallGuardIgnoresMissingSignal(t *testing.T) {
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	stable := false
	g := newTestStallGuard(&now, &stable)

	if failFor(g, &now, time.Minute) {
		t.Fatal("paused while the HDMI signal was missing")
	}

	// The signal returns but must stay stable for the full timeout.
	stable = true
	if failFor(g, &now, encoderStallTimeout-encoderStallCheck) {
		t.Fatal("paused before the signal was stable long enough")
	}
	if !failFor(g, &now, 2*encoderStallCheck+time.Second) {
		t.Fatal("did not pause once the signal was stable long enough")
	}
}

func TestEncoderStallGuardKeepsRunningWhileFramesArrive(t *testing.T) {
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	stable := true
	g := newTestStallGuard(&now, &stable)

	// Polling at 120Hz against a 60fps encoder fails every other read.
	for i := 0; i < 120*60; i++ {
		now = now.Add(time.Second / 120)
		if g.paused() {
			t.Fatalf("paused after %s of normal streaming", time.Duration(i)*time.Second/120)
		}
		if i%2 == 0 {
			g.observe(4)
		} else {
			g.observe(-1)
		}
	}
}
