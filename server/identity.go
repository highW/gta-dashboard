package main

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
)

// identity is who is calling, resolved once per request.
type identity struct {
	Role     role
	UserID   int // 0 when the caller used an API key
	Username string
}

type identityCtxKey struct{}

func identityFrom(r *http.Request) (identity, bool) {
	id, ok := r.Context().Value(identityCtxKey{}).(identity)
	return id, ok
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// csrfOK protects cookie-authenticated writes. Browsers attach cookies to
// requests started by any site, but a custom header can only be sent by our
// own frontend (CORS only allows our origin to send it), and the Origin
// header, when present, must be ours.
func csrfOK(r *http.Request) bool {
	if r.Header.Get("X-Requested-With") != "dashboard" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != frontendURL {
		return false
	}
	return true
}

func roleFromName(name string) role {
	switch name {
	case "admin":
		return roleAdmin
	case "judge":
		return roleJudge
	case "police":
		return rolePolice
	case "viewer":
		return roleViewer
	}
	return roleNone // unknown names get no access
}

// sessionUser returns the user id behind the session cookie, if it is valid.
func sessionUser(r *http.Request) (int, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return 0, false
	}
	val, err := cache.client.Get(context.Background(), sessionKey(c.Value)).Result()
	if err != nil {
		return 0, false // unknown, expired, or Redis down: fail closed
	}
	id, err := strconv.Atoi(val)
	return id, err == nil
}

// identityForUser loads a user and their role on ONE server.
// A user with no membership there gets roleNone (shown as "pending").
func identityForUser(userID, serverID int) identity {
	var username string
	var roleStr sql.NullString
	err := db.QueryRow(`SELECT u.username, m.role
		FROM users u
		LEFT JOIN memberships m ON m.user_id = u.id AND m.server_id = ?
		WHERE u.id = ?`, serverID, userID).Scan(&username, &roleStr)
	if err != nil {
		return identity{} // user no longer exists
	}
	id := identity{UserID: userID, Username: username}
	if roleStr.Valid {
		id.Role = roleFromName(roleStr.String)
	}
	return id
}

// resolveIdentity: an API key wins; otherwise the session cookie.
func resolveIdentity(r *http.Request) identity {
	if key := r.Header.Get("X-API-Key"); key != "" {
		return identity{Role: roleFromKey(key)}
	}
	if userID, ok := sessionUser(r); ok {
		return identityForUser(userID, serverFor(r))
	}
	return identity{}
}

// withIdentity resolves the caller once and attaches it to the request, so
// getRole()/can() don't hit Redis and MySQL again for every check.
func withIdentity(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := resolveIdentity(r)
		if id.UserID != 0 && !safeMethod(r.Method) && !csrfOK(r) {
			http.Error(w, "Forbidden: missing or invalid request origin", http.StatusForbidden)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), identityCtxKey{}, id)))
	}
}
