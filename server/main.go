package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

// Dates and times in the API:
//   - a moment in time (lastSeen, audit timestamp) is UTC in RFC 3339 form,
//     "2026-11-16T21:04:05Z"; the browser turns it into local time.
//   - a calendar day (date, dateIssued) is "2026-11-16".
// The database stores them as DATETIME (always UTC) and DATE.

type Player struct {
	ID            int     `json:"id"`
	Name          string  `json:"name"`
	Ping          int     `json:"ping"`
	Status        string  `json:"status"`
	PlaytimeHours int     `json:"playtimeHours"`
	Vehicle       string  `json:"vehicle"`
	LastSeen      *string `json:"lastSeen"` // null = never seen
}

type ArrestRecord struct {
	ID         int    `json:"id"`
	PlayerID   int    `json:"playerId"`
	PlayerName string `json:"playerName"` // filled in by the server; ignored on input
	Charge     string `json:"charge"`
	Officer    string `json:"officer"`
	Date       string `json:"date"` // "" only for old rows whose date was unreadable
	Status     string `json:"status"`
}

type Warrant struct {
	ID         int    `json:"id"`
	PlayerID   int    `json:"playerId"`
	PlayerName string `json:"playerName"` // filled in by the server; ignored on input
	Reason     string `json:"reason"`
	IssuedBy   string `json:"issuedBy"`   // who wrote it: the requesting officer or the issuing judge
	DateIssued string `json:"dateIssued"` // "" only for old rows whose date was unreadable
	// requested -> active (approved) or denied; active -> cleared.
	// Set by the server; ignored on input.
	Status       string  `json:"status"`
	Active       bool    `json:"active"` // status == "active", kept for older clients
	DecidedBy    *string `json:"decidedBy"`
	DecidedAt    *string `json:"decidedAt"` // UTC, RFC 3339
	DecisionNote *string `json:"decisionNote"`
}

// Warrant statuses.
const (
	warrantRequested = "requested"
	warrantActive    = "active"
	warrantDenied    = "denied"
	warrantCleared   = "cleared"
)

var warrantStatuses = map[string]bool{
	warrantRequested: true, warrantActive: true, warrantDenied: true, warrantCleared: true,
}

// Arrest statuses. A voided record stays visible but can't change again.
var arrestStatuses = map[string]bool{
	"pending": true, "convicted": true, "released": true, "voided": true,
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

const defaultRedisURL = "redis://localhost:6379"

// redisOptions reads where Redis lives from REDIS_URL:
//
//	redis://[user:password@]host:port[/db]    (rediss:// for TLS)
//
// Unset means a Redis on this machine, which is what local development uses.
func redisOptions() *redis.Options {
	raw := os.Getenv("REDIS_URL")
	if raw == "" {
		raw = defaultRedisURL
	}
	opt, err := redis.ParseURL(raw)
	if err != nil {
		// The URL can contain a password, so neither it nor the parser's
		// message (which repeats it) goes into the log.
		log.Fatal("REDIS_URL is not a valid Redis URL: expected redis://[user:password@]host:port[/db]")
	}
	// Lets a single call give up early through its context (the rate limiter
	// does this). Calls without a deadline behave exactly as before.
	opt.ContextTimeoutEnabled = true
	return opt
}

func NewCache(ttl time.Duration) *Cache {
	return &Cache{client: redis.NewClient(redisOptions()), ttl: ttl}
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

// How long a cached list page may be served before it is rebuilt.
const cacheTTL = 10 * time.Second

// Built here so the package always has a cache (tests replace it). main()
// builds it again once server/.env is loaded, so a REDIS_URL set in that
// file takes effect.
var cache = NewCache(cacheTTL)

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

// serveUncachedList builds a list with load() and sends it without caching.
// Searches use it: almost every search text is different, so caching them
// would fill Redis with pages nobody asks for twice.
func serveUncachedList(w http.ResponseWriter, load func() (any, error)) {
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
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "BYPASS")
	w.Write(data)
}

// (Rate limiting lives in ratelimit.go, the client's address in clientip.go.)

// ---------------------------------------------------------------------
// Auth (API keys for now; replaced by accounts + memberships later)
// ---------------------------------------------------------------------

var db *sql.DB

type role int

const (
	roleNone role = iota
	roleViewer
	rolePolice
	roleSupervisor
	roleProsecutor
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
	permArrestsDecide permission = "arrests.decide" // convicted / released / back to pending
	permArrestsVoid   permission = "arrests.void"   // strike a record made in error
	permWarrantsRead  permission = "warrants.read"
	permWarrantsReq   permission = "warrants.request" // ask a judge for a warrant
	permWarrantsIssue permission = "warrants.issue"   // issue one directly, already active
	permWarrantsDec   permission = "warrants.decide"  // approve or deny a request
	permWarrantsClear permission = "warrants.clear"
	permStatsRead     permission = "stats.read"
	permAuditRead     permission = "audit.read"
	permMembersManage permission = "members.manage"
)

var allPermissions = []permission{
	permPlayersRead, permPlayersUpdate, permPlayersDelete,
	permArrestsRead, permArrestsCreate, permArrestsDecide, permArrestsVoid,
	permWarrantsRead, permWarrantsReq, permWarrantsIssue, permWarrantsDec, permWarrantsClear,
	permStatsRead, permAuditRead, permMembersManage,
}

var viewerPermissions = []permission{
	permPlayersRead, permArrestsRead, permWarrantsRead, permStatsRead,
}

// rolePermissions is the single place that decides what each role may do.
// To change a role, edit this map (and the table in TestRolePermissions).
//
//	viewer      reads everything
//	police      + logs arrests, requests warrants
//	supervisor  + police, voids records made in error, reads the audit log
//	prosecutor  + decides arrests (convicted / released), requests warrants
//	judge       + approves or denies warrant requests, issues and clears warrants
//	admin       everything, including members and deleting players
var rolePermissions = map[role][]permission{
	roleViewer: viewerPermissions,
	rolePolice: withViewer(permArrestsCreate, permWarrantsReq),
	roleSupervisor: withViewer(permArrestsCreate, permWarrantsReq,
		permArrestsVoid, permAuditRead),
	roleProsecutor: withViewer(permArrestsDecide, permWarrantsReq),
	roleJudge:      withViewer(permWarrantsDec, permWarrantsIssue, permWarrantsClear),
	roleAdmin:      allPermissions,
}

// withViewer is the viewer's read permissions plus the ones given.
func withViewer(extra ...permission) []permission {
	return append(append([]permission{}, viewerPermissions...), extra...)
}

// One API key per role, for testing and scripts. People normally sign in
// with Discord and get their role on the Members page. An unset key
// disables that key (not the role).
var (
	adminKey      string
	viewerKey     string
	policeKey     string
	supervisorKey string
	prosecutorKey string
	judgeKey      string
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
		{prosecutorKey, roleProsecutor},
		{supervisorKey, roleSupervisor},
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

func initDB(dataSource string, migrate bool) {
	cfg, err := mysql.ParseDSN(dataSource)
	if err != nil {
		log.Fatal(err)
	}
	// Only the migration path needs several statements per Exec. The running
	// app never does, so leaving this off stops a SQL injection from chaining
	// a second statement.
	cfg.MultiStatements = migrate
	// RowsAffected reports rows *matched*, not just rows changed, so applying
	// the same value twice (e.g. clearing an already-cleared warrant) is not
	// mistaken for "not found".
	cfg.ClientFoundRows = true

	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		log.Fatal(err)
	}
	db = sql.OpenDB(connector)

	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	if migrate {
		schema, err := os.ReadFile("schema.sql")
		if err != nil {
			log.Fatal(err)
		}
		if _, err := db.Exec(string(schema)); err != nil {
			log.Fatal(err)
		}
	}

	seedOnce()
}

// seedOnce adds the demo data to an empty database, exactly once even when
// several API instances start at the same moment (as they do under Docker
// Compose): without the lock, each sees no players and each adds its own
// copy. A MySQL named lock belongs to one connection, so check and seed run
// while this connection holds it, and the others wait their turn.
func seedOnce() {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	var locked sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK('dashboard_seed', 30)").Scan(&locked); err != nil {
		log.Fatal(err)
	}
	if locked.Int64 != 1 {
		log.Fatal("timed out waiting for another API instance to finish adding the demo data")
	}
	defer conn.ExecContext(ctx, "DO RELEASE_LOCK('dashboard_seed')")

	var count int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM players WHERE server_id = ?", defaultServerID).Scan(&count); err != nil {
		log.Fatal(err)
	}
	if count == 0 {
		seed()
	}
}

func seed() {
	players := []struct {
		name          string
		ping          int
		status        string
		playtimeHours int
		vehicle       string
		minutesAgo    int // last seen this long before the moment of seeding
	}{
		{"ShadowRunner", 34, "online", 142, "Adder", 0},
		{"NightCrawler99", 87, "online", 58, "Zentorno", 0},
		{"GhostDriver", 152, "idle", 301, "Sultan RS", 3},
		{"VendettaX", 41, "online", 89, "Kuruma", 0},
		{"RustyWheels", 0, "offline", 12, "Blista", 120},
	}
	for _, p := range players {
		db.Exec(`INSERT INTO players (server_id, name, ping, status, playtime_hours, vehicle, last_seen)
			VALUES (?, ?, ?, ?, ?, ?, UTC_TIMESTAMP() - INTERVAL ? MINUTE)`,
			defaultServerID, p.name, p.ping, p.status, p.playtimeHours, p.vehicle, p.minutesAgo)
	}

	arrests := []ArrestRecord{
		{PlayerID: 3, Charge: "Grand Theft Auto", Officer: "Officer Reyes", Date: "2026-08-14", Status: "convicted"},
		{PlayerID: 3, Charge: "Evading Police", Officer: "Officer Reyes", Date: "2026-08-14", Status: "convicted"},
		{PlayerID: 5, Charge: "Reckless Driving", Officer: "Officer Chen", Date: "2026-08-20", Status: "released"},
		{PlayerID: 1, Charge: "Assault", Officer: "Officer Chen", Date: "2026-08-25", Status: "pending"},
	}
	for _, a := range arrests {
		db.Exec(`INSERT INTO arrests (server_id, player_id, charge, officer, date, status) VALUES (?, ?, ?, ?, ?, ?)`,
			defaultServerID, a.PlayerID, a.Charge, a.Officer, a.Date, a.Status)
	}

	warrants := []Warrant{
		{PlayerID: 3, Reason: "Failure to appear in court", IssuedBy: "Judge Alvarez", DateIssued: "2026-08-22", Status: warrantActive},
		{PlayerID: 4, Reason: "Outstanding fines", IssuedBy: "Judge Alvarez", DateIssued: "2026-08-18", Status: warrantActive},
		{PlayerID: 5, Reason: "Probation violation", IssuedBy: "Judge Kim", DateIssued: "2026-07-30", Status: warrantCleared},
	}
	for _, w := range warrants {
		db.Exec(`INSERT INTO warrants (server_id, player_id, reason, issued_by, date_issued, status) VALUES (?, ?, ?, ?, ?, ?)`,
			defaultServerID, w.PlayerID, w.Reason, w.IssuedBy, w.DateIssued, w.Status)
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
		// Lets the frontend read how long a rate-limited (429) request should wait.
		w.Header().Set("Access-Control-Expose-Headers", "Retry-After")
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

// The longest search text the API looks at; anything longer is cut off.
const maxSearchLength = 100

// searchTerm returns the trimmed ?q= text, or "" when nothing is searched.
func searchTerm(r *http.Request) string {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if runes := []rune(q); len(runes) > maxSearchLength {
		q = strings.TrimSpace(string(runes[:maxSearchLength]))
	}
	return q
}

// likeEscaper makes user text safe inside a LIKE pattern. Without it a
// search for "50%" or "a_b" would treat % and _ as wildcards. The queries
// name "!" as the escape character themselves (ESCAPE '!'), so this does
// not depend on how the database server treats backslashes.
var likeEscaper = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")

// containsPattern turns search text into a LIKE pattern meaning "contains".
func containsPattern(q string) string {
	return "%" + likeEscaper.Replace(q) + "%"
}

const dateLayout = "2006-01-02"

// normalizeDate checks a calendar day sent by a client, e.g. "2026-11-16".
// Empty means today (UTC). ok is false for anything that is not a real day.
func normalizeDate(s string) (date string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Now().UTC().Format(dateLayout), true
	}
	t, err := time.Parse(dateLayout, s)
	if err != nil || t.Year() < 1000 { // MySQL's DATE starts at year 1000
		return "", false
	}
	return t.Format(dateLayout), true
}

// badDate is the 400 message for a date field that normalizeDate rejected.
func badDate(field string) string {
	return field + " must be a calendar day like 2026-01-31"
}

// playerName looks up a player's name on one server ("" if there is none).
func playerName(serverID, playerID int) string {
	var name string
	db.QueryRow(`SELECT name FROM players WHERE id = ? AND server_id = ?`, playerID, serverID).Scan(&name)
	return name
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
	if a.Status == "" {
		return ""
	}
	if a.Status != "pending" && a.Status != "convicted" && a.Status != "released" {
		return "status must be pending, convicted, or released"
	}
	return ""
}

// actorName is how the audit trail and warrant decisions name the caller:
// the Discord username, or "api key".
func actorName(r *http.Request) string {
	id, ok := identityFrom(r)
	if !ok {
		id = resolveIdentity(r)
	}
	if id.Username != "" {
		return id.Username
	}
	return "api key"
}

// statusFilter reads ?status= for a list. ok is false for a value that isn't
// one of the allowed statuses; "" means no filter.
func statusFilter(r *http.Request, allowed map[string]bool) (status string, ok bool) {
	status = strings.TrimSpace(r.URL.Query().Get("status"))
	return status, status == "" || allowed[status]
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
		rows, err := db.Query(`SELECT id, name, ping, status, playtime_hours, vehicle,
				DATE_FORMAT(last_seen, '%Y-%m-%dT%H:%i:%sZ')
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
		q := searchTerm(r) // matches player name, charge or officer
		status, ok := statusFilter(r, arrestStatuses)
		if !ok {
			http.Error(w, "status must be pending, convicted, released or voided", http.StatusBadRequest)
			return
		}

		load := func() (any, error) {
			query := `SELECT a.id, a.player_id, COALESCE(p.name, ''), a.charge, a.officer,
					COALESCE(DATE_FORMAT(a.date, '%Y-%m-%d'), ''), a.status
				FROM arrests a
				LEFT JOIN players p ON p.id = a.player_id AND p.server_id = a.server_id
				WHERE a.server_id = ?`
			args := []any{serverID}
			if q != "" {
				query += ` AND (p.name LIKE ? ESCAPE '!' OR a.charge LIKE ? ESCAPE '!' OR a.officer LIKE ? ESCAPE '!')`
				pattern := containsPattern(q)
				args = append(args, pattern, pattern, pattern)
			}
			if status != "" {
				query += ` AND a.status = ?`
				args = append(args, status)
			}
			query += ` ORDER BY a.id LIMIT ? OFFSET ?`
			args = append(args, limit, offset)

			rows, err := db.Query(query, args...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()

			arrests := []ArrestRecord{}
			for rows.Next() {
				var a ArrestRecord
				if err := rows.Scan(&a.ID, &a.PlayerID, &a.PlayerName, &a.Charge, &a.Officer, &a.Date, &a.Status); err != nil {
					return nil, err
				}
				arrests = append(arrests, a)
			}
			return arrests, rows.Err()
		}

		if q != "" {
			serveUncachedList(w, load)
		} else {
			serveCachedList(w, serverID, "arrests", fmt.Sprintf("%d:%d:%s", limit, offset, status), load)
		}

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
		if a.Status == "" {
			a.Status = "pending"
		}
		// Logging an arrest is police work; deciding the case is not.
		if a.Status != "pending" && !can(r, permArrestsDecide) {
			http.Error(w, "Forbidden: only a prosecutor can log an arrest as convicted or released", http.StatusForbidden)
			return
		}
		date, ok := normalizeDate(a.Date)
		if !ok {
			http.Error(w, badDate("date"), http.StatusBadRequest)
			return
		}
		a.Date = date

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
		a.PlayerName = playerName(serverID, a.PlayerID)

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
		q := searchTerm(r) // matches player name, reason or who issued it
		status, ok := statusFilter(r, warrantStatuses)
		if !ok {
			http.Error(w, "status must be requested, active, denied or cleared", http.StatusBadRequest)
			return
		}

		load := func() (any, error) {
			query := `SELECT w.id, w.player_id, COALESCE(p.name, ''), w.reason, w.issued_by,
					COALESCE(DATE_FORMAT(w.date_issued, '%Y-%m-%d'), ''), w.status,
					w.decided_by, DATE_FORMAT(w.decided_at, '%Y-%m-%dT%H:%i:%sZ'), w.decision_note
				FROM warrants w
				LEFT JOIN players p ON p.id = w.player_id AND p.server_id = w.server_id
				WHERE w.server_id = ?`
			args := []any{serverID}
			if q != "" {
				query += ` AND (p.name LIKE ? ESCAPE '!' OR w.reason LIKE ? ESCAPE '!' OR w.issued_by LIKE ? ESCAPE '!')`
				pattern := containsPattern(q)
				args = append(args, pattern, pattern, pattern)
			}
			if status != "" {
				query += ` AND w.status = ?`
				args = append(args, status)
			}
			query += ` ORDER BY w.id LIMIT ? OFFSET ?`
			args = append(args, limit, offset)

			rows, err := db.Query(query, args...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()

			warrants := []Warrant{}
			for rows.Next() {
				var wRow Warrant
				if err := rows.Scan(&wRow.ID, &wRow.PlayerID, &wRow.PlayerName, &wRow.Reason, &wRow.IssuedBy, &wRow.DateIssued,
					&wRow.Status, &wRow.DecidedBy, &wRow.DecidedAt, &wRow.DecisionNote); err != nil {
					return nil, err
				}
				wRow.Active = wRow.Status == warrantActive
				warrants = append(warrants, wRow)
			}
			return warrants, rows.Err()
		}

		if q != "" {
			serveUncachedList(w, load)
		} else {
			serveCachedList(w, serverID, "warrants", fmt.Sprintf("%d:%d:%s", limit, offset, status), load)
		}

	case "POST":
		// A judge's warrant is active at once; anyone else who may ask for
		// one creates a request that waits for a judge.
		var status string
		switch {
		case can(r, permWarrantsIssue):
			status = warrantActive
		case can(r, permWarrantsReq):
			status = warrantRequested
		default:
			forbidden(w)
			return
		}
		var newWarrant Warrant
		if err := json.NewDecoder(r.Body).Decode(&newWarrant); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		newWarrant.Status = status
		newWarrant.Active = status == warrantActive
		newWarrant.DecidedBy, newWarrant.DecidedAt, newWarrant.DecisionNote = nil, nil, nil

		if msg := validateWarrant(newWarrant); msg != "" {
			http.Error(w, msg, http.StatusBadRequest)
			return
		}
		date, ok := normalizeDate(newWarrant.DateIssued)
		if !ok {
			http.Error(w, badDate("dateIssued"), http.StatusBadRequest)
			return
		}
		newWarrant.DateIssued = date

		result, err := db.Exec(`INSERT INTO warrants (server_id, player_id, reason, issued_by, date_issued, status) VALUES (?, ?, ?, ?, ?, ?)`,
			serverID, newWarrant.PlayerID, newWarrant.Reason, newWarrant.IssuedBy, newWarrant.DateIssued, newWarrant.Status)
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
		newWarrant.PlayerName = playerName(serverID, newWarrant.PlayerID)

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
			rows, err = db.Query(`SELECT id, DATE_FORMAT(timestamp, '%Y-%m-%dT%H:%i:%sZ'), role, actor, method, path, ip
				FROM audit_log
				WHERE server_id = ? AND id < ? ORDER BY id DESC LIMIT ?`, serverID, beforeID, limit)
		} else {
			rows, err = db.Query(`SELECT id, DATE_FORMAT(timestamp, '%Y-%m-%dT%H:%i:%sZ'), role, actor, method, path, ip
				FROM audit_log
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
		// {"action": "approve" | "deny" | "clear", "note": "..."}
		// The older {"active": false} still means "clear".
		var body struct {
			Action string `json:"action"`
			Note   string `json:"note"`
			Active *bool  `json:"active"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if body.Action == "" && body.Active != nil && !*body.Active {
			body.Action = "clear"
		}

		// Each action: who may do it, and the one status it starts from.
		transitions := map[string]struct {
			perm     permission
			from, to string
		}{
			"approve": {permWarrantsDec, warrantRequested, warrantActive},
			"deny":    {permWarrantsDec, warrantRequested, warrantDenied},
			"clear":   {permWarrantsClear, warrantActive, warrantCleared},
		}
		t, known := transitions[body.Action]
		if !known {
			http.Error(w, "action must be approve, deny or clear", http.StatusBadRequest)
			return
		}
		if !can(r, t.perm) {
			forbidden(w)
			return
		}
		note := strings.TrimSpace(body.Note)
		if len([]rune(note)) > 500 {
			http.Error(w, "note must be at most 500 characters", http.StatusBadRequest)
			return
		}

		// The status check is part of the UPDATE, so two judges acting on
		// the same request at once can't both succeed.
		var result sql.Result
		var err error
		if t.perm == permWarrantsDec {
			result, err = db.Exec(`UPDATE warrants
				SET status = ?, decided_by = ?, decided_at = UTC_TIMESTAMP(), decision_note = NULLIF(?, '')
				WHERE id = ? AND server_id = ? AND status = ?`,
				t.to, actorName(r), note, id, serverID, t.from)
		} else {
			result, err = db.Exec(`UPDATE warrants SET status = ? WHERE id = ? AND server_id = ? AND status = ?`,
				t.to, id, serverID, t.from)
		}
		if err != nil {
			serverError(w, err)
			return
		}
		if rows, _ := result.RowsAffected(); rows == 0 {
			var current string
			err := db.QueryRow(`SELECT status FROM warrants WHERE id = ? AND server_id = ?`, id, serverID).Scan(&current)
			switch {
			case err == sql.ErrNoRows:
				http.Error(w, "Warrant not found", http.StatusNotFound)
			case err != nil:
				serverError(w, err)
			case current == t.to:
				// Already done (a double click, or another judge got there
				// first): nothing to change.
				w.WriteHeader(http.StatusOK)
			default:
				http.Error(w, fmt.Sprintf("This warrant is %s, so it can't be %s", current, map[string]string{
					"approve": "approved", "deny": "denied", "clear": "cleared"}[body.Action]), http.StatusConflict)
			}
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
		if !can(r, permArrestsDecide) && !can(r, permArrestsVoid) {
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
		if !arrestStatuses[body.Status] {
			http.Error(w, "status must be pending, convicted, released or voided", http.StatusBadRequest)
			return
		}
		// Voiding strikes a record made in error (supervisor); every other
		// status is a decision on the case (prosecutor).
		needed := permArrestsDecide
		if body.Status == "voided" {
			needed = permArrestsVoid
		}
		if !can(r, needed) {
			forbidden(w)
			return
		}

		// A voided record never changes again; the WHERE clause makes that
		// check and the update one step.
		result, err := db.Exec(`UPDATE arrests SET status = ? WHERE id = ? AND server_id = ? AND status <> 'voided'`,
			body.Status, id, serverID)
		if err != nil {
			serverError(w, err)
			return
		}
		if rows, _ := result.RowsAffected(); rows == 0 {
			var current string
			err := db.QueryRow(`SELECT status FROM arrests WHERE id = ? AND server_id = ?`, id, serverID).Scan(&current)
			switch {
			case err == sql.ErrNoRows:
				http.Error(w, "Arrest not found", http.StatusNotFound)
			case err != nil:
				serverError(w, err)
			case current == body.Status:
				// Already so (a double click): nothing to change.
				w.WriteHeader(http.StatusOK)
			default: // voided
				http.Error(w, "This arrest was voided and can't be changed", http.StatusConflict)
			}
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
		Total     int `json:"total"`
		Active    int `json:"active"`
		Requested int `json:"requested"` // waiting for a judge
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

		err = db.QueryRow(`SELECT COUNT(*), COUNT(CASE WHEN status = 'active' THEN 1 END),
				COUNT(CASE WHEN status = 'requested' THEN 1 END)
			FROM warrants WHERE server_id = ?`, serverID).
			Scan(&s.Warrants.Total, &s.Warrants.Active, &s.Warrants.Requested)
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
	case roleProsecutor:
		return "prosecutor"
	case roleSupervisor:
		return "supervisor"
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
	// clientIP, not r.RemoteAddr: behind the load balancer the latter is
	// always the balancer. The time is the database's own UTC clock, so
	// entries from different API instances share one clock.
	_, err := db.Exec(
		`INSERT INTO audit_log (server_id, timestamp, role, actor, method, path, ip) VALUES (?, UTC_TIMESTAMP(), ?, ?, ?, ?, ?)`,
		serverID, roleName(id.Role), actor, r.Method, r.URL.Path, clientIP(r),
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

	// These read server/.env, so they have to come after it is loaded.
	cache.client.Close()
	cache = NewCache(cacheTTL) // REDIS_URL
	loadTrustedProxies()       // TRUSTED_PROXIES

	// API keys come from the environment only. An unset key switches that role
	// off: there are no built-in defaults, because this repository is public.
	adminKey = os.Getenv("ADMIN_KEY")
	viewerKey = os.Getenv("VIEWER_KEY")
	policeKey = os.Getenv("POLICE_KEY")
	supervisorKey = os.Getenv("SUPERVISOR_KEY")
	prosecutorKey = os.Getenv("PROSECUTOR_KEY")
	judgeKey = os.Getenv("JUDGE_KEY")
	for name, key := range map[string]string{
		"ADMIN_KEY": adminKey, "VIEWER_KEY": viewerKey,
		"POLICE_KEY": policeKey, "SUPERVISOR_KEY": supervisorKey,
		"PROSECUTOR_KEY": prosecutorKey, "JUDGE_KEY": judgeKey,
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
	initDB(mysqlDSN, false)

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

		// Without limits, a client can keep a connection (and the goroutine
		// serving it) busy forever by sending its request a few bytes at a
		// time. Each phase of a request now has a deadline.
		ReadHeaderTimeout: 5 * time.Second,  // request line and headers
		ReadTimeout:       15 * time.Second, // headers plus body
		// From the end of the headers to the end of the response. Must stay
		// above the slowest handler: the Discord callback can wait on two
		// outbound calls of up to 10s each.
		WriteTimeout: 30 * time.Second,
		// How long an idle keep-alive connection is kept. Must stay ABOVE the
		// load balancer's own idle timeout (90s in server/lb), or the balancer
		// can reuse a connection at the moment this side closes it.
		IdleTimeout: 120 * time.Second,
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
