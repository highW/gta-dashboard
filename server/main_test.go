package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// ---------------------------------------------------------------------
// Setup. Tests run against a REAL MySQL database (the same engine as
// production, so the foreign keys that enforce tenancy are really tested).
//
//   TEST_MYSQL_DSN=user:pass@tcp(localhost:3306)/dashboard_test
//
// The database name MUST end in _test: every table in it is dropped
// before each test. Redis must be running on localhost:6379.
// ---------------------------------------------------------------------

func setupTestDB(t *testing.T) {
	t.Helper()

	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not set (point it at a database whose name ends in _test)")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("bad TEST_MYSQL_DSN: %v", err)
	}
	if !strings.HasSuffix(cfg.DBName, "_test") {
		t.Fatalf("refusing to run: database %q does not end in _test", cfg.DBName)
	}

	resetTestTables(t, dsn)

	initDB(dsn) // creates tables from schema.sql and seeds server 1
	t.Cleanup(func() { db.Close() })

	adminKey = "test-admin-key"
	viewerKey = "test-viewer-key"
	policeKey = "test-police-key"
	judgeKey = "test-judge-key"
	bootstrapAdminDiscordID = ""
	frontendURL = "http://localhost:5173"
	cookieSecure = false

	// Fresh cache namespace per test so results never bleed between tests.
	cache = NewCache(3 * time.Second)
	cache.prefix = fmt.Sprintf("test:%d:", time.Now().UnixNano())
}

func resetTestTables(t *testing.T, dsn string) {
	t.Helper()
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	defer conn.Close()
	// children first, so foreign keys don't block the drops
	if _, err := conn.Exec(`DROP TABLE IF EXISTS audit_log, warrants, arrests, players, memberships, users, servers`); err != nil {
		t.Fatalf("reset test tables: %v", err)
	}
}

func newReq(method, path string, body any, key string) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	return req
}

func addServer(t *testing.T, slug string) int {
	t.Helper()
	res, err := db.Exec(`INSERT INTO servers (name, slug, status) VALUES (?, ?, 'active')`, slug, slug)
	if err != nil {
		t.Fatalf("add server: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func addPlayer(t *testing.T, serverID int, name string) int {
	t.Helper()
	res, err := db.Exec(`INSERT INTO players (server_id, name, ping, status, playtime_hours, vehicle, last_seen)
		VALUES (?, ?, 0, 'online', 0, 'None', 'Now')`, serverID, name)
	if err != nil {
		t.Fatalf("add player: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func getPlayers(t *testing.T, serverID int) ([]Player, string) {
	t.Helper()
	w := httptest.NewRecorder()
	playersHandler(w, withServer(newReq("GET", "/api/players", nil, viewerKey), serverID))
	if w.Code != http.StatusOK {
		t.Fatalf("GET players: expected 200, got %d", w.Code)
	}
	var players []Player
	if err := json.NewDecoder(w.Body).Decode(&players); err != nil {
		t.Fatalf("decode players: %v", err)
	}
	return players, w.Header().Get("X-Cache")
}

// ---------------------------------------------------------------------
// Existing behaviour
// ---------------------------------------------------------------------

func TestGetPlayers(t *testing.T) {
	setupTestDB(t)

	players, _ := getPlayers(t, defaultServerID)
	if len(players) != 5 {
		t.Errorf("expected 5 seeded players, got %d", len(players))
	}
}

func TestCreateArrest(t *testing.T) {
	setupTestDB(t)

	newArrest := ArrestRecord{
		PlayerID: 1,
		Charge:   "Test Charge",
		Officer:  "Officer Test",
		Date:     "2026-08-30",
		Status:   "pending",
	}
	w := httptest.NewRecorder()
	arrestsHandler(w, newReq("POST", "/api/arrests", newArrest, adminKey))

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", w.Code)
	}

	var created ArrestRecord
	json.NewDecoder(w.Body).Decode(&created)

	if created.ID == 0 {
		t.Error("expected a non-zero ID to be assigned")
	}
	if created.Charge != "Test Charge" {
		t.Errorf("expected charge 'Test Charge', got %q", created.Charge)
	}
}

func TestCreateArrest_ViewerForbidden(t *testing.T) {
	setupTestDB(t)

	arrest := ArrestRecord{PlayerID: 1, Charge: "x", Officer: "y", Date: "2026-08-30", Status: "pending"}
	w := httptest.NewRecorder()
	arrestsHandler(w, newReq("POST", "/api/arrests", arrest, viewerKey))

	if w.Code != http.StatusForbidden {
		t.Fatalf("viewer creating an arrest: expected 403, got %d", w.Code)
	}
}

func TestClearWarrant(t *testing.T) {
	setupTestDB(t)

	// warrant id=1 exists from seed data and starts active
	req := newReq("PATCH", "/api/warrants/1", map[string]bool{"active": false}, adminKey)
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	warrantByIDHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var active int
	db.QueryRow("SELECT active FROM warrants WHERE id = 1").Scan(&active)
	if active != 0 {
		t.Errorf("expected warrant to be inactive, got active=%d", active)
	}
}

// Clearing an already-cleared warrant must not look like "not found".
func TestClearWarrant_Twice(t *testing.T) {
	setupTestDB(t)

	for i := 1; i <= 2; i++ {
		req := newReq("PATCH", "/api/warrants/1", map[string]bool{"active": false}, adminKey)
		req.SetPathValue("id", "1")
		w := httptest.NewRecorder()
		warrantByIDHandler(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("attempt %d: expected 200, got %d", i, w.Code)
		}
	}
}

func TestClearWarrant_NotFound(t *testing.T) {
	setupTestDB(t)

	req := newReq("PATCH", "/api/warrants/999", map[string]bool{"active": false}, adminKey)
	req.SetPathValue("id", "999")
	w := httptest.NewRecorder()
	warrantByIDHandler(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for nonexistent warrant, got %d", w.Code)
	}
}

func TestCacheHitOnSecondRequest(t *testing.T) {
	setupTestDB(t)

	_, c1 := getPlayers(t, defaultServerID)
	if c1 != "MISS" {
		t.Errorf("expected first request to MISS, got %s", c1)
	}
	_, c2 := getPlayers(t, defaultServerID)
	if c2 != "HIT" {
		t.Errorf("expected second request to HIT, got %s", c2)
	}
}

// Regression: writes used to leave stale lists in the cache.
func TestCacheClearedAfterWrite(t *testing.T) {
	setupTestDB(t)

	list := func() (int, string) {
		w := httptest.NewRecorder()
		arrestsHandler(w, newReq("GET", "/api/arrests", nil, viewerKey))
		var a []ArrestRecord
		json.NewDecoder(w.Body).Decode(&a)
		return len(a), w.Header().Get("X-Cache")
	}

	n, c := list()
	if n != 4 || c != "MISS" {
		t.Fatalf("first read: expected 4 arrests / MISS, got %d / %s", n, c)
	}
	if _, c := list(); c != "HIT" {
		t.Fatalf("second read: expected HIT, got %s", c)
	}

	arrest := ArrestRecord{PlayerID: 1, Charge: "New", Officer: "Officer Test", Date: "2026-09-01", Status: "pending"}
	w := httptest.NewRecorder()
	arrestsHandler(w, newReq("POST", "/api/arrests", arrest, adminKey))
	if w.Code != http.StatusCreated {
		t.Fatalf("create arrest: expected 201, got %d", w.Code)
	}

	n, c = list()
	if n != 5 || c != "MISS" {
		t.Errorf("after write: expected 5 arrests / MISS, got %d / %s", n, c)
	}
}

// ---------------------------------------------------------------------
// Tenant isolation: server 2 must never see or touch server 1's data.
// ---------------------------------------------------------------------

func TestIsolation_PlayersList(t *testing.T) {
	setupTestDB(t)
	s2 := addServer(t, "second")
	addPlayer(t, s2, "OnlyOnServer2")

	p1, _ := getPlayers(t, defaultServerID)
	if len(p1) != 5 {
		t.Errorf("server 1 should see its 5 players, got %d", len(p1))
	}

	p2, _ := getPlayers(t, s2)
	if len(p2) != 1 || p2[0].Name != "OnlyOnServer2" {
		t.Errorf("server 2 should see only its own player, got %+v", p2)
	}
}

func TestIsolation_CacheDoesNotLeak(t *testing.T) {
	setupTestDB(t)
	s2 := addServer(t, "second")
	addPlayer(t, s2, "OnlyOnServer2")

	getPlayers(t, defaultServerID) // fills server 1's cache

	p2, cacheStatus := getPlayers(t, s2)
	if cacheStatus != "MISS" {
		t.Errorf("server 2 must not be served server 1's cached page (X-Cache=%s)", cacheStatus)
	}
	if len(p2) != 1 {
		t.Errorf("server 2 should see 1 player, got %d", len(p2))
	}
}

func TestIsolation_CannotArrestOtherServersPlayer(t *testing.T) {
	setupTestDB(t)
	s2 := addServer(t, "second")

	// player id 1 belongs to server 1
	arrest := ArrestRecord{PlayerID: 1, Charge: "Cross-tenant", Officer: "Officer X", Date: "2026-09-01", Status: "pending"}
	w := httptest.NewRecorder()
	arrestsHandler(w, withServer(newReq("POST", "/api/arrests", arrest, adminKey), s2))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for another server's player, got %d", w.Code)
	}
	var n int
	db.QueryRow("SELECT COUNT(*) FROM arrests WHERE server_id = ?", s2).Scan(&n)
	if n != 0 {
		t.Errorf("no arrest should exist on server 2, found %d", n)
	}
}

func TestIsolation_CannotChangeOtherServersWarrant(t *testing.T) {
	setupTestDB(t)
	s2 := addServer(t, "second")

	req := withServer(newReq("PATCH", "/api/warrants/1", map[string]bool{"active": false}, adminKey), s2)
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	warrantByIDHandler(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	var active int
	db.QueryRow("SELECT active FROM warrants WHERE id = 1").Scan(&active)
	if active != 1 {
		t.Errorf("server 1's warrant must stay active, got active=%d", active)
	}
}

func TestIsolation_CannotDeleteOtherServersPlayer(t *testing.T) {
	setupTestDB(t)
	s2 := addServer(t, "second")

	req := withServer(newReq("DELETE", "/api/players/2", nil, adminKey), s2)
	req.SetPathValue("id", "2")
	w := httptest.NewRecorder()
	playerByIDHandler(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	p1, _ := getPlayers(t, defaultServerID)
	if len(p1) != 5 {
		t.Errorf("server 1 should still have 5 players, got %d", len(p1))
	}
}

func TestIsolation_AuditLog(t *testing.T) {
	setupTestDB(t)
	s2 := addServer(t, "second")

	logAudit(withServer(newReq("POST", "/api/arrests", nil, adminKey), defaultServerID))
	logAudit(withServer(newReq("POST", "/api/warrants", nil, adminKey), s2))

	w := httptest.NewRecorder()
	auditLogHandler(w, withServer(newReq("GET", "/api/audit-log", nil, adminKey), s2))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Entries []struct {
			Path string `json:"path"`
		} `json:"entries"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Entries) != 1 || resp.Entries[0].Path != "/api/warrants" {
		t.Errorf("server 2 should see only its own audit entry, got %+v", resp.Entries)
	}
}

// ---------------------------------------------------------------------
// /api/stats
// ---------------------------------------------------------------------

type statsPayload struct {
	Players  struct{ Total, Online, Idle, Offline int }
	Warrants struct{ Total, Active int }
	Arrests  struct{ Total, Pending int }
}

func getStats(t *testing.T, serverID int) (statsPayload, string) {
	t.Helper()
	w := httptest.NewRecorder()
	statsHandler(w, withServer(newReq("GET", "/api/stats", nil, viewerKey), serverID))
	if w.Code != http.StatusOK {
		t.Fatalf("GET stats: expected 200, got %d", w.Code)
	}
	var s statsPayload
	if err := json.NewDecoder(w.Body).Decode(&s); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	return s, w.Header().Get("X-Cache")
}

func TestStats_SeededCounts(t *testing.T) {
	setupTestDB(t)

	s, _ := getStats(t, defaultServerID)

	if s.Players.Total != 5 || s.Players.Online != 3 || s.Players.Idle != 1 || s.Players.Offline != 1 {
		t.Errorf("players: expected 5 total (3 online, 1 idle, 1 offline), got %+v", s.Players)
	}
	if s.Warrants.Total != 3 || s.Warrants.Active != 2 {
		t.Errorf("warrants: expected 3 total / 2 active, got %+v", s.Warrants)
	}
	if s.Arrests.Total != 4 || s.Arrests.Pending != 1 {
		t.Errorf("arrests: expected 4 total / 1 pending, got %+v", s.Arrests)
	}
}

func TestStats_Isolation(t *testing.T) {
	setupTestDB(t)
	s2 := addServer(t, "second")
	addPlayer(t, s2, "OnlyOnServer2")

	getStats(t, defaultServerID) // fills server 1's cache first

	s, cacheStatus := getStats(t, s2)
	if cacheStatus != "MISS" {
		t.Errorf("server 2 must not get server 1's cached stats (X-Cache=%s)", cacheStatus)
	}
	if s.Players.Total != 1 || s.Players.Online != 1 {
		t.Errorf("server 2 players: expected 1 total / 1 online, got %+v", s.Players)
	}
	if s.Warrants.Total != 0 || s.Arrests.Total != 0 {
		t.Errorf("server 2 should have no warrants or arrests, got %+v / %+v", s.Warrants, s.Arrests)
	}

	s1, _ := getStats(t, defaultServerID)
	if s1.Players.Total != 5 {
		t.Errorf("server 1 should still count 5 players, got %d", s1.Players.Total)
	}
}

func TestStats_RefreshAfterWrite(t *testing.T) {
	setupTestDB(t)

	s, c := getStats(t, defaultServerID)
	if s.Warrants.Active != 2 || c != "MISS" {
		t.Fatalf("first read: expected 2 active / MISS, got %d / %s", s.Warrants.Active, c)
	}
	if _, c := getStats(t, defaultServerID); c != "HIT" {
		t.Fatalf("second read: expected HIT, got %s", c)
	}

	warrant := Warrant{PlayerID: 1, Reason: "New", IssuedBy: "Judge Test", DateIssued: "2026-10-06"}
	w := httptest.NewRecorder()
	warrantsHandler(w, newReq("POST", "/api/warrants", warrant, adminKey))
	if w.Code != http.StatusCreated {
		t.Fatalf("create warrant: expected 201, got %d", w.Code)
	}

	s, c = getStats(t, defaultServerID)
	if s.Warrants.Active != 3 || c != "MISS" {
		t.Errorf("after write: expected 3 active / MISS, got %d / %s", s.Warrants.Active, c)
	}
}

// ---------------------------------------------------------------------
// Roles and permissions
// ---------------------------------------------------------------------

func call(h http.HandlerFunc, req *http.Request) int {
	w := httptest.NewRecorder()
	h(w, req)
	return w.Code
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Every role against every action, through the real auth wrapper.
// Changing what a role may do means changing this table on purpose.
func TestRolePermissions(t *testing.T) {
	setupTestDB(t)

	keys := map[string]string{
		"none": "", "viewer": viewerKey, "police": policeKey, "judge": judgeKey, "admin": adminKey,
	}
	roleOrder := []string{"none", "viewer", "police", "judge", "admin"}
	everyone := []string{"viewer", "police", "judge", "admin"}

	idReq := func(method, path, id string, body any, key string) *http.Request {
		req := newReq(method, path, body, key)
		req.SetPathValue("id", id)
		return req
	}

	actions := []struct {
		name    string
		allowed []string
		run     func(key string) int
	}{
		{"read players", everyone, func(k string) int {
			return call(requireAuth(playersHandler), newReq("GET", "/api/players", nil, k))
		}},
		{"read arrests", everyone, func(k string) int {
			return call(requireAuth(arrestsHandler), newReq("GET", "/api/arrests", nil, k))
		}},
		{"read warrants", everyone, func(k string) int {
			return call(requireAuth(warrantsHandler), newReq("GET", "/api/warrants", nil, k))
		}},
		{"read stats", everyone, func(k string) int {
			return call(requireAuth(statsHandler), newReq("GET", "/api/stats", nil, k))
		}},
		{"read audit log", []string{"admin"}, func(k string) int {
			return call(requireAuth(auditLogHandler), newReq("GET", "/api/audit-log", nil, k))
		}},
		{"create arrest", []string{"police", "admin"}, func(k string) int {
			body := ArrestRecord{PlayerID: 1, Charge: "x", Officer: "y", Date: "2026-10-11", Status: "pending"}
			return call(requireAuth(arrestsHandler), newReq("POST", "/api/arrests", body, k))
		}},
		{"update arrest", []string{"police", "admin"}, func(k string) int {
			return call(requireAuth(arrestByIDHandler), idReq("PATCH", "/api/arrests/1", "1", map[string]string{"status": "pending"}, k))
		}},
		{"issue warrant", []string{"judge", "admin"}, func(k string) int {
			body := Warrant{PlayerID: 1, Reason: "r", IssuedBy: "i", DateIssued: "2026-10-11"}
			return call(requireAuth(warrantsHandler), newReq("POST", "/api/warrants", body, k))
		}},
		{"clear warrant", []string{"judge", "admin"}, func(k string) int {
			return call(requireAuth(warrantByIDHandler), idReq("PATCH", "/api/warrants/1", "1", map[string]bool{"active": false}, k))
		}},
		{"update player", []string{"admin"}, func(k string) int {
			return call(requireAuth(playerByIDHandler), idReq("PATCH", "/api/players/1", "1", map[string]string{"status": "online"}, k))
		}},
		{"list members", []string{"admin"}, func(k string) int {
			return call(requireAuth(membersHandler), newReq("GET", "/api/members", nil, k))
		}},
		{"set member role", []string{"admin"}, func(k string) int {
			id := fmt.Sprint(addUser(t, fmt.Sprint(time.Now().UnixNano()), "temp"))
			return call(requireAuth(memberByIDHandler), idReq("PUT", "/api/members/"+id, id, map[string]string{"role": "viewer"}, k))
		}},
		{"delete player", []string{"admin"}, func(k string) int {
			id := fmt.Sprint(addPlayer(t, defaultServerID, "Temp"))
			return call(requireAuth(playerByIDHandler), idReq("DELETE", "/api/players/"+id, id, nil, k))
		}},
	}

	for _, a := range actions {
		for _, roleKey := range roleOrder {
			t.Run(a.name+"/"+roleKey, func(t *testing.T) {
				status := a.run(keys[roleKey])
				switch {
				case has(a.allowed, roleKey):
					if status != http.StatusOK && status != http.StatusCreated {
						t.Errorf("%s should be allowed to %s, got %d", roleKey, a.name, status)
					}
				case roleKey == "none":
					if status != http.StatusUnauthorized {
						t.Errorf("no key should get 401 for %s, got %d", a.name, status)
					}
				default:
					if status != http.StatusForbidden {
						t.Errorf("%s should be forbidden to %s (403), got %d", roleKey, a.name, status)
					}
				}
			})
		}
	}
}

// A role whose key is not configured must be impossible to hit,
// including with an empty or arbitrary header.
func TestUnsetKeysDisableRole(t *testing.T) {
	adminKey, viewerKey, policeKey, judgeKey = "", "", "", ""

	for _, key := range []string{"", "anything", "test-police-key"} {
		if got := getRole(newReq("GET", "/api/players", nil, key)); got != roleNone {
			t.Errorf("key %q with no keys configured: expected no role, got %v", key, got)
		}
	}
}

func TestWhoami_ReportsPermissions(t *testing.T) {
	adminKey, viewerKey, policeKey, judgeKey = "a", "v", "p", "j"

	w := httptest.NewRecorder()
	whoamiHandler(w, newReq("GET", "/api/whoami", nil, "p"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body struct {
		Role        string   `json:"role"`
		Permissions []string `json:"permissions"`
	}
	json.NewDecoder(w.Body).Decode(&body)

	if body.Role != "police" {
		t.Errorf("expected role police, got %q", body.Role)
	}
	if !has(body.Permissions, "arrests.create") {
		t.Errorf("police should report arrests.create, got %v", body.Permissions)
	}
	if has(body.Permissions, "warrants.issue") {
		t.Errorf("police must not report warrants.issue, got %v", body.Permissions)
	}
}

// ---------------------------------------------------------------------
// Discord login, sessions and per-server roles (Discord is faked)
// ---------------------------------------------------------------------

func fakeDiscord(t *testing.T, discordID, username string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			json.NewEncoder(w).Encode(map[string]string{"access_token": "tok", "token_type": "Bearer"})
		case "/me":
			if r.Header.Get("Authorization") != "Bearer tok" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"id": discordID, "username": username})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	discordTokenURL = srv.URL + "/token"
	discordUserURL = srv.URL + "/me"
	discordClientID, discordClientSecret, discordRedirectURI = "cid", "secret", "http://localhost/cb"
}

func cookieNamed(res *http.Response, name string) *http.Cookie {
	for _, c := range res.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// discordLogin walks the whole flow (start, then callback) and returns the session cookie.
func discordLogin(t *testing.T) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	discordStartHandler(w, httptest.NewRequest("GET", "/api/auth/discord", nil))
	if w.Code != http.StatusFound {
		t.Fatalf("start: expected 302, got %d", w.Code)
	}
	state := cookieNamed(w.Result(), "oauth_state")
	if state == nil {
		t.Fatal("start did not set an oauth_state cookie")
	}

	req := httptest.NewRequest("GET", "/api/auth/discord/callback?code=abc&state="+state.Value, nil)
	req.AddCookie(state)
	w2 := httptest.NewRecorder()
	discordCallbackHandler(w2, req)
	if w2.Code != http.StatusFound {
		t.Fatalf("callback: expected 302, got %d", w2.Code)
	}
	session := cookieNamed(w2.Result(), "session")
	if session == nil {
		t.Fatalf("callback did not set a session cookie (redirected to %s)", w2.Header().Get("Location"))
	}
	return session
}

func cookieReq(method, path string, body any, c *http.Cookie, csrf bool) *http.Request {
	req := newReq(method, path, body, "")
	req.AddCookie(c)
	if csrf {
		req.Header.Set("X-Requested-With", "dashboard")
	}
	return req
}

func TestDiscord_NewUserHasNoAccessUntilGranted(t *testing.T) {
	setupTestDB(t)
	fakeDiscord(t, "111", "alice")
	session := discordLogin(t)

	var users int
	db.QueryRow("SELECT COUNT(*) FROM users WHERE discord_id = '111'").Scan(&users)
	if users != 1 {
		t.Fatalf("expected the user to be created, found %d", users)
	}

	// signed in, but nobody has granted a role yet: refused, and reported as pending
	if got := call(requireAuth(playersHandler), cookieReq("GET", "/api/players", nil, session, false)); got != http.StatusForbidden {
		t.Errorf("no membership: expected 403, got %d", got)
	}
	w := httptest.NewRecorder()
	whoamiHandler(w, cookieReq("GET", "/api/whoami", nil, session, false))
	var who struct {
		Role string `json:"role"`
	}
	json.NewDecoder(w.Body).Decode(&who)
	if w.Code != http.StatusOK || who.Role != "pending" {
		t.Errorf("whoami: expected 200 / pending, got %d / %q", w.Code, who.Role)
	}

	// a staff member grants viewer: reads work, writes still don't
	db.Exec(`INSERT INTO memberships (user_id, server_id, role) SELECT id, 1, 'viewer' FROM users WHERE discord_id = '111'`)
	if got := call(requireAuth(playersHandler), cookieReq("GET", "/api/players", nil, session, false)); got != http.StatusOK {
		t.Errorf("viewer read: expected 200, got %d", got)
	}
	arrest := ArrestRecord{PlayerID: 1, Charge: "x", Officer: "y", Date: "2026-10-12", Status: "pending"}
	if got := call(requireAuth(arrestsHandler), cookieReq("POST", "/api/arrests", arrest, session, true)); got != http.StatusForbidden {
		t.Errorf("viewer create arrest: expected 403, got %d", got)
	}
}

func TestDiscord_BootstrapAdminAndCSRF(t *testing.T) {
	setupTestDB(t)
	fakeDiscord(t, "111", "alice")
	bootstrapAdminDiscordID = "111"
	session := discordLogin(t)

	arrest := ArrestRecord{PlayerID: 1, Charge: "x", Officer: "y", Date: "2026-10-12", Status: "pending"}

	// a cookie-authenticated write without our custom header must be refused
	if got := call(requireAuth(arrestsHandler), cookieReq("POST", "/api/arrests", arrest, session, false)); got != http.StatusForbidden {
		t.Errorf("write without CSRF header: expected 403, got %d", got)
	}
	// a request that claims to come from another site must be refused
	evil := cookieReq("POST", "/api/arrests", arrest, session, true)
	evil.Header.Set("Origin", "http://evil.example")
	if got := call(requireAuth(arrestsHandler), evil); got != http.StatusForbidden {
		t.Errorf("write from another origin: expected 403, got %d", got)
	}
	// the real frontend works
	if got := call(requireAuth(arrestsHandler), cookieReq("POST", "/api/arrests", arrest, session, true)); got != http.StatusCreated {
		t.Errorf("admin write with header: expected 201, got %d", got)
	}
}

func TestDiscord_BadStateRejected(t *testing.T) {
	setupTestDB(t)
	fakeDiscord(t, "111", "alice")

	w := httptest.NewRecorder()
	discordStartHandler(w, httptest.NewRequest("GET", "/api/auth/discord", nil))
	state := cookieNamed(w.Result(), "oauth_state")

	req := httptest.NewRequest("GET", "/api/auth/discord/callback?code=abc&state=forged", nil)
	req.AddCookie(state)
	w2 := httptest.NewRecorder()
	discordCallbackHandler(w2, req)

	if loc := w2.Header().Get("Location"); !strings.Contains(loc, "error=state") {
		t.Errorf("expected redirect with error=state, got %q", loc)
	}
	if cookieNamed(w2.Result(), "session") != nil && cookieNamed(w2.Result(), "session").Value != "" {
		t.Error("a forged state must not create a session")
	}
	var users int
	db.QueryRow("SELECT COUNT(*) FROM users").Scan(&users)
	if users != 0 {
		t.Errorf("a forged state must not create a user, found %d", users)
	}
}

func TestDiscord_MembershipIsPerServer(t *testing.T) {
	setupTestDB(t)
	fakeDiscord(t, "111", "alice")
	session := discordLogin(t)
	s2 := addServer(t, "second")

	// admin on server 2 only
	db.Exec(`INSERT INTO memberships (user_id, server_id, role) SELECT id, ?, 'admin' FROM users WHERE discord_id = '111'`, s2)

	if got := call(requireAuth(playersHandler), cookieReq("GET", "/api/players", nil, session, false)); got != http.StatusForbidden {
		t.Errorf("server 1: a server 2 admin must have no access, got %d", got)
	}
	if got := call(requireAuth(playersHandler), withServer(cookieReq("GET", "/api/players", nil, session, false), s2)); got != http.StatusOK {
		t.Errorf("server 2: expected 200, got %d", got)
	}
}

func TestLogout_EndsSession(t *testing.T) {
	setupTestDB(t)
	fakeDiscord(t, "111", "alice")
	bootstrapAdminDiscordID = "111"
	session := discordLogin(t)

	if got := call(requireAuth(playersHandler), cookieReq("GET", "/api/players", nil, session, false)); got != http.StatusOK {
		t.Fatalf("before logout: expected 200, got %d", got)
	}
	if got := call(withIdentity(logoutHandler), cookieReq("POST", "/api/auth/logout", nil, session, true)); got != http.StatusNoContent {
		t.Fatalf("logout: expected 204, got %d", got)
	}
	if got := call(requireAuth(playersHandler), cookieReq("GET", "/api/players", nil, session, false)); got != http.StatusUnauthorized {
		t.Errorf("after logout the old cookie must be dead: expected 401, got %d", got)
	}
}

// ---------------------------------------------------------------------
// Members page: granting roles, and who acted (audit actor)
// ---------------------------------------------------------------------

func addUser(t *testing.T, discordID, username string) int {
	t.Helper()
	res, err := db.Exec(`INSERT INTO users (discord_id, username, display_name) VALUES (?, ?, ?)`,
		discordID, username, username)
	if err != nil {
		t.Fatalf("add user: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func listMembers(t *testing.T, c *http.Cookie, serverID int) []member {
	t.Helper()
	w := httptest.NewRecorder()
	requireAuth(membersHandler)(w, withServer(cookieReq("GET", "/api/members", nil, c, false), serverID))
	if w.Code != http.StatusOK {
		t.Fatalf("list members: expected 200, got %d", w.Code)
	}
	var out []member
	json.NewDecoder(w.Body).Decode(&out)
	return out
}

func putMember(c *http.Cookie, userID int, role string, serverID int) int {
	req := withServer(cookieReq("PUT", fmt.Sprintf("/api/members/%d", userID), map[string]string{"role": role}, c, true), serverID)
	req.SetPathValue("id", fmt.Sprint(userID))
	return call(requireAuth(memberByIDHandler), req)
}

func bootstrapAdminSession(t *testing.T) *http.Cookie {
	t.Helper()
	fakeDiscord(t, "111", "alice")
	bootstrapAdminDiscordID = "111"
	return discordLogin(t)
}

func TestMembers_GrantAndRevoke(t *testing.T) {
	setupTestDB(t)
	admin := bootstrapAdminSession(t)
	bob := addUser(t, "222", "bob")

	var found *member
	for _, m := range listMembers(t, admin, defaultServerID) {
		if m.UserID == bob {
			mm := m
			found = &mm
		}
	}
	if found == nil || found.Role != nil {
		t.Fatalf("bob should be listed as pending (no role), got %+v", found)
	}

	if got := putMember(admin, bob, "police", defaultServerID); got != http.StatusOK {
		t.Fatalf("grant police: expected 200, got %d", got)
	}
	var role string
	db.QueryRow(`SELECT role FROM memberships WHERE user_id = ? AND server_id = 1`, bob).Scan(&role)
	if role != "police" {
		t.Errorf("expected role police in the database, got %q", role)
	}

	if got := putMember(admin, bob, "superuser", defaultServerID); got != http.StatusBadRequest {
		t.Errorf("unknown role: expected 400, got %d", got)
	}

	if got := putMember(admin, bob, "none", defaultServerID); got != http.StatusOK {
		t.Fatalf("remove access: expected 200, got %d", got)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM memberships WHERE user_id = ?`, bob).Scan(&n)
	if n != 0 {
		t.Errorf("access should be removed, found %d memberships", n)
	}
}

func TestMembers_LastAdminProtected(t *testing.T) {
	setupTestDB(t)
	admin := bootstrapAdminSession(t)

	var alice int
	db.QueryRow(`SELECT id FROM users WHERE discord_id = '111'`).Scan(&alice)

	if got := putMember(admin, alice, "viewer", defaultServerID); got != http.StatusConflict {
		t.Errorf("demoting the only admin: expected 409, got %d", got)
	}
	if got := putMember(admin, alice, "none", defaultServerID); got != http.StatusConflict {
		t.Errorf("removing the only admin: expected 409, got %d", got)
	}

	bob := addUser(t, "222", "bob")
	if got := putMember(admin, bob, "admin", defaultServerID); got != http.StatusOK {
		t.Fatalf("promote bob: expected 200, got %d", got)
	}
	if got := putMember(admin, alice, "viewer", defaultServerID); got != http.StatusOK {
		t.Errorf("with a second admin, demoting alice should work, got %d", got)
	}
}

func TestMembers_OtherServersUsersHidden(t *testing.T) {
	setupTestDB(t)
	admin := bootstrapAdminSession(t)
	s2 := addServer(t, "second")

	carol := addUser(t, "333", "carol")
	db.Exec(`INSERT INTO memberships (user_id, server_id, role) VALUES (?, ?, 'viewer')`, carol, s2)

	for _, m := range listMembers(t, admin, defaultServerID) {
		if m.UserID == carol {
			t.Error("server 1's admin must not see a member of server 2")
		}
	}
	if got := putMember(admin, carol, "police", defaultServerID); got != http.StatusNotFound {
		t.Errorf("changing another server's member: expected 404, got %d", got)
	}
	var role string
	db.QueryRow(`SELECT role FROM memberships WHERE user_id = ? AND server_id = ?`, carol, s2).Scan(&role)
	if role != "viewer" {
		t.Errorf("carol's server 2 role must be untouched, got %q", role)
	}
}

func TestAudit_RecordsActor(t *testing.T) {
	setupTestDB(t)
	admin := bootstrapAdminSession(t)
	arrest := ArrestRecord{PlayerID: 1, Charge: "x", Officer: "y", Date: "2026-10-12", Status: "pending"}

	latestActor := func() string {
		var actor string
		db.QueryRow(`SELECT actor FROM audit_log WHERE server_id = 1 ORDER BY id DESC LIMIT 1`).Scan(&actor)
		return actor
	}

	call(requireAuth(arrestsHandler), cookieReq("POST", "/api/arrests", arrest, admin, true))
	if got := latestActor(); got != "alice" {
		t.Errorf("a Discord session should be logged under its username, got %q", got)
	}

	call(requireAuth(arrestsHandler), newReq("POST", "/api/arrests", arrest, adminKey))
	if got := latestActor(); got != "api key" {
		t.Errorf("an API-key caller should be logged as \"api key\", got %q", got)
	}
}
