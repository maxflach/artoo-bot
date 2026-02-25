package main

import (
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed openapi.yaml
var openapiSpec []byte

const swaggerUIHTML = `<!DOCTYPE html>
<html>
<head>
  <title>%s API</title>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
  SwaggerUIBundle({
    url: "/openapi.yaml",
    dom_id: '#swagger-ui',
    presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
    layout: "BaseLayout"
  });
</script>
</body>
</html>
`

// mcpConns holds active SSE connections, keyed by connection ID.
var mcpConns sync.Map // string → chan []byte

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
	mux.HandleFunc("/mcp/sse", b.requireAPIKey(b.handleMCPSSE))
	mux.HandleFunc("/mcp/message", b.requireAPIKey(b.handleMCPMessage))
	mux.HandleFunc("/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(openapiSpec)
	})
	mux.HandleFunc("/docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, swaggerUIHTML, b.cfg.Persona.Name)
	})

	// Register WebChat routes if the transport is configured.
	b.transportsMu.RLock()
	if wc, ok := b.transports["wc"].(*WebChatTransport); ok {
		wc.RegisterRoutes(mux)
		log.Printf("webchat: chat UI available at :%d/chat", port)
	}
	b.transportsMu.RUnlock()

	addr := fmt.Sprintf(":%d", port)
	log.Printf("api: listening on %s", addr)
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("api: server error: %v", err)
		}
	}()

	b.writeMCPConfig(port)
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
		if !b.mem.lookupAPIKey(hashKey(raw)) {
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
	userID := body.UserID
	if userID == 0 {
		userID = b.cfg.Telegram.AdminUserID
	}
	if userID == 0 {
		apiError(w, http.StatusBadRequest, "user_id required (no admin configured)")
		return
	}
	b.reply(b.chatIDForUser(userID), body.Text)
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
		wd = projectDir(userWorkingDir(b.cfg.Backend.WorkingDir, userID), body.Workspace)
	}
	sess.mu.Unlock()

	// Acknowledge immediately, run task in background
	apiJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})

	chatID := b.chatIDForUser(userID)
	go b.runScheduledTask(0, userID, chatID, ws, wd, body.Prompt)
	_ = model
}

// --- MCP server (JSON-RPC 2.0 over SSE) ---

// GET /mcp/sse — open SSE stream, send endpoint event, keep alive
func (b *Bot) handleMCPSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	connID := generateConnID()
	ch := make(chan []byte, 8)
	mcpConns.Store(connID, ch)
	defer mcpConns.Delete(connID)

	// Tell the client where to POST messages
	msgURL := fmt.Sprintf("http://localhost:%d/mcp/message?id=%s", b.cfg.API.Port, connID)
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", msgURL)
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// POST /mcp/message — receive JSON-RPC 2.0 requests; respond via SSE
func (b *Bot) handleMCPMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	connID := r.URL.Query().Get("id")
	if connID == "" {
		apiError(w, http.StatusBadRequest, "missing id parameter")
		return
	}
	chVal, ok := mcpConns.Load(connID)
	if !ok {
		apiError(w, http.StatusGone, "SSE connection expired or not found")
		return
	}
	ch := chVal.(chan []byte)

	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Notifications (no id) require no response
	if len(req.ID) == 0 || string(req.ID) == "null" {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	var response any
	switch req.Method {
	case "initialize":
		response = map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "artoo", "version": "0.3.0"},
			},
		}

	case "tools/list":
		b.skillsMu.RLock()
		tools := make([]map[string]any, 0, len(b.skills))
		for _, skill := range b.skills {
			desc := skill.Description
			if desc == "" {
				desc = fmt.Sprintf("Run the %s skill", skill.Name)
			}
			tools = append(tools, map[string]any{
				"name":        skill.Name,
				"description": desc,
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"input": map[string]any{
							"type":        "string",
							"description": "Input to pass to the skill",
						},
					},
				},
			})
		}
		b.skillsMu.RUnlock()
		response = map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  map[string]any{"tools": tools},
		}

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			response = mcpErrorResponse(req.ID, -32602, "invalid params")
			break
		}
		var args struct {
			Input string `json:"input"`
		}
		json.Unmarshal(params.Arguments, &args)

		b.skillsMu.RLock()
		skill, found := b.skills[params.Name]
		b.skillsMu.RUnlock()
		if !found {
			response = mcpErrorResponse(req.ID, -32602, fmt.Sprintf("unknown skill: %s", params.Name))
			break
		}

		// Use admin user context for MCP-triggered skills
		adminID := b.cfg.Telegram.AdminUserID
		if adminID == 0 {
			b.sessionsMu.RLock()
			for id := range b.sessions {
				adminID = id
				break
			}
			b.sessionsMu.RUnlock()
		}
		runFn := func(prompt string) (string, error) {
			wd := userWorkingDir(b.cfg.Backend.WorkingDir, adminID)
			return b.runClaude(adminID, adminID, prompt, "global", wd, b.cfg.Backend.DefaultModel, nil)
		}

		result, err := b.dispatchSkill(skill, args.Input, nil, runFn)
		if err != nil {
			response = mcpErrorResponse(req.ID, -32603, err.Error())
			break
		}
		response = map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": result},
				},
			},
		}

	default:
		response = mcpErrorResponse(req.ID, -32601, fmt.Sprintf("unknown method: %s", req.Method))
	}

	data, err := json.Marshal(response)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	ch <- data
	w.WriteHeader(http.StatusAccepted)
}

// writeMCPConfig registers the artoo MCP server in ~/.claude.json.
// Skips if already configured. Generates a dedicated API key on first run.
func (b *Bot) writeMCPConfig(port int) {
	home, _ := os.UserHomeDir()
	claudeJSONPath := filepath.Join(home, ".claude.json")

	var config map[string]any
	if data, err := os.ReadFile(claudeJSONPath); err == nil {
		json.Unmarshal(data, &config)
	}
	if config == nil {
		config = make(map[string]any)
	}

	// Skip if artoo is already registered
	if servers, ok := config["mcpServers"].(map[string]any); ok {
		if artoo, ok := servers["artoo"].(map[string]any); ok {
			if url, _ := artoo["url"].(string); url != "" {
				log.Printf("mcp: artoo already registered in ~/.claude.json")
				return
			}
		}
	}

	// Generate a dedicated API key for MCP access
	raw, hash, err := generateAPIKey()
	if err != nil {
		log.Printf("mcp: failed to generate key: %v", err)
		return
	}
	if _, err := b.mem.createAPIKey("artoo-mcp", hash); err != nil {
		log.Printf("mcp: failed to save key: %v", err)
		return
	}

	// Merge artoo entry into mcpServers
	if config["mcpServers"] == nil {
		config["mcpServers"] = map[string]any{}
	}
	servers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		servers = map[string]any{}
		config["mcpServers"] = servers
	}
	servers["artoo"] = map[string]any{
		"type": "sse",
		"url":  fmt.Sprintf("http://localhost:%d/mcp/sse", port),
		"headers": map[string]string{
			"Authorization": "Bearer " + raw,
		},
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		log.Printf("mcp: failed to marshal config: %v", err)
		return
	}
	if err := os.WriteFile(claudeJSONPath, data, 0600); err != nil {
		log.Printf("mcp: failed to write ~/.claude.json: %v", err)
		return
	}
	log.Printf("mcp: registered artoo MCP server at http://localhost:%d/mcp/sse", port)
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

func mcpErrorResponse(id json.RawMessage, code int, message string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": message},
	}
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

func generateConnID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
