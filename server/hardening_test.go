package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// ---------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------

// resetLimiter forgets everything the in-memory limiter knows, as if this
// were a freshly started API instance, and marks Redis as healthy.
func resetLimiter() {
	visitorsMu.Lock()
	visitors = make(map[string]*visitor)
	visitorsMu.Unlock()
	limiterSkipRedisUntil.Store(0)
}

// setupRedis is for tests that need Redis but no database.
func setupRedis(t *testing.T) {
	t.Helper()
	c := NewCache(3 * time.Second)
	if err := c.client.Ping(context.Background()).Err(); err != nil {
		c.client.Close()
		t.Skipf("Redis is not reachable: %v", err)
	}
	c.prefix = fmt.Sprintf("test:%d:", time.Now().UnixNano())

	previous := cache
	cache = c
	resetLimiter()
	t.Cleanup(func() {
		c.client.Close()
		cache = previous
		resetLimiter()
	})
}

func trustProxies(t *testing.T, list string) {
	t.Helper()
	previous := trustedProxies
	trustedProxies = mustParseProxies(list)
	t.Cleanup(func() { trustedProxies = previous })
}

// reqFrom builds a request as it arrives from a given TCP peer, optionally
// carrying an X-Forwarded-For header.
func reqFrom(remoteAddr, forwardedFor string) *http.Request {
	req := httptest.NewRequest("GET", "/api/players", nil)
	req.RemoteAddr = remoteAddr
	if forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", forwardedFor)
	}
	return req
}

// burst sends n requests in a row and returns how many were allowed and how
// many requests the steady rate could have added back meanwhile (so the
// assertions stay exact even on a slow machine).
func burst(n int, send func() bool) (allowed, refilled int) {
	start := time.Now()
	for i := 0; i < n; i++ {
		if send() {
			allowed++
		}
	}
	refilled = int(time.Since(start)/(time.Second/rateLimitPerSecond)) + 1
	return allowed, refilled
}

func listArrests(t *testing.T, serverID int, q string) ([]ArrestRecord, *httptest.ResponseRecorder) {
	t.Helper()
	path := "/api/arrests"
	if q != "" {
		path += "?q=" + url.QueryEscape(q)
	}
	w := httptest.NewRecorder()
	arrestsHandler(w, withServer(newReq("GET", path, nil, viewerKey), serverID))
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d (%s)", path, w.Code, w.Body.String())
	}
	var out []ArrestRecord
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode arrests: %v", err)
	}
	return out, w
}

func listWarrants(t *testing.T, serverID int, q string) []Warrant {
	t.Helper()
	path := "/api/warrants"
	if q != "" {
		path += "?q=" + url.QueryEscape(q)
	}
	w := httptest.NewRecorder()
	warrantsHandler(w, withServer(newReq("GET", path, nil, viewerKey), serverID))
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d (%s)", path, w.Code, w.Body.String())
	}
	var out []Warrant
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode warrants: %v", err)
	}
	return out
}

func addArrest(t *testing.T, serverID, playerID int, charge string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO arrests (server_id, player_id, charge, officer, date, status)
		VALUES (?, ?, ?, 'Officer Test', '2026-01-01', 'pending')`, serverID, playerID, charge)
	if err != nil {
		t.Fatalf("add arrest: %v", err)
	}
}

func columnType(t *testing.T, table, column string) string {
	t.Helper()
	var dataType string
	err := db.QueryRow(`SELECT DATA_TYPE FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`, table, column).Scan(&dataType)
	if err != nil {
		t.Fatalf("column type of %s.%s: %v", table, column, err)
	}
	return strings.ToLower(dataType)
}

// ---------------------------------------------------------------------
// The client's real address (fix: behind the load balancer every request
// looked like it came from the load balancer)
// ---------------------------------------------------------------------

func TestClientIP(t *testing.T) {
	trustProxies(t, "127.0.0.0/8, ::1, 10.0.0.0/8")

	cases := []struct {
		name         string
		remoteAddr   string
		forwardedFor string
		want         string
	}{
		{"direct client, no header", "203.0.113.9:4000", "", "203.0.113.9"},
		{"direct client can't forge its address", "203.0.113.9:4000", "6.6.6.6", "203.0.113.9"},
		{"through our proxy", "127.0.0.1:4000", "203.0.113.9", "203.0.113.9"},
		{"forged entry in front of the real one is ignored", "127.0.0.1:4000", "6.6.6.6, 203.0.113.9", "203.0.113.9"},
		{"two forged entries", "127.0.0.1:4000", "6.6.6.6, 7.7.7.7, 203.0.113.9", "203.0.113.9"},
		{"two of our proxies in a row", "127.0.0.1:4000", "203.0.113.9, 10.0.0.5", "203.0.113.9"},
		{"forged entry, then two of our proxies", "127.0.0.1:4000", "6.6.6.6, 203.0.113.9, 10.0.0.5", "203.0.113.9"},
		{"proxy sent no header", "127.0.0.1:4000", "", "127.0.0.1"},
		{"request made on the proxy machine itself", "127.0.0.1:4000", "127.0.0.1", "127.0.0.1"},
		{"header is not an address", "127.0.0.1:4000", "definitely-not-an-ip", "127.0.0.1"},
		{"junk after the real address", "127.0.0.1:4000", "203.0.113.9, junk", "127.0.0.1"},
		{"IPv6 proxy and client", "[::1]:4000", "2001:db8::7", "2001:db8::7"},
		{"IPv4 written as IPv6", "127.0.0.1:4000", "::ffff:203.0.113.9", "203.0.113.9"},
		{"entry with a port", "127.0.0.1:4000", "203.0.113.9:51000", "203.0.113.9"},
		{"IPv6 entry with a port", "127.0.0.1:4000", "[2001:db8::7]:51000", "2001:db8::7"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := clientIP(reqFrom(c.remoteAddr, c.forwardedFor)); got != c.want {
				t.Errorf("clientIP = %q, want %q", got, c.want)
			}
		})
	}

	t.Run("header split over two lines", func(t *testing.T) {
		req := reqFrom("127.0.0.1:4000", "")
		req.Header.Add("X-Forwarded-For", "6.6.6.6")
		req.Header.Add("X-Forwarded-For", "203.0.113.9")
		if got := clientIP(req); got != "203.0.113.9" {
			t.Errorf("clientIP = %q, want 203.0.113.9", got)
		}
	})
}

func TestClientIP_TrustNobody(t *testing.T) {
	trustProxies(t, "none")

	if got := clientIP(reqFrom("127.0.0.1:4000", "203.0.113.9")); got != "127.0.0.1" {
		t.Errorf("with no trusted proxies the header must be ignored, got %q", got)
	}
}

func TestParseProxies(t *testing.T) {
	prefixes, err := parseProxies(" 127.0.0.1, 10.0.0.0/8 ,, ::1 ")
	if err != nil {
		t.Fatalf("valid list rejected: %v", err)
	}
	if len(prefixes) != 3 {
		t.Fatalf("expected 3 entries, got %v", prefixes)
	}
	if !prefixes[1].Contains(netip.MustParseAddr("10.200.3.4")) {
		t.Errorf("10.0.0.0/8 should contain 10.200.3.4")
	}
	if prefixes[0].Contains(netip.MustParseAddr("127.0.0.2")) {
		t.Errorf("a single address must not match its neighbours")
	}

	for _, bad := range []string{"localhost", "10.0.0.0/99", "1.2.3", "10.0.0.1-10.0.0.9"} {
		if _, err := parseProxies(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

// ---------------------------------------------------------------------
// Rate limiting (fix: the count lived in each instance's memory)
// ---------------------------------------------------------------------

func TestLimiterBucket(t *testing.T) {
	if got := limiterBucket("203.0.113.9"); got != "203.0.113.9" {
		t.Errorf("IPv4 is counted per address, got %q", got)
	}
	a := limiterBucket("2001:db8:1:2:aaaa::1")
	b := limiterBucket("2001:db8:1:2:bbbb::2")
	other := limiterBucket("2001:db8:1:3::1")
	if a != b {
		t.Errorf("two addresses in one /64 must share a bucket: %q vs %q", a, b)
	}
	if a == other {
		t.Errorf("a different /64 must get its own bucket, both were %q", a)
	}
}

func TestRateLimit_BurstThenSteadyRate(t *testing.T) {
	setupRedis(t)
	const ip = "203.0.113.10"

	allowed, refilled := burst(3*rateLimitBurst, func() bool {
		ok, _ := allowRequest(ip)
		return ok
	})
	if allowed < rateLimitBurst || allowed > rateLimitBurst+refilled {
		t.Fatalf("expected about %d of %d requests to pass, got %d", rateLimitBurst, 3*rateLimitBurst, allowed)
	}

	ok, retryAfter := allowRequest(ip)
	if ok || retryAfter <= 0 {
		t.Fatalf("an exhausted client must be refused and told how long to wait, got ok=%v wait=%v", ok, retryAfter)
	}

	// The steady rate hands back one request every 1/rateLimitPerSecond.
	time.Sleep(time.Second/rateLimitPerSecond + 100*time.Millisecond)
	if ok, _ := allowRequest(ip); !ok {
		t.Errorf("after waiting one interval a request should pass again")
	}
}

// The reason the limiter moved to Redis: two API instances behind the load
// balancer used to give every client two separate allowances.
func TestRateLimit_SharedBetweenInstances(t *testing.T) {
	setupRedis(t)
	const ip = "203.0.113.20"

	// "Instance A" uses up the client's burst.
	allowed, _ := burst(3*rateLimitBurst, func() bool {
		ok, _ := allowRequest(ip)
		return ok
	})
	if allowed < rateLimitBurst {
		t.Fatalf("instance A: expected the burst of %d to pass, got %d", rateLimitBurst, allowed)
	}

	// "Instance B": its own Redis connection and empty memory, same Redis.
	instanceB := NewCache(3 * time.Second)
	instanceB.prefix = cache.prefix
	t.Cleanup(func() { instanceB.client.Close() })
	cache = instanceB
	resetLimiter()

	allowedOnB, refilled := burst(rateLimitBurst, func() bool {
		ok, _ := allowRequest(ip)
		return ok
	})
	if allowedOnB > refilled {
		t.Errorf("instance B let %d more requests through; the allowance must be shared, not per instance", allowedOnB)
	}
}

// The original bug: behind the proxy, everyone shared the proxy's bucket.
func TestRateLimit_PerRealClientBehindProxy(t *testing.T) {
	setupRedis(t)
	trustProxies(t, "127.0.0.1")
	h := rateLimit(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	send := func(remoteAddr, forwardedFor string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h(w, reqFrom(remoteAddr, forwardedFor))
		return w
	}

	// One client hammers the API through the proxy until it is refused.
	var refused *httptest.ResponseRecorder
	for i := 0; i < 3*rateLimitBurst && refused == nil; i++ {
		if w := send("127.0.0.1:4000", "203.0.113.31"); w.Code == http.StatusTooManyRequests {
			refused = w
		}
	}
	if refused == nil {
		t.Fatal("the noisy client was never rate limited")
	}
	if refused.Header().Get("Retry-After") == "" {
		t.Error("a 429 should say when to retry (Retry-After header)")
	}

	// A different client behind the same proxy is unaffected.
	if w := send("127.0.0.1:4000", "203.0.113.32"); w.Code != http.StatusOK {
		t.Errorf("another client behind the same proxy got %d; it must have its own allowance", w.Code)
	}

	// Sending a made-up header does not buy a fresh allowance: the proxy
	// appends the real address, and that is the one that counts.
	if w := send("127.0.0.1:4000", "9.9.9.9, 203.0.113.31"); w.Code != http.StatusTooManyRequests {
		t.Errorf("a forged X-Forwarded-For entry got the refused client through (%d)", w.Code)
	}

	// Nor does it help a client that connects directly.
	var directRefused bool
	for i := 0; i < 3*rateLimitBurst; i++ {
		if w := send("198.51.100.50:4000", fmt.Sprintf("10.9.9.%d", i)); w.Code == http.StatusTooManyRequests {
			directRefused = true
			break
		}
	}
	if !directRefused {
		t.Error("a direct client rotating X-Forwarded-For values was never rate limited")
	}
}

// The frontend waits for Retry-After and tries again, which only works if
// the browser is allowed to read that header from another origin.
func TestRateLimit_RetryAfterIsReadableByTheFrontend(t *testing.T) {
	setupRedis(t)
	h := withCORS(rateLimit(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	var refused *httptest.ResponseRecorder
	for i := 0; i < 3*rateLimitBurst && refused == nil; i++ {
		w := httptest.NewRecorder()
		h(w, reqFrom("203.0.113.60:4000", ""))
		if w.Code == http.StatusTooManyRequests {
			refused = w
		}
	}
	if refused == nil {
		t.Fatal("the client was never rate limited")
	}
	if refused.Header().Get("Retry-After") == "" {
		t.Error("429 without a Retry-After header")
	}
	if !strings.Contains(refused.Header().Get("Access-Control-Expose-Headers"), "Retry-After") {
		t.Errorf("Retry-After is not exposed to the frontend: Access-Control-Expose-Headers=%q",
			refused.Header().Get("Access-Control-Expose-Headers"))
	}
	if refused.Header().Get("Access-Control-Allow-Origin") != frontendURL {
		t.Error("a 429 must carry the CORS headers, or the browser reports a network error instead")
	}
}

func TestRateLimit_FallsBackWhenRedisIsDown(t *testing.T) {
	previous := cache
	// Nothing listens on port 1, so every Redis call fails immediately.
	cache = &Cache{client: redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}), ttl: time.Second, prefix: "down:"}
	resetLimiter()
	t.Cleanup(func() {
		cache.client.Close()
		cache = previous
		resetLimiter()
	})

	start := time.Now()
	allowed, refilled := burst(3*rateLimitBurst, func() bool {
		ok, _ := allowRequest("203.0.113.40")
		return ok
	})
	elapsed := time.Since(start)

	if allowed < rateLimitBurst || allowed > rateLimitBurst+refilled {
		t.Errorf("without Redis the limit must still hold: expected about %d allowed, got %d", rateLimitBurst, allowed)
	}
	if limiterSkipRedisUntil.Load() == 0 {
		t.Error("after a Redis failure the limiter should stop asking Redis for a while")
	}
	// Only the first request may wait for Redis; the rest must not.
	if elapsed > 2*time.Second {
		t.Errorf("%d requests took %v with Redis down; they should not each wait for it", 3*rateLimitBurst, elapsed)
	}
}

func TestRateLimit_UsesRedisAgainOnceItIsBack(t *testing.T) {
	setupRedis(t)

	// As if Redis failed a while ago and the pause has just run out.
	limiterSkipRedisUntil.Store(time.Now().Add(-time.Second).UnixNano())

	if ok, _ := allowRequest("203.0.113.50"); !ok {
		t.Fatal("first request should pass")
	}
	if limiterSkipRedisUntil.Load() != 0 {
		t.Error("a successful Redis call should mark Redis as healthy again")
	}
	visitorsMu.Lock()
	local := len(visitors)
	visitorsMu.Unlock()
	if local != 0 {
		t.Errorf("the request should have been counted in Redis, not in memory (%d local entries)", local)
	}
}

// ---------------------------------------------------------------------
// REDIS_URL (fix: the address was hardcoded)
// ---------------------------------------------------------------------

func TestRedisOptions(t *testing.T) {
	t.Setenv("REDIS_URL", "")
	if got := redisOptions().Addr; got != "localhost:6379" {
		t.Errorf("default address: got %q, want localhost:6379", got)
	}

	t.Setenv("REDIS_URL", "redis://app:s3cret@cache.internal:6380/2")
	opt := redisOptions()
	if opt.Addr != "cache.internal:6380" || opt.Username != "app" || opt.Password != "s3cret" || opt.DB != 2 {
		t.Errorf("REDIS_URL not applied: addr=%q user=%q db=%d", opt.Addr, opt.Username, opt.DB)
	}
	if opt.TLSConfig != nil {
		t.Error("redis:// must not turn TLS on")
	}

	t.Setenv("REDIS_URL", "rediss://cache.example.com:6379")
	if redisOptions().TLSConfig == nil {
		t.Error("rediss:// must turn TLS on")
	}
}

// ---------------------------------------------------------------------
// Search (fix: the page filtered only the rows it had already loaded)
// ---------------------------------------------------------------------

func TestSearchHelpers(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/arrests?q="+url.QueryEscape("  Ghost  "), nil)
	if got := searchTerm(req); got != "Ghost" {
		t.Errorf("searchTerm should trim, got %q", got)
	}

	long := strings.Repeat("й", maxSearchLength+50) // two bytes per letter
	req = httptest.NewRequest("GET", "/api/arrests?q="+url.QueryEscape(long), nil)
	if got := []rune(searchTerm(req)); len(got) != maxSearchLength {
		t.Errorf("searchTerm should cut to %d characters, got %d", maxSearchLength, len(got))
	}

	if got := containsPattern("50%_off!"); got != "%50!%!_off!!%" {
		t.Errorf("containsPattern = %q", got)
	}
}

func TestSearch_Arrests(t *testing.T) {
	setupTestDB(t)

	// Seed data: GhostDriver (player 3) has two arrests by Officer Reyes,
	// RustyWheels (5) and ShadowRunner (1) one each by Officer Chen.
	cases := []struct {
		q    string
		want int
		why  string
	}{
		{"ghost", 2, "player name, any letter case"},
		{"GHOSTDRIVER", 2, "player name, upper case"},
		{"reckless", 1, "charge"},
		{"chen", 2, "officer"},
		{"e", 4, "a letter that appears in every row"},
		{"no such thing", 0, "no match"},
		{"' OR '1'='1", 0, "SQL in the search text is just text"},
		{"%", 0, "a percent sign is a literal character, not a wildcard"},
	}
	for _, c := range cases {
		got, w := listArrests(t, defaultServerID, c.q)
		if len(got) != c.want {
			t.Errorf("q=%q (%s): expected %d arrests, got %d", c.q, c.why, c.want, len(got))
		}
		if cacheHeader := w.Header().Get("X-Cache"); cacheHeader != "BYPASS" {
			t.Errorf("q=%q: searches must not be cached, X-Cache=%s", c.q, cacheHeader)
		}
	}

	// No match is an empty list, never null.
	if _, w := listArrests(t, defaultServerID, "no such thing"); strings.TrimSpace(w.Body.String()) != "[]" {
		t.Errorf("no match should be [], got %s", w.Body.String())
	}

	// Rows carry the player's name, so the page no longer needs its own list.
	got, _ := listArrests(t, defaultServerID, "reckless")
	if len(got) == 1 && got[0].PlayerName != "RustyWheels" {
		t.Errorf("expected playerName RustyWheels, got %q", got[0].PlayerName)
	}
	all, w := listArrests(t, defaultServerID, "")
	if len(all) != 4 || all[0].PlayerName != "GhostDriver" {
		t.Errorf("the plain list should carry player names too, got %+v", all)
	}
	if w.Header().Get("X-Cache") != "MISS" {
		t.Errorf("the plain list is still cached, X-Cache=%s", w.Header().Get("X-Cache"))
	}
}

// The bug itself: a record outside the first page could not be found.
func TestSearch_FindsRowsBeyondTheFirstPage(t *testing.T) {
	setupTestDB(t)

	for i := 0; i < 60; i++ {
		addArrest(t, defaultServerID, 1, fmt.Sprintf("Filler %d", i))
	}
	addArrest(t, defaultServerID, 2, "Needle in the haystack")

	firstPage, _ := listArrests(t, defaultServerID, "")
	if len(firstPage) != 50 {
		t.Fatalf("expected a full first page of 50, got %d", len(firstPage))
	}
	for _, a := range firstPage {
		if a.Charge == "Needle in the haystack" {
			t.Fatal("test setup: the needle should not be on the first page")
		}
	}

	found, _ := listArrests(t, defaultServerID, "needle")
	if len(found) != 1 || found[0].Charge != "Needle in the haystack" || found[0].PlayerName != "NightCrawler99" {
		t.Errorf("search should find the record beyond page one, got %+v", found)
	}

	// Search results page like everything else.
	w := httptest.NewRecorder()
	arrestsHandler(w, newReq("GET", "/api/arrests?q=filler&limit=25&offset=50", nil, viewerKey))
	var page []ArrestRecord
	json.Unmarshal(w.Body.Bytes(), &page)
	if len(page) != 10 {
		t.Errorf("60 matches, offset 50: expected the last 10, got %d", len(page))
	}
}

func TestSearch_WildcardCharactersAreLiteral(t *testing.T) {
	setupTestDB(t)
	addArrest(t, defaultServerID, 1, "100% proof")
	addArrest(t, defaultServerID, 1, "100 proof")
	addArrest(t, defaultServerID, 1, "code a_b")
	addArrest(t, defaultServerID, 1, "code axb")
	addArrest(t, defaultServerID, 1, "wow! really")

	for q, want := range map[string]string{
		"100%": "100% proof",
		"a_b":  "code a_b",
		"w!":   "wow! really",
	} {
		got, _ := listArrests(t, defaultServerID, q)
		if len(got) != 1 || got[0].Charge != want {
			t.Errorf("q=%q should match only %q, got %+v", q, want, got)
		}
	}
}

func TestSearch_Warrants(t *testing.T) {
	setupTestDB(t)

	// Seed data: two warrants by Judge Alvarez (GhostDriver, VendettaX),
	// one by Judge Kim (RustyWheels).
	for q, want := range map[string]int{"alvarez": 2, "vendetta": 1, "probation": 1, "judge": 3, "nobody": 0} {
		if got := listWarrants(t, defaultServerID, q); len(got) != want {
			t.Errorf("q=%q: expected %d warrants, got %d", q, want, len(got))
		}
	}
	if got := listWarrants(t, defaultServerID, "vendetta"); len(got) == 1 && got[0].PlayerName != "VendettaX" {
		t.Errorf("expected playerName VendettaX, got %q", got[0].PlayerName)
	}
}

func TestSearch_StaysInsideOneServer(t *testing.T) {
	setupTestDB(t)
	s2 := addServer(t, "second")
	p2 := addPlayer(t, s2, "OnlyOnServer2")
	addArrest(t, s2, p2, "Server two business")

	if got, _ := listArrests(t, s2, "grand theft"); len(got) != 0 {
		t.Errorf("server 2 must not find server 1's arrests, got %+v", got)
	}
	if got, _ := listArrests(t, s2, "ghost"); len(got) != 0 {
		t.Errorf("server 2 must not find arrests through server 1's player names, got %+v", got)
	}
	if got, _ := listArrests(t, defaultServerID, "server two"); len(got) != 0 {
		t.Errorf("server 1 must not find server 2's arrests, got %+v", got)
	}
	if got, _ := listArrests(t, s2, "server two"); len(got) != 1 || got[0].PlayerName != "OnlyOnServer2" {
		t.Errorf("server 2 should find its own arrest, got %+v", got)
	}
}

func TestSearch_SeesNewRowsImmediately(t *testing.T) {
	setupTestDB(t)

	if got, _ := listArrests(t, defaultServerID, "jaywalking"); len(got) != 0 {
		t.Fatalf("expected no match yet, got %d", len(got))
	}
	arrest := ArrestRecord{PlayerID: 1, Charge: "Jaywalking", Officer: "Officer Test", Status: "pending"}
	if code := call(arrestsHandler, newReq("POST", "/api/arrests", arrest, adminKey)); code != http.StatusCreated {
		t.Fatalf("create arrest: expected 201, got %d", code)
	}
	if got, _ := listArrests(t, defaultServerID, "jaywalking"); len(got) != 1 {
		t.Errorf("a search right after a write must see the new row, got %d", len(got))
	}
}

func TestCreate_ReturnsServerSidePlayerName(t *testing.T) {
	setupTestDB(t)

	// The client claims a name; the server's own lookup must win.
	body := map[string]any{"playerId": 1, "playerName": "Spoofed", "charge": "x", "officer": "y", "status": "pending"}
	w := httptest.NewRecorder()
	arrestsHandler(w, newReq("POST", "/api/arrests", body, adminKey))
	var created ArrestRecord
	json.Unmarshal(w.Body.Bytes(), &created)
	if w.Code != http.StatusCreated || created.PlayerName != "ShadowRunner" {
		t.Errorf("arrest: expected 201 with playerName ShadowRunner, got %d / %q", w.Code, created.PlayerName)
	}

	w = httptest.NewRecorder()
	warrantsHandler(w, newReq("POST", "/api/warrants", map[string]any{"playerId": 4, "reason": "r", "issuedBy": "i"}, adminKey))
	var warrant Warrant
	json.Unmarshal(w.Body.Bytes(), &warrant)
	if w.Code != http.StatusCreated || warrant.PlayerName != "VendettaX" {
		t.Errorf("warrant: expected 201 with playerName VendettaX, got %d / %q", w.Code, warrant.PlayerName)
	}
}

// ---------------------------------------------------------------------
// Real date columns (fix: dates were free text in VARCHAR(255))
// ---------------------------------------------------------------------

func TestNormalizeDate(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	good := map[string]string{
		"2026-08-30":   "2026-08-30",
		" 2026-08-30 ": "2026-08-30",
		"2028-02-29":   "2028-02-29", // leap day
		"":             today,
	}
	for in, want := range good {
		if got, ok := normalizeDate(in); !ok || got != want {
			t.Errorf("normalizeDate(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"2026-02-30", "2026-13-01", "08/30/2026", "2026-8-3", "yesterday",
		"2026-08-30T10:00:00Z", "0999-01-01", "' OR 1=1 --", "<script>"} {
		if _, ok := normalizeDate(bad); ok {
			t.Errorf("normalizeDate(%q) should be rejected", bad)
		}
	}
}

func TestDates_ColumnsAreRealDateTypes(t *testing.T) {
	setupTestDB(t)

	want := map[[2]string]string{
		{"audit_log", "timestamp"}:    "datetime",
		{"players", "last_seen"}:      "datetime",
		{"arrests", "date"}:           "date",
		{"warrants", "date_issued"}:   "date",
		{"users", "last_login_at"}:    "datetime",
		{"servers", "created_at"}:     "datetime",
		{"memberships", "created_at"}: "datetime",
	}
	for col, typ := range want {
		if got := columnType(t, col[0], col[1]); got != typ {
			t.Errorf("%s.%s is %s, want %s", col[0], col[1], got, typ)
		}
	}
}

func TestDates_ArrestAndWarrantDatesAreValidated(t *testing.T) {
	setupTestDB(t)

	arrest := func(date string) (int, ArrestRecord) {
		w := httptest.NewRecorder()
		arrestsHandler(w, newReq("POST", "/api/arrests",
			ArrestRecord{PlayerID: 1, Charge: "Dated", Officer: "Officer Test", Date: date, Status: "pending"}, adminKey))
		var created ArrestRecord
		json.Unmarshal(w.Body.Bytes(), &created)
		return w.Code, created
	}
	warrant := func(date string) (int, Warrant) {
		w := httptest.NewRecorder()
		warrantsHandler(w, newReq("POST", "/api/warrants",
			Warrant{PlayerID: 1, Reason: "Dated", IssuedBy: "Judge Test", DateIssued: date}, adminKey))
		var created Warrant
		json.Unmarshal(w.Body.Bytes(), &created)
		return w.Code, created
	}

	for _, bad := range []string{"2026-02-30", "08/30/2026", "tomorrow", "' OR 1=1 --"} {
		if code, _ := arrest(bad); code != http.StatusBadRequest {
			t.Errorf("arrest with date %q: expected 400, got %d", bad, code)
		}
		if code, _ := warrant(bad); code != http.StatusBadRequest {
			t.Errorf("warrant with dateIssued %q: expected 400, got %d", bad, code)
		}
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM arrests WHERE charge = 'Dated'`).Scan(&n)
	if n != 0 {
		t.Errorf("a rejected arrest must not be stored, found %d", n)
	}

	// A good date is stored and read back unchanged.
	if code, a := arrest("2026-08-30"); code != http.StatusCreated || a.Date != "2026-08-30" {
		t.Errorf("arrest 2026-08-30: got %d / %q", code, a.Date)
	}
	if code, wa := warrant("2026-10-06"); code != http.StatusCreated || wa.DateIssued != "2026-10-06" {
		t.Errorf("warrant 2026-10-06: got %d / %q", code, wa.DateIssued)
	}

	// No date means today.
	today := time.Now().UTC().Format("2006-01-02")
	if code, a := arrest(""); code != http.StatusCreated || a.Date != today {
		t.Errorf("arrest without a date: got %d / %q, want 201 / %s", code, a.Date, today)
	}

	// The database really compares them as dates now.
	db.QueryRow(`SELECT COUNT(*) FROM arrests WHERE charge = 'Dated' AND date BETWEEN '2026-08-01' AND '2026-08-31'`).Scan(&n)
	if n != 1 {
		t.Errorf("date range query: expected 1 arrest in August 2026, got %d", n)
	}

	found, _ := listArrests(t, defaultServerID, "dated")
	dates := map[string]bool{}
	for _, a := range found {
		dates[a.Date] = true
	}
	if !dates["2026-08-30"] || !dates[today] {
		t.Errorf("list should return the dates as YYYY-MM-DD, got %v", dates)
	}
}

// An old row whose text date could not be converted has NULL; the API
// reports it as an empty string instead of failing the whole list.
func TestDates_UnreadableOldDateIsEmpty(t *testing.T) {
	setupTestDB(t)

	if _, err := db.Exec(`INSERT INTO arrests (server_id, player_id, charge, officer, date, status)
		VALUES (1, 1, 'Old junk', 'Officer Test', NULL, 'pending')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO warrants (server_id, player_id, reason, issued_by, date_issued, active)
		VALUES (1, 1, 'Old junk', 'Judge Test', NULL, 1)`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if got, _ := listArrests(t, defaultServerID, "old junk"); len(got) != 1 || got[0].Date != "" {
		t.Errorf("expected one arrest with an empty date, got %+v", got)
	}
	if got := listWarrants(t, defaultServerID, "old junk"); len(got) != 1 || got[0].DateIssued != "" {
		t.Errorf("expected one warrant with an empty date, got %+v", got)
	}
	if all, _ := listArrests(t, defaultServerID, ""); len(all) != 5 {
		t.Errorf("the plain list must still work with a NULL date in it, got %d rows", len(all))
	}
}

func TestDates_AuditTimestampIsUTC(t *testing.T) {
	setupTestDB(t)

	before := time.Now().UTC().Add(-2 * time.Second)
	logAudit(newReq("POST", "/api/arrests", nil, adminKey))
	after := time.Now().UTC().Add(2 * time.Second)

	w := httptest.NewRecorder()
	auditLogHandler(w, newReq("GET", "/api/audit-log", nil, adminKey))
	var resp struct {
		Entries []struct {
			Timestamp string `json:"timestamp"`
		} `json:"entries"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Entries) != 1 {
		t.Fatalf("expected one audit entry, got %d", len(resp.Entries))
	}

	raw := resp.Entries[0].Timestamp
	if !strings.HasSuffix(raw, "Z") {
		t.Errorf("timestamp should be UTC in RFC 3339 form, got %q", raw)
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("timestamp %q is not RFC 3339: %v", raw, err)
	}
	// Off by hours here means local time was stored instead of UTC.
	if ts.Before(before) || ts.After(after) {
		t.Errorf("timestamp %s is not the current UTC time (now %s)", raw, time.Now().UTC().Format(time.RFC3339))
	}

	// What retention needs: comparing the column with a point in time.
	var recent, old int
	db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE timestamp > UTC_TIMESTAMP() - INTERVAL 1 MINUTE`).Scan(&recent)
	db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE timestamp < UTC_TIMESTAMP() - INTERVAL 30 DAY`).Scan(&old)
	if recent != 1 || old != 0 {
		t.Errorf("time range queries: expected 1 recent / 0 old, got %d / %d", recent, old)
	}
}

func TestDates_PlayerLastSeen(t *testing.T) {
	setupTestDB(t)
	if _, err := db.Exec(`INSERT INTO players (server_id, name, ping, status, playtime_hours, vehicle, last_seen)
		VALUES (1, 'NeverSeen', 0, 'offline', 0, 'None', NULL)`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	w := httptest.NewRecorder()
	playersHandler(w, newReq("GET", "/api/players", nil, viewerKey))
	var players []Player
	if err := json.Unmarshal(w.Body.Bytes(), &players); err != nil {
		t.Fatalf("decode players: %v", err)
	}

	byName := map[string]Player{}
	for _, p := range players {
		byName[p.Name] = p
	}

	// Seeded "2 hours ago".
	rusty := byName["RustyWheels"]
	if rusty.LastSeen == nil {
		t.Fatal("RustyWheels should have a last-seen time")
	}
	ts, err := time.Parse(time.RFC3339, *rusty.LastSeen)
	if err != nil || !strings.HasSuffix(*rusty.LastSeen, "Z") {
		t.Fatalf("lastSeen %q is not RFC 3339 UTC: %v", *rusty.LastSeen, err)
	}
	if ago := time.Since(ts); ago < 119*time.Minute || ago > 121*time.Minute {
		t.Errorf("RustyWheels was seeded 2 hours ago, lastSeen says %v ago", ago.Round(time.Second))
	}

	if byName["NeverSeen"].LastSeen != nil {
		t.Errorf("a player never seen should have lastSeen null, got %q", *byName["NeverSeen"].LastSeen)
	}
	if !strings.Contains(w.Body.String(), `"lastSeen":null`) {
		t.Error(`expected "lastSeen":null in the JSON for a player never seen`)
	}
}

// ---------------------------------------------------------------------
// Audit log records the real client (same root cause as the rate limiter)
// ---------------------------------------------------------------------

func TestAudit_RecordsRealClientAddress(t *testing.T) {
	setupTestDB(t)
	trustProxies(t, "127.0.0.1")
	arrest := ArrestRecord{PlayerID: 1, Charge: "x", Officer: "y", Status: "pending"}

	latestIP := func() string {
		var ip string
		db.QueryRow(`SELECT ip FROM audit_log ORDER BY id DESC LIMIT 1`).Scan(&ip)
		return ip
	}

	// Through the load balancer: its address is 127.0.0.1, the client's is in the header.
	req := newReq("POST", "/api/arrests", arrest, adminKey)
	req.RemoteAddr = "127.0.0.1:4000"
	req.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.77")
	if code := call(requireAuth(arrestsHandler), req); code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", code)
	}
	if got := latestIP(); got != "203.0.113.77" {
		t.Errorf("behind the proxy the audit log should record the client, got %q", got)
	}

	// Directly: the header is ignored.
	req = newReq("POST", "/api/arrests", arrest, adminKey)
	req.RemoteAddr = "198.51.100.9:4000"
	req.Header.Set("X-Forwarded-For", "6.6.6.6")
	call(requireAuth(arrestsHandler), req)
	if got := latestIP(); got != "198.51.100.9" {
		t.Errorf("a direct client must not be able to forge its audit address, got %q", got)
	}
}
