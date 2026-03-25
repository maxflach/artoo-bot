package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxMsgLen = 4096

type Message struct {
	Role    string // "user" or "assistant"
	Content string
}

// ScheduleWizard holds state for the interactive /schedule and /at wizards.
type ScheduleWizard struct {
	IsAt           bool   // true = /at (one-shot), false = /schedule (recurring)
	Name           string // schedule name; "" = auto-derived from prompt
	RecurrenceBase string // e.g. "every day" — needs a time appended before parsing
	CronExpr       string // complete cron expression
	CronDesc       string // human-readable description
	Prompt         string // pre-supplied prompt (e.g. from "/at | my task" syntax)
	Step           string // "rec" | "time" | "awaiting_prompt"
}

// Session holds all per-user mutable state.
// Access any field while holding mu.
type Session struct {
	mu         sync.Mutex
	userID     int64
	chatID     string // transport-prefixed, e.g. "tg:123456789"
	workingDir string
	workspace  string
	model           string // session-level model override; "" means use workspace or default
	claudeSessionID string // Claude session UUID for multi-turn; "" means fresh
	history         []Message
	sharedOwnerID   int64  // non-zero when working in a shared project
	sharedAccess    string // "read" or "write" when in a shared project
	scheduleWizard  *ScheduleWizard // non-nil while a /schedule or /at wizard is active
}

// knownModels lists the models available in the /model button menu.
var knownModels = []struct {
	ID    string
	Label string
}{
	{"claude-sonnet-4-6", "Sonnet 4.6"},
	{"claude-opus-4-6", "Opus 4.6"},
	{"claude-haiku-4-5", "Haiku 4.5"},
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
	allowedPaths       map[int64][]AllowedPath
}

func newBot(cfg *Config) (*Bot, error) {
	allowed := make(map[int64]bool)
	for _, id := range cfg.Telegram.AllowedUserIDs {
		allowed[id] = true
	}
	for _, id := range cfg.Discord.AllowedUserIDs {
		allowed[id] = true
	}
	for _, num := range cfg.WhatsApp.AllowedNumbers {
		if id, err := parsePhoneNumber(num); err == nil {
			allowed[id] = true
		}
	}

	mem, err := newMemoryStore()
	if err != nil {
		return nil, fmt.Errorf("memory store: %w", err)
	}

	// Collect all allowed user IDs: config + DB-approved users
	allUserIDs := make([]int64, 0, len(cfg.Telegram.AllowedUserIDs)+len(cfg.Discord.AllowedUserIDs)+len(cfg.WhatsApp.AllowedNumbers))
	allUserIDs = append(allUserIDs, cfg.Telegram.AllowedUserIDs...)
	allUserIDs = append(allUserIDs, cfg.Discord.AllowedUserIDs...)
	for _, num := range cfg.WhatsApp.AllowedNumbers {
		if id, err := parsePhoneNumber(num); err == nil {
			allUserIDs = append(allUserIDs, id)
		}
	}
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

	allowedPaths := make(map[int64][]AllowedPath)
	for username, paths := range cfg.AllowedPaths {
		uid := mem.userIDForUsername(username)
		if uid == 0 {
			log.Printf("allowed_paths: username %q not found in approved_users — skipping", username)
			continue
		}
		allowedPaths[uid] = paths
	}

	b := &Bot{
		cfg:             cfg,
		transports:      make(map[string]Transport),
		allowedIDs:      allowed,
		mem:             mem,
		sessions:        sessions,
		secretKey:       secretKey,
		pendingProjects: make(map[string]*PendingProject),
		allowedPaths:    allowedPaths,
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
	if len(cfg.WhatsApp.AllowedNumbers) > 0 {
		wa, err := newWhatsAppTransport(b)
		if err != nil {
			return nil, fmt.Errorf("whatsapp: %w", err)
		}
		b.transports["wa"] = wa
	}

	if len(b.transports) == 0 {
		return nil, fmt.Errorf("no transports configured (set telegram.token, discord.token, or webchat.enabled)")
	}

	return b, nil
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
	if b.cfg.WhatsApp.AdminNumber != "" {
		if id, err := parsePhoneNumber(b.cfg.WhatsApp.AdminNumber); err == nil && userID == id {
			return true
		}
	}
	return false
}

// allowedPathsFor returns the list of allowed external paths for a user.
func (b *Bot) allowedPathsFor(userID int64) []AllowedPath {
	return b.allowedPaths[userID]
}

// matchAllowedPath returns the AllowedPath matching a name/alias/path, if any.
func (b *Bot) matchAllowedPath(userID int64, name string) (AllowedPath, bool) {
	for _, ap := range b.allowedPathsFor(userID) {
		if name == ap.Path || name == ap.Alias || name == "~/"+ap.Alias {
			return ap, true
		}
	}
	return AllowedPath{}, false
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

	// If a schedule wizard is awaiting a prompt, intercept this message.
	sess.mu.Lock()
	wiz := sess.scheduleWizard
	sess.mu.Unlock()
	if wiz != nil && wiz.Step == "awaiting_prompt" {
		b.handleScheduleWizardPrompt(msg.ChatID, sess, text)
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
		sess.resetSession()
		sess.mu.Lock()
		sess.history = nil
		sess.workspace = "global"
		wd := userWorkingDir(b.cfg.Backend.WorkingDir, sess.userID)
		sess.workingDir = wd
		sess.model = ""
		sess.sharedOwnerID = 0
		sess.sharedAccess = ""
		sess.mu.Unlock()
		b.mem.saveUserState(sess.userID, "global", wd)
		b.reply(chatID, "Session reset. Fresh start.")

	case "clear":
		sess.resetSession()
		sess.mu.Lock()
		sess.history = nil
		sess.mu.Unlock()
		b.reply(chatID, "Conversation history cleared.")

	case "workspace", "project":
		if args == "" || args == "list" {
			b.handleProjectList(chatID, sess)
			return
		}
		if args == "shares" {
			b.handleProjectShares(chatID, sess)
			return
		}
		if args == "share" || strings.HasPrefix(args, "share ") {
			shareArgs := strings.TrimSpace(strings.TrimPrefix(args, "share"))
			b.handleProjectShare(chatID, sess, shareArgs)
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
		b.handleFileList(chatID, sess, ws)

	case "model":
		if args == "" {
			b.handleModelMenu(chatID, sess)
			return
		}
		b.handleModelSwitch(chatID, sess, args)

	case "at":
		b.handleAt(chatID, sess, args)

	case "schedule":
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

	case "email":
		b.handleEmailCommand(chatID, sess, args)

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
