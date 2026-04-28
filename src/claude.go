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
	sharedOwnerID := sess.sharedOwnerID
	sharedAccess := sess.sharedAccess
	sess.mu.Unlock()

	// For shared projects, load memories under the owner's ID so the grantee sees project context.
	// Save new memories under the owner's ID for write access, grantee's own ID for read access.
	memLoadUserID := sess.userID
	memSaveUserID := sess.userID
	if sharedOwnerID != 0 {
		memLoadUserID = sharedOwnerID
		if sharedAccess == "write" {
			memSaveUserID = sharedOwnerID
		}
	}

	phrase := ackForPrompt(text)
	ackIdx++ // advance for next call
	ack := "_" + phrase + "_"
	if ws != "global" {
		ack = fmt.Sprintf("_[%s] %s_", ws, phrase)
	}
	b.reply(chatID, ack)

	before := snapshotFiles(wd)

	var response string
	var err error

	if b.cfg.Backend.REPL && b.cfg.Backend.Type != "opencode" {
		response, err = b.runClaudeSession(sess, memLoadUserID, text)
		if err != nil {
			log.Printf("repl: failed, falling back to fire-and-wait: %v", err)
			response, err = b.runClaude(sess.userID, memLoadUserID, text, ws, wd, model, hist)
		}
	} else {
		response, err = b.runClaude(sess.userID, memLoadUserID, text, ws, wd, model, hist)
	}
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
				go b.autoEmailReport(sess.userID, ws, outPath, wd)
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

	go b.mem.extractAndSave(memSaveUserID, ws, text, response, func(prompt string) (string, error) {
		return b.runClaude(sess.userID, memLoadUserID, prompt, ws, wd, b.cfg.Backend.ExtractModel, nil)
	})
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

// handleModelMenu shows a button menu for switching models, or a text fallback.
func (b *Bot) handleModelMenu(chatID string, sess *Session) {
	sess.mu.Lock()
	active := b.activeModelForSession(sess)
	sess.mu.Unlock()

	if rt, localChatID, ok := b.richTransport(chatID); ok {
		buttons := make([]Button, 0, len(knownModels)+1)
		for _, m := range knownModels {
			label := m.Label
			if m.ID == active {
				label = "✓ " + label
			}
			buttons = append(buttons, Button{Label: label, Data: "modelswitch:" + m.ID})
		}
		buttons = append(buttons, Button{Label: "Save current to project", Data: "modelsave"})
		rt.SendButtonMenu(localChatID, fmt.Sprintf("*Switch model*\nCurrent: `%s`", active), buttons)
		return
	}

	b.reply(chatID, fmt.Sprintf("Current model: `%s`", active))
}

func (b *Bot) handleModelSwitch(chatID string, sess *Session, arg string) {
	parts := strings.Fields(arg)
	if len(parts) == 0 {
		return
	}
	modelName := parts[0]
	save := len(parts) > 1 && parts[1] == "--save"

	sess.resetSession()
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

// buildSystemPrompt constructs the system prompt for a given user/workspace/workingDir,
// including persona, working dir rules, allowed paths, report guidance, README, memories, and skills.
// memUserID is used for memory loading (may differ from userID when in a shared project).
// It does NOT include conversation history (that's managed natively by REPL, or appended by runClaude for fire-and-wait).
func (b *Bot) buildSystemPrompt(userID, memUserID int64, workspace, workingDir string) string {
	home, _ := os.UserHomeDir()
	botHome := userWorkingDir(b.cfg.Backend.WorkingDir, userID)
	allowedForUser := b.allowedPathsFor(userID)
	displayWD := displayPathEx(workingDir, botHome, allowedForUser)
	systemPrompt := b.cfg.Persona.SystemPrompt
	systemPrompt += fmt.Sprintf(
		"\n\n## Working Directory\n"+
			"Your working directory alias is: %s\n"+
			"(~ = %s)\n"+
			"STRICT RULES:\n"+
			"- You MUST stay inside your working directory (or any allowed external directories listed below)\n"+
			"- Never read, write, or reference files outside this directory or the listed allowed paths\n"+
			"- All outputs (files, PDFs, data) go here and nowhere else\n"+
			"- When mentioning paths in responses, always use ~/... shorthand, never the full path",
		displayWD, workingDir)
	if len(allowedForUser) > 0 {
		var extLines []string
		for _, ap := range allowedForUser {
			extLines = append(extLines, fmt.Sprintf("- ~/%s  (= %s)", ap.Alias, ap.Path))
		}
		systemPrompt += "\n\n## Allowed External Directories\n" +
			"You may also read and write files in these additional directories:\n" +
			strings.Join(extLines, "\n") + "\n" +
			"When mentioning files in these directories, use ~/alias/... shorthand.\n" +
			"Do NOT access any other directories outside your workspace or these listed paths."
	}

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

	if uploads := b.recentUploadsBlock(memUserID, workspace, workingDir); uploads != "" {
		systemPrompt += "\n\n" + uploads
	}

	if mem := b.mem.load(memUserID, workspace, b.cfg.Memory.MaxAgeDays); mem != "" {
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

	return systemPrompt
}

// recentUploadsBlock returns a "## Recent Uploads" section listing up to 10 most-recent
// files the user has uploaded into the working directory, so Claude knows they exist.
// Files no longer present on disk are skipped. Returns "" when there's nothing to show.
func (b *Bot) recentUploadsBlock(userID int64, workspace, workingDir string) string {
	files, err := b.mem.listFilesForUser(userID, workspace)
	if err != nil || len(files) == 0 {
		return ""
	}
	const maxFiles = 10
	var lines []string
	for _, f := range files {
		if len(lines) >= maxFiles {
			break
		}
		base := filepath.Base(f.Path)
		if base == "" {
			base = f.Filename
		}
		path := f.Path
		if path == "" {
			path = filepath.Join(workingDir, base)
		}
		if _, statErr := os.Stat(path); statErr != nil {
			continue
		}
		line := "- " + base
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		mdCompanion := filepath.Join(filepath.Dir(path), stem+".md")
		if base != stem+".md" {
			if _, mdErr := os.Stat(mdCompanion); mdErr == nil {
				line += fmt.Sprintf(" (extracted to %s.md)", stem)
			}
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	return "## Recent Uploads\n" +
		"Files the user has uploaded into your working directory (most recent first):\n" +
		strings.Join(lines, "\n")
}

// runClaude executes the Claude CLI with all context provided explicitly (no session access).
// memUserID is used for memory loading; pass userID for the normal case.
func (b *Bot) runClaude(userID, memUserID int64, prompt, workspace, workingDir, model string, history []Message) (string, error) {
	systemPrompt := b.buildSystemPrompt(userID, memUserID, workspace, workingDir)

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
