package circuitbreaker

import (
	"sync"
	"time"
)

// bucket holds counts for a single time slice of the sliding window.
type bucket struct {
	failures uint32
	total    uint32
	ts       time.Time
}

// slidingWindow tracks error rate over a rolling time window using fixed buckets.
type slidingWindow struct {
	mu         sync.Mutex
	buckets    []bucket
	numBuckets int
	windowSize time.Duration
	bucketSize time.Duration
	threshold  float64 // 0.0–1.0
}

func newSlidingWindow(windowSize time.Duration, numBuckets int, threshold float64) *slidingWindow {
	return &slidingWindow{
		buckets:    make([]bucket, numBuckets),
		numBuckets: numBuckets,
		windowSize: windowSize,
		bucketSize: windowSize / time.Duration(numBuckets),
		threshold:  threshold,
	}
}

func (w *slidingWindow) currentBucketIndex(now time.Time) int {
	return int(now.UnixNano()/int64(w.bucketSize)) % w.numBuckets
}

func (w *slidingWindow) record(now time.Time, failure bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	idx := w.currentBucketIndex(now)
	b := &w.buckets[idx]

	// reset bucket if it belongs to a previous window cycle
	if now.Sub(b.ts) >= w.bucketSize {
		b.failures = 0
		b.total = 0
		b.ts = now
	}

	b.total++
	if failure {
		b.failures++
	}
}

// errorRate returns the error rate across all active buckets.
func (w *slidingWindow) errorRate(now time.Time) float64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	var totalFailures, totalRequests uint32
	cutoff := now.Add(-w.windowSize)

	for i := range w.buckets {
		b := &w.buckets[i]
		if b.ts.IsZero() || b.ts.Before(cutoff) {
			continue
		}
		totalFailures += b.failures
		totalRequests += b.total
	}

	if totalRequests == 0 {
		return 0
	}
	return float64(totalFailures) / float64(totalRequests)
}

func (w *slidingWindow) shouldOpen(now time.Time) bool {
	return w.errorRate(now) >= w.threshold
}

func (w *slidingWindow) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buckets = make([]bucket, w.numBuckets)
}
