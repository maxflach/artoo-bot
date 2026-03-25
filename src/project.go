package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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
	activeWD := sess.workingDir
	activeSharedOwnerID := sess.sharedOwnerID
	sess.mu.Unlock()

	type proj struct {
		name  string
		title string
		dir   string
		data  string // button callback data
	}
	projects := []proj{{"global", "Global (default)", baseWD, "projswitch:global"}}
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
		projects = append(projects, proj{name, title, dir, "projswitch:" + name})
	}

	for _, ap := range b.allowedPathsFor(sess.userID) {
		title := "[ext] " + ap.Alias
		if readme := readWorkspaceReadme(ap.Path); readme != "" {
			for _, line := range strings.Split(readme, "\n") {
				if strings.HasPrefix(line, "# ") {
					title = "[ext] " + strings.TrimPrefix(line, "# ")
					break
				}
			}
		}
		projects = append(projects, proj{"ext:" + ap.Alias, title, ap.Path, "projpath:" + ap.Path})
	}

	// Add projects shared with me.
	for _, s := range b.mem.listSharedWithMe(sess.userID) {
		ownerBaseDir := userWorkingDir(b.cfg.Backend.WorkingDir, s.OwnerID)
		wsDir := projectDir(ownerBaseDir, s.Workspace)
		displayName := s.OwnerName
		if displayName == "" {
			displayName = fmt.Sprintf("%d", s.OwnerID)
		}
		switchKey := fmt.Sprintf("@%s/%s", displayName, s.Workspace)
		accessLabel := s.Access
		if s.Access == "write" {
			accessLabel = "read & write"
		}
		title := fmt.Sprintf("@%s/%s (%s)", displayName, s.Workspace, accessLabel)
		// Use owner ID in callback to avoid relying on username lookup
		callbackKey := fmt.Sprintf("@id:%d/%s", s.OwnerID, s.Workspace)
		projects = append(projects, proj{switchKey, title, wsDir, "projswitch:" + callbackKey})
	}

	isActive := func(p proj) bool {
		if activeSharedOwnerID != 0 && strings.HasPrefix(p.name, "@") {
			return p.dir == activeWD
		}
		if activeSharedOwnerID == 0 && p.name == activeWS {
			return true
		}
		if strings.HasPrefix(p.name, "ext:") && p.dir == activeWD {
			return true
		}
		return false
	}

	if rt, localChatID, ok := b.richTransport(chatID); ok {
		buttons := make([]Button, len(projects))
		for i, p := range projects {
			label := p.title
			if isActive(p) {
				label = "✓ " + label
			}
			buttons[i] = Button{Label: label, Data: p.data}
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
		if isActive(p) {
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

	// Shared project: @owner/project
	if strings.HasPrefix(name, "@") {
		b.handleSharedWorkspace(chatID, sess, name)
		return
	}

	// External path branch — check before any other logic.
	if ap, ok := b.matchAllowedPath(sess.userID, name); ok {
		if _, err := os.Stat(ap.Path); err != nil {
			b.reply(chatID, fmt.Sprintf("External directory not accessible: %v", err))
			return
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
		b.reply(chatID, fmt.Sprintf("Switched to *%s* (external) ✓", ap.Alias))
		return
	}

	// Reject bare absolute paths not in the allowed list.
	if filepath.IsAbs(name) {
		b.reply(chatID, "That path is not in your allowed directories.")
		return
	}

	// Always derive base from the user's canonical workspace root, regardless
	// of whether they are currently in an external path.
	baseWD := userWorkingDir(b.cfg.Backend.WorkingDir, sess.userID)

	wsDir := projectDir(baseWD, name)
	isNew := false
	if _, err := os.Stat(wsDir); os.IsNotExist(err) {
		isNew = true
	}

	if err := os.MkdirAll(wsDir, 0755); err != nil {
		b.reply(chatID, fmt.Sprintf("Failed to create project directory: %v", err))
		return
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

// handleSharedWorkspace switches to a project owned by another user.
// Accepts "@owner/project" (username) or "@id:NNNN/project" (numeric owner ID).
func (b *Bot) handleSharedWorkspace(chatID string, sess *Session, name string) {
	atPart := strings.TrimPrefix(name, "@")
	slashIdx := strings.Index(atPart, "/")
	if slashIdx < 0 {
		b.reply(chatID, "Invalid shared project format. Use @owner/project.")
		return
	}
	ownerPart := atPart[:slashIdx]
	projectName := atPart[slashIdx+1:]

	var ownerID int64
	var ownerUsername string
	if strings.HasPrefix(ownerPart, "id:") {
		// Numeric ID from button callback — reliable, no username lookup needed.
		fmt.Sscanf(strings.TrimPrefix(ownerPart, "id:"), "%d", &ownerID)
		ownerUsername = ownerPart // display fallback
	} else {
		ownerUsername = ownerPart
		ownerID = b.mem.userIDForUsername(ownerUsername)
	}
	if ownerID == 0 {
		b.reply(chatID, fmt.Sprintf("User @%s not found.", ownerUsername))
		return
	}

	access := b.mem.getShareAccess(sess.userID, ownerID, projectName)
	if access == "" {
		b.reply(chatID, fmt.Sprintf("You don't have access to @%s/%s.", ownerUsername, projectName))
		return
	}

	ownerBaseDir := userWorkingDir(b.cfg.Backend.WorkingDir, ownerID)
	wsDir := projectDir(ownerBaseDir, projectName)
	if _, err := os.Stat(wsDir); err != nil {
		b.reply(chatID, fmt.Sprintf("Shared project directory not found: %v", err))
		return
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

	accessLabel := "read"
	if access == "write" {
		accessLabel = "read & write"
	}
	b.reply(chatID, fmt.Sprintf("Switched to *@%s/%s* (%s access) ✓", ownerUsername, projectName, accessLabel))
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

// generateWorkspaceReadme runs Claude to generate README content, then writes it to disk.
func (b *Bot) generateWorkspaceReadme(chatID string, sess *Session, name, description string, opts *ProjectOptions) {
	sess.mu.Lock()
	wsDir := sess.workingDir
	model := b.activeModelForSession(sess)
	sess.mu.Unlock()

	prompt := buildReadmePrompt(name, description, opts)

	response, err := b.runClaude(sess.userID, sess.userID, prompt, name, wsDir, model, nil)
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
	canWrite := sess.sharedOwnerID == 0 || sess.sharedAccess == "write"
	sess.mu.Unlock()

	if !canWrite {
		b.reply(chatID, "You have read-only access to this shared project. Ask the owner for write access.")
		return
	}

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
	response, err := b.runClaude(sess.userID, sess.userID, prompt, ws, wsDir, model, nil)
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
	sess.mu.Lock()
	canWrite := sess.sharedOwnerID == 0 || sess.sharedAccess == "write"
	ws := sess.workspace
	sess.mu.Unlock()

	if !canWrite {
		b.reply(chatID, "You have read-only access to this shared project. Ask the owner for write access.")
		return
	}

	rt, localChatID, ok := b.richTransport(chatID)
	if !ok {
		b.handleProjectUpdate(chatID, sess, "")
		return
	}
	rt.SendButtonMenu(localChatID,
		fmt.Sprintf("Update *%s* — what would you like to change?", ws),
		[]Button{
			{Label: "Improve README", Data: "projupdate:readme"},
			{Label: "Change agent style", Data: "projupdate:style"},
			{Label: "View schedules", Data: "projupdate:schedules"},
		},
	)
}

// canWriteProject returns true if the user has write access to the current project.
// Caller should hold sess.mu or have already snapshotted the relevant fields.
func canWriteProject(sharedOwnerID int64, sharedAccess string) bool {
	return sharedOwnerID == 0 || sharedAccess == "write"
}

// handleProjectShare is the entry point for the share wizard.
// If args is a project name, jump to step 2 (user picker). Otherwise show project picker.
func (b *Bot) handleProjectShare(chatID string, sess *Session, args string) {
	rt, localChatID, ok := b.richTransport(chatID)
	if !ok {
		b.reply(chatID, "Use /project share in a button-enabled chat.")
		return
	}

	if args != "" {
		b.sendShareUserPicker(rt, localChatID, sess.userID, args)
		return
	}

	// Step 1: show own projects as buttons.
	baseWD := userWorkingDir(b.cfg.Backend.WorkingDir, sess.userID)
	entries, err := os.ReadDir(baseWD)
	if err != nil {
		b.reply(chatID, "Could not read projects directory.")
		return
	}
	var buttons []Button
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		buttons = append(buttons, Button{Label: name, Data: "projshare:step2:" + name})
	}
	if len(buttons) == 0 {
		b.reply(chatID, "You have no projects to share.")
		return
	}
	rt.SendButtonMenu(localChatID, "*Share a project* — pick one:", buttons)
}

// sendShareUserPicker shows a list of approved users (excluding owner) to pick a grantee.
func (b *Bot) sendShareUserPicker(rt RichTransport, localChatID string, ownerID int64, project string) {
	users := b.mem.listApprovedUsers()
	var buttons []Button
	for _, u := range users {
		if u.UserID == ownerID {
			continue
		}
		label := u.Name
		if u.Username != "" {
			label += " (@" + u.Username + ")"
		}
		buttons = append(buttons, Button{
			Label: label,
			Data:  fmt.Sprintf("projshare:step3:%s:%d", project, u.UserID),
		})
	}
	if len(buttons) == 0 {
		rt.SendButtonMenu(localChatID, "No other approved users to share with.", []Button{})
		return
	}
	rt.SendButtonMenu(localChatID, fmt.Sprintf("*Share _%s_* — pick a user:", project), buttons)
}

// handleProjectShares lists all shares owned by (and shared with) the current user.
func (b *Bot) handleProjectShares(chatID string, sess *Session) {
	sharedByMe := b.mem.listSharedByMe(sess.userID)
	sharedWithMe := b.mem.listSharedWithMe(sess.userID)

	if len(sharedByMe) == 0 && len(sharedWithMe) == 0 {
		b.reply(chatID, "No shares configured.\n\nUse `/project share` to share a project with another user.")
		return
	}

	rt, localChatID, hasButtons := b.richTransport(chatID)

	if len(sharedByMe) > 0 {
		if hasButtons {
			b.reply(chatID, "*Projects I've shared:*")
			for _, s := range sharedByMe {
				granteeUsername := b.mem.usernameFor(s.GranteeID)
				accessLabel := s.Access
				if s.Access == "write" {
					accessLabel = "read & write"
				}
				text := fmt.Sprintf("*%s* → @%s (%s access)", s.Workspace, granteeUsername, accessLabel)
				rt.SendWithButtons(localChatID, text, []Button{
					{Label: "Revoke", Data: fmt.Sprintf("projunshare:%s:%d", s.Workspace, s.GranteeID)},
				})
			}
		} else {
			var lines []string
			lines = append(lines, "*Projects I've shared:*")
			for _, s := range sharedByMe {
				granteeUsername := b.mem.usernameFor(s.GranteeID)
				lines = append(lines, fmt.Sprintf("• *%s* → @%s (%s)", s.Workspace, granteeUsername, s.Access))
			}
			b.reply(chatID, strings.Join(lines, "\n"))
		}
	}

	if len(sharedWithMe) > 0 {
		var lines []string
		lines = append(lines, "*Shared with me:*")
		for _, s := range sharedWithMe {
			accessLabel := s.Access
			if s.Access == "write" {
				accessLabel = "read & write"
			}
			lines = append(lines, fmt.Sprintf("• @%s/*%s* (%s)", s.OwnerName, s.Workspace, accessLabel))
		}
		b.reply(chatID, strings.Join(lines, "\n"))
	}
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
	ws := sess.workspace
	sess.mu.Unlock()

	readmePath := filepath.Join(wsDir, "README.md")
	raw, err := os.ReadFile(readmePath)
	if err != nil {
		// No README yet — create one with just the agent section.
		content := "# " + ws + "\n" + section
		if err := os.WriteFile(readmePath, []byte(content), 0644); err != nil {
			b.reply(chatID, fmt.Sprintf("Failed to create README: %v", err))
			return
		}
		label := style
		for _, s := range agentStyles {
			if s.Key == style {
				label = s.Label
				break
			}
		}
		b.reply(chatID, fmt.Sprintf("Created README with agent style *%s* ✓", label))
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
