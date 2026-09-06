package http

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type requestMetrics struct {
	total    atomic.Uint64
	failures atomic.Uint64
	inFlight atomic.Int64
}

var metrics requestMetrics

func metricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		metrics.total.Add(1)
		metrics.inFlight.Add(1)
		defer metrics.inFlight.Add(-1)
		c.Next()
		if c.Writer.Status() >= 500 {
			metrics.failures.Add(1)
		}
	}
}

type rateLimiter struct {
	mu     sync.Mutex
	window time.Time
	counts map[string]int
	limit  int
}

func newRateLimiter(limit int) *rateLimiter {
	return &rateLimiter{counts: map[string]int{}, limit: limit}
}
func (l *rateLimiter) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if l.limit <= 0 {
			c.Next()
			return
		}
		key := c.ClientIP()
		now := time.Now()
		l.mu.Lock()
		if l.window.IsZero() || now.Sub(l.window) >= time.Minute {
			l.window = now
			l.counts = map[string]int{}
		}
		l.counts[key]++
		count := l.counts[key]
		l.mu.Unlock()
		if count > l.limit {
			c.Header("Retry-After", "60")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, errorEnvelope(c, "RATE_LIMITED", "request rate limit exceeded"))
			return
		}
		c.Next()
	}
}

func registerMetricsRoute(r *gin.Engine) {
	r.GET("/metrics", func(c *gin.Context) {
		c.Header("Content-Type", "text/plain; version=0.0.4")
		c.String(http.StatusOK, "agentx_http_requests_total %d\nagentx_http_request_failures_total %d\nagentx_http_in_flight %s\n", metrics.total.Load(), metrics.failures.Load(), strconv.FormatInt(metrics.inFlight.Load(), 10))
	})
}
