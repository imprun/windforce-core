package server

import (
	"math"
	"sync"
	"time"
)

const defaultPublicAPIRPS = 100

type requestRateLimiter struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func newRequestRateLimiter(rate float64, burst int) *requestRateLimiter {
	if rate <= 0 {
		rate = defaultPublicAPIRPS
	}
	if burst <= 0 {
		burst = int(math.Ceil(rate))
	}
	return &requestRateLimiter{rate: rate, burst: float64(burst), tokens: float64(burst)}
}

func (l *requestRateLimiter) Allow(now time.Time) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.last.IsZero() {
		l.last = now
	} else if now.After(l.last) {
		l.tokens = math.Min(l.burst, l.tokens+now.Sub(l.last).Seconds()*l.rate)
		l.last = now
	}
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}
