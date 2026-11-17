package main

import (
	"sync"
	"testing"
)

// Under Docker Compose both API instances start together on an empty
// database. Each used to see no players and add its own copy of the demo
// data; seedOnce must add it exactly once.
func TestSeedOnce_SimultaneousStartsSeedOnce(t *testing.T) {
	setupTestDB(t)

	// Empty, the way a brand-new database is.
	for _, q := range []string{
		"DELETE FROM audit_log", "DELETE FROM arrests", "DELETE FROM warrants", "DELETE FROM players",
		"ALTER TABLE players AUTO_INCREMENT = 1",
		"ALTER TABLE arrests AUTO_INCREMENT = 1",
		"ALTER TABLE warrants AUTO_INCREMENT = 1",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			seedOnce()
		}()
	}
	wg.Wait()

	counts := map[string]int{}
	for _, table := range []string{"players", "arrests", "warrants"} {
		var n int
		db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n)
		counts[table] = n
	}
	if counts["players"] != 5 || counts["arrests"] != 4 || counts["warrants"] != 3 {
		t.Errorf("four instances starting together should add the demo data once (5 players, 4 arrests, 3 warrants), got %v", counts)
	}

	// Starting again on a database that has data adds nothing.
	seedOnce()
	var players int
	db.QueryRow("SELECT COUNT(*) FROM players").Scan(&players)
	if players != 5 {
		t.Errorf("a restart must not add demo data again, players = %d", players)
	}
}
