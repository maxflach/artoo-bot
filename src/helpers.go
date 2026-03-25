package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// userWorkingDir returns the per-user working directory.
func userWorkingDir(baseDir string, userID int64) string {
	return filepath.Join(baseDir, fmt.Sprintf("%d", userID))
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

// displayPath replaces the user base dir prefix with ~ for user-visible output.
func displayPath(path, userBaseDir string) string {
	return displayPathEx(path, userBaseDir, nil)
}

// displayPathEx replaces the user base dir prefix with ~, and also maps allowed
// external paths to their ~/alias/... shorthand.
func displayPathEx(path, userBaseDir string, allowed []AllowedPath) string {
	if strings.HasPrefix(path, userBaseDir) {
		rel := strings.TrimPrefix(path, userBaseDir)
		if rel == "" {
			return "~"
		}
		return "~" + rel
	}
	for _, ap := range allowed {
		if path == ap.Path || strings.HasPrefix(path, ap.Path+"/") {
			rel := strings.TrimPrefix(path, ap.Path)
			if rel == "" {
				return "~/" + ap.Alias
			}
			return "~/" + ap.Alias + rel
		}
	}
	return path
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
			"/project share — share a project with another user (button wizard)\n"+
			"/project shares — list shares and revoke access\n"+
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
			"*Email*\n"+
			"/email — show email status\n"+
			"/email setup — setup instructions\n"+
			"/email test [to] — send a test email\n"+
			"/email send <to> | <subject> | <body> — send an email\n"+
			"/email report [to] — email current report as PDF\n\n"+
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

// handleFileList lists the project's files. On rich transports each file gets a Download button.
func (b *Bot) handleFileList(chatID string, sess *Session, workspace string) {
	files, err := b.mem.listFilesForUser(sess.userID, workspace)
	if err != nil || len(files) == 0 {
		b.reply(chatID, "No files in this project yet.")
		return
	}

	rt, localChatID, hasButtons := b.richTransport(chatID)

	for _, f := range files {
		text := fmt.Sprintf("📄 *%s*\n%s · %s ago", f.Filename, humanSize(f.Size), formatAge(f.CreatedAt))
		if hasButtons {
			rt.SendWithButtons(localChatID, text, []Button{
				{Label: "📥 Download", Data: fmt.Sprintf("sendfile:%d", f.ID)},
				{Label: "🗑 Delete", Data: fmt.Sprintf("delfile:%d", f.ID)},
			})
		} else {
			b.reply(chatID, text)
		}
	}
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
		return b.runClaude(userID, userID, prompt, ws, wd, model, hist)
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
				go b.autoEmailReport(userID, ws, outPath, wd)
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
