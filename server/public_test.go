package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicStats(t *testing.T) {
	setupTestDB(t)
	// A second live server with one online player, and a server still being
	// set up, whose players must not count.
	live := addServer(t, "live-two")
	addPlayer(t, live, "OnlineElsewhere") // addPlayer creates players online
	pending := addServer(t, "not-live-yet")
	addPlayer(t, pending, "HiddenPlayer")
	db.Exec(`UPDATE servers SET status = 'pending' WHERE id = ?`, pending)

	w := httptest.NewRecorder()
	publicStatsHandler(w, httptest.NewRequest("GET", "/api/public/stats", nil)) // no key, no cookie
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 without signing in, got %d", w.Code)
	}
	var got publicStats
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	// Demo data: 3 online players on server 1.
	if got.Servers.Active != 2 || got.Players.Online != 4 {
		t.Errorf("expected 2 live servers and 4 players online, got %+v", got)
	}
	w = httptest.NewRecorder()
	publicStatsHandler(w, httptest.NewRequest("POST", "/api/public/stats", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: expected 405, got %d", w.Code)
	}
	body := ""
	{
		rec := httptest.NewRecorder()
		publicStatsHandler(rec, httptest.NewRequest("GET", "/api/public/stats", nil))
		body = rec.Body.String()
	}
	for _, leak := range []string{"OnlineElsewhere", "HiddenPlayer", "live-two", "not-live-yet", "Default community"} {
		if strings.Contains(body, leak) {
			t.Errorf("public stats must not reveal %q: %s", leak, body)
		}
	}
}
