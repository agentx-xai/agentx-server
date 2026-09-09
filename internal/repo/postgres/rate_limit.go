package postgres

import (
	"context"
	"sync/atomic"
	"time"
)

type RateLimiter struct {
	store       *Store
	limit       int
	lastCleanup atomic.Int64
}

func NewRateLimiter(store *Store, limit int) *RateLimiter {
	limiter := &RateLimiter{store: store, limit: limit}
	limiter.lastCleanup.Store(time.Now().Unix())
	return limiter
}

func (l *RateLimiter) Allow(ctx context.Context, identity string) (bool, time.Duration, error) {
	var count int64
	var windowStart time.Time
	err := l.store.Pool.QueryRow(ctx, `
WITH current_window AS (SELECT date_trunc('minute', clock_timestamp()) AS started),
incremented AS (
    INSERT INTO rate_limit_windows(identity,window_start,request_count)
    SELECT $1,started,1 FROM current_window
    ON CONFLICT(identity,window_start)
    DO UPDATE SET request_count=rate_limit_windows.request_count+1
    RETURNING request_count,window_start
)
SELECT request_count,window_start FROM incremented`, identity).Scan(&count, &windowStart)
	if err != nil {
		return false, 0, err
	}
	nowUnix := time.Now().Unix()
	lastCleanup := l.lastCleanup.Load()
	if nowUnix-lastCleanup >= int64(time.Hour/time.Second) && l.lastCleanup.CompareAndSwap(lastCleanup, nowUnix) {
		_, _ = l.store.Pool.Exec(ctx, `DELETE FROM rate_limit_windows WHERE window_start < clock_timestamp()-interval '2 hours'`)
	}
	retryAfter := time.Until(windowStart.Add(time.Minute))
	if retryAfter < time.Second {
		retryAfter = time.Second
	}
	return count <= int64(l.limit), retryAfter, nil
}
