package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------

func postWarrant(t *testing.T, key string) (int, Warrant) {
	t.Helper()
	w := httptest.NewRecorder()
	body := Warrant{PlayerID: 1, Reason: "Armed robbery", IssuedBy: "Officer Test", DateIssued: "2026-11-17"}
	warrantsHandler(w, newReq("POST", "/api/warrants", body, key))
	var created Warrant
	json.Unmarshal(w.Body.Bytes(), &created)
	return w.Code, created
}

func warrantAction(id int, key, action, note string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := newReq("PATCH", fmt.Sprintf("/api/warrants/%d", id), map[string]string{"action": action, "note": note}, key)
	req.SetPathValue("id", fmt.Sprint(id))
	warrantByIDHandler(w, req)
	return w
}

func arrestStatus(id int, key, status string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := newReq("PATCH", fmt.Sprintf("/api/arrests/%d", id), map[string]string{"status": status}, key)
	req.SetPathValue("id", fmt.Sprint(id))
	arrestByIDHandler(w, req)
	return w
}

func warrantRow(t *testing.T, id int) Warrant {
	t.Helper()
	for _, wa := range listWarrants(t, defaultServerID, "") {
		if wa.ID == id {
			return wa
		}
	}
	t.Fatalf("warrant %d not in the list", id)
	return Warrant{}
}

func arrestStatusInDB(id int) string {
	var s string
	db.QueryRow(`SELECT status FROM arrests WHERE id = ?`, id).Scan(&s)
	return s
}

// ---------------------------------------------------------------------
// Warrants: request -> approve / deny, active -> clear
// ---------------------------------------------------------------------

func TestWarrant_PoliceRequestsJudgeIssues(t *testing.T) {
	setupTestDB(t)

	code, req := postWarrant(t, policeKey)
	if code != http.StatusCreated || req.Status != "requested" || req.Active {
		t.Errorf("police: expected 201 and a request, got %d / status=%q active=%v", code, req.Status, req.Active)
	}
	code, issued := postWarrant(t, judgeKey)
	if code != http.StatusCreated || issued.Status != "active" || !issued.Active {
		t.Errorf("judge: expected 201 and an active warrant, got %d / status=%q active=%v", code, issued.Status, issued.Active)
	}
	if code, _ := postWarrant(t, viewerKey); code != http.StatusForbidden {
		t.Errorf("viewer: expected 403, got %d", code)
	}

	// The client can't choose the status or fake a decision.
	w := httptest.NewRecorder()
	warrantsHandler(w, newReq("POST", "/api/warrants", map[string]any{
		"playerId": 1, "reason": "r", "issuedBy": "i", "status": "active", "active": true, "decidedBy": "Judge Fake",
	}, policeKey))
	var forged Warrant
	json.Unmarshal(w.Body.Bytes(), &forged)
	if forged.Status != "requested" || forged.DecidedBy != nil {
		t.Errorf("a police request must stay a request whatever the body says, got %+v", forged)
	}
	if got := warrantRow(t, forged.ID); got.Status != "requested" || got.Active {
		t.Errorf("stored: expected requested / inactive, got %q / %v", got.Status, got.Active)
	}
}

func TestWarrant_ApproveRecordsTheDecision(t *testing.T) {
	setupTestDB(t)
	_, req := postWarrant(t, policeKey)

	if w := warrantAction(req.ID, judgeKey, "approve", "  Probable cause established  "); w.Code != http.StatusOK {
		t.Fatalf("approve: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	got := warrantRow(t, req.ID)
	if got.Status != "active" || !got.Active {
		t.Errorf("after approval: expected active, got %q / %v", got.Status, got.Active)
	}
	if got.DecidedBy == nil || *got.DecidedBy != "api key" {
		t.Errorf("decidedBy should name who decided, got %v", got.DecidedBy)
	}
	if got.DecidedAt == nil || !strings.HasSuffix(*got.DecidedAt, "Z") {
		t.Errorf("decidedAt should be a UTC time, got %v", got.DecidedAt)
	}
	if got.DecisionNote == nil || *got.DecisionNote != "Probable cause established" {
		t.Errorf("note should be stored trimmed, got %v", got.DecisionNote)
	}

	// Approving again is harmless; denying an active warrant is a conflict.
	if w := warrantAction(req.ID, judgeKey, "approve", ""); w.Code != http.StatusOK {
		t.Errorf("approving twice: expected 200, got %d", w.Code)
	}
	if w := warrantAction(req.ID, judgeKey, "deny", ""); w.Code != http.StatusConflict ||
		!strings.Contains(w.Body.String(), "active") {
		t.Errorf("deny after approval: expected 409 naming the status, got %d (%s)", w.Code, w.Body.String())
	}

	// Then it can be cleared like any warrant.
	if w := warrantAction(req.ID, judgeKey, "clear", ""); w.Code != http.StatusOK {
		t.Errorf("clear: expected 200, got %d", w.Code)
	}
	if got := warrantRow(t, req.ID); got.Status != "cleared" || got.Active {
		t.Errorf("after clearing: expected cleared, got %q", got.Status)
	}
}

func TestWarrant_DenyIsFinal(t *testing.T) {
	setupTestDB(t)
	_, req := postWarrant(t, prosecutorKey)
	if req.Status != "requested" {
		t.Fatalf("prosecutor: expected a request, got %q", req.Status)
	}

	if w := warrantAction(req.ID, judgeKey, "deny", "Not enough evidence"); w.Code != http.StatusOK {
		t.Fatalf("deny: expected 200, got %d", w.Code)
	}
	got := warrantRow(t, req.ID)
	if got.Status != "denied" || got.Active || got.DecisionNote == nil {
		t.Errorf("after denial: got %+v", got)
	}
	for _, action := range []string{"approve", "clear"} {
		if w := warrantAction(req.ID, judgeKey, action, ""); w.Code != http.StatusConflict {
			t.Errorf("%s a denied warrant: expected 409, got %d", action, w.Code)
		}
	}
	// An active warrant can't be "cleared" from a request either.
	_, req2 := postWarrant(t, policeKey)
	if w := warrantAction(req2.ID, judgeKey, "clear", ""); w.Code != http.StatusConflict {
		t.Errorf("clear a request: expected 409, got %d", w.Code)
	}
}

func TestWarrant_BadInput(t *testing.T) {
	setupTestDB(t)
	_, req := postWarrant(t, policeKey)

	if w := warrantAction(req.ID, judgeKey, "pardon", ""); w.Code != http.StatusBadRequest {
		t.Errorf("unknown action: expected 400, got %d", w.Code)
	}
	if w := warrantAction(req.ID, judgeKey, "approve", strings.Repeat("x", 501)); w.Code != http.StatusBadRequest {
		t.Errorf("501-character note: expected 400, got %d", w.Code)
	}
	if w := warrantAction(999999, judgeKey, "approve", ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown warrant: expected 404, got %d", w.Code)
	}
	// Police may request but never decide.
	if w := warrantAction(req.ID, policeKey, "approve", ""); w.Code != http.StatusForbidden {
		t.Errorf("police approving: expected 403, got %d", w.Code)
	}
	if got := warrantRow(t, req.ID); got.Status != "requested" {
		t.Errorf("nothing above may change the request, got %q", got.Status)
	}
}

// Two judges acting on the same request at the same moment: exactly one
// decision wins, and the other judge is told.
func TestWarrant_SimultaneousDecisions(t *testing.T) {
	setupTestDB(t)
	_, req := postWarrant(t, policeKey)

	actions := []string{"approve", "deny", "approve", "deny", "approve", "deny", "approve", "deny"}
	codes := make([]int, len(actions))
	var wg sync.WaitGroup
	for i, a := range actions {
		wg.Add(1)
		go func(i int, a string) {
			defer wg.Done()
			codes[i] = warrantAction(req.ID, judgeKey, a, "").Code
		}(i, a)
	}
	wg.Wait()

	final := warrantRow(t, req.ID).Status
	winner := map[string]string{"active": "approve", "denied": "deny"}[final]
	if winner == "" {
		t.Fatalf("expected the request to end active or denied, got %q", final)
	}
	for i, a := range actions {
		want := http.StatusConflict
		if a == winner {
			want = http.StatusOK
		}
		if codes[i] != want {
			t.Errorf("%s (#%d) with final status %s: expected %d, got %d", a, i, final, want, codes[i])
		}
	}
}

func TestWarrant_StatusFilterAndStats(t *testing.T) {
	setupTestDB(t)
	_, r1 := postWarrant(t, policeKey)
	_, r2 := postWarrant(t, policeKey)

	list := func(status string) []Warrant {
		w := httptest.NewRecorder()
		warrantsHandler(w, newReq("GET", "/api/warrants?status="+status, nil, judgeKey))
		if w.Code != http.StatusOK {
			t.Fatalf("status=%s: expected 200, got %d", status, w.Code)
		}
		var out []Warrant
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	requested := func() int {
		w := httptest.NewRecorder()
		statsHandler(w, newReq("GET", "/api/stats", nil, judgeKey))
		var s statsResponse
		json.Unmarshal(w.Body.Bytes(), &s)
		return s.Warrants.Requested
	}

	if got := list("requested"); len(got) != 2 {
		t.Errorf("expected 2 requests, got %d", len(got))
	}
	if got := requested(); got != 2 {
		t.Errorf("stats: expected 2 requests waiting, got %d", got)
	}

	warrantAction(r1.ID, judgeKey, "approve", "")

	// Both lists are cached; the approval must show up at once in each.
	if got := list("requested"); len(got) != 1 || got[0].ID != r2.ID {
		t.Errorf("after one approval: expected only request %d waiting, got %+v", r2.ID, got)
	}
	if got := list("active"); len(got) != 3 { // 2 from the demo data + the approved one
		t.Errorf("expected 3 active warrants, got %d", len(got))
	}
	if got := requested(); got != 1 {
		t.Errorf("stats after approval: expected 1 request waiting, got %d", got)
	}

	w := httptest.NewRecorder()
	warrantsHandler(w, newReq("GET", "/api/warrants?status=pending", nil, judgeKey))
	if w.Code != http.StatusBadRequest {
		t.Errorf("unknown status filter: expected 400, got %d", w.Code)
	}
}

func TestWarrant_StaysInsideOneServer(t *testing.T) {
	setupTestDB(t)
	s2 := addServer(t, "second")
	p2 := addPlayer(t, s2, "Elsewhere")
	res, err := db.Exec(`INSERT INTO warrants (server_id, player_id, reason, issued_by, date_issued, status)
		VALUES (?, ?, 'r', 'i', '2026-11-17', 'requested')`, s2, p2)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()

	// A judge acting on server 1 can't decide server 2's request.
	if w := warrantAction(int(id), judgeKey, "approve", ""); w.Code != http.StatusNotFound {
		t.Errorf("expected 404 from another server, got %d", w.Code)
	}
	var status string
	db.QueryRow(`SELECT status FROM warrants WHERE id = ?`, id).Scan(&status)
	if status != "requested" {
		t.Errorf("server 2's request must be untouched, got %q", status)
	}
}

// ---------------------------------------------------------------------
// Arrests: police log, prosecutors decide, supervisors void
// ---------------------------------------------------------------------

func TestArrest_OnlyProsecutorsDecideCases(t *testing.T) {
	setupTestDB(t)
	post := func(key, status string) (int, ArrestRecord) {
		w := httptest.NewRecorder()
		arrestsHandler(w, newReq("POST", "/api/arrests",
			map[string]any{"playerId": 1, "charge": "Theft", "officer": "Officer T", "status": status}, key))
		var a ArrestRecord
		json.Unmarshal(w.Body.Bytes(), &a)
		return w.Code, a
	}

	if code, a := post(policeKey, ""); code != http.StatusCreated || a.Status != "pending" {
		t.Errorf("police, no status: expected 201 pending, got %d %q", code, a.Status)
	}
	if code, _ := post(policeKey, "convicted"); code != http.StatusForbidden {
		t.Errorf("police logging a conviction: expected 403, got %d", code)
	}
	if code, a := post(prosecutorKey, "convicted"); code != http.StatusForbidden {
		// prosecutors decide cases but don't log arrests
		t.Errorf("prosecutor creating an arrest: expected 403, got %d (%q)", code, a.Status)
	}
	if code, _ := post(policeKey, "voided"); code != http.StatusBadRequest {
		t.Errorf("creating a voided arrest: expected 400, got %d", code)
	}

	_, a := post(policeKey, "")
	if w := arrestStatus(a.ID, policeKey, "convicted"); w.Code != http.StatusForbidden {
		t.Errorf("police deciding: expected 403, got %d", w.Code)
	}
	if w := arrestStatus(a.ID, supervisorKey, "released"); w.Code != http.StatusForbidden {
		t.Errorf("supervisor deciding: expected 403, got %d", w.Code)
	}
	if w := arrestStatus(a.ID, prosecutorKey, "convicted"); w.Code != http.StatusOK {
		t.Errorf("prosecutor deciding: expected 200, got %d", w.Code)
	}
	if got := arrestStatusInDB(a.ID); got != "convicted" {
		t.Errorf("expected convicted, got %q", got)
	}
	// A decision can be reopened by a prosecutor.
	if w := arrestStatus(a.ID, prosecutorKey, "pending"); w.Code != http.StatusOK || arrestStatusInDB(a.ID) != "pending" {
		t.Errorf("reopening: got %d / %q", w.Code, arrestStatusInDB(a.ID))
	}
	// Any text used to be accepted as a status.
	if w := arrestStatus(a.ID, prosecutorKey, "totally-made-up"); w.Code != http.StatusBadRequest {
		t.Errorf("made-up status: expected 400, got %d", w.Code)
	}
}

func TestArrest_VoidIsFinal(t *testing.T) {
	setupTestDB(t)
	id := 1 // demo arrest, convicted

	if w := arrestStatus(id, prosecutorKey, "voided"); w.Code != http.StatusForbidden {
		t.Errorf("prosecutor voiding: expected 403, got %d", w.Code)
	}
	if w := arrestStatus(id, supervisorKey, "voided"); w.Code != http.StatusOK {
		t.Fatalf("supervisor voiding: expected 200, got %d", w.Code)
	}
	if w := arrestStatus(id, supervisorKey, "voided"); w.Code != http.StatusOK {
		t.Errorf("voiding twice (a double click): expected 200, got %d", w.Code)
	}
	for _, s := range []string{"pending", "convicted", "released"} {
		if w := arrestStatus(id, adminKey, s); w.Code != http.StatusConflict {
			t.Errorf("changing a voided arrest to %s: expected 409, got %d", s, w.Code)
		}
	}
	if got := arrestStatusInDB(id); got != "voided" {
		t.Errorf("expected voided, got %q", got)
	}

	// Voided records still show, and can be filtered.
	w := httptest.NewRecorder()
	arrestsHandler(w, newReq("GET", "/api/arrests?status=voided", nil, viewerKey))
	var voided []ArrestRecord
	json.Unmarshal(w.Body.Bytes(), &voided)
	if len(voided) != 1 || voided[0].ID != id {
		t.Errorf("status=voided: expected arrest %d, got %+v", id, voided)
	}
	if w := arrestStatus(424242, supervisorKey, "voided"); w.Code != http.StatusNotFound {
		t.Errorf("unknown arrest: expected 404, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------
// The new roles reach the frontend and can be granted
// ---------------------------------------------------------------------

func TestNewRoles_WhoamiAndMembers(t *testing.T) {
	setupTestDB(t)

	for key, want := range map[string]struct {
		role string
		has  []string
		not  []string
	}{
		supervisorKey: {"supervisor", []string{"arrests.create", "arrests.void", "warrants.request", "audit.read"}, []string{"arrests.decide", "warrants.decide", "members.manage"}},
		prosecutorKey: {"prosecutor", []string{"arrests.decide", "warrants.request"}, []string{"arrests.create", "arrests.void", "warrants.decide", "audit.read"}},
		judgeKey:      {"judge", []string{"warrants.decide", "warrants.issue", "warrants.clear"}, []string{"arrests.create", "arrests.decide", "warrants.request"}},
		policeKey:     {"police", []string{"arrests.create", "warrants.request"}, []string{"arrests.decide", "arrests.void", "warrants.issue"}},
	} {
		w := httptest.NewRecorder()
		whoamiHandler(w, newReq("GET", "/api/whoami", nil, key))
		var resp struct {
			Role        string   `json:"role"`
			Permissions []string `json:"permissions"`
		}
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Role != want.role {
			t.Errorf("expected role %s, got %q", want.role, resp.Role)
		}
		for _, p := range want.has {
			if !has(resp.Permissions, p) {
				t.Errorf("%s should have %s", want.role, p)
			}
		}
		for _, p := range want.not {
			if has(resp.Permissions, p) {
				t.Errorf("%s should not have %s", want.role, p)
			}
		}
	}

	// An admin can grant the new roles to a Discord member.
	for _, roleName := range []string{"supervisor", "prosecutor"} {
		uid := addUser(t, "discord-"+roleName, roleName+"-user")
		req := newReq("PUT", fmt.Sprintf("/api/members/%d", uid), map[string]string{"role": roleName}, adminKey)
		req.SetPathValue("id", fmt.Sprint(uid))
		if code := call(requireAuth(memberByIDHandler), req); code != http.StatusOK {
			t.Errorf("grant %s: expected 200, got %d", roleName, code)
		}
		var stored string
		db.QueryRow(`SELECT role FROM memberships WHERE user_id = ?`, uid).Scan(&stored)
		if stored != roleName {
			t.Errorf("membership should store %q, got %q", roleName, stored)
		}
	}
}

// The frontend keeps its own list of role names and treats any other as a
// failed sign-in. A role added here but not there could never log in (this
// happened once), so the two lists are checked against each other.
func TestFrontendKnowsEveryRole(t *testing.T) {
	src, err := os.ReadFile("../gta-dashboard/src/context/AuthContext.tsx")
	if err != nil {
		t.Skipf("frontend source not found: %v", err)
	}
	m := regexp.MustCompile(`const ROLES = \[([^\]]*)\]`).FindSubmatch(src)
	if m == nil {
		t.Fatal("couldn't find `const ROLES = [...]` in AuthContext.tsx")
	}
	for rl := range rolePermissions {
		name := roleName(rl)
		if !strings.Contains(string(m[1]), `"`+name+`"`) {
			t.Errorf("role %q is missing from ROLES in gta-dashboard/src/context/AuthContext.tsx", name)
		}
	}
}
