package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxMsgLen = 4096

type Message struct {
	Role    string // "user" or "assistant"
	Content string
}

// Session holds all per-user mutable state.
// Access any field while holding mu.
type Session struct {
	mu         sync.Mutex
	userID     int64
	chatID     string // transport-prefixed, e.g. "tg:123456789"
	workingDir string
	workspace  string
	model      string // session-level model override; "" means use workspace or default
	history    []Message
}

// ProjectOptions captures answers from the interactive new-project setup flow.
type ProjectOptions struct {
	IsResearch bool
	AutoReport bool
	AgentStyle string // key: "general", "researcher", "engineer", "analyst", "writer"
}

// PendingProject holds state for a project being configured via Telegram buttons.
type PendingProject struct {
	Token       string
	Name        string
	Description string
	WorkingDir  string
	UserID      int64
	ChatID      string
	Model       string
	Step        int // 1=research?, 2=auto-report?, 3=agent style?
	IsResearch  bool
	AutoReport  bool
	AgentStyle  string
}

// agentStyleDef defines a named agent working style preset.
type agentStyleDef struct {
	Key         string
	Label       string
	Description string
}

// agentStyles lists the available agent style presets shown during project creation.
var agentStyles = []agentStyleDef{
	{"general", "General", "Balanced — handles any task without a strong bias. Be pragmatic and efficient."},
	{"researcher", "Researcher", "Research-focused — thorough and methodical. Prioritise breadth, cite sources, verify facts from multiple sources, structure output with clear sections."},
	{"engineer", "Engineer", "Engineering-focused — code first, prose second. Prefer working implementations over explanations. Write tests where appropriate. Be direct."},
	{"analyst", "Analyst", "Data-driven and quantitative. Use tables, numbers, and metrics where possible. Challenge assumptions and surface insights clearly."},
	{"writer", "Writer", "Writing-focused — clear prose and strong structure. Favour readability, logical flow, and precise language."},
}

// agentStyleButtons returns a slice of Buttons for the given callback prefix, one per style.
func agentStyleButtons(prefix string) []Button {
	buttons := make([]Button, len(agentStyles))
	for i, s := range agentStyles {
		buttons[i] = Button{Label: s.Label, Data: fmt.Sprintf("%s:%s", prefix, s.Key)}
	}
	return buttons
}

// agentStyleSection returns the ## Agent README section content for a given style key.
func agentStyleSection(style string) string {
	for _, s := range agentStyles {
		if s.Key == style {
			return fmt.Sprintf("\n## Agent\nStyle: **%s** — %s\n", s.Label, s.Description)
		}
	}
	return ""
}

type Bot struct {
	cfg                *Config
	transports         map[string]Transport
	transportsMu       sync.RWMutex
	allowedIDs         map[int64]bool
	mem                *MemoryStore
	cron               *CronRunner
	sessionsMu         sync.RWMutex
	sessions           map[int64]*Session
	skillsMu           sync.RWMutex
	skills             map[string]*Skill
	secretKey          []byte
	pendingProjectsMu  sync.RWMutex
	pendingProjects    map[string]*PendingProject
}

func newBot(cfg *Config) (*Bot, error) {
	allowed := make(map[int64]bool)
	for _, id := range cfg.Telegram.AllowedUserIDs {
		allowed[id] = true
	}
	for _, id := range cfg.Discord.AllowedUserIDs {
		allowed[id] = true
	}

	mem, err := newMemoryStore()
	if err != nil {
		return nil, fmt.Errorf("memory store: %w", err)
	}

	// Collect all allowed user IDs: config + DB-approved users
	allUserIDs := make([]int64, 0, len(cfg.Telegram.AllowedUserIDs)+len(cfg.Discord.AllowedUserIDs))
	allUserIDs = append(allUserIDs, cfg.Telegram.AllowedUserIDs...)
	allUserIDs = append(allUserIDs, cfg.Discord.AllowedUserIDs...)
	for _, u := range mem.listApprovedUsers() {
		allUserIDs = append(allUserIDs, u.UserID)
	}

	sessions := make(map[int64]*Session, len(allUserIDs))
	for _, id := range allUserIDs {
		sessions[id] = newSession(id, cfg.Backend.WorkingDir, mem)
	}

	secretKey, err := loadSecretKey()
	if err != nil {
		return nil, fmt.Errorf("secret key: %w", err)
	}

	b := &Bot{
		cfg:             cfg,
		transports:      make(map[string]Transport),
		allowedIDs:      allowed,
		mem:             mem,
		sessions:        sessions,
		secretKey:       secretKey,
		pendingProjects: make(map[string]*PendingProject),
	}
	b.cron = newCronRunner(b)
	b.skills = loadSkills(skillPaths(""))

	// Initialize transports.
	if cfg.Telegram.Token != "" {
		tg, err := newTelegramTransport(b)
		if err != nil {
			return nil, fmt.Errorf("telegram: %w", err)
		}
		b.transports["tg"] = tg
	}
	if cfg.Discord.Token != "" {
		dc, err := newDiscordTransport(b)
		if err != nil {
			return nil, fmt.Errorf("discord: %w", err)
		}
		b.transports["dc"] = dc
	}
	if cfg.WebChat.Enabled {
		b.transports["wc"] = newWebChatTransport(b)
	}

	if len(b.transports) == 0 {
		return nil, fmt.Errorf("no transports configured (set telegram.token, discord.token, or webchat.enabled)")
	}

	return b, nil
}

// userWorkingDir returns the per-user working directory.
func userWorkingDir(baseDir string, userID int64) string {
	return filepath.Join(baseDir, fmt.Sprintf("%d", userID))
}

// newSession creates and initialises a session for a user, restoring workspace from DB.
func newSession(id int64, baseDir string, mem *MemoryStore) *Session {
	baseWD := userWorkingDir(baseDir, id)
	os.MkdirAll(baseWD, 0755)
	wd, ws := baseWD, "global"
	if savedWS, savedWD := mem.loadUserState(id); savedWS != "" && savedWD != "" {
		if _, err := os.Stat(savedWD); err == nil {
			wd, ws = savedWD, savedWS
		}
	}
	return &Session{
		userID:     id,
		chatID:     tgChatID(id), // default to Telegram DM; updated on first message
		workingDir: wd,
		workspace:  ws,
	}
}

// getSession returns the session for a given user ID, or nil if not allowed.
func (b *Bot) getSession(userID int64) *Session {
	b.sessionsMu.RLock()
	defer b.sessionsMu.RUnlock()
	return b.sessions[userID]
}

// addSession creates a new session for a newly approved user.
func (b *Bot) addSession(userID int64) *Session {
	sess := newSession(userID, b.cfg.Backend.WorkingDir, b.mem)
	b.sessionsMu.Lock()
	b.sessions[userID] = sess
	b.sessionsMu.Unlock()
	return sess
}

// isAdmin returns true if userID is a configured admin across any transport.
func (b *Bot) isAdmin(userID int64) bool {
	if b.cfg.Telegram.AdminUserID != 0 && userID == b.cfg.Telegram.AdminUserID {
		return true
	}
	if b.cfg.Discord.AdminUserID != 0 && userID == b.cfg.Discord.AdminUserID {
		return true
	}
	return false
}

// chatIDForUser returns the active chatID for a userID, falling back to Telegram DM.
func (b *Bot) chatIDForUser(userID int64) string {
	if sess := b.getSession(userID); sess != nil {
		sess.mu.Lock()
		cid := sess.chatID
		sess.mu.Unlock()
		if cid != "" {
			return cid
		}
	}
	return tgChatID(userID)
}

func (b *Bot) run() {
	b.cron.start()
	defer b.cron.stop()

	b.startAPIServer()

	var wg sync.WaitGroup
	b.transportsMu.RLock()
	for _, tp := range b.transports {
		tp := tp
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := tp.Start(b.handleIncomingMessage); err != nil {
				log.Printf("transport %s stopped: %v", tp.Name(), err)
			}
		}()
	}
	b.transportsMu.RUnlock()

	wg.Wait()
}

// handleIncomingMessage is the central dispatcher for all incoming messages.
func (b *Bot) handleIncomingMessage(msg IncomingMessage) {
	sess := b.getSession(msg.UserID)
	if sess == nil {
		// Unknown user not handled by the transport — silently ignore.
		return
	}

	sess.mu.Lock()
	sess.chatID = msg.ChatID
	sess.mu.Unlock()

	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	if strings.HasPrefix(text, "/") {
		b.handleCommand(msg.ChatID, sess, text)
		return
	}

	b.runUserMessage(msg.ChatID, sess, text)
}

// handleCommand dispatches all /commands. Unknown commands get an error, not Claude.
func (b *Bot) handleCommand(chatID string, sess *Session, text string) {
	parts := strings.SplitN(text, " ", 2)
	cmd := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	// Strip @botname suffix (e.g. /help@mybot)
	if i := strings.Index(cmd, "@"); i != -1 {
		cmd = cmd[:i]
	}
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	switch cmd {
	case "help":
		b.reply(chatID, b.helpText())

	case "new":
		sess.mu.Lock()
		sess.history = nil
		sess.workspace = "global"
		wd := userWorkingDir(b.cfg.Backend.WorkingDir, sess.userID)
		sess.workingDir = wd
		sess.model = ""
		sess.mu.Unlock()
		b.mem.saveUserState(sess.userID, "global", wd)
		b.reply(chatID, "Session reset. Fresh start.")

	case "clear":
		sess.mu.Lock()
		sess.history = nil
		sess.mu.Unlock()
		b.reply(chatID, "Conversation history cleared.")

	case "workspace", "project":
		if args == "" || args == "list" {
			b.handleProjectList(chatID, sess)
			return
		}
		if args == "update" || args == "edit" {
			b.handleProjectUpdateButtons(chatID, sess)
			return
		}
		if strings.HasPrefix(args, "update ") || strings.HasPrefix(args, "edit ") {
			instruction := strings.TrimPrefix(strings.TrimPrefix(args, "update "), "edit ")
			b.handleProjectUpdate(chatID, sess, instruction)
			return
		}
		b.handleWorkspace(chatID, sess, args)

	case "remember":
		if args == "" {
			b.reply(chatID, "Usage: `/remember <fact>` or `/remember --global <fact>`")
			return
		}
		ws := sess.workspace
		if strings.HasPrefix(args, "--global ") {
			args = strings.TrimPrefix(args, "--global ")
			ws = "global"
		}
		if err := b.mem.save(sess.userID, ws, args, "manual"); err != nil {
			b.reply(chatID, "Failed to save.")
		} else {
			wsLabel := ws
			if ws == "global" {
				wsLabel = "global memory"
			}
			b.reply(chatID, fmt.Sprintf("Remembered in *%s* ✓", wsLabel))
		}

	case "memory":
		sess.mu.Lock()
		ws := sess.workspace
		sess.mu.Unlock()
		b.reply(chatID, b.mem.list(sess.userID, ws, b.cfg.Memory.MaxAgeDays))

	case "files":
		sess.mu.Lock()
		ws := sess.workspace
		sess.mu.Unlock()
		b.reply(chatID, b.mem.listFiles(sess.userID, ws))

	case "model":
		if args == "" {
			b.reply(chatID, fmt.Sprintf("Current model: `%s`", b.activeModelForSession(sess)))
			return
		}
		b.handleModelSwitch(chatID, sess, args)

	case "at":
		if args == "" {
			b.reply(chatID, "Usage: `/at <time> | <prompt>`\n\nExamples:\n`/at tomorrow 18:00 | give me a pasta recipe with meat`\n`/at friday 09:00 | summarize my week`\n`/at in 2h | remind me to take a break`\n`/at 2026-03-01 10:00 | wish me happy March`")
			return
		}
		b.handleAt(chatID, sess, args)

	case "schedule":
		if args == "" {
			b.reply(chatID, "Usage: `/schedule <name> | <cron> | <prompt>`")
			return
		}
		b.handleScheduleAdd(chatID, sess, args)

	case "schedules":
		b.handleScheduleList(chatID, sess)

	case "unschedule":
		if args == "" {
			b.reply(chatID, "Usage: `/unschedule <id>` — get IDs with /schedules")
			return
		}
		b.handleScheduleDelete(chatID, sess, args)

	case "apikey", "apikeys":
		if !b.isAdmin(sess.userID) {
			b.reply(chatID, "Only the admin can manage API keys.")
			return
		}
		b.handleAPIKey(chatID, cmd, args)

	case "report":
		if args == "reload" {
			b.reply(chatID, "Templates are loaded fresh on each render ✓")
			return
		}
		sess.mu.Lock()
		wd := sess.workingDir
		ws := sess.workspace
		uid := sess.userID
		sess.mu.Unlock()
		reportPath := filepath.Join(wd, "report.md")
		if _, err := os.Stat(reportPath); os.IsNotExist(err) {
			b.reply(chatID, "No `report.md` found in the current project.\n\nAsk me to generate a report and I'll create one.")
			return
		}
		userBaseDir := userWorkingDir(b.cfg.Backend.WorkingDir, uid)
		tmpl, _ := loadReportTemplate(wd, userBaseDir)
		outPath := strings.TrimSuffix(reportPath, ".md") + "_" + time.Now().Format("2006-01-02") + ".pdf"
		if err := RenderMarkdownReport(reportPath, outPath, tmpl); err != nil {
			b.reply(chatID, fmt.Sprintf("Report render failed: %v", err))
			return
		}
		b.sendFileAuto(chatID, outPath)
		if info, err := os.Stat(outPath); err == nil {
			b.mem.recordFile(uid, ws, filepath.Base(outPath), outPath, info.Size())
		}

	case "skills":
		b.handleSkillsCommand(chatID, sess, args)

	case "secret":
		b.handleSecretCommand(chatID, sess, args)

	case "wish":
		if args == "" {
			b.reply(chatID, "Usage: `/wish <your message>`")
			return
		}
		username := b.mem.usernameFor(sess.userID)
		if err := b.mem.addWish(sess.userID, username, args); err != nil {
			b.reply(chatID, fmt.Sprintf("Failed to save wish: %v", err))
			return
		}
		b.reply(chatID, "Wish submitted ✓ I'll pass it along.")

	case "wishes":
		if !b.isAdmin(sess.userID) {
			b.reply(chatID, "Admin only.")
			return
		}
		if strings.HasPrefix(args, "done ") {
			var id int64
			fmt.Sscanf(strings.TrimPrefix(args, "done "), "%d", &id)
			b.mem.markWishDone(id)
			b.reply(chatID, fmt.Sprintf("Wish #%d marked done ✓", id))
			return
		}
		wishes, err := b.mem.listWishes()
		if err != nil || len(wishes) == 0 {
			b.reply(chatID, "No wishes yet.")
			return
		}
		if rt, localChatID, ok := b.richTransport(chatID); ok {
			for _, w := range wishes {
				status := "•"
				if w.Done {
					status = "✓"
				}
				text := fmt.Sprintf("%s *#%d* @%s\n_%s_", status, w.ID, w.Username, w.Message)
				if !w.Done {
					rt.SendWithButtons(localChatID, text, []Button{
						{Label: "✓ Mark done", Data: fmt.Sprintf("wishdone:%d", w.ID)},
					})
				} else {
					b.reply(chatID, text)
				}
			}
			return
		}
		var sb strings.Builder
		sb.WriteString("*Wishlist*\n\n")
		for _, w := range wishes {
			status := "•"
			if w.Done {
				status = "✓"
			}
			sb.WriteString(fmt.Sprintf("%s *#%d* @%s\n_%s_\n\n", status, w.ID, w.Username, w.Message))
		}
		b.reply(chatID, strings.TrimSpace(sb.String()))

	default:
		b.skillsMu.RLock()
		skill, isSkill := b.skills[cmd]
		b.skillsMu.RUnlock()
		if isSkill {
			go b.runSkill(chatID, sess, skill, args)
			return
		}
		b.reply(chatID, fmt.Sprintf("Unknown command: `/%s`\n\nType /help to see available commands.", cmd))
	}
}

// projectDir returns the directory for a named project, always inside the user's base dir.
func projectDir(baseWD, name string) string {
	if name == "global" {
		return baseWD
	}
	return filepath.Join(baseWD, name)
}

// handleProjectList lists all projects for the current user.
func (b *Bot) handleProjectList(chatID string, sess *Session) {
	baseWD := userWorkingDir(b.cfg.Backend.WorkingDir, sess.userID)
	entries, err := os.ReadDir(baseWD)
	if err != nil {
		b.reply(chatID, "Could not read projects directory.")
		return
	}

	sess.mu.Lock()
	activeWS := sess.workspace
	sess.mu.Unlock()

	type proj struct{ name, title string }
	projects := []proj{{"global", "Global (default)"}}
	for _, e := range entries {
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
		projects = append(projects, proj{name, title})
	}

	if rt, localChatID, ok := b.richTransport(chatID); ok {
		buttons := make([]Button, len(projects))
		for i, p := range projects {
			label := p.title
			if p.name == activeWS {
				label = "✓ " + label
			}
			buttons[i] = Button{Label: label, Data: "projswitch:" + p.name}
		}
		rt.SendButtonMenu(localChatID,
			"*Switch project*\nEach project has its own memory, files, and context — switching changes what I know and where I work.",
			buttons)
		return
	}

	// Text fallback.
	var lines []string
	for _, p := range projects {
		marker := ""
		if p.name == activeWS {
			marker = " ◀ active"
		}
		lines = append(lines, fmt.Sprintf("• *%s*%s", p.name, marker))
	}
	b.reply(chatID, "*Projects* — each has its own memory, files, and context:\n"+strings.Join(lines, "\n"))
}

// handleWorkspace switches to (or creates) a named workspace.
func (b *Bot) handleWorkspace(chatID string, sess *Session, args string) {
	parts := strings.SplitN(args, "|", 2)
	name := strings.TrimSpace(parts[0])
	description := ""
	if len(parts) == 2 {
		description = strings.TrimSpace(parts[1])
	}

	sess.mu.Lock()
	baseWD := sess.workingDir
	if sess.workspace != "global" {
		baseWD = filepath.Dir(sess.workingDir)
	}
	sess.mu.Unlock()

	wsDir := projectDir(baseWD, name)
	isNew := false
	if _, err := os.Stat(wsDir); os.IsNotExist(err) {
		isNew = true
	}

	if err := os.MkdirAll(wsDir, 0755); err != nil {
		b.reply(chatID, fmt.Sprintf("Failed to create project directory: %v", err))
		return
	}

	sess.mu.Lock()
	sess.workspace = name
	sess.workingDir = wsDir
	sess.model = ""
	sess.history = nil
	sess.mu.Unlock()

	b.mem.saveUserState(sess.userID, name, wsDir)

	model := b.activeModelForSession(sess)

	if isNew && description != "" {
		b.startProjectSetup(chatID, sess, name, description, wsDir, model)
	} else if isNew {
		b.reply(chatID, fmt.Sprintf("Project *%s* created.\nModel: `%s`\n\nTip: describe what this project is for and I'll write a README for it.", name, model))
	} else {
		readme := readWorkspaceReadme(wsDir)
		if readme != "" {
			b.reply(chatID, fmt.Sprintf("Switched to *%s* ✓", name))
		} else {
			b.reply(chatID, fmt.Sprintf("Switched to *%s* ✓\nModel: `%s`", name, model))
		}
	}
}

// startProjectSetup begins interactive project configuration via buttons if the transport
// supports it, otherwise falls back to immediate README generation.
func (b *Bot) startProjectSetup(chatID string, sess *Session, name, description, wsDir, model string) {
	transportName, localChatID := splitChatID(chatID)
	b.transportsMu.RLock()
	tp := b.transports[transportName]
	b.transportsMu.RUnlock()

	rt, hasButtons := tp.(RichTransport)
	if !hasButtons {
		b.reply(chatID, fmt.Sprintf("Project *%s* created. Writing README...", name))
		b.generateWorkspaceReadme(chatID, sess, name, description, nil)
		return
	}

	token := fmt.Sprintf("%d-%d", sess.userID, time.Now().UnixNano())
	pending := &PendingProject{
		Token:       token,
		Name:        name,
		Description: description,
		WorkingDir:  wsDir,
		UserID:      sess.userID,
		ChatID:      chatID,
		Model:       model,
		Step:        1,
	}
	b.pendingProjectsMu.Lock()
	b.pendingProjects[token] = pending
	b.pendingProjectsMu.Unlock()

	rt.SendWithButtons(localChatID,
		fmt.Sprintf("Project *%s* created — quick setup:\n\nDoes this project involve web research or data gathering?", name),
		[]Button{
			{Label: "Yes — research", Data: fmt.Sprintf("projsetup:%s:research:yes", token)},
			{Label: "No — other", Data: fmt.Sprintf("projsetup:%s:research:no", token)},
		},
	)
}

// richTransport returns the RichTransport for the given chatID if the transport supports buttons.
func (b *Bot) richTransport(chatID string) (RichTransport, string, bool) {
	transportName, localChatID := splitChatID(chatID)
	b.transportsMu.RLock()
	tp := b.transports[transportName]
	b.transportsMu.RUnlock()
	rt, ok := tp.(RichTransport)
	return rt, localChatID, ok
}

// generateWorkspaceReadme runs Claude to generate README content, then writes it to disk.
func (b *Bot) generateWorkspaceReadme(chatID string, sess *Session, name, description string, opts *ProjectOptions) {
	sess.mu.Lock()
	wsDir := sess.workingDir
	model := b.activeModelForSession(sess)
	sess.mu.Unlock()

	prompt := buildReadmePrompt(name, description, opts)

	response, err := b.runClaude(sess.userID, prompt, name, wsDir, model, nil)
	if err != nil {
		b.reply(chatID, fmt.Sprintf("Workspace created but README generation failed: %v", err))
		return
	}

	content := response
	if opts != nil && opts.AgentStyle != "" {
		content = strings.TrimRight(content, "\n") + agentStyleSection(opts.AgentStyle)
	}

	readmePath := filepath.Join(wsDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(content), 0644); err != nil {
		b.reply(chatID, fmt.Sprintf("README generated but failed to save: %v", err))
		return
	}

	b.reply(chatID, fmt.Sprintf("Project *%s* ready ✓", name))
}

// buildReadmePrompt constructs the Claude prompt for README generation based on project options.
func buildReadmePrompt(name, description string, opts *ProjectOptions) string {
	if opts != nil && opts.IsResearch {
		reportStep := "5. Keep all files strictly inside the working directory"
		if opts.AutoReport {
			reportStep = "5. Write a `report.md` summarising new findings:\n" +
				"   - First line: `# " + name + " Digest — [Month Year]`\n" +
				"   - Group items under `## [Category]` sections\n" +
				"   - Each item as `### [Title]` with an italic metadata line\n" +
				"   (The bot converts report.md to a styled PDF automatically)\n" +
				"6. Keep all files strictly inside the working directory"
		}
		return fmt.Sprintf(
			`Generate the full content of a README.md file for a project called "%s".

Description: %s

Structure it with these sections:
# %s

## Purpose
What this research tracks and why.

## Focus
The specific topics, sources, and search queries to use (list several concrete search strings).

## Output
What gets produced each run.

## Data
All findings are tracked in data.json to prevent duplicates.
Include the exact JSON schema:
{"found": [{"date": "YYYY-MM-DD", "title": "...", "url": "...", "summary": "..."}]}

## Instructions for AI
Step-by-step instructions to follow on every run:
1. Read data.json — note all previously found items
2. Search the web using the Focus queries (run at least 4–6 searches)
3. Filter out anything already in data.json
4. Update data.json with new findings
%s

Return ONLY the markdown content, no explanations.`,
			name, description, name, reportStep)
	}

	// General project
	reportInstruction := ""
	if opts != nil && opts.AutoReport {
		reportInstruction = "\nAfter completing significant work, write a `report.md` summarising what was done:\n" +
			"- First line: `# " + name + " Report — [Date]`\n" +
			"- Sections: `## [Topic]`\n" +
			"(The bot converts report.md to a styled PDF automatically)\n"
	}
	return fmt.Sprintf(
		`Generate the full content of a README.md file for a project called "%s".

Description: %s

Structure it with these sections:
# %s

## Purpose
What this project is for.

## Focus
The specific work, topics, or tasks for this project.

## Instructions for AI
Guidelines for working in this project.%s

Return ONLY the markdown content, no explanations.`,
		name, description, name, reportInstruction)
}

// handleProjectUpdate runs Claude to update the workspace README, then writes it to disk.
func (b *Bot) handleProjectUpdate(chatID string, sess *Session, instruction string) {
	sess.mu.Lock()
	wsDir := sess.workingDir
	ws := sess.workspace
	model := b.activeModelForSession(sess)
	sess.mu.Unlock()

	existing := readWorkspaceReadme(wsDir)
	if existing == "" {
		b.reply(chatID, fmt.Sprintf("No README.md found in *%s*. Use `/project <name> | <description>` to create one.", ws))
		return
	}

	var prompt string
	if instruction != "" {
		prompt = fmt.Sprintf("Update this README.md based on the following instruction: %s\n\nCurrent README:\n%s\n\nReturn ONLY the updated markdown content, no explanations.", instruction, existing)
	} else {
		prompt = fmt.Sprintf("Review and improve this README.md. Ensure the AI Instructions section is thorough and self-contained. Keep all existing information intact.\n\nCurrent README:\n%s\n\nReturn ONLY the updated markdown content, no explanations.", existing)
	}

	b.reply(chatID, "Updating README...")
	response, err := b.runClaude(sess.userID, prompt, ws, wsDir, model, nil)
	if err != nil {
		b.reply(chatID, fmt.Sprintf("Failed: %v", err))
		return
	}

	readmePath := filepath.Join(wsDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(response), 0644); err != nil {
		b.reply(chatID, fmt.Sprintf("README generated but failed to save: %v", err))
		return
	}
	b.reply(chatID, "README updated ✓")
}

// handleProjectUpdateButtons shows a button menu for updating parts of the current project.
func (b *Bot) handleProjectUpdateButtons(chatID string, sess *Session) {
	rt, localChatID, ok := b.richTransport(chatID)
	if !ok {
		b.handleProjectUpdate(chatID, sess, "")
		return
	}
	sess.mu.Lock()
	ws := sess.workspace
	sess.mu.Unlock()
	rt.SendButtonMenu(localChatID,
		fmt.Sprintf("Update *%s* — what would you like to change?", ws),
		[]Button{
			{Label: "Improve README", Data: "projupdate:readme"},
			{Label: "Change agent style", Data: "projupdate:style"},
			{Label: "View schedules", Data: "projupdate:schedules"},
		},
	)
}

// handleProjectStyleUpdate replaces the ## Agent section in the current project README.
func (b *Bot) handleProjectStyleUpdate(chatID string, sess *Session, style string) {
	section := agentStyleSection(style)
	if section == "" {
		b.reply(chatID, "Unknown style.")
		return
	}

	sess.mu.Lock()
	wsDir := sess.workingDir
	sess.mu.Unlock()

	readmePath := filepath.Join(wsDir, "README.md")
	raw, err := os.ReadFile(readmePath)
	if err != nil {
		b.reply(chatID, fmt.Sprintf("Could not read README: %v", err))
		return
	}
	readme := string(raw)

	// Replace existing ## Agent section or append.
	const agentHeader = "\n## Agent\n"
	if idx := strings.Index(readme, agentHeader); idx != -1 {
		// Find the next ## section after ## Agent.
		rest := readme[idx+len(agentHeader):]
		if end := strings.Index(rest, "\n## "); end != -1 {
			readme = readme[:idx] + section + rest[end:]
		} else {
			readme = readme[:idx] + section
		}
	} else {
		readme = strings.TrimRight(readme, "\n") + section
	}

	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		b.reply(chatID, fmt.Sprintf("Failed to save README: %v", err))
		return
	}

	label := style
	for _, s := range agentStyles {
		if s.Key == style {
			label = s.Label
			break
		}
	}
	b.reply(chatID, fmt.Sprintf("Agent style updated to *%s* ✓", label))
}

// readWorkspaceReadme returns the contents of README.md in the given directory, or "".
func readWorkspaceReadme(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		return ""
	}
	return string(data)
}

// ackPhrases are used to send a brief acknowledgment before a long Claude call.
var ackPhrases = []string{
	"On it...", "On it...", "On it...", // weighted more common
	"Right away...", "Got it, working on it...", "Let me check...",
	"Stand by...", "Working on it...", "Roger that...",
}

// slowAckPhrases are used when the request looks like it will take a while.
var slowAckPhrases = []string{
	"On it — this might take a few minutes...",
	"On it — this could take a little while...",
	"Working on it — might be a few minutes...",
	"On it, bear with me — this one may take a while...",
}

// slowKeywords are terms that suggest a long-running task.
var slowKeywords = []string{
	"research", "search", "find", "look up", "look for",
	"digest", "report", "summary", "summarise", "summarize",
	"scan", "scrape", "fetch", "download", "crawl",
	"all ", "every ", "full ", "entire ", "complete ",
	"week", "month", "year", "history", "historical",
	"analyse", "analyze", "audit", "review all",
	"generate", "write a report", "write a summary",
	"update data", "run the", "scheduled",
}

var ackIdx int

func nextAck() string {
	p := ackPhrases[ackIdx%len(ackPhrases)]
	ackIdx++
	return p
}

// ackForPrompt returns an ack phrase, using a "may take a while" variant if
// the prompt contains keywords that suggest a long-running task.
func ackForPrompt(prompt string) string {
	lower := strings.ToLower(prompt)
	for _, kw := range slowKeywords {
		if strings.Contains(lower, kw) {
			return slowAckPhrases[ackIdx%len(slowAckPhrases)]
		}
	}
	return nextAck()
}

// runUserMessage handles plain text messages sent to Claude.
func (b *Bot) runUserMessage(chatID string, sess *Session, text string) {
	transportName, localChatID := splitChatID(chatID)

	// Keep typing indicator alive while Claude works.
	stopTyping := make(chan struct{})
	go func() {
		b.transportsMu.RLock()
		tp := b.transports[transportName]
		b.transportsMu.RUnlock()
		for {
			select {
			case <-stopTyping:
				return
			default:
				if tp != nil {
					tp.SendTyping(localChatID)
				}
				time.Sleep(4 * time.Second)
			}
		}
	}()

	// Snapshot session state — release lock before slow Claude call.
	sess.mu.Lock()
	wd := sess.workingDir
	ws := sess.workspace
	model := b.activeModelForSession(sess)
	hist := make([]Message, len(sess.history))
	copy(hist, sess.history)
	sess.mu.Unlock()

	phrase := ackForPrompt(text)
	ackIdx++ // advance for next call
	ack := "_" + phrase + "_"
	if ws != "global" {
		ack = fmt.Sprintf("_[%s] %s_", ws, phrase)
	}
	b.reply(chatID, ack)

	before := snapshotFiles(wd)

	response, err := b.runClaude(sess.userID, text, ws, wd, model, hist)
	close(stopTyping)
	if err != nil {
		b.reply(chatID, fmt.Sprintf("Error: %s", err))
		return
	}

	for _, chunk := range splitMessage(response, maxMsgLen) {
		b.reply(chatID, chunk)
	}

	for _, path := range newFiles(wd, before) {
		if filepath.Base(path) == "report.md" {
			userBaseDir := userWorkingDir(b.cfg.Backend.WorkingDir, sess.userID)
			rtmpl, _ := loadReportTemplate(wd, userBaseDir)
			outPath := strings.TrimSuffix(path, ".md") + "_" + time.Now().Format("2006-01-02") + ".pdf"
			if err := RenderMarkdownReport(path, outPath, rtmpl); err != nil {
				b.reply(chatID, fmt.Sprintf("Report render failed: %v", err))
			} else {
				b.sendFileAuto(chatID, outPath)
				if info, err := os.Stat(outPath); err == nil {
					b.mem.recordFile(sess.userID, ws, filepath.Base(outPath), outPath, info.Size())
				}
			}
			continue
		}
		b.sendFileAuto(chatID, path)
		if info, err := os.Stat(path); err == nil {
			b.mem.recordFile(sess.userID, ws, filepath.Base(path), path, info.Size())
		}
	}

	sess.mu.Lock()
	sess.history = append(sess.history, Message{"user", text}, Message{"assistant", response})
	if len(sess.history) > 20 {
		sess.history = sess.history[len(sess.history)-20:]
	}
	sess.mu.Unlock()

	go b.mem.extractAndSave(sess.userID, ws, text, response, func(prompt string) (string, error) {
		return b.runClaude(sess.userID, prompt, ws, wd, b.cfg.Backend.ExtractModel, nil)
	})
}

func (b *Bot) handleScheduleAdd(chatID string, sess *Session, args string) {
	parts := strings.SplitN(args, "|", 3)
	if len(parts) != 3 {
		b.reply(chatID, "Usage: `/schedule <name> | <when> | <prompt>`\n\nExamples:\n`/schedule morning-news | every day 08:00 | Fetch top headlines`\n`/schedule standup | every weekday 09:00 | What should I focus on today?`\n`/schedule weekly | every monday 08:00 | Summarize my week ahead`")
		return
	}
	name := strings.TrimSpace(parts[0])
	whenStr := strings.TrimSpace(parts[1])
	prompt := strings.TrimSpace(parts[2])

	schedule, desc, err := parseRecurring(whenStr)
	if err != nil {
		b.reply(chatID, fmt.Sprintf("%v", err))
		return
	}

	sess.mu.Lock()
	ws := sess.workspace
	wd := sess.workingDir
	cid := sess.chatID
	sess.mu.Unlock()

	if err := b.mem.addSchedule(sess.userID, cid, name, schedule, prompt, ws, wd, false); err != nil {
		b.reply(chatID, fmt.Sprintf("Failed to add schedule: %v", err))
		return
	}
	b.cron.reload()
	b.reply(chatID, fmt.Sprintf("Schedule *%s* added — %s ✓", name, desc))
}

func (b *Bot) handleAt(chatID string, sess *Session, args string) {
	parts := strings.SplitN(args, "|", 2)
	if len(parts) != 2 {
		b.reply(chatID, "Usage: `/at <time> | <prompt>`\n\nExample: `/at tomorrow 18:00 | give me a pasta recipe`")
		return
	}
	timeStr := strings.TrimSpace(parts[0])
	prompt := strings.TrimSpace(parts[1])

	cronExpr, desc, err := parseAtTime(timeStr)
	if err != nil {
		b.reply(chatID, fmt.Sprintf("Couldn't parse time: %v", err))
		return
	}

	sess.mu.Lock()
	ws := sess.workspace
	wd := sess.workingDir
	cid := sess.chatID
	sess.mu.Unlock()

	name := fmt.Sprintf("at:%s", timeStr)
	if err := b.mem.addSchedule(sess.userID, cid, name, cronExpr, prompt, ws, wd, true); err != nil {
		b.reply(chatID, fmt.Sprintf("Failed to set reminder: %v", err))
		return
	}
	b.cron.reload()
	b.reply(chatID, fmt.Sprintf("Scheduled for *%s* ✓", desc))
}

func (b *Bot) handleScheduleList(chatID string, sess *Session) {
	schedules, err := b.mem.listSchedulesForUser(sess.userID)
	if err != nil || len(schedules) == 0 {
		b.reply(chatID, "No schedules configured.")
		return
	}

	transportName, localChatID := splitChatID(chatID)
	b.transportsMu.RLock()
	tp := b.transports[transportName]
	b.transportsMu.RUnlock()

	for _, s := range schedules {
		status := "✅"
		if !s.Enabled {
			status = "⏸"
		}
		if s.OneShot {
			status = "⏰"
		}
		kind := "recurring"
		if s.OneShot {
			kind = "one-off"
		}
		last := "never"
		if s.LastRun != nil {
			last = formatAge(*s.LastRun)
		}
		project := s.Workspace
		if project == "" {
			project = "global"
		}
		text := fmt.Sprintf(
			"%s *%s*\nProject: _%s_ · %s\n`%s` — last: %s\n_%s_",
			status, s.Name, project, kind, s.Schedule, last, s.Prompt,
		)

		if rt, ok := tp.(RichTransport); ok {
			rt.SendWithButtons(localChatID, text, []Button{
				{Label: "🗑 Remove", Data: fmt.Sprintf("delschedule:%d", s.ID)},
			})
		} else {
			b.reply(chatID, text+fmt.Sprintf("\n`/unschedule %d` to remove", s.ID))
		}
	}
}

func (b *Bot) handleScheduleDelete(chatID string, sess *Session, arg string) {
	var id int64
	if _, err := fmt.Sscanf(strings.TrimSpace(arg), "%d", &id); err != nil {
		b.reply(chatID, "Usage: `/unschedule <id>` — get IDs with /schedules")
		return
	}
	if err := b.mem.deleteSchedule(sess.userID, id); err != nil {
		b.reply(chatID, fmt.Sprintf("Failed: %v", err))
		return
	}
	b.cron.reload()
	b.reply(chatID, fmt.Sprintf("Schedule %d removed.", id))
}

// runScheduledTask is called by the cron runner.
func (b *Bot) runScheduledTask(id, userID int64, chatID string, workspace, workingDir, prompt string) {
	sess := b.getSession(userID)
	if sess == nil {
		return
	}

	sess.mu.Lock()
	model := b.activeModelForSession(sess)
	hist := make([]Message, len(sess.history))
	copy(hist, sess.history)
	sess.mu.Unlock()

	wd := workingDir
	if wd == "" {
		sess.mu.Lock()
		wd = sess.workingDir
		sess.mu.Unlock()
	}

	before := snapshotFiles(wd)

	response, err := b.runClaude(userID, prompt, workspace, wd, model, hist)
	b.mem.updateLastRun(id)

	if err != nil {
		log.Printf("cron task %d error: %v", id, err)
		b.reply(chatID, fmt.Sprintf("⚠️ Scheduled task failed: %s", err))
		return
	}

	for _, chunk := range splitMessage(response, maxMsgLen) {
		b.reply(chatID, chunk)
	}

	for _, path := range newFiles(wd, before) {
		if filepath.Base(path) == "report.md" {
			userBaseDir := userWorkingDir(b.cfg.Backend.WorkingDir, userID)
			rtmpl, _ := loadReportTemplate(wd, userBaseDir)
			outPath := strings.TrimSuffix(path, ".md") + "_" + time.Now().Format("2006-01-02") + ".pdf"
			if err := RenderMarkdownReport(path, outPath, rtmpl); err != nil {
				b.reply(chatID, fmt.Sprintf("Report render failed: %v", err))
			} else {
				b.sendFile(chatID, outPath)
				if info, err := os.Stat(outPath); err == nil {
					b.mem.recordFile(userID, workspace, filepath.Base(outPath), outPath, info.Size())
				}
			}
			continue
		}
		b.sendFile(chatID, path)
		if info, err := os.Stat(path); err == nil {
			b.mem.recordFile(userID, workspace, filepath.Base(path), path, info.Size())
		}
	}
}

// activeModelForSession returns the effective model. Caller should hold sess.mu (or be on the same goroutine).
func (b *Bot) activeModelForSession(sess *Session) string {
	if sess.model != "" {
		return sess.model
	}
	if ws := b.mem.getWorkspaceModel(sess.userID, sess.workspace); ws != "" {
		return ws
	}
	return b.cfg.Backend.DefaultModel
}

func (b *Bot) handleModelSwitch(chatID string, sess *Session, arg string) {
	parts := strings.Fields(arg)
	if len(parts) == 0 {
		return
	}
	modelName := parts[0]
	save := len(parts) > 1 && parts[1] == "--save"

	sess.mu.Lock()
	sess.model = modelName
	ws := sess.workspace
	sess.mu.Unlock()

	if save {
		if err := b.mem.setWorkspaceModel(sess.userID, ws, modelName); err != nil {
			b.reply(chatID, fmt.Sprintf("Model set to `%s` (failed to save: %v)", modelName, err))
			return
		}
		b.reply(chatID, fmt.Sprintf("Model set to `%s` and saved for project *%s*.", modelName, ws))
	} else {
		b.reply(chatID, fmt.Sprintf("Model set to `%s` for this session.", modelName))
	}
}

// buildAICommand constructs the exec.Cmd for the configured backend.
func (b *Bot) buildAICommand(prompt, model, systemPrompt string) *exec.Cmd {
	bin := b.cfg.Backend.Binary
	switch b.cfg.Backend.Type {
	case "opencode":
		cmd := exec.Command(bin, "run",
			"--model", model,
			"--system", systemPrompt,
			prompt,
		)
		return cmd
	default: // "claude-code"
		cmd := exec.Command(bin, "-p", prompt,
			"--model", model,
			"--system-prompt", systemPrompt,
			"--dangerously-skip-permissions",
			"--allowedTools", "Bash,Read,Write,Edit,Glob,Grep,WebSearch,WebFetch",
		)
		env := []string{}
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "CLAUDECODE=") {
				env = append(env, e)
			}
		}
		cmd.Env = env
		return cmd
	}
}

// displayPath replaces the user base dir prefix with ~ for user-visible output.
func displayPath(path, userBaseDir string) string {
	if strings.HasPrefix(path, userBaseDir) {
		rel := strings.TrimPrefix(path, userBaseDir)
		if rel == "" {
			return "~"
		}
		return "~" + rel
	}
	return path
}

// runClaude executes the Claude CLI with all context provided explicitly (no session access).
func (b *Bot) runClaude(userID int64, prompt, workspace, workingDir, model string, history []Message) (string, error) {
	home, _ := os.UserHomeDir()
	botHome := userWorkingDir(b.cfg.Backend.WorkingDir, userID)
	displayWD := displayPath(workingDir, botHome)
	systemPrompt := b.cfg.Persona.SystemPrompt
	systemPrompt += fmt.Sprintf(
		"\n\n## Working Directory\n"+
			"Your working directory alias is: %s\n"+
			"(~ = %s)\n"+
			"STRICT RULES:\n"+
			"- You MUST stay inside this directory at all times\n"+
			"- Never read, write, or reference files outside this directory\n"+
			"- All outputs (files, PDFs, data) go here and nowhere else\n"+
			"- When mentioning paths in responses, always use ~/... shorthand, never the full path",
		displayWD, workingDir)

	systemPrompt += "\n\n## Report Generation\n" +
		"When creating a report, digest, or summary for the user, write it as a markdown\n" +
		"file named `report.md` in the working directory. Use this structure:\n" +
		"- First line: # Title of the Report\n" +
		"- Sections: ## Section Name\n" +
		"- Sub-items: ### Item Title (optional italic line for metadata)\n" +
		"- Body paragraphs, bullet lists as normal markdown\n" +
		"The file will be automatically converted to a styled PDF and sent. Do not call\n" +
		"any PDF tools directly."

	if readme := readWorkspaceReadme(workingDir); readme != "" {
		readmeContent := strings.ReplaceAll(readme, "~", home)
		systemPrompt += "\n\n## Workspace Configuration (README.md)\n\n" + readmeContent
	}

	if mem := b.mem.load(userID, workspace, b.cfg.Memory.MaxAgeDays); mem != "" {
		memoriesContent := strings.ReplaceAll(mem, "~", home)
		systemPrompt += "\n\n" + memoriesContent
	}

	b.skillsMu.RLock()
	if len(b.skills) > 0 {
		var skillLines []string
		for name, s := range b.skills {
			desc := s.Description
			if desc == "" {
				desc = s.Type
			}
			skillLines = append(skillLines, fmt.Sprintf("- /%s: %s", name, desc))
		}
		sort.Strings(skillLines)
		systemPrompt += "\n\n## Available Skills\nThe following custom /commands are available:\n" + strings.Join(skillLines, "\n")
	}
	b.skillsMu.RUnlock()

	if len(history) > 0 {
		systemPrompt += "\n\n## Recent conversation\n"
		for _, m := range history {
			systemPrompt += fmt.Sprintf("%s: %s\n", m.Role, m.Content)
		}
	}

	cmd := b.buildAICommand(prompt, model, systemPrompt)
	cmd.Dir = workingDir

	out, err := cmd.Output()
	if err != nil {
		var detail string
		if exitErr, ok := err.(*exec.ExitError); ok {
			detail = strings.TrimSpace(string(exitErr.Stderr))
		}
		if detail == "" {
			detail = strings.TrimSpace(string(out))
		}
		if detail == "" {
			detail = err.Error()
		}
		log.Printf("claude error: %s", detail)
		return "", fmt.Errorf("%s", detail)
	}

	result := strings.TrimSpace(string(out))
	if result == "" {
		return "(no output)", nil
	}
	return result, nil
}

func (b *Bot) handleAPIKey(chatID string, cmd, args string) {
	if cmd == "apikeys" || args == "" || args == "list" {
		keys := b.mem.listAPIKeys()
		if len(keys) == 0 {
			b.reply(chatID, "No API keys. Use `/apikey new <name>` to create one.")
			return
		}
		var lines []string
		for _, k := range keys {
			last := "never used"
			if k.LastUsed != "" {
				last = "last used " + k.LastUsed
			}
			lines = append(lines, fmt.Sprintf("• *%s* (ID: %d) — %s", k.Name, k.ID, last))
		}
		b.reply(chatID, "*API Keys:*\n"+strings.Join(lines, "\n")+"\n\n`/apikey revoke <id>` to remove a key.")
		return
	}

	parts := strings.SplitN(args, " ", 2)
	sub := parts[0]
	rest := ""
	if len(parts) > 1 {
		rest = strings.TrimSpace(parts[1])
	}

	switch sub {
	case "new", "create":
		name := rest
		if name == "" {
			b.reply(chatID, "Usage: `/apikey new <name>`")
			return
		}
		raw, hash, err := generateAPIKey()
		if err != nil {
			b.reply(chatID, fmt.Sprintf("Failed to generate key: %v", err))
			return
		}
		id, err := b.mem.createAPIKey(name, hash)
		if err != nil {
			b.reply(chatID, fmt.Sprintf("Failed to save key: %v", err))
			return
		}
		b.reply(chatID, fmt.Sprintf(
			"*API key created* (ID: %d)\nName: %s\n\n`%s`\n\n⚠️ Copy this now — it won't be shown again.",
			id, name, raw))

	case "revoke", "delete", "remove":
		var id int64
		fmt.Sscanf(rest, "%d", &id)
		if id == 0 {
			b.reply(chatID, "Usage: `/apikey revoke <id>`")
			return
		}
		if err := b.mem.revokeAPIKey(id); err != nil {
			b.reply(chatID, fmt.Sprintf("Failed: %v", err))
			return
		}
		b.reply(chatID, fmt.Sprintf("API key %d revoked ✓", id))

	default:
		b.reply(chatID, "Usage:\n`/apikey new <name>` — create a key\n`/apikeys` — list keys\n`/apikey revoke <id>` — revoke a key")
	}
}

// runSkill dispatches a skill command for a user.
func (b *Bot) runSkill(chatID string, sess *Session, skill *Skill, args string) {
	sess.mu.Lock()
	userID := sess.userID
	ws := sess.workspace
	wd := sess.workingDir
	model := b.activeModelForSession(sess)
	hist := make([]Message, len(sess.history))
	copy(hist, sess.history)
	sess.mu.Unlock()

	b.reply(chatID, "_"+nextAck()+"_")

	before := snapshotFiles(wd)

	extraEnv := map[string]string{"ARTOO_WD": wd}
	encSecrets, err := b.mem.getSecretsForSkill(userID, ws, skill.Name)
	if err == nil {
		for name, encVal := range encSecrets {
			plain, err := decryptSecret(b.secretKey, encVal)
			if err == nil {
				extraEnv["ARTOO_SECRET_"+name] = plain
			}
		}
	}

	runFn := func(prompt string) (string, error) {
		return b.runClaude(userID, prompt, ws, wd, model, hist)
	}
	result, err := b.dispatchSkill(skill, args, extraEnv, runFn)
	if err != nil {
		b.reply(chatID, fmt.Sprintf("Skill error: %v", err))
		return
	}
	if result != "" && result != "(no output)" {
		for _, chunk := range splitMessage(result, maxMsgLen) {
			b.reply(chatID, chunk)
		}
	}

	for _, path := range newFiles(wd, before) {
		if filepath.Base(path) == "report.md" {
			userBaseDir := userWorkingDir(b.cfg.Backend.WorkingDir, userID)
			rtmpl, _ := loadReportTemplate(wd, userBaseDir)
			outPath := strings.TrimSuffix(path, ".md") + "_" + time.Now().Format("2006-01-02") + ".pdf"
			if err := RenderMarkdownReport(path, outPath, rtmpl); err != nil {
				b.reply(chatID, fmt.Sprintf("Report render failed: %v", err))
			} else {
				b.sendFileAuto(chatID, outPath)
				if info, err := os.Stat(outPath); err == nil {
					b.mem.recordFile(userID, ws, filepath.Base(outPath), outPath, info.Size())
				}
			}
			continue
		}
		b.sendFileAuto(chatID, path)
		if info, err := os.Stat(path); err == nil {
			b.mem.recordFile(userID, ws, filepath.Base(path), path, info.Size())
		}
	}
}

// handleSecretCommand handles /secret set|list|del subcommands.
func (b *Bot) handleSecretCommand(chatID string, sess *Session, args string) {
	isAdmin := b.isAdmin(sess.userID)
	usage := "Usage:\n" +
		"`/secret set <name> <value> --skill <skill>` — store for current project\n" +
		"`/secret set --global <name> <value> --skill <skill>` — store for all your projects\n" +
		"`/secret set --system <name> <value> --skill <skill>` — store for all users _(admin only)_\n" +
		"`/secret list` — show key names in current project + global\n" +
		"`/secret del <name>` — delete from current project\n" +
		"`/secret del --global <name>` — delete from your global scope\n" +
		"`/secret del --system <name>` — delete system secret _(admin only)_"

	if args == "" {
		b.reply(chatID, usage)
		return
	}

	parts := strings.SplitN(args, " ", 2)
	sub := parts[0]
	rest := ""
	if len(parts) > 1 {
		rest = strings.TrimSpace(parts[1])
	}

	sess.mu.Lock()
	ws := sess.workspace
	sess.mu.Unlock()

	switch sub {
	case "set":
		global, system, skillName, positional := parseSecretFlags(rest)
		if system && !isAdmin {
			b.reply(chatID, "Only the admin can set system secrets.")
			return
		}
		if len(positional) < 2 {
			b.reply(chatID, "Usage: `/secret set [--global|--system] <name> <value> --skill <skill>`")
			return
		}
		if skillName == "" {
			b.reply(chatID, "Missing `--skill <name>`. Secrets must be locked to a specific skill.\n\nExample: `/secret set GEMINI_API_KEY mykey --skill imagine`")
			return
		}
		name := positional[0]
		value := positional[1]

		targetUserID := sess.userID
		scope := ws
		scopeLabel := fmt.Sprintf("project *%s*", scope)
		if system {
			targetUserID = SystemUserID
			scope = "*"
			scopeLabel = "all users (system)"
		} else if global {
			scope = "*"
			scopeLabel = "all your projects (global)"
		}

		encVal, err := encryptSecret(b.secretKey, value)
		if err != nil {
			b.reply(chatID, fmt.Sprintf("Encryption error: %v", err))
			return
		}
		if err := b.mem.setSecret(targetUserID, scope, name, encVal, skillName); err != nil {
			b.reply(chatID, fmt.Sprintf("Failed to save secret: %v", err))
			return
		}
		b.reply(chatID, fmt.Sprintf("Secret `%s` saved for %s, skill: `%s` ✓", name, scopeLabel, skillName))

	case "list":
		_, system, _, _ := parseSecretFlags(rest)
		if system && !isAdmin {
			b.reply(chatID, "Only the admin can list system secrets.")
			return
		}
		entries, err := b.mem.listSecrets(sess.userID, ws, system || isAdmin)
		if err != nil || len(entries) == 0 {
			b.reply(chatID, "No secrets stored for this project.")
			return
		}
		var lines []string
		for _, e := range entries {
			scopeLabel := fmt.Sprintf("project: %s", e.Scope)
			switch e.Scope {
			case "*":
				scopeLabel = "global"
			case "system":
				scopeLabel = "system (all users)"
			}
			lines = append(lines, fmt.Sprintf("• `%s` — skill: `%s` (%s)", e.Name, e.AllowedSkill, scopeLabel))
		}
		b.reply(chatID, "*Secrets:*\n"+strings.Join(lines, "\n"))

	case "del", "delete", "rm":
		global, system, _, positional := parseSecretFlags(rest)
		if system && !isAdmin {
			b.reply(chatID, "Only the admin can delete system secrets.")
			return
		}
		if len(positional) == 0 {
			b.reply(chatID, "Usage: `/secret del [--global|--system] <name>`")
			return
		}
		name := positional[0]
		targetUserID := sess.userID
		scope := ws
		if system {
			targetUserID = SystemUserID
			scope = "*"
		} else if global {
			scope = "*"
		}
		if err := b.mem.deleteSecret(targetUserID, scope, name); err != nil {
			b.reply(chatID, fmt.Sprintf("Failed: %v", err))
			return
		}
		b.reply(chatID, fmt.Sprintf("Secret `%s` deleted ✓", name))

	default:
		b.reply(chatID, usage)
	}
}

// parseSecretFlags parses --global, --system, and --skill flags from secret subcommand args.
func parseSecretFlags(args string) (global, system bool, skillName string, positional []string) {
	fields := strings.Fields(args)
	i := 0
	for i < len(fields) {
		switch fields[i] {
		case "--global":
			global = true
			i++
		case "--system":
			system = true
			i++
		case "--skill":
			if i+1 < len(fields) {
				skillName = fields[i+1]
				i += 2
			} else {
				i++
			}
		default:
			positional = append(positional, fields[i])
			i++
		}
	}
	return
}

// handleSkillsCommand handles /skills and /skills reload.
func (b *Bot) handleSkillsCommand(chatID string, sess *Session, args string) {
	if args == "reload" {
		if !b.isAdmin(sess.userID) {
			b.reply(chatID, "Only the admin can reload skills.")
			return
		}
		sess.mu.Lock()
		wd := sess.workingDir
		sess.mu.Unlock()
		loaded := loadSkills(skillPaths(wd))
		b.skillsMu.Lock()
		b.skills = loaded
		b.skillsMu.Unlock()
		names := make([]string, 0, len(loaded))
		for name := range loaded {
			names = append(names, "/"+name)
		}
		sort.Strings(names)
		b.reply(chatID, fmt.Sprintf("Loaded %d skill(s): %s", len(names), strings.Join(names, ", ")))
		return
	}

	b.skillsMu.RLock()
	skills := make([]*Skill, 0, len(b.skills))
	for _, s := range b.skills {
		skills = append(skills, s)
	}
	b.skillsMu.RUnlock()

	if len(skills) == 0 {
		b.reply(chatID, "No skills loaded.\n\nDrop `.md` or executable files into a `skills/` directory:\n• `~/.config/bot/skills/` — global\n• `~/.config/bot/<instance>/skills/` — per-instance\n• `<project>/skills/` — per-project")
		return
	}

	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })

	if rt, localChatID, ok := b.richTransport(chatID); ok {
		buttons := make([]Button, len(skills))
		for i, s := range skills {
			desc := s.Description
			if desc == "" {
				desc = s.Type
			}
			buttons[i] = Button{
				Label: fmt.Sprintf("/%s — %s", s.Name, desc),
				Data:  "skillrun:" + s.Name,
			}
		}
		rt.SendButtonMenu(localChatID, "*Skills:*", buttons)
		return
	}

	var lines []string
	for _, s := range skills {
		desc := s.Description
		if desc == "" {
			desc = s.Type
		}
		lines = append(lines, fmt.Sprintf("• `/%s` — %s", s.Name, desc))
	}
	b.reply(chatID, "*Skills:*\n"+strings.Join(lines, "\n"))
}

// reply sends a text message via the appropriate transport.
func (b *Bot) reply(chatID string, text string) {
	transportName, local := splitChatID(chatID)
	b.transportsMu.RLock()
	tp := b.transports[transportName]
	b.transportsMu.RUnlock()
	if tp == nil {
		log.Printf("reply: no transport %q for chatID %q", transportName, chatID)
		return
	}
	if err := tp.Send(local, text); err != nil {
		log.Printf("reply: send error (transport=%s, chat=%s): %v", transportName, local, err)
	}
}

// sendFile delivers a file via the appropriate transport.
func (b *Bot) sendFile(chatID string, path string) {
	transportName, local := splitChatID(chatID)
	b.transportsMu.RLock()
	tp := b.transports[transportName]
	b.transportsMu.RUnlock()
	if tp == nil {
		return
	}
	if err := tp.SendFile(local, path); err != nil {
		log.Printf("sendFile: error (transport=%s, chat=%s, path=%s): %v", transportName, local, path, err)
	}
}

// sendFileAuto sends images as photos on Telegram, otherwise falls back to sendFile.
func (b *Bot) sendFileAuto(chatID string, path string) {
	transportName, local := splitChatID(chatID)
	b.transportsMu.RLock()
	tp := b.transports[transportName]
	b.transportsMu.RUnlock()
	if tp == nil {
		return
	}
	if tg, ok := tp.(*TelegramTransport); ok {
		tg.sendFileAuto(local, path)
		return
	}
	if err := tp.SendFile(local, path); err != nil {
		log.Printf("sendFileAuto: error (transport=%s, chat=%s, path=%s): %v", transportName, local, path, err)
	}
}

func (b *Bot) helpText() string {
	return fmt.Sprintf(
		"*%s — your personal robot assistant*\n\n"+
			"Send any message and I'll get it done.\n\n"+
			"*Session*\n"+
			"/new — fresh start (clear history + reset to global)\n"+
			"/clear — clear conversation history only\n\n"+
			"*Projects*\n"+
			"Each project is a separate context: its own directory, README, memory, files, and schedules. "+
			"Switching projects changes what the AI knows and where it works — memories from one project don't bleed into another. "+
			"Use _global_ for general tasks with no project context.\n\n"+
			"/project — list projects and switch\n"+
			"/project <name> — switch to (or create) a project\n"+
			"/project <name> | <description> — create project with README\n"+
			"/project update — improve current project README\n"+
			"/project update <instruction> — update README with specific changes\n"+
			"Send a file → saved to project + extracted as markdown memory\n\n"+
			"*Memory*\n"+
			"/memory — show memories for the current project\n"+
			"/remember <fact> — save to current project memory\n"+
			"/remember --global <fact> — save to global memory (available in all projects)\n"+
			"/files — show recently created files\n\n"+
			"*Model*\n"+
			"/model — show active model\n"+
			"/model <name> — switch model for this session\n"+
			"/model <name> --save — persist model for current workspace\n\n"+
			"*Schedules*\n"+
			"/at <time> | <prompt> — one-off reminder (tomorrow 18:00, friday 09:00, in 2h)\n"+
			"/schedule <name> | <cron> | <prompt> — recurring scheduled task\n"+
			"/schedules — list your scheduled tasks\n"+
			"/unschedule <id> — remove a scheduled task\n\n"+
			"*Reports*\n"+
			"/report — render report.md as a PDF and send it\n"+
			"Upload template.yaml to a project to customise the report style\n\n"+
			"*Skills*\n"+
			"/skills — list loaded custom commands\n"+
			"/secret set <name> <value> --skill <skill> — store credential for a skill\n"+
			"/secret set --global <name> <value> --skill <skill> — store for all projects\n"+
			"/secret list — show stored secret names\n"+
			"/secret del <name> — remove a secret\n\n"+
			"*Wishlist*\n"+
			"/wish <message> — submit a feature request\n"+
			"/wishes — list all wishes (admin)\n"+
			"/wishes done <id> — mark a wish as done (admin)\n\n"+
			"*Legacy Skills*\n"+
			"/skills reload — reload skills from disk (admin)\n\n"+
			"*Examples*\n"+
			"_what's in my downloads folder?_\n"+
			"_summarize recent git commits in ~/code/myproject_\n"+
			"_/project myproject — switch to project context_\n"+
			"_/schedule digest | 0 8 * * * | summarize today's news_",
		b.cfg.Persona.Name)
}

// snapshotFiles returns a map of filepath→modtime for all files in dir.
func snapshotFiles(dir string) map[string]int64 {
	snap := make(map[string]int64)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return snap
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		snap[filepath.Join(dir, e.Name())] = info.ModTime().UnixNano()
	}
	return snap
}

// newFiles returns files in dir that are new or modified since the before snapshot.
func newFiles(dir string, before map[string]int64) []string {
	var result []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return result
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		if prevMod, existed := before[path]; !existed || info.ModTime().UnixNano() != prevMod {
			result = append(result, path)
		}
	}
	return result
}

// downloadURL downloads a URL to a local file path.
func downloadURL(url, dest string) (int64, error) {
	cmd := exec.Command("curl", "-sL", "-o", dest, url)
	if err := cmd.Run(); err != nil {
		return 0, err
	}
	info, err := os.Stat(dest)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func splitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}
	var chunks []string
	for len(text) > maxLen {
		chunks = append(chunks, text[:maxLen])
		text = text[maxLen:]
	}
	if len(text) > 0 {
		chunks = append(chunks, text)
	}
	return chunks
}

func main() {
	inst := flag.String("instance", "default", "bot instance name (for config/memory isolation)")
	setup := flag.Bool("setup", false, "run interactive setup wizard")
	flag.Parse()

	instance = *inst // global used by configDir() in config.go

	if *setup {
		runOnboarding()
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config not found for instance %q. Run with --setup to configure.\n", instance)
		os.Exit(1)
	}

	bot, err := newBot(cfg)
	if err != nil {
		log.Fatal(err)
	}

	bot.run()
}
