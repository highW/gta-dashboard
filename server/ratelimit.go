package main

import (
	"context"
	"log"
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
)

// ---------------------------------------------------------------------
// Rate limiting
//
// Each client may send rateLimitPerSecond requests per second on average,
// with short bursts of up to rateLimitBurst.
//
// The count lives in Redis, so it is shared: with two API instances behind
// the load balancer a client still gets ONE allowance, not one per instance.
// If Redis cannot answer, each instance falls back to counting in its own
// memory, so an outage weakens the limit instead of removing it or taking
// the API down.
// ---------------------------------------------------------------------

const (
	rateLimitPerSecond = 5
	rateLimitBurst     = 10

	// How long one request may wait for Redis before using the local
	// limiter, and how long Redis is then left alone before the next try.
	limiterRedisTimeout = 250 * time.Millisecond
	limiterRetryEvery   = 5 * time.Second
)

// The shared limiter. One Redis key per client holds the time at which
// that client's allowance is fully used up ("theoretical arrival time",
// the GCRA form of a token bucket). Running it as a script makes the
// read-decide-write step atomic, and using Redis's own clock means the API
// instances never disagree about what time it is.
//
//	KEYS[1]  the client's key
//	ARGV[1]  milliseconds between requests at the steady rate
//	ARGV[2]  burst size
//
// Returns 0 when the request is allowed, otherwise how many milliseconds
// the client has to wait.
var rateLimitScript = redis.NewScript(`
local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
local interval = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])

local tat = tonumber(redis.call('GET', KEYS[1]))
if not tat or tat < now then
  tat = now
end

local new_tat = tat + interval
local allow_at = new_tat - interval * burst
if allow_at > now then
  return allow_at - now
end

redis.call('SET', KEYS[1], string.format('%.0f', new_tat), 'PX', new_tat - now)
return 0
`)

// limiterSkipRedisUntil is 0 while Redis is healthy. After a failure it
// holds the time (Unix nanoseconds) before which Redis is not tried again,
// so a broken Redis costs one slow request every few seconds, not every one.
var limiterSkipRedisUntil atomic.Int64

// allowShared asks the limiter in Redis. ok is false when Redis could not
// answer; the caller then uses this instance's own limiter.
func allowShared(bucket string) (wait time.Duration, ok bool) {
	now := time.Now().UnixNano()
	skipUntil := limiterSkipRedisUntil.Load()
	if now < skipUntil {
		return 0, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), limiterRedisTimeout)
	defer cancel()
	ms, err := rateLimitScript.Run(ctx, cache.client,
		[]string{cache.prefix + "ratelimit:" + bucket},
		1000/rateLimitPerSecond, rateLimitBurst).Int64()
	if err != nil {
		if limiterSkipRedisUntil.Swap(now+int64(limiterRetryEvery)) == 0 {
			log.Printf("rate limiter: Redis is not answering (%v); each API instance limits on its own until it is back", err)
		}
		return 0, false
	}
	if skipUntil != 0 && limiterSkipRedisUntil.CompareAndSwap(skipUntil, 0) {
		log.Println("rate limiter: Redis is back; the limit is shared between instances again")
	}
	return time.Duration(ms) * time.Millisecond, true
}

// The local fallback: one in-memory limiter per client, forgotten after a
// few idle minutes.
type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

var (
	visitors   = make(map[string]*visitor)
	visitorsMu sync.Mutex
)

func getVisitor(bucket string) *rate.Limiter {
	visitorsMu.Lock()
	defer visitorsMu.Unlock()

	v, exists := visitors[bucket]
	if !exists {
		limiter := rate.NewLimiter(rateLimitPerSecond, rateLimitBurst)
		visitors[bucket] = &visitor{limiter: limiter, lastSeen: time.Now()}
		return limiter
	}
	v.lastSeen = time.Now()
	return v.limiter
}

func cleanupVisitors() {
	for {
		time.Sleep(time.Minute)
		visitorsMu.Lock()
		for bucket, v := range visitors {
			if time.Since(v.lastSeen) > 3*time.Minute {
				delete(visitors, bucket)
			}
		}
		visitorsMu.Unlock()
	}
}

// limiterBucket is what a client's requests are counted against: the
// address itself for IPv4, the surrounding /64 for IPv6. One IPv6 customer
// normally controls a whole /64, so counting single addresses would let
// them dodge the limit by using a new address for every request.
func limiterBucket(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil || addr.Is4() {
		return ip
	}
	prefix, err := addr.Prefix(64)
	if err != nil {
		return ip
	}
	return prefix.String()
}

// allowRequest reports whether a request from this client may proceed, and
// if not, how long the client should wait before trying again.
func allowRequest(ip string) (allowed bool, retryAfter time.Duration) {
	bucket := limiterBucket(ip)

	if wait, ok := allowShared(bucket); ok {
		return wait == 0, wait
	}
	if getVisitor(bucket).Allow() {
		return true, 0
	}
	return false, time.Second / rateLimitPerSecond
}

func rateLimit(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		allowed, retryAfter := allowRequest(clientIP(r))
		if !allowed {
			seconds := int(math.Ceil(retryAfter.Seconds()))
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			http.Error(w, "Too many requests", http.StatusTooManyRequests)
			return
		}

		h(w, r)
	}
}
