package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------
// Configuration (all from server/.env; Discord login is off if unset)
// ---------------------------------------------------------------------

var (
	discordClientID     string
	discordClientSecret string
	discordRedirectURI  string

	// variables so tests can point them at a fake Discord
	discordAuthorizeURL = "https://discord.com/oauth2/authorize"
	discordTokenURL     = "https://discord.com/api/oauth2/token"
	discordUserURL      = "https://discord.com/api/users/@me"

	frontendURL             = "http://localhost:5173"
	cookieSecure            bool // set COOKIE_SECURE=true when served over HTTPS
	bootstrapAdminDiscordID string
)

const (
	sessionCookie = "session"
	stateCookie   = "oauth_state"
	sessionTTL    = 7 * 24 * time.Hour
	stateTTL      = 10 * time.Minute
)

func loadAuthConfig() {
	discordClientID = os.Getenv("DISCORD_CLIENT_ID")
	discordClientSecret = os.Getenv("DISCORD_CLIENT_SECRET")
	discordRedirectURI = os.Getenv("DISCORD_REDIRECT_URI")
	if v := os.Getenv("FRONTEND_URL"); v != "" {
		frontendURL = strings.TrimRight(v, "/")
	}
	cookieSecure = os.Getenv("COOKIE_SECURE") == "true"
	bootstrapAdminDiscordID = os.Getenv("BOOTSTRAP_ADMIN_DISCORD_ID")

	if !discordEnabled() {
		log.Println("Discord login disabled (set DISCORD_CLIENT_ID, DISCORD_CLIENT_SECRET and DISCORD_REDIRECT_URI to enable)")
	}
}

func discordEnabled() bool {
	return discordClientID != "" && discordClientSecret != "" && discordRedirectURI != ""
}

// ---------------------------------------------------------------------
// Tokens, cookies, sessions
// ---------------------------------------------------------------------

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Sessions are stored under a hash of the token, so a leaked Redis dump
// can't be replayed as live logins.
func sessionKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return cache.prefix + "session:" + hex.EncodeToString(sum[:])
}

func setCookie(w http.ResponseWriter, name, value, path string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		MaxAge:   maxAge,
		HttpOnly: true, // JavaScript can never read it
		Secure:   cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func createSession(w http.ResponseWriter, userID int) error {
	token, err := newToken()
	if err != nil {
		return err
	}
	err = cache.client.Set(context.Background(), sessionKey(token), strconv.Itoa(userID), sessionTTL).Err()
	if err != nil {
		return err
	}
	setCookie(w, sessionCookie, token, "/", int(sessionTTL.Seconds()))
	return nil
}

func loginRedirect(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, frontendURL+"/login?error="+code, http.StatusFound)
}

// ---------------------------------------------------------------------
// Discord OAuth2 (authorization code flow, "identify" scope only)
// ---------------------------------------------------------------------

// GET /api/auth/discord: send the browser to Discord.
func discordStartHandler(w http.ResponseWriter, r *http.Request) {
	if !discordEnabled() {
		loginRedirect(w, r, "discord_unavailable")
		return
	}
	state, err := newToken()
	if err != nil {
		loginRedirect(w, r, "server")
		return
	}
	// The state lives in a cookie (not server memory), so it works no matter
	// which backend instance receives the callback.
	setCookie(w, stateCookie, state, "/api/auth", int(stateTTL.Seconds()))

	q := url.Values{}
	q.Set("client_id", discordClientID)
	q.Set("redirect_uri", discordRedirectURI)
	q.Set("response_type", "code")
	q.Set("scope", "identify")
	q.Set("state", state)
	q.Set("prompt", "none")
	http.Redirect(w, r, discordAuthorizeURL+"?"+q.Encode(), http.StatusFound)
}

type discordUser struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
}

var discordHTTP = &http.Client{Timeout: 10 * time.Second}

// fetchDiscordUser swaps the one-time code for a token, reads who the user
// is, and throws the token away (we never store it).
func fetchDiscordUser(code string) (discordUser, error) {
	var user discordUser

	form := url.Values{}
	form.Set("client_id", discordClientID)
	form.Set("client_secret", discordClientSecret)
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", discordRedirectURI)

	res, err := discordHTTP.PostForm(discordTokenURL, form)
	if err != nil {
		return user, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return user, fmt.Errorf("token exchange returned status %d", res.StatusCode)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&tok); err != nil || tok.AccessToken == "" {
		return user, errors.New("unreadable token response")
	}

	req, err := http.NewRequest("GET", discordUserURL, nil)
	if err != nil {
		return user, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	res2, err := discordHTTP.Do(req)
	if err != nil {
		return user, err
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		return user, fmt.Errorf("user lookup returned status %d", res2.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res2.Body, 1<<20)).Decode(&user); err != nil || user.ID == "" {
		return user, errors.New("unreadable user response")
	}
	return user, nil
}

func upsertUser(du discordUser) (int, error) {
	display := du.GlobalName
	if display == "" {
		display = du.Username
	}
	_, err := db.Exec(`INSERT INTO users (discord_id, username, display_name, last_login_at)
		VALUES (?, ?, ?, NOW()) AS new
		ON DUPLICATE KEY UPDATE username = new.username,
			display_name = new.display_name, last_login_at = NOW()`,
		du.ID, du.Username, display)
	if err != nil {
		return 0, err
	}
	var id int
	err = db.QueryRow(`SELECT id FROM users WHERE discord_id = ?`, du.ID).Scan(&id)
	return id, err
}

// GET /api/auth/discord/callback: Discord sends the browser back here.
func discordCallbackHandler(w http.ResponseWriter, r *http.Request) {
	if !discordEnabled() {
		loginRedirect(w, r, "discord_unavailable")
		return
	}

	// The state we issued must come back unchanged, in the same browser.
	// This blocks someone feeding a victim a login link for the attacker's account.
	state := r.URL.Query().Get("state")
	c, cookieErr := r.Cookie(stateCookie)
	setCookie(w, stateCookie, "", "/api/auth", -1) // single use
	if cookieErr != nil || state == "" ||
		subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
		loginRedirect(w, r, "state")
		return
	}

	code := r.URL.Query().Get("code")
	if r.URL.Query().Get("error") != "" || code == "" {
		loginRedirect(w, r, "denied")
		return
	}

	user, err := fetchDiscordUser(code)
	if err != nil {
		log.Printf("discord login failed: %v", err)
		loginRedirect(w, r, "discord")
		return
	}

	userID, err := upsertUser(user)
	if err != nil {
		log.Printf("discord login: saving user failed: %v", err)
		loginRedirect(w, r, "server")
		return
	}

	// The very first admin has to come from somewhere. This Discord ID (from
	// server/.env) is granted admin on the default server at login; later
	// changes to its role are never overwritten.
	if bootstrapAdminDiscordID != "" && user.ID == bootstrapAdminDiscordID {
		_, err := db.Exec(`INSERT IGNORE INTO memberships (user_id, server_id, role) VALUES (?, ?, 'admin')`,
			userID, defaultServerID)
		if err != nil {
			log.Printf("bootstrap admin membership failed: %v", err)
		}
	}

	if err := createSession(w, userID); err != nil {
		log.Printf("discord login: creating session failed: %v", err)
		loginRedirect(w, r, "server")
		return
	}
	http.Redirect(w, r, frontendURL+"/dashboard", http.StatusFound)
}

// POST /api/auth/logout (wrapped in withIdentity, which enforces CSRF).
func logoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		cache.client.Del(context.Background(), sessionKey(c.Value))
	}
	setCookie(w, sessionCookie, "", "/", -1)
	w.WriteHeader(http.StatusNoContent)
}
