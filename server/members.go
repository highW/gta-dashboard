package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

type member struct {
	UserID      int     `json:"userId"`
	DiscordID   string  `json:"discordId"`
	Username    string  `json:"username"`
	DisplayName string  `json:"displayName"`
	LastLogin   *string `json:"lastLogin"`
	Role        *string `json:"role"` // null = signed in, no access yet ("pending")
}

// Who an admin of ONE server may see and manage: people who are members of
// this server, plus brand-new sign-ins who belong to no server yet. Members
// of other servers stay invisible, so one community can't read another's roster.
// (Expects the query to LEFT JOIN memberships as m for this server.)
const manageableUsers = `(m.id IS NOT NULL OR NOT EXISTS (SELECT 1 FROM memberships x WHERE x.user_id = u.id))`

// GET /api/members
func membersHandler(w http.ResponseWriter, r *http.Request) {
	if !can(r, permMembersManage) {
		forbidden(w)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	serverID := serverFor(r)

	rows, err := db.Query(`SELECT u.id, u.discord_id, u.username, u.display_name, u.last_login_at, m.role
		FROM users u
		LEFT JOIN memberships m ON m.user_id = u.id AND m.server_id = ?
		WHERE `+manageableUsers+`
		ORDER BY (m.role IS NULL) DESC, u.username`, serverID)
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()

	members := []member{}
	for rows.Next() {
		var m member
		var last, roleStr sql.NullString
		if err := rows.Scan(&m.UserID, &m.DiscordID, &m.Username, &m.DisplayName, &last, &roleStr); err != nil {
			serverError(w, err)
			return
		}
		if last.Valid {
			m.LastLogin = &last.String
		}
		if roleStr.Valid {
			m.Role = &roleStr.String
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		serverError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(members)
}

// PUT /api/members/{id}  body: {"role": "viewer|police|judge|admin|none"}
// "none" removes the person's access. Takes effect on their very next request,
// because roles are read from the database on every call.
func memberByIDHandler(w http.ResponseWriter, r *http.Request) {
	if !can(r, permMembersManage) {
		forbidden(w)
		return
	}
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	serverID := serverFor(r)

	userID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || userID <= 0 {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	var body struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	remove := body.Role == "none"
	if !remove && roleFromName(body.Role) == roleNone {
		http.Error(w, "role must be admin, judge, police, viewer or none", http.StatusBadRequest)
		return
	}

	// The target must be someone this server's admin is allowed to see.
	var currentRole sql.NullString
	err = db.QueryRow(`SELECT m.role FROM users u
		LEFT JOIN memberships m ON m.user_id = u.id AND m.server_id = ?
		WHERE u.id = ? AND `+manageableUsers, serverID, userID).Scan(&currentRole)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	// Never leave a server without an admin.
	if currentRole.Valid && currentRole.String == "admin" && body.Role != "admin" {
		var admins int
		if err := db.QueryRow(`SELECT COUNT(*) FROM memberships WHERE server_id = ? AND role = 'admin'`, serverID).Scan(&admins); err != nil {
			serverError(w, err)
			return
		}
		if admins <= 1 {
			http.Error(w, "Can't remove the last admin on this server", http.StatusConflict)
			return
		}
	}

	if remove {
		_, err = db.Exec(`DELETE FROM memberships WHERE user_id = ? AND server_id = ?`, userID, serverID)
	} else {
		_, err = db.Exec(`INSERT INTO memberships (user_id, server_id, role) VALUES (?, ?, ?) AS new
			ON DUPLICATE KEY UPDATE role = new.role`, userID, serverID, body.Role)
	}
	if err != nil {
		serverError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"userId": userID, "role": body.Role})
}
