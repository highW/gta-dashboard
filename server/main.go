package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
)

type Player struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Ping          int    `json:"ping"`
	Status        string `json:"status"`
	PlaytimeHours int    `json:"playtimeHours"`
	Vehicle       string `json:"vehicle"`
	LastSeen      string `json:"lastSeen"`
}

type ArrestRecord struct {
	ID       int    `json:"id"`
	PlayerID int    `json:"playerId"`
	Charge   string `json:"charge"`
	Officer  string `json:"officer"`
	Date     string `json:"date"`
	Status   string `json:"status"`
}

type Warrant struct {
	ID         int    `json:"id"`
	PlayerID   int    `json:"playerId"`
	Reason     string `json:"reason"`
	IssuedBy   string `json:"issuedBy"`
	DateIssued string `json:"dateIssued"`
	Active     bool   `json:"active"`
}

// ---------------------------------------------------------------------
// Tenancy: every request acts on exactly one server.
// ---------------------------------------------------------------------

const defaultServerID = 1

type ctxKey string

const serverCtxKey ctxKey = "serverID"

// serverFor resolves which server (tenant) a request is acting on.
// Only server-side code can set it (clients cannot send it), so it is safe
// to trust. Today every request is server 1; when accounts arrive, the auth
// middleware will call withServer() after checking the user's membership.
func serverFor(r *http.Request) int {
	if id, ok := r.Context().Value(serverCtxKey).(int); ok {
		return id
	}
	return defaultServerID
}

func withServer(r *http.Request, serverID int) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), serverCtxKey, serverID))
}

// ---------------------------------------------------------------------
// Cache: keys always include the server; writes invalidate per server.
// ---------------------------------------------------------------------

type Cache struct {
	client *redis.Client
	ttl    time.Duration
	prefix string // empty in production; tests set a unique one
}

func NewCache(ttl time.Duration) *Cache {
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	return &Cache{client: client, ttl: ttl}
}

func (c *Cache) versionKey(serverID int, resource string) string {
	return fmt.Sprintf("%scachever:%d:%s", c.prefix, serverID, resource)
}

// ListKey builds the cache key for one page of one resource on one server.
// It embeds a per-(server, resource) version number; Invalidate bumps the
// version, which instantly orphans every cached page for that resource
// (the orphans then expire by TTL). No key scanning needed.
func (c *Cache) ListKey(serverID int, resource, suffix string) string {
	version, err := c.client.Get(context.Background(), c.versionKey(serverID, resource)).Result()
	if err != nil {
		version = "0"
	}
	return fmt.Sprintf("%sserver:%d:%s:v%s:%s", c.prefix, serverID, resource, version, suffix)
}

func (c *Cache) Get(key string) ([]byte, bool) {
	val, err := c.client.Get(context.Background(), key).Bytes()
	if err != nil {
		return nil, false
	}
	return val, true
}

func (c *Cache) Set(key string, data []byte) {
	c.client.Set(context.Background(), key, data, c.ttl)
}

// Invalidate drops all cached pages of one resource for one server.
func (c *Cache) Invalidate(serverID int, resource string) {
	c.client.Incr(context.Background(), c.versionKey(serverID, resource))
	// The stats summary is built from these three, so it goes stale with them.
	// Doing it here means no call site can forget.
	if resource == "players" || resource == "arrests" || resource == "warrants" {
		c.client.Incr(context.Background(), c.versionKey(serverID, "stats"))
	}
}

var cache = NewCache(10 * time.Second)

// serveCachedList answers a list request from the cache, or builds it with
// load(), caches it, and sends it.
func serveCachedList(w http.ResponseWriter, serverID int, resource, suffix string, load func() (any, error)) {
	key := cache.ListKey(serverID, resource, suffix)

	if cached, ok := cache.Get(key); ok {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		w.Write(cached)
		return
	}

	result, err := load()
	if err != nil {
		serverError(w, err)
		return
	}
	data, err := json.Marshal(result)
	if err != nil {
		serverError(w, err)
		return
	}
	cache.Set(key, data)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "MISS")
	w.Write(data)
}

// ---------------------------------------------------------------------
// Rate limiting
// ---------------------------------------------------------------------

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

var (
	visitors   = make(map[string]*visitor)
	visitorsMu sync.Mutex
)

func getVisitor(ip string) *rate.Limiter {
	visitorsMu.Lock()
	defer visitorsMu.Unlock()

	v, exists := visitors[ip]
	if !exists {
		limiter := rate.NewLimiter(5, 10)
		visitors[ip] = &visitor{limiter: limiter, lastSeen: time.Now()}
		return limiter
	}
	v.lastSeen = time.Now()
	return v.limiter
}

func cleanupVisitors() {
	for {
		time.Sleep(time.Minute)
		visitorsMu.Lock()
		for ip, v := range visitors {
			if time.Since(v.lastSeen) > 3*time.Minute {
				delete(visitors, ip)
			}
		}
		visitorsMu.Unlock()
	}
}

func rateLimit(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}

		limiter := getVisitor(ip)
		if !limiter.Allow() {
			http.Error(w, "Too many requests", http.StatusTooManyRequests)
			return
		}

		h(w, r)
	}
}

// ---------------------------------------------------------------------
// Auth (API keys for now; replaced by accounts + memberships later)
// ---------------------------------------------------------------------

var db *sql.DB

type role int

const (
	roleNone role = iota
	roleViewer
	rolePolice
	roleJudge
	roleAdmin
)

// permission names are what the API checks, and what /api/whoami reports to
// the frontend. Handlers never ask "is this an admin?", only "can they do X?".
type permission string

const (
	permPlayersRead   permission = "players.read"
	permPlayersUpdate permission = "players.update"
	permPlayersDelete permission = "players.delete"
	permArrestsRead   permission = "arrests.read"
	permArrestsCreate permission = "arrests.create"
	permArrestsUpdate permission = "arrests.update"
	permWarrantsRead  permission = "warrants.read"
	permWarrantsIssue permission = "warrants.issue"
	permWarrantsClear permission = "warrants.clear"
	permStatsRead     permission = "stats.read"
	permAuditRead     permission = "audit.read"
	permMembersManage permission = "members.manage"
)

var allPermissions = []permission{
	permPlayersRead, permPlayersUpdate, permPlayersDelete,
	permArrestsRead, permArrestsCreate, permArrestsUpdate,
	permWarrantsRead, permWarrantsIssue, permWarrantsClear,
	permStatsRead, permAuditRead, permMembersManage,
}

var viewerPermissions = []permission{
	permPlayersRead, permArrestsRead, permWarrantsRead, permStatsRead,
}

// rolePermissions is the single place that decides what each role may do.
// To change a role, edit this map (and the table in TestRolePermissions).
var rolePermissions = map[role][]permission{
	roleViewer: viewerPermissions,
	rolePolice: append(append([]permission{}, viewerPermissions...),
		permArrestsCreate, permArrestsUpdate),
	roleJudge: append(append([]permission{}, viewerPermissions...),
		permWarrantsIssue, permWarrantsClear),
	roleAdmin: allPermissions,
}

// One API key per role for now. An unset key disables that role.
var (
	adminKey  string
	viewerKey string
	policeKey string
	judgeKey  string
)

func getRole(r *http.Request) role {
	if id, ok := identityFrom(r); ok {
		return id.Role
	}
	return roleFromKey(r.Header.Get("X-API-Key"))
}

// roleFromKey maps an API key to its role.
func roleFromKey(key string) role {
	// A missing key must never match an unset (empty) key.
	if key == "" {
		return roleNone
	}
	candidates := []struct {
		key  string
		role role
	}{
		{adminKey, roleAdmin},
		{judgeKey, roleJudge},
		{policeKey, rolePolice},
		{viewerKey, roleViewer},
	}
	for _, c := range candidates {
		if c.key != "" && subtle.ConstantTimeCompare([]byte(key), []byte(c.key)) == 1 {
			return c.role
		}
	}
	return roleNone
}

func hasPermission(rl role, p permission) bool {
	for _, allowed := range rolePermissions[rl] {
		if allowed == p {
			return true
		}
	}
	return false
}

func can(r *http.Request, p permission) bool {
	return hasPermission(getRole(r), p)
}

func forbidden(w http.ResponseWriter) {
	http.Error(w, "Forbidden: you don't have permission for this action", http.StatusForbidden)
}

// requireAuth resolves who is calling (API key or Discord session) and
// rejects anonymous callers (401) and signed-in users with no role on this
// server (403). What each role may actually do is decided per action by can()
// inside the handlers. Every write attempt is audit-logged, including denied ones.
func requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return withIdentity(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			h(w, r)
			return
		}
		id, _ := identityFrom(r)
		if id.Role == roleNone {
			if id.UserID != 0 {
				http.Error(w, "Forbidden: no access granted on this server yet", http.StatusForbidden)
			} else {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
			}
			return
		}
		if r.Method != "GET" {
			logAudit(r)
		}
		h(w, r)
	})
}

// ---------------------------------------------------------------------
// Database
// ---------------------------------------------------------------------

func initDB(dataSource string) {
	cfg, err := mysql.ParseDSN(dataSource)
	if err != nil {
		log.Fatal(err)
	}
	cfg.MultiStatements = true // schema.sql holds several statements
	// RowsAffected reports rows *matched*, not just rows changed, so applying
	// the same value twice (e.g. clearing an already-cleared warrant) is not
	// mistaken for "not found".
	cfg.ClientFoundRows = true

	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		log.Fatal(err)
	}
	db = sql.OpenDB(connector)

	schema, err := os.ReadFile("schema.sql")
	if err != nil {
		log.Fatal(err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		log.Fatal(err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM players WHERE server_id = ?", defaultServerID).Scan(&count)
	if count == 0 {
		seed()
	}
}

func seed() {
	players := []Player{
		{0, "ShadowRunner", 34, "online", 142, "Adder", "Now"},
		{0, "NightCrawler99", 87, "online", 58, "Zentorno", "Now"},
		{0, "GhostDriver", 152, "idle", 301, "Sultan RS", "3 min ago"},
		{0, "VendettaX", 41, "online", 89, "Kuruma", "Now"},
		{0, "RustyWheels", 0, "offline", 12, "Blista", "2 hours ago"},
	}
	for _, p := range players {
		db.Exec(`INSERT INTO players (server_id, name, ping, status, playtime_hours, vehicle, last_seen) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			defaultServerID, p.Name, p.Ping, p.Status, p.PlaytimeHours, p.Vehicle, p.LastSeen)
	}

	arrests := []ArrestRecord{
		{0, 3, "Grand Theft Auto", "Officer Reyes", "2026-08-14", "convicted"},
		{0, 3, "Evading Police", "Officer Reyes", "2026-08-14", "convicted"},
		{0, 5, "Reckless Driving", "Officer Chen", "2026-08-20", "released"},
		{0, 1, "Assault", "Officer Chen", "2026-08-25", "pending"},
	}
	for _, a := range arrests {
		db.Exec(`INSERT INTO arrests (server_id, player_id, charge, officer, date, status) VALUES (?, ?, ?, ?, ?, ?)`,
			defaultServerID, a.PlayerID, a.Charge, a.Officer, a.Date, a.Status)
	}

	warrants := []Warrant{
		{0, 3, "Failure to appear in court", "Judge Alvarez", "2026-08-22", true},
		{0, 4, "Outstanding fines", "Judge Alvarez", "2026-08-18", true},
		{0, 5, "Probation violation", "Judge Kim", "2026-07-30", false},
	}
	for _, w := range warrants {
		db.Exec(`INSERT INTO warrants (server_id, player_id, reason, issued_by, date_issued, active) VALUES (?, ?, ?, ?, ?, ?)`,
			defaultServerID, w.PlayerID, w.Reason, w.IssuedBy, w.DateIssued, w.Active)
	}
}

// isForeignKeyError reports MySQL error 1452: the referenced row does not
// exist. With the composite (player_id, server_id) keys this also fires when
// a request names a player that belongs to a different server.
func isForeignKeyError(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1452
}

// ---------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------

func withCORS(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", frontendURL)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, X-Requested-With")
		w.Header().Add("Vary", "Origin")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		h(w, r)
	}
}

func paging(r *http.Request, defaultLimit int) (limit, offset int) {
	limit = defaultLimit
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}

func validateArrest(a ArrestRecord) string {
	if a.PlayerID <= 0 {
		return "playerId is required"
	}
	if strings.TrimSpace(a.Charge) == "" {
		return "charge is required"
	}
	if strings.TrimSpace(a.Officer) == "" {
		return "officer is required"
	}
	validStatuses := map[string]bool{"pending": true, "convicted": true, "released": true}
	if !validStatuses[a.Status] {
		return "status must be pending, convicted, or released"
	}
	return ""
}

func validateWarrant(wa Warrant) string {
	if wa.PlayerID <= 0 {
		return "playerId is required"
	}
	if strings.TrimSpace(wa.Reason) == "" {
		return "reason is required"
	}
	if strings.TrimSpace(wa.IssuedBy) == "" {
		return "issuedBy is required"
	}
	return ""
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("ERROR: %v", err)
	http.Error(w, "Internal server error", http.StatusInternalServerError)
}

// ---------------------------------------------------------------------
// Handlers (every query is scoped to serverFor(r))
// ---------------------------------------------------------------------

func playersHandler(w http.ResponseWriter, r *http.Request) {
	if !can(r, permPlayersRead) {
		forbidden(w)
		return
	}
	serverID := serverFor(r)
	limit, offset := paging(r, 50)

	serveCachedList(w, serverID, "players", fmt.Sprintf("%d:%d", limit, offset), func() (any, error) {
		rows, err := db.Query(`SELECT id, name, ping, status, playtime_hours, vehicle, last_seen
			FROM players WHERE server_id = ? ORDER BY id LIMIT ? OFFSET ?`, serverID, limit, offset)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		players := []Player{}
		for rows.Next() {
			var p Player
			if err := rows.Scan(&p.ID, &p.Name, &p.Ping, &p.Status, &p.PlaytimeHours, &p.Vehicle, &p.LastSeen); err != nil {
				return nil, err
			}
			players = append(players, p)
		}
		return players, rows.Err()
	})
}

func arrestsHandler(w http.ResponseWriter, r *http.Request) {
	serverID := serverFor(r)

	switch r.Method {
	case "GET":
		if !can(r, permArrestsRead) {
			forbidden(w)
			return
		}
		limit, offset := paging(r, 50)

		serveCachedList(w, serverID, "arrests", fmt.Sprintf("%d:%d", limit, offset), func() (any, error) {
			rows, err := db.Query(`SELECT id, player_id, charge, officer, date, status
				FROM arrests WHERE server_id = ? ORDER BY id LIMIT ? OFFSET ?`, serverID, limit, offset)
			if err != nil {
				return nil, err
			}
			defer rows.Close()

			arrests := []ArrestRecord{}
			for rows.Next() {
				var a ArrestRecord
				if err := rows.Scan(&a.ID, &a.PlayerID, &a.Charge, &a.Officer, &a.Date, &a.Status); err != nil {
					return nil, err
				}
				arrests = append(arrests, a)
			}
			return arrests, rows.Err()
		})

	case "POST":
		if !can(r, permArrestsCreate) {
			forbidden(w)
			return
		}
		var a ArrestRecord
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if msg := validateArrest(a); msg != "" {
			http.Error(w, msg, http.StatusBadRequest)
			return
		}

		result, err := db.Exec(`INSERT INTO arrests (server_id, player_id, charge, officer, date, status) VALUES (?, ?, ?, ?, ?, ?)`,
			serverID, a.PlayerID, a.Charge, a.Officer, a.Date, a.Status)
		if err != nil {
			if isForeignKeyError(err) {
				http.Error(w, "playerId does not exist on this server", http.StatusBadRequest)
				return
			}
			serverError(w, err)
			return
		}

		id, _ := result.LastInsertId()
		a.ID = int(id)

		cache.Invalidate(serverID, "arrests")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(a)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func warrantsHandler(w http.ResponseWriter, r *http.Request) {
	serverID := serverFor(r)

	switch r.Method {
	case "GET":
		if !can(r, permWarrantsRead) {
			forbidden(w)
			return
		}
		limit, offset := paging(r, 50)

		serveCachedList(w, serverID, "warrants", fmt.Sprintf("%d:%d", limit, offset), func() (any, error) {
			rows, err := db.Query(`SELECT id, player_id, reason, issued_by, date_issued, active
				FROM warrants WHERE server_id = ? ORDER BY id LIMIT ? OFFSET ?`, serverID, limit, offset)
			if err != nil {
				return nil, err
			}
			defer rows.Close()

			warrants := []Warrant{}
			for rows.Next() {
				var wRow Warrant
				if err := rows.Scan(&wRow.ID, &wRow.PlayerID, &wRow.Reason, &wRow.IssuedBy, &wRow.DateIssued, &wRow.Active); err != nil {
					return nil, err
				}
				warrants = append(warrants, wRow)
			}
			return warrants, rows.Err()
		})

	case "POST":
		if !can(r, permWarrantsIssue) {
			forbidden(w)
			return
		}
		var newWarrant Warrant
		if err := json.NewDecoder(r.Body).Decode(&newWarrant); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		newWarrant.Active = true

		if msg := validateWarrant(newWarrant); msg != "" {
			http.Error(w, msg, http.StatusBadRequest)
			return
		}

		result, err := db.Exec(`INSERT INTO warrants (server_id, player_id, reason, issued_by, date_issued, active) VALUES (?, ?, ?, ?, ?, ?)`,
			serverID, newWarrant.PlayerID, newWarrant.Reason, newWarrant.IssuedBy, newWarrant.DateIssued, newWarrant.Active)
		if err != nil {
			if isForeignKeyError(err) {
				http.Error(w, "playerId does not exist on this server", http.StatusBadRequest)
				return
			}
			serverError(w, err)
			return
		}

		id, _ := result.LastInsertId()
		newWarrant.ID = int(id)

		cache.Invalidate(serverID, "warrants")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(newWarrant)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

type auditEntry struct {
	ID        int    `json:"id"`
	Timestamp string `json:"timestamp"`
	Role      string `json:"role"`
	Actor     string `json:"actor"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	IP        string `json:"ip"`
}

type auditResponse struct {
	Entries    []auditEntry `json:"entries"`
	NextCursor *int         `json:"next_cursor"`
}

func auditLogHandler(w http.ResponseWriter, r *http.Request) {
	if !can(r, permAuditRead) {
		forbidden(w)
		return
	}

	serverID := serverFor(r)
	limit, _ := paging(r, 100)

	beforeID := 0
	if b := r.URL.Query().Get("before_id"); b != "" {
		if parsed, err := strconv.Atoi(b); err == nil && parsed > 0 {
			beforeID = parsed
		}
	}

	serveCachedList(w, serverID, "audit_log", fmt.Sprintf("%d:%d", limit, beforeID), func() (any, error) {
		var rows *sql.Rows
		var err error
		if beforeID > 0 {
			rows, err = db.Query(`SELECT id, timestamp, role, actor, method, path, ip FROM audit_log
				WHERE server_id = ? AND id < ? ORDER BY id DESC LIMIT ?`, serverID, beforeID, limit)
		} else {
			rows, err = db.Query(`SELECT id, timestamp, role, actor, method, path, ip FROM audit_log
				WHERE server_id = ? ORDER BY id DESC LIMIT ?`, serverID, limit)
		}
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		entries := []auditEntry{}
		for rows.Next() {
			var e auditEntry
			if err := rows.Scan(&e.ID, &e.Timestamp, &e.Role, &e.Actor, &e.Method, &e.Path, &e.IP); err != nil {
				return nil, err
			}
			entries = append(entries, e)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}

		resp := auditResponse{Entries: entries}
		if len(entries) == limit {
			lastID := entries[len(entries)-1].ID
			resp.NextCursor = &lastID
		}
		return resp, nil
	})
}

func playerByIDHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	serverID := serverFor(r)

	switch r.Method {
	case "PATCH":
		if !can(r, permPlayersUpdate) {
			forbidden(w)
			return
		}
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		result, err := db.Exec(`UPDATE players SET status = ? WHERE id = ? AND server_id = ?`, body.Status, id, serverID)
		if err != nil {
			serverError(w, err)
			return
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			http.Error(w, "Player not found", http.StatusNotFound)
			return
		}

		cache.Invalidate(serverID, "players")
		w.WriteHeader(http.StatusOK)

	case "DELETE":
		if !can(r, permPlayersDelete) {
			forbidden(w)
			return
		}
		result, err := db.Exec(`DELETE FROM players WHERE id = ? AND server_id = ?`, id, serverID)
		if err != nil {
			serverError(w, err)
			return
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			http.Error(w, "Player not found", http.StatusNotFound)
			return
		}

		cache.Invalidate(serverID, "players")
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func warrantByIDHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	serverID := serverFor(r)

	switch r.Method {
	case "PATCH":
		if !can(r, permWarrantsClear) {
			forbidden(w)
			return
		}
		var body struct {
			Active bool `json:"active"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		result, err := db.Exec(`UPDATE warrants SET active = ? WHERE id = ? AND server_id = ?`, body.Active, id, serverID)
		if err != nil {
			serverError(w, err)
			return
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			http.Error(w, "Warrant not found", http.StatusNotFound)
			return
		}

		cache.Invalidate(serverID, "warrants")
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func arrestByIDHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	serverID := serverFor(r)

	switch r.Method {
	case "PATCH":
		if !can(r, permArrestsUpdate) {
			forbidden(w)
			return
		}
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		result, err := db.Exec(`UPDATE arrests SET status = ? WHERE id = ? AND server_id = ?`, body.Status, id, serverID)
		if err != nil {
			serverError(w, err)
			return
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			http.Error(w, "Arrest not found", http.StatusNotFound)
			return
		}

		cache.Invalidate(serverID, "arrests")
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

type statsResponse struct {
	Players struct {
		Total   int `json:"total"`
		Online  int `json:"online"`
		Idle    int `json:"idle"`
		Offline int `json:"offline"`
	} `json:"players"`
	Warrants struct {
		Total  int `json:"total"`
		Active int `json:"active"`
	} `json:"warrants"`
	Arrests struct {
		Total   int `json:"total"`
		Pending int `json:"pending"`
	} `json:"arrests"`
}

// statsHandler returns exact counts for one server, computed in the
// database (COUNT) rather than from a page of rows, and cached per server.
func statsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !can(r, permStatsRead) {
		forbidden(w)
		return
	}
	serverID := serverFor(r)

	serveCachedList(w, serverID, "stats", "summary", func() (any, error) {
		var s statsResponse

		err := db.QueryRow(`SELECT COUNT(*),
				COUNT(CASE WHEN status = 'online'  THEN 1 END),
				COUNT(CASE WHEN status = 'idle'    THEN 1 END),
				COUNT(CASE WHEN status = 'offline' THEN 1 END)
			FROM players WHERE server_id = ?`, serverID).
			Scan(&s.Players.Total, &s.Players.Online, &s.Players.Idle, &s.Players.Offline)
		if err != nil {
			return nil, err
		}

		err = db.QueryRow(`SELECT COUNT(*), COUNT(CASE WHEN active = 1 THEN 1 END)
			FROM warrants WHERE server_id = ?`, serverID).
			Scan(&s.Warrants.Total, &s.Warrants.Active)
		if err != nil {
			return nil, err
		}

		err = db.QueryRow(`SELECT COUNT(*), COUNT(CASE WHEN status = 'pending' THEN 1 END)
			FROM arrests WHERE server_id = ?`, serverID).
			Scan(&s.Arrests.Total, &s.Arrests.Pending)
		if err != nil {
			return nil, err
		}

		return s, nil
	})
}

func roleName(rl role) string {
	switch rl {
	case roleAdmin:
		return "admin"
	case roleJudge:
		return "judge"
	case rolePolice:
		return "police"
	case roleViewer:
		return "viewer"
	default:
		return "none"
	}
}

func logAudit(r *http.Request) {
	serverID := serverFor(r)
	// Who did it: the Discord username, or "api key" for key callers.
	id, ok := identityFrom(r)
	if !ok {
		id = resolveIdentity(r)
	}
	actor := id.Username
	if actor == "" {
		actor = "api key"
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	_, err = db.Exec(
		`INSERT INTO audit_log (server_id, timestamp, role, actor, method, path, ip) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		serverID, time.Now().Format(time.RFC3339), roleName(id.Role), actor, r.Method, r.URL.Path, ip,
	)
	if err != nil {
		log.Printf("audit log failed: %v", err)
		return
	}
	// New entry written: make the next audit-log read fresh.
	cache.Invalidate(serverID, "audit_log")
}

func whoamiHandler(w http.ResponseWriter, r *http.Request) {
	id := resolveIdentity(r)
	if id.Role == roleNone && id.UserID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	resp := map[string]any{}
	if id.Role == roleNone {
		// Signed in with Discord, but no role has been granted here yet.
		resp["role"] = "pending"
		resp["permissions"] = []permission{}
	} else {
		resp["role"] = roleName(id.Role)
		resp["permissions"] = rolePermissions[id.Role]
	}
	if id.Username != "" {
		resp["username"] = id.Username
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(resp)
}

func main() {
	godotenv.Load()

	// API keys come from the environment only. An unset key switches that role
	// off: there are no built-in defaults, because this repository is public.
	adminKey = os.Getenv("ADMIN_KEY")
	viewerKey = os.Getenv("VIEWER_KEY")
	policeKey = os.Getenv("POLICE_KEY")
	judgeKey = os.Getenv("JUDGE_KEY")
	for name, key := range map[string]string{
		"ADMIN_KEY": adminKey, "VIEWER_KEY": viewerKey,
		"POLICE_KEY": policeKey, "JUDGE_KEY": judgeKey,
	} {
		if key == "" {
			log.Printf("%s not set: that API-key role is disabled", name)
		} else if len(key) < 16 {
			log.Printf("%s is short: use 24+ random characters", name)
		}
	}

	loadAuthConfig()

	if !discordEnabled() && adminKey == "" && viewerKey == "" && policeKey == "" && judgeKey == "" {
		log.Fatal("no way to sign in: set at least one API key (ADMIN_KEY, ...) or configure Discord login in server/.env")
	}

	mysqlDSN := os.Getenv("MYSQL_DSN")
	if mysqlDSN == "" {
		log.Fatal("MYSQL_DSN is not set: copy server/.env.example to server/.env and fill it in")
	}
	initDB(mysqlDSN)

	go cleanupVisitors()

	http.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	http.HandleFunc("/api/players", withCORS(rateLimit(requireAuth(playersHandler))))
	http.HandleFunc("/api/arrests", withCORS(rateLimit(requireAuth(arrestsHandler))))
	http.HandleFunc("/api/warrants", withCORS(rateLimit(requireAuth(warrantsHandler))))
	http.HandleFunc("/api/arrests/{id}", withCORS(rateLimit(requireAuth(arrestByIDHandler))))
	http.HandleFunc("/api/warrants/{id}", withCORS(rateLimit(requireAuth(warrantByIDHandler))))
	http.HandleFunc("/api/players/{id}", withCORS(rateLimit(requireAuth(playerByIDHandler))))
	http.HandleFunc("/api/whoami", withCORS(rateLimit(whoamiHandler)))
	http.HandleFunc("/api/audit-log", withCORS(rateLimit(requireAuth(auditLogHandler))))
	http.HandleFunc("/api/stats", withCORS(rateLimit(requireAuth(statsHandler))))
	http.HandleFunc("/api/members", withCORS(rateLimit(requireAuth(membersHandler))))
	http.HandleFunc("/api/members/{id}", withCORS(rateLimit(requireAuth(memberByIDHandler))))
	http.HandleFunc("GET /api/auth/discord", withCORS(rateLimit(discordStartHandler)))
	http.HandleFunc("GET /api/auth/discord/callback", withCORS(rateLimit(discordCallbackHandler)))
	http.HandleFunc("/api/auth/logout", withCORS(rateLimit(withIdentity(logoutHandler))))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: nil,
	}

	go func() {
		log.Printf("Server running on http://localhost:%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutdown signal received, draining in-flight requests...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("forced shutdown:", err)
	}
	log.Println("server exited cleanly")
}
