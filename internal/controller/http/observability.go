package http

import (
	"agentx/server/internal/job"
	"context"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type requestMetrics struct {
	total          atomic.Uint64
	failures       atomic.Uint64
	inFlight       atomic.Int64
	uploadFailures atomic.Uint64
	driftReports   atomic.Uint64
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
		route := c.FullPath()
		if c.Request.Method == http.MethodPost && strings.HasSuffix(route, "/packages/:name/releases") && c.Writer.Status() >= 400 {
			metrics.uploadFailures.Add(1)
		}
		if c.Request.Method == http.MethodGet && strings.HasSuffix(route, "/drift") && c.Writer.Status() < 400 {
			metrics.driftReports.Add(1)
		}
	}
}

type rateLimiter struct {
	mu     sync.Mutex
	window time.Time
	counts map[string]int
	limit  int
}

type RequestRateLimiter interface {
	Allow(context.Context, string) (bool, time.Duration, error)
}

func newRateLimiter(limit int) *rateLimiter {
	return &rateLimiter{counts: map[string]int{}, limit: limit}
}
func (l *rateLimiter) Allow(_ context.Context, key string) (bool, time.Duration, error) {
	if l.limit <= 0 {
		return true, 0, nil
	}
	now := time.Now()
	l.mu.Lock()
	if l.window.IsZero() || now.Sub(l.window) >= time.Minute {
		l.window = now
		l.counts = map[string]int{}
	}
	l.counts[key]++
	count := l.counts[key]
	retryAfter := time.Until(l.window.Add(time.Minute))
	l.mu.Unlock()
	return count <= l.limit, retryAfter, nil
}

func rateLimitMiddleware(limiter RequestRateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/healthz" || c.Request.URL.Path == "/readyz" || c.Request.URL.Path == "/metrics" {
			c.Next()
			return
		}
		allowed, retryAfter, err := limiter.Allow(c.Request.Context(), c.ClientIP())
		if err != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, errorEnvelope(c, "RATE_LIMIT_UNAVAILABLE", "request rate limiter is unavailable"))
			return
		}
		if !allowed {
			seconds := int(retryAfter.Round(time.Second) / time.Second)
			if seconds < 1 {
				seconds = 1
			}
			c.Header("Retry-After", strconv.Itoa(seconds))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, errorEnvelope(c, "RATE_LIMITED", "request rate limit exceeded"))
			return
		}
		c.Next()
	}
}

var _ RequestRateLimiter = (*rateLimiter)(nil)

func registerMetricsRoute(r *gin.Engine, workerMetrics *job.Metrics) {
	r.GET("/metrics", func(c *gin.Context) {
		worker := workerMetrics.Snapshot()
		c.Header("Content-Type", "text/plain; version=0.0.4")
		c.String(http.StatusOK, "agentx_http_requests_total %d\nagentx_http_request_failures_total %d\nagentx_http_in_flight %s\nagentx_artifact_upload_failures_total %d\nagentx_drift_reports_total %d\nagentx_worker_run_failures_total %d\nagentx_worker_handler_failures_total %d\nagentx_worker_retries_total %d\nagentx_worker_dead_letters_total %d\nagentx_worker_repository_failures_total %d\n", metrics.total.Load(), metrics.failures.Load(), strconv.FormatInt(metrics.inFlight.Load(), 10), metrics.uploadFailures.Load(), metrics.driftReports.Load(), worker.RunFailures, worker.HandlerFailures, worker.Retries, worker.DeadLetters, worker.RepositoryFailures)
	})
}
