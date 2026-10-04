package common

import (
	"os"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	LT6911StatusPath = "/proc/lt6911_info/status"

	encoderStallTimeout   = 3 * time.Second
	encoderStallCheck     = time.Second
	encoderIdleResetPause = 7 * time.Second
)

type encoderStallGuard struct {
	mu          sync.Mutex
	lastOK      time.Time
	lastCheck   time.Time
	stableSince time.Time
	pausedUntil time.Time

	now          func() time.Time
	signalStable func() bool
}

func newEncoderStallGuard() *encoderStallGuard {
	return &encoderStallGuard{
		now:          time.Now,
		signalStable: isSignalStable,
	}
}

// paused reports whether reads must be skipped so libkvm can drop the encoder.
func (g *encoderStallGuard) paused() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.now().Before(g.pausedUntil)
}

func (g *encoderStallGuard) observe(result int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now()
	if result >= 0 {
		g.lastOK = now
		return
	}
	if g.lastOK.IsZero() {
		g.lastOK = now
	}

	if now.Sub(g.lastCheck) < encoderStallCheck {
		return
	}
	g.lastCheck = now

	if !g.signalStable() {
		g.stableSince = time.Time{}
		return
	}
	if g.stableSince.IsZero() {
		g.stableSince = now
	}

	if now.Sub(g.stableSince) < encoderStallTimeout || now.Sub(g.lastOK) < encoderStallTimeout {
		return
	}

	log.Warnf("no video frames for %s while HDMI input is stable, pausing reads to reset the encoder",
		now.Sub(g.lastOK).Round(time.Second))

	g.pausedUntil = now.Add(encoderIdleResetPause)
	g.lastOK = g.pausedUntil
	g.stableSince = time.Time{}
}

func isSignalStable() bool {
	data, err := os.ReadFile(LT6911StatusPath)
	if err != nil {
		return false
	}
	if strings.TrimSpace(string(data)) != "stable" {
		return false
	}

	return readSize(WidthPath) > 0 && readSize(HeightPath) > 0
}
