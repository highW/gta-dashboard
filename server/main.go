package main
import "os"
import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
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

type cacheEntry struct {
	data      []byte
	expiresAt time.Time
}

type Cache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	ttl     time.Duration
}

func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		entries: make(map[string]cacheEntry),
		ttl:     ttl,
	}
}

func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.data, true
}

func (c *Cache) Set(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{data: data, expiresAt: time.Now().Add(c.ttl)}
}

func (c *Cache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

var cache = NewCache(10 * time.Second)

var db *sql.DB

// change this to a real random string before ever deploying anywhere public
type role int

const (
	roleNone role = iota
	roleViewer
	roleAdmin
)

var adminKey string
var viewerKey string

func init() {
	adminKey = os.Getenv("ADMIN_KEY")
	if adminKey == "" {
		adminKey = "change-this-admin-key-later"
	}
	viewerKey = os.Getenv("VIEWER_KEY")
	if viewerKey == "" {
		viewerKey = "change-this-viewer-key-later"
	}
}


func getRole(r *http.Request) role {
	switch r.Header.Get("X-API-Key") {
	case adminKey:
		return roleAdmin
	case viewerKey:
		return roleViewer
	default:
		return roleNone
	}
}

// requireRole wraps a handler so it only runs if the caller's role meets the minimum.
func requireRole(minRole role, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			h(w, r)
			return
		}
		callerRole := getRole(r)
		if callerRole == roleNone {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if callerRole < minRole {
			http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

func initDB(dataSource string) {
	var err error
	db, err = sql.Open("sqlite", dataSource)
	if err != nil {
		log.Fatal(err)
	}

	schema, err := os.ReadFile("schema.sql")
	if err != nil {
		log.Fatal(err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		log.Fatal(err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM players").Scan(&count)
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
		db.Exec(`INSERT INTO players (name, ping, status, playtime_hours, vehicle, last_seen) VALUES (?, ?, ?, ?, ?, ?)`,
			p.Name, p.Ping, p.Status, p.PlaytimeHours, p.Vehicle, p.LastSeen)
	}

	arrests := []ArrestRecord{
		{0, 3, "Grand Theft Auto", "Officer Reyes", "2026-08-14", "convicted"},
		{0, 3, "Evading Police", "Officer Reyes", "2026-08-14", "convicted"},
		{0, 5, "Reckless Driving", "Officer Chen", "2026-08-20", "released"},
		{0, 1, "Assault", "Officer Chen", "2026-08-25", "pending"},
	}
	for _, a := range arrests {
		db.Exec(`INSERT INTO arrests (player_id, charge, officer, date, status) VALUES (?, ?, ?, ?, ?)`,
			a.PlayerID, a.Charge, a.Officer, a.Date, a.Status)
	}

	warrants := []Warrant{
		{0, 3, "Failure to appear in court", "Judge Alvarez", "2026-08-22", true},
		{0, 4, "Outstanding fines", "Judge Alvarez", "2026-08-18", true},
		{0, 5, "Probation violation", "Judge Kim", "2026-07-30", false},
	}
	for _, w := range warrants {
		db.Exec(`INSERT INTO warrants (player_id, reason, issued_by, date_issued, active) VALUES (?, ?, ?, ?, ?)`,
			w.PlayerID, w.Reason, w.IssuedBy, w.DateIssued, w.Active)
	}
}

func withCORS(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		h(w, r)
	}
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

func playersHandler(w http.ResponseWriter, r *http.Request) {
	if cached, ok := cache.Get("players"); ok {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		w.Write(cached)
		return
	}
	rows, err := db.Query(`SELECT id, name, ping, status, playtime_hours, vehicle, last_seen FROM players`)
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()

	players := []Player{}
	for rows.Next() {
		var p Player
		rows.Scan(&p.ID, &p.Name, &p.Ping, &p.Status, &p.PlaytimeHours, &p.Vehicle, &p.LastSeen)
		players = append(players, p)
	}

	data, _ := json.Marshal(players)
	cache.Set("players", data)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "MISS")
	json.NewEncoder(w).Encode(players)
}

func arrestsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
	//	if getRole(r) != roleAdmin {
	  //      http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
	    //    return
        //}
		rows, err := db.Query(`SELECT id, player_id, charge, officer, date, status FROM arrests`)
		if err != nil {
			serverError(w, err)
			return
		}
		defer rows.Close()

		arrests := []ArrestRecord{}
		for rows.Next() {
			var a ArrestRecord
			rows.Scan(&a.ID, &a.PlayerID, &a.Charge, &a.Officer, &a.Date, &a.Status)
			arrests = append(arrests, a)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(arrests)

	case "POST":
		if getRole(r) != roleAdmin {
	       http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
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

		result, err := db.Exec(`INSERT INTO arrests (player_id, charge, officer, date, status) VALUES (?, ?, ?, ?, ?)`,
			a.PlayerID, a.Charge, a.Officer, a.Date, a.Status)
		if err != nil {
			serverError(w, err)
			return
		}

		id, _ := result.LastInsertId()
		a.ID = int(id)

		cache.Invalidate("arrests")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(a)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func warrantsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
	    //if getRole(r) != roleAdmin {
	        //http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
	        //return
        //}
		rows, err := db.Query(`SELECT id, player_id, reason, issued_by, date_issued, active FROM warrants`)
		if err != nil {
			serverError(w, err)
			return
		}
		defer rows.Close()

		warrants := []Warrant{}
		for rows.Next() {
			var wRow Warrant
			rows.Scan(&wRow.ID, &wRow.PlayerID, &wRow.Reason, &wRow.IssuedBy, &wRow.DateIssued, &wRow.Active)
			warrants = append(warrants, wRow)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(warrants)

	case "POST":
		if getRole(r) != roleAdmin {
	       http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
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

		result, err := db.Exec(`INSERT INTO warrants (player_id, reason, issued_by, date_issued, active) VALUES (?, ?, ?, ?, ?)`,
			newWarrant.PlayerID, newWarrant.Reason, newWarrant.IssuedBy, newWarrant.DateIssued, newWarrant.Active)
		if err != nil {
			serverError(w, err)
			return
		}

		id, _ := result.LastInsertId()
		newWarrant.ID = int(id)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(newWarrant)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func playerByIDHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	switch r.Method {
	case "PATCH":
		if getRole(r) != roleAdmin {
	        http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
	        return    
        }
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		result, err := db.Exec(`UPDATE players SET status = ? WHERE id = ?`, body.Status, id)
		if err != nil {
			serverError(w, err)
			return
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			http.Error(w, "Player not found", http.StatusNotFound)
			return
		}

		cache.Invalidate("players")
		w.WriteHeader(http.StatusOK)

	case "DELETE":
		if getRole(r) != roleAdmin {
	        http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
	        return
        }
		result, err := db.Exec(`DELETE FROM players WHERE id = ?`, id)
		if err != nil {
			serverError(w, err)
			return
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			http.Error(w, "Player not found", http.StatusNotFound)
			return
		}

		cache.Invalidate("players")
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func warrantByIDHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	switch r.Method {
	case "PATCH":
		if getRole(r) != roleAdmin {
	        http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
	        return
        }
		var body struct {
			Active bool `json:"active"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		result, err := db.Exec(`UPDATE warrants SET active = ? WHERE id = ?`, body.Active, id)
		if err != nil {
			serverError(w, err)
			return
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			http.Error(w, "Warrant not found", http.StatusNotFound)
			return
		}

		cache.Invalidate("warrants")
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func arrestByIDHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	switch r.Method {
	case "PATCH":
		if getRole(r) != roleAdmin {
	        http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
	        return
        }
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		result, err := db.Exec(`UPDATE arrests SET status = ? WHERE id = ?`, body.Status, id)
		if err != nil {
			serverError(w, err)
			return
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			http.Error(w, "Arrest not found", http.StatusNotFound)
			return
		}

		cache.Invalidate("arrests")
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("ERROR: %v", err)
	http.Error(w, "Internal server error", http.StatusInternalServerError)
}

func whoamiHandler(w http.ResponseWriter, r *http.Request){
	role:= getRole(r)
	if role == roleNone{
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	roleName := "viewer"
	if role == roleAdmin {
		roleName = "admin"
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"role": roleName})
}

func main() {
	initDB("dashboard.db")
	defer db.Close()

	http.HandleFunc("/api/players", withCORS(requireRole(roleViewer, playersHandler)))
    http.HandleFunc("/api/arrests", withCORS(requireRole(roleViewer, arrestsHandler)))
    http.HandleFunc("/api/warrants", withCORS(requireRole(roleViewer, warrantsHandler)))
    http.HandleFunc("/api/arrests/{id}", withCORS(requireRole(roleViewer, arrestByIDHandler)))
    http.HandleFunc("/api/warrants/{id}", withCORS(requireRole(roleViewer, warrantByIDHandler)))
    http.HandleFunc("/api/players/{id}", withCORS(requireRole(roleViewer, playerByIDHandler)))
	http.HandleFunc("/api/whoami", withCORS(whoamiHandler))

	log.Println("Server running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}