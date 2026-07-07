package middleware

import (
	"context"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/platform/metrics"
)

type endpointGroup string

const (
	groupRead    endpointGroup = "read"
	groupWrite   endpointGroup = "write"
	groupTrigger endpointGroup = "trigger"
	groupAdmin   endpointGroup = "admin"
	groupPublic  endpointGroup = "public"
)

type groupConfig struct {
	rps   int
	burst int
}

var defaultLimits = map[endpointGroup]groupConfig{
	groupRead:    {rps: 100, burst: 100},
	groupWrite:   {rps: 10, burst: 10},
	groupTrigger: {rps: 5, burst: 5},
	groupAdmin:   {rps: 5, burst: 5},
}

// RateLimiter implements per-tenant, per-endpoint-group token bucket rate limiting.
type RateLimiter struct {
	limits  map[endpointGroup]groupConfig
	mu      sync.Mutex
	buckets map[endpointGroup]map[string]*tokenBucket
}

type tokenBucket struct {
	tokens   float64
	lastTime time.Time
	lastUsed time.Time
}

func NewRateLimiter(ctx context.Context) *RateLimiter {
	limits := make(map[endpointGroup]groupConfig, len(defaultLimits))
	for g, c := range defaultLimits {
		limits[g] = groupConfig{
			rps:   loadEnvInt(groupEnvKey(g), c.rps),
			burst: loadEnvInt(groupEnvKey(g), c.burst),
		}
	}
	rl := &RateLimiter{
		limits:  limits,
		buckets: make(map[endpointGroup]map[string]*tokenBucket),
	}
	go rl.reapLoop(ctx, 30*time.Minute)
	return rl
}

func groupEnvKey(g endpointGroup) string {
	switch g {
	case groupRead:
		return "RATELIMIT_READ_RPS"
	case groupWrite:
		return "RATELIMIT_WRITE_RPS"
	case groupTrigger:
		return "RATELIMIT_TRIGGER_RPS"
	case groupAdmin:
		return "RATELIMIT_ADMIN_RPS"
	default:
		return ""
	}
}

func loadEnvInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 1 {
		return fallback
	}
	return v
}

// Middleware returns a gin middleware that enforces per-tenant rate limits.
func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		group := classifyEndpoint(c.Request.Method, c.FullPath())
		if group == groupPublic {
			c.Next()
			return
		}

		tenantID := GetTenantID(c)
		cfg := rl.limits[group]

		if !rl.tryAllow(group, tenantID, cfg) {
			metrics.RateLimitHits.WithLabelValues(tenantID, string(group)).Inc()
			apperror.Write(c, 429, apperror.APIError{
				Code:    apperror.CodeRateLimited,
				Message: "rate limit exceeded",
			})
			return
		}

		metrics.RateLimitPassed.WithLabelValues(tenantID, string(group)).Inc()
		c.Next()
	}
}

func classifyEndpoint(method, path string) endpointGroup {
	switch path {
	case "/healthz", "/openapi.json", "/metrics":
		return groupPublic
	case "/api/v1/jobs/:id/trigger":
		return groupTrigger
	case "/api/v1/instances/:run_id/cancel":
		return groupAdmin
	}

	if method == "GET" || method == "HEAD" {
		return groupRead
	}
	return groupWrite
}

func (rl *RateLimiter) tryAllow(group endpointGroup, tenantID string, cfg groupConfig) bool {
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	groupBuckets, ok := rl.buckets[group]
	if !ok {
		groupBuckets = make(map[string]*tokenBucket)
		rl.buckets[group] = groupBuckets
	}

	bucket, ok := groupBuckets[tenantID]
	if !ok {
		bucket = &tokenBucket{
			tokens:   float64(cfg.burst),
			lastTime: now,
		}
		groupBuckets[tenantID] = bucket
	}

	elapsed := now.Sub(bucket.lastTime).Seconds()
	bucket.tokens += elapsed * float64(cfg.rps)
	if bucket.tokens > float64(cfg.burst) {
		bucket.tokens = float64(cfg.burst)
	}
	bucket.lastTime = now
	bucket.lastUsed = now

	if bucket.tokens >= 1.0 {
		bucket.tokens--
		return true
	}
	return false
}

func (rl *RateLimiter) reapLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rl.mu.Lock()
			cutoff := time.Now().Add(-interval)
			for _, groupBuckets := range rl.buckets {
				for tid, entry := range groupBuckets {
					if entry.lastUsed.Before(cutoff) {
						delete(groupBuckets, tid)
					}
				}
			}
			rl.mu.Unlock()
		}
	}
}
