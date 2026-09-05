package middlewares

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type byoaRateEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type byoaIPRateLimiter struct {
	mu        sync.Mutex
	entries   map[string]*byoaRateEntry
	rate      rate.Limit
	burst     int
	lastSweep time.Time
}

func newBYOAIPRateLimiter(r rate.Limit, burst int) *byoaIPRateLimiter {
	return &byoaIPRateLimiter{
		entries:   make(map[string]*byoaRateEntry),
		rate:      r,
		burst:     burst,
		lastSweep: time.Now(),
	}
}

func (l *byoaIPRateLimiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 定期淘汰长期不活跃 IP，避免公网扫描让内存表无限增长。
	if now.Sub(l.lastSweep) >= 5*time.Minute || len(l.entries) > 10000 {
		cutoff := now.Add(-10 * time.Minute)
		for key, entry := range l.entries {
			if entry.lastSeen.Before(cutoff) {
				delete(l.entries, key)
			}
		}
		l.lastSweep = now
	}

	entry := l.entries[ip]
	if entry == nil {
		entry = &byoaRateEntry{limiter: rate.NewLimiter(l.rate, l.burst)}
		l.entries[ip] = entry
	}
	entry.lastSeen = now
	return entry.limiter.AllowN(now, 1)
}

func byoaRateLimitMiddleware(limiter *byoaIPRateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if ip == "" {
			ip = "unknown"
		}
		if !limiter.allow(ip, time.Now()) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code":    http.StatusTooManyRequests,
				"message": "扫码请求过于频繁，请稍后重试",
				"data":    nil,
			})
			return
		}
		c.Next()
	}
}

// BYOAQRStartRateLimit 限制创建二维码。持续约 1 次/秒，允许短时 burst 5。
// 仅使用当前进程内存，不引入 Redis、Session 或用户数据库。
func BYOAQRStartRateLimit() gin.HandlerFunc {
	return byoaRateLimitMiddleware(newBYOAIPRateLimiter(rate.Every(time.Second), 5))
}

// BYOAQRStatusRateLimit 允许正常扫码轮询，持续约 5 次/秒，允许短时 burst 20。
func BYOAQRStatusRateLimit() gin.HandlerFunc {
	return byoaRateLimitMiddleware(newBYOAIPRateLimiter(rate.Every(200*time.Millisecond), 20))
}
