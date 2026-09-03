package middlewares

import (
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestBYOAIPRateLimiterBurstAndRefill(t *testing.T) {
	limiter := newBYOAIPRateLimiter(rate.Every(time.Second), 2)
	now := time.Unix(1000, 0)

	if !limiter.allow("192.0.2.1", now) || !limiter.allow("192.0.2.1", now) {
		t.Fatal("initial burst should be allowed")
	}
	if limiter.allow("192.0.2.1", now) {
		t.Fatal("request beyond burst should be limited")
	}
	if !limiter.allow("198.51.100.2", now) {
		t.Fatal("different IP should have an independent limiter")
	}
	if !limiter.allow("192.0.2.1", now.Add(time.Second)) {
		t.Fatal("limiter should refill after one interval")
	}
}
