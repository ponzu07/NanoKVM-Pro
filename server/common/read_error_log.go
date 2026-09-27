package common

import (
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const readErrorLogInterval = time.Minute

type readErrorLog struct {
	mu         sync.Mutex
	name       string
	lastResult int
	lastLog    time.Time
	suppressed int
	now        func() time.Time
}

func newReadErrorLog(name string) *readErrorLog {
	return &readErrorLog{
		name: name,
		now:  time.Now,
	}
}

func (l *readErrorLog) failed(result int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if !l.lastLog.IsZero() && result == l.lastResult && now.Sub(l.lastLog) < readErrorLogInterval {
		l.suppressed++
		return false
	}

	if l.suppressed > 0 {
		log.Errorf("failed to read %s: %d (%d errors suppressed since last log)", l.name, result, l.suppressed)
	} else {
		log.Errorf("failed to read %s: %d", l.name, result)
	}

	l.lastResult = result
	l.lastLog = now
	l.suppressed = 0
	return true
}
