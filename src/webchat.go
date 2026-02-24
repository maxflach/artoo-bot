package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

//go:embed artoo.png
var avatarPNG []byte

//go:embed webchat_dist
var webchatDist embed.FS

// WebChatTransport implements Transport using Server-Sent Events + HTTP POST.
// Browsers connect to GET /chat/sse for a real-time event stream, and POST
// to /chat/message to send messages. Access is gated behind the bot's API
// key mechanism. All web chat sessions run as the configured admin user.
type WebChatTransport struct {
	bot     *Bot
	handler func(IncomingMessage)
	clients sync.Map // sessionID → chan string
}

func newWebChatTransport(bot *Bot) *WebChatTransport {
	return &WebChatTransport{bot: bot}
}

func (wc *WebChatTransport) Name() string { return "wc" }

func (wc *WebChatTransport) Send(chatID string, text string) error {
	chVal, ok := wc.clients.Load(chatID)
	if !ok {
		return fmt.Errorf("webchat: no active session %q", chatID)
	}
	ch := chVal.(chan string)
	select {
	case ch <- text:
	default:
		log.Printf("webchat: client %q send buffer full, dropping message", chatID)
	}
	return nil
}

func (wc *WebChatTransport) SendFile(chatID string, filePath string) error {
	return wc.Send(chatID, fmt.Sprintf("📎 File created: %s", filePath))
}

func (wc *WebChatTransport) SendTyping(_ string) {} // no-op

func (wc *WebChatTransport) Start(handler func(IncomingMessage)) error {
	wc.handler = handler
	// Routes are registered via RegisterRoutes called from startAPIServer.
	// This goroutine just stays alive until the process exits.
	select {}
}

// RegisterRoutes mounts the web chat endpoints on the given mux.
// Must be called before Start().
func (wc *WebChatTransport) RegisterRoutes(mux *http.ServeMux) {
	// Static files: serve the built React app under /chat/
	sub, _ := fs.Sub(webchatDist, "webchat_dist")
	mux.Handle("/chat/", http.StripPrefix("/chat/", http.FileServer(http.FS(sub))))

	// Explicit handlers take priority over the /chat/ catch-all above.
	mux.HandleFunc("/chat/avatar.png", wc.handleAvatar)
	mux.HandleFunc("/chat/sse", wc.handleSSE)
	mux.HandleFunc("/chat/message", wc.bot.requireAPIKey(wc.handleMessage))
	mux.HandleFunc("/chat/projects", wc.bot.requireAPIKey(wc.handleProjects))
	mux.HandleFunc("/chat/switch", wc.bot.requireAPIKey(wc.handleSwitch))
}

func (wc *WebChatTransport) handleAvatar(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(avatarPNG)
}

// handleSSE opens a Server-Sent Events stream. Auth via ?key= query param so the
// browser's EventSource (which can't set headers) can authenticate.
func (wc *WebChatTransport) handleSSE(w http.ResponseWriter, r *http.Request) {
	// Validate API key from query param.
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "missing key", http.StatusUnauthorized)
		return
	}
	if !wc.bot.mem.lookupAPIKey(hashKey(key)) {
		http.Error(w, "invalid key", http.StatusUnauthorized)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	sessionID := generateConnID()
	ch := make(chan string, 16)
	wc.clients.Store(sessionID, ch)
	defer wc.clients.Delete(sessionID)

	// Tell the client its session ID for use in POST requests.
	fmt.Fprintf(w, "event: session\ndata: %s\n\n", sessionID)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			// Encode newlines as \r; client decodes back to \n.
			escaped := strings.ReplaceAll(msg, "\n", "\r")
			fmt.Fprintf(w, "data: %s\n\n", escaped)
			flusher.Flush()
		}
	}
}

// handleMessage receives a user message from the web chat client.
func (wc *WebChatTransport) handleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	var body struct {
		Text      string `json:"text"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Text == "" {
		apiError(w, http.StatusBadRequest, "text is required")
		return
	}

	// Web chat runs as the admin user.
	userID := wc.adminUserID()
	if userID == 0 {
		apiError(w, http.StatusInternalServerError, "no user configured")
		return
	}

	sessionID := body.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("user-%d", userID)
	}
	chatID := makeChatID("wc", sessionID)

	if wc.handler != nil {
		go wc.handler(IncomingMessage{
			UserID: userID,
			ChatID: chatID,
			Text:   body.Text,
		})
	}

	apiJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

// projectEntry is the JSON shape for a single project in the /chat/projects response.
type projectEntry struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Type  string `json:"type"` // "local", "external", "shared"
}

// handleProjects returns the project list for the admin user.
func (wc *WebChatTransport) handleProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}

	userID := wc.adminUserID()
	if userID == 0 {
		apiError(w, http.StatusInternalServerError, "no user configured")
		return
	}

	sess := wc.bot.getSession(userID)
	if sess == nil {
		apiError(w, http.StatusInternalServerError, "no session for user")
		return
	}

	active, entries := wc.bot.buildProjectList(userID, sess)
	apiJSON(w, http.StatusOK, map[string]any{
		"active":   active,
		"projects": entries,
	})
}

// handleSwitch switches the active project for the admin user's session.
func (wc *WebChatTransport) handleSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	var body struct {
		Project string `json:"project"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Project == "" {
		apiError(w, http.StatusBadRequest, "project is required")
		return
	}

	userID := wc.adminUserID()
	if userID == 0 {
		apiError(w, http.StatusInternalServerError, "no user configured")
		return
	}

	sess := wc.bot.getSession(userID)
	if sess == nil {
		apiError(w, http.StatusInternalServerError, "no session for user")
		return
	}

	name, title, err := wc.bot.webchatSwitchProject(sess, body.Project)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}

	apiJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"name":  name,
		"title": title,
	})
}

// adminUserID returns the configured admin user ID, falling back to the first
// known session if no explicit admin is configured.
func (wc *WebChatTransport) adminUserID() int64 {
	if wc.bot.cfg.Telegram.AdminUserID != 0 {
		return wc.bot.cfg.Telegram.AdminUserID
	}
	wc.bot.sessionsMu.RLock()
	defer wc.bot.sessionsMu.RUnlock()
	for id := range wc.bot.sessions {
		return id
	}
	return 0
}

// buildProjectList returns the active project name and the full project list
// for a given user, mirroring the logic in handleProjectList.
func (b *Bot) buildProjectList(userID int64, sess *Session) (active string, entries []projectEntry) {
	sess.mu.Lock()
	activeWS := sess.workspace
	activeWD := sess.workingDir
	activeSharedOwnerID := sess.sharedOwnerID
	sess.mu.Unlock()

	baseWD := userWorkingDir(b.cfg.Backend.WorkingDir, userID)

	// Global project is always first.
	entries = append(entries, projectEntry{Name: "global", Title: "Global (default)", Type: "local"})
	active = "global"
	if activeSharedOwnerID == 0 && activeWS != "" {
		active = activeWS
	}

	// Local sub-projects.
	if dirEntries, err := os.ReadDir(baseWD); err == nil {
		for _, e := range dirEntries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			title := name
			dir := filepath.Join(baseWD, name)
			if readme := readWorkspaceReadme(dir); readme != "" {
				for _, line := range strings.Split(readme, "\n") {
					if strings.HasPrefix(line, "# ") {
						title = strings.TrimPrefix(line, "# ")
						break
					}
				}
			}
			entries = append(entries, projectEntry{Name: name, Title: title, Type: "local"})
		}
	}

	// External paths.
	for _, ap := range b.allowedPathsFor(userID) {
		title := "[ext] " + ap.Alias
		if readme := readWorkspaceReadme(ap.Path); readme != "" {
			for _, line := range strings.Split(readme, "\n") {
				if strings.HasPrefix(line, "# ") {
					title = "[ext] " + strings.TrimPrefix(line, "# ")
					break
				}
			}
		}
		extName := "ext:" + ap.Alias
		entries = append(entries, projectEntry{Name: extName, Title: title, Type: "external"})
		if activeSharedOwnerID == 0 && (activeWS == ap.Alias || activeWD == ap.Path) {
			active = extName
		}
	}

	// Projects shared with this user.
	for _, s := range b.mem.listSharedWithMe(userID) {
		ownerBaseDir := userWorkingDir(b.cfg.Backend.WorkingDir, s.OwnerID)
		wsDir := projectDir(ownerBaseDir, s.Workspace)
		switchKey := fmt.Sprintf("@%s/%s", s.OwnerName, s.Workspace)
		accessLabel := s.Access
		if s.Access == "write" {
			accessLabel = "read & write"
		}
		title := fmt.Sprintf("@%s/%s (%s)", s.OwnerName, s.Workspace, accessLabel)
		entries = append(entries, projectEntry{Name: switchKey, Title: title, Type: "shared"})
		if activeSharedOwnerID != 0 && activeWD == wsDir {
			active = switchKey
		}
	}

	return active, entries
}

// webchatSwitchProject switches the session to the named project without sending
// any reply messages. Returns the canonical name and display title.
func (b *Bot) webchatSwitchProject(sess *Session, name string) (resultName, title string, err error) {
	// Shared project: @owner/project
	if strings.HasPrefix(name, "@") {
		atPart := strings.TrimPrefix(name, "@")
		slashIdx := strings.Index(atPart, "/")
		if slashIdx < 0 {
			return "", "", fmt.Errorf("invalid shared project format; use @owner/project")
		}
		ownerUsername := atPart[:slashIdx]
		projectName := atPart[slashIdx+1:]

		ownerID := b.mem.userIDForUsername(ownerUsername)
		if ownerID == 0 {
			return "", "", fmt.Errorf("user @%s not found", ownerUsername)
		}

		access := b.mem.getShareAccess(sess.userID, ownerID, projectName)
		if access == "" {
			return "", "", fmt.Errorf("no access to @%s/%s", ownerUsername, projectName)
		}

		ownerBaseDir := userWorkingDir(b.cfg.Backend.WorkingDir, ownerID)
		wsDir := projectDir(ownerBaseDir, projectName)
		if _, statErr := os.Stat(wsDir); statErr != nil {
			return "", "", fmt.Errorf("shared project directory not found")
		}

		sess.resetSession()
		sess.mu.Lock()
		sess.workspace = projectName
		sess.workingDir = wsDir
		sess.model = ""
		sess.history = nil
		sess.sharedOwnerID = ownerID
		sess.sharedAccess = access
		sess.mu.Unlock()
		b.mem.saveUserState(sess.userID, projectName, wsDir)

		accessLabel := access
		if access == "write" {
			accessLabel = "read & write"
		}
		return name, fmt.Sprintf("@%s/%s (%s)", ownerUsername, projectName, accessLabel), nil
	}

	// External path: ext:alias or bare alias
	if ap, ok := b.matchAllowedPath(sess.userID, name); ok {
		if _, statErr := os.Stat(ap.Path); statErr != nil {
			return "", "", fmt.Errorf("external directory not accessible: %v", statErr)
		}
		sess.resetSession()
		sess.mu.Lock()
		sess.workspace = ap.Alias
		sess.workingDir = ap.Path
		sess.model = ""
		sess.history = nil
		sess.sharedOwnerID = 0
		sess.sharedAccess = ""
		sess.mu.Unlock()
		b.mem.saveUserState(sess.userID, ap.Alias, ap.Path)
		return "ext:" + ap.Alias, "[ext] " + ap.Alias, nil
	}

	// Reject bare absolute paths.
	if filepath.IsAbs(name) {
		return "", "", fmt.Errorf("absolute paths not allowed")
	}

	// Regular local project.
	baseWD := userWorkingDir(b.cfg.Backend.WorkingDir, sess.userID)
	wsDir := projectDir(baseWD, name)
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create project directory: %v", err)
	}

	sess.resetSession()
	sess.mu.Lock()
	sess.workspace = name
	sess.workingDir = wsDir
	sess.model = ""
	sess.history = nil
	sess.sharedOwnerID = 0
	sess.sharedAccess = ""
	sess.mu.Unlock()
	b.mem.saveUserState(sess.userID, name, wsDir)

	title = name
	if readme := readWorkspaceReadme(wsDir); readme != "" {
		for _, line := range strings.Split(readme, "\n") {
			if strings.HasPrefix(line, "# ") {
				title = strings.TrimPrefix(line, "# ")
				break
			}
		}
	}
	return name, title, nil
}
