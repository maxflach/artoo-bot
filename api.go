package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

// startAPIServer starts the HTTP REST API on the configured port.
func (b *Bot) startAPIServer() {
	port := b.cfg.API.Port
	if port == 0 {
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", b.apiHealth)
	mux.HandleFunc("/v1/send", b.requireAPIKey(b.apiSend))
	mux.HandleFunc("/v1/run", b.requireAPIKey(b.apiRun))

	addr := fmt.Sprintf(":%d", port)
	log.Printf("api: listening on %s", addr)
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("api: server error: %v", err)
		}
	}()
}

// requireAPIKey is middleware that validates the Bearer token.
func (b *Bot) requireAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			apiError(w, http.StatusUnauthorized, "missing Authorization: Bearer <key>")
			return
		}
		raw := strings.TrimPrefix(auth, "Bearer ")
		hash := hashKey(raw)
		if !b.mem.lookupAPIKey(hash) {
			apiError(w, http.StatusUnauthorized, "invalid API key")
			return
		}
		next(w, r)
	}
}

// GET /v1/health — no auth required
func (b *Bot) apiHealth(w http.ResponseWriter, r *http.Request) {
	apiJSON(w, http.StatusOK, map[string]string{"status": "ok", "bot": b.cfg.Persona.Name})
}

// POST /v1/send — send a message to a user
// Body: {"user_id": 123, "text": "hello"}
// user_id is optional; defaults to admin
func (b *Bot) apiSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var body struct {
		UserID int64  `json:"user_id"`
		Text   string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Text == "" {
		apiError(w, http.StatusBadRequest, "text is required")
		return
	}
	chatID := body.UserID
	if chatID == 0 {
		chatID = b.cfg.Telegram.AdminUserID
	}
	if chatID == 0 {
		apiError(w, http.StatusBadRequest, "user_id required (no admin configured)")
		return
	}
	b.reply(chatID, body.Text)
	apiJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// POST /v1/run — run a prompt for a user and send the result via Telegram
// Body: {"user_id": 123, "prompt": "summarize my week", "workspace": "global"}
func (b *Bot) apiRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var body struct {
		UserID    int64  `json:"user_id"`
		Prompt    string `json:"prompt"`
		Workspace string `json:"workspace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Prompt == "" {
		apiError(w, http.StatusBadRequest, "prompt is required")
		return
	}
	userID := body.UserID
	if userID == 0 {
		userID = b.cfg.Telegram.AdminUserID
	}
	sess := b.getSession(userID)
	if sess == nil {
		apiError(w, http.StatusForbidden, "user not found or not allowed")
		return
	}

	sess.mu.Lock()
	ws := sess.workspace
	wd := sess.workingDir
	model := b.activeModelForSession(sess)
	if body.Workspace != "" {
		ws = body.Workspace
		wd = projectDir(wd, body.Workspace)
	}
	sess.mu.Unlock()

	// Acknowledge immediately, run task in background
	apiJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})

	go b.runScheduledTask(0, userID, sess.chatID, ws, wd, body.Prompt)
	_ = model
}

// --- helpers ---

func apiJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func apiError(w http.ResponseWriter, status int, msg string) {
	apiJSON(w, status, map[string]string{"error": msg})
}

func generateAPIKey() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return
	}
	raw = "artoo_" + hex.EncodeToString(b)
	hash = hashKey(raw)
	return
}

func hashKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
