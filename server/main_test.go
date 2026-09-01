package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func setupTestDB(t *testing.T) {
	t.Helper()
	initDB(":memory:")
	cache = NewCache(3 * 1e9) // fresh cache each test, avoids leftover state between tests
}

func TestGetPlayers(t *testing.T) {
	setupTestDB(t)

	req := httptest.NewRequest("GET", "/api/players", nil)
	w := httptest.NewRecorder()

	playersHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var players []Player
	if err := json.NewDecoder(w.Body).Decode(&players); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

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
	body, _ := json.Marshal(newArrest)

	req := httptest.NewRequest("POST", "/api/arrests", bytes.NewReader(body))
	w := httptest.NewRecorder()

	arrestsHandler(w, req)

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

func TestClearWarrant(t *testing.T) {
	setupTestDB(t)

	// warrant with id=1 exists from seed data and starts active
	body, _ := json.Marshal(map[string]bool{"active": false})
	req := httptest.NewRequest("PATCH", "/api/warrants/1", bytes.NewReader(body))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()

	warrantByIDHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var count int
	db.QueryRow("SELECT active FROM warrants WHERE id = 1").Scan(&count)
	if count != 0 {
		t.Errorf("expected warrant to be inactive, got active=%d", count)
	}
}

func TestClearWarrant_NotFound(t *testing.T) {
	setupTestDB(t)

	body, _ := json.Marshal(map[string]bool{"active": false})
	req := httptest.NewRequest("PATCH", "/api/warrants/999", bytes.NewReader(body))
	req.SetPathValue("id", "999")
	w := httptest.NewRecorder()

	warrantByIDHandler(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for nonexistent warrant, got %d", w.Code)
	}
}

func TestCacheHitOnSecondRequest(t *testing.T) {
	setupTestDB(t)

	req1 := httptest.NewRequest("GET", "/api/players", nil)
	w1 := httptest.NewRecorder()
	playersHandler(w1, req1)

	if w1.Header().Get("X-Cache") != "MISS" {
		t.Errorf("expected first request to MISS, got %s", w1.Header().Get("X-Cache"))
	}

	req2 := httptest.NewRequest("GET", "/api/players", nil)
	w2 := httptest.NewRecorder()
	playersHandler(w2, req2)

	if w2.Header().Get("X-Cache") != "HIT" {
		t.Errorf("expected second request to HIT, got %s", w2.Header().Get("X-Cache"))
	}
}
