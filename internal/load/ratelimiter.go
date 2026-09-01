package load

import (
	"context"
	mathrand "math/rand"
	"sync"
	"time"
)

// RateLimiter modulates the execution rate of workers between minRate and maxRate ops/sec.
type RateLimiter struct {
	minRate float64
	maxRate float64

	mu         sync.Mutex
	curRate    float64
	tokens     float64
	maxTokens  float64
	lastRefill time.Time
	rng        *mathrand.Rand

	stopCh chan struct{}
}

// NewRateLimiter creates a new rate limiter with min and max ops/sec.
func NewRateLimiter(minRate, maxRate float64) *RateLimiter {
	if minRate <= 0 && maxRate > 0 {
		minRate = maxRate
	}
	if maxRate < minRate && maxRate > 0 {
		maxRate = minRate
	}
	if minRate < 0 {
		minRate = 0
	}
	if maxRate < 0 {
		maxRate = 0
	}

	initialRate := minRate
	if maxRate > minRate {
		initialRate = (minRate + maxRate) / 2.0
	}

	burst := initialRate
	if burst < 1.0 {
		burst = 1.0
	}
	if burst > 100.0 {
		burst = 100.0
	}

	rl := &RateLimiter{
		minRate:    minRate,
		maxRate:    maxRate,
		curRate:    initialRate,
		tokens:     burst,
		maxTokens:  burst,
		lastRefill: time.Now(),
		rng:        mathrand.New(mathrand.NewSource(time.Now().UnixNano())),
		stopCh:     make(chan struct{}),
	}

	// If rate varies, start background rate modulator
	if maxRate > minRate {
		go rl.modulateRate()
	}

	return rl
}

// Stop stops any background modulation goroutines.
func (rl *RateLimiter) Stop() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	select {
	case <-rl.stopCh:
		// already closed
	default:
		close(rl.stopCh)
	}
}

// CurrentTargetRate returns the current instantaneous target operations per second.
func (rl *RateLimiter) CurrentTargetRate() float64 {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.curRate
}

// Wait blocks until a token is available or context is cancelled.
func (rl *RateLimiter) Wait(ctx context.Context) error {
	if rl.minRate <= 0 && rl.maxRate <= 0 {
		// Unlimited rate
		return ctx.Err()
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		rl.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(rl.lastRefill).Seconds()
		rl.lastRefill = now

		// Refill tokens
		rl.tokens += elapsed * rl.curRate
		if rl.tokens > rl.maxTokens {
			rl.tokens = rl.maxTokens
		}

		if rl.tokens >= 1.0 {
			rl.tokens -= 1.0
			rl.mu.Unlock()
			return nil
		}

		// Calculate sleep time needed for 1 token
		needed := 1.0 - rl.tokens
		sleepSec := needed / rl.curRate
		if sleepSec < 0.001 {
			sleepSec = 0.001
		}
		rl.mu.Unlock()

		sleepDur := time.Duration(sleepSec * float64(time.Second))
		if sleepDur > 50*time.Millisecond {
			sleepDur = 50 * time.Millisecond
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleepDur):
		}
	}
}

// modulateRate smoothly varies the rate between minRate and maxRate over time.
func (rl *RateLimiter) modulateRate() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rl.stopCh:
			return
		case <-ticker.C:
			rl.mu.Lock()
			// Generate random target rate within bounds
			target := rl.minRate + rl.rng.Float64()*(rl.maxRate-rl.minRate)
			// Smooth transition (move 40% toward target per second)
			rl.curRate = rl.curRate*0.6 + target*0.4
			rl.maxTokens = rl.curRate
			if rl.maxTokens < 1.0 {
				rl.maxTokens = 1.0
			}
			if rl.maxTokens > 100.0 {
				rl.maxTokens = 100.0
			}
			rl.mu.Unlock()
		}
	}
}
