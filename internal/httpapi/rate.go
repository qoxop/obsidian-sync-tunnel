package httpapi

import (
	"math"
	"sync"
	"time"
)

type rateBucket struct {
	windowStart time.Time
	requests    int
	bytes       int64
}

type rateLimiter struct {
	mu            sync.Mutex
	requestsLimit int
	bytesLimit    int64
	buckets       map[string]rateBucket
}

func newRateLimiter(requests int, bytes int64) *rateLimiter {
	return &rateLimiter{requestsLimit: requests, bytesLimit: bytes, buckets: make(map[string]rateBucket)}
}

func (l *rateLimiter) allow(key string, bytes int64) (bool, int) {
	return l.consume(key, 1, bytes)
}

func (l *rateLimiter) chargeBytes(key string, bytes int64) (bool, int) {
	return l.consume(key, 0, bytes)
}

func (l *rateLimiter) consume(key string, requests int, bytes int64) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	bucket := l.buckets[key]
	if bucket.windowStart.IsZero() || now.Sub(bucket.windowStart) >= time.Minute {
		bucket = rateBucket{windowStart: now}
	}
	if bucket.requests+requests > l.requestsLimit || bucket.bytes+bytes > l.bytesLimit {
		return false, int(math.Max(1, math.Ceil(time.Until(bucket.windowStart.Add(time.Minute)).Seconds())))
	}
	bucket.requests += requests
	bucket.bytes += bytes
	l.buckets[key] = bucket
	return true, 0
}
