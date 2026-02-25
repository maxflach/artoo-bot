package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TelegramTransport implements Transport using the Telegram Bot API.
type TelegramTransport struct {
	bot *Bot
	api *tgbotapi.BotAPI
}

func newTelegramTransport(bot *Bot) (*TelegramTransport, error) {
	api, err := tgbotapi.NewBotAPI(bot.cfg.Telegram.Token)
	if err != nil {
		return nil, err
	}
	return &TelegramTransport{bot: bot, api: api}, nil
}

func (t *TelegramTransport) Name() string { return "tg" }

func (t *TelegramTransport) Send(chatID string, text string) error {
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: invalid chatID %q: %w", chatID, err)
	}
	msg := tgbotapi.NewMessage(id, text)
	msg.ParseMode = "Markdown"
	if _, err := t.api.Send(msg); err != nil {
		msg.ParseMode = ""
		_, err = t.api.Send(msg)
		return err
	}
	return nil
}

func (t *TelegramTransport) SendFile(chatID string, filePath string) error {
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: invalid chatID %q: %w", chatID, err)
	}
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", filePath, err)
	}
	defer f.Close()
	doc := tgbotapi.NewDocument(id, tgbotapi.FileReader{
		Name:   filepath.Base(filePath),
		Reader: f,
	})
	_, err = t.api.Send(doc)
	return err
}

func (t *TelegramTransport) SendTyping(chatID string) {
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return
	}
	t.api.Send(tgbotapi.NewChatAction(id, tgbotapi.ChatTyping))
}

// SendWithButtons implements RichTransport — sends a message with inline keyboard buttons.
func (t *TelegramTransport) SendWithButtons(chatID string, text string, buttons []Button) error {
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: invalid chatID %q: %w", chatID, err)
	}
	row := make([]tgbotapi.InlineKeyboardButton, len(buttons))
	for i, b := range buttons {
		row[i] = tgbotapi.NewInlineKeyboardButtonData(b.Label, b.Data)
	}
	keyboard := tgbotapi.NewInlineKeyboardMarkup(row)
	msg := tgbotapi.NewMessage(id, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	_, err = t.api.Send(msg)
	return err
}

// SendButtonMenu implements RichTransport — sends a message with one button per row.
func (t *TelegramTransport) SendButtonMenu(chatID string, text string, buttons []Button) error {
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: invalid chatID %q: %w", chatID, err)
	}
	rows := make([][]tgbotapi.InlineKeyboardButton, len(buttons))
	for i, b := range buttons {
		rows[i] = tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(b.Label, b.Data))
	}
	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	msg := tgbotapi.NewMessage(id, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	_, err = t.api.Send(msg)
	return err
}

// SendGrid implements RichTransport — sends a message with buttons organized into custom rows.
func (t *TelegramTransport) SendGrid(chatID string, text string, rows [][]Button) error {
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: invalid chatID %q: %w", chatID, err)
	}
	telegramRows := make([][]tgbotapi.InlineKeyboardButton, len(rows))
	for i, row := range rows {
		telegramRows[i] = make([]tgbotapi.InlineKeyboardButton, len(row))
		for j, b := range row {
			telegramRows[i][j] = tgbotapi.NewInlineKeyboardButtonData(b.Label, b.Data)
		}
	}
	keyboard := tgbotapi.NewInlineKeyboardMarkup(telegramRows...)
	msg := tgbotapi.NewMessage(id, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	_, err = t.api.Send(msg)
	return err
}

// sendPhoto sends an image file as a Telegram photo (not a document).
func (t *TelegramTransport) sendPhoto(chatID string, filePath string) error {
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return err
	}
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	photo := tgbotapi.NewPhoto(id, tgbotapi.FileReader{
		Name:   filepath.Base(filePath),
		Reader: f,
	})
	_, err = t.api.Send(photo)
	return err
}

// sendFileAuto sends an image as a photo and other files as documents.
func (t *TelegramTransport) sendFileAuto(localChatID string, filePath string) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp":
		if err := t.sendPhoto(localChatID, filePath); err != nil {
			log.Printf("failed to send photo %s, falling back to document: %v", filePath, err)
			if err := t.SendFile(localChatID, filePath); err != nil {
				log.Printf("failed to send document %s: %v", filePath, err)
			}
		}
	default:
		if err := t.SendFile(localChatID, filePath); err != nil {
			log.Printf("failed to send file %s: %v", filePath, err)
		}
	}
}

// tgChatID formats a Telegram int64 chat ID as a prefixed chatID string.
func tgChatID(id int64) string {
	return fmt.Sprintf("tg:%d", id)
}

// Start begins polling for Telegram updates. Blocks until the update channel closes.
func (t *TelegramTransport) Start(handler func(IncomingMessage)) error {
	log.Printf("%s online (@%s) via Telegram", t.bot.cfg.Persona.Name, t.api.Self.UserName)
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := t.api.GetUpdatesChan(u)

	for update := range updates {
		if update.CallbackQuery != nil {
			go t.handleCallback(update.CallbackQuery)
			continue
		}
		if update.Message == nil {
			continue
		}
		go t.handleTelegramMessage(update.Message, handler)
	}
	return nil
}

// handleTelegramMessage processes a single Telegram message and routes it.
func (t *TelegramTransport) handleTelegramMessage(msg *tgbotapi.Message, handler func(IncomingMessage)) {
	b := t.bot
	chatID := tgChatID(msg.Chat.ID)

	sess := b.getSession(msg.From.ID)
	if sess == nil {
		t.handleUnknownUser(msg)
		return
	}

	sess.mu.Lock()
	sess.chatID = chatID
	sess.mu.Unlock()

	// Handle file/document uploads
	if msg.Document != nil {
		go t.handleFileUpload(chatID, sess, msg.Document)
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	handler(IncomingMessage{
		UserID: msg.From.ID,
		ChatID: chatID,
		Text:   text,
	})
}

// handleUnknownUser notifies the admin when an unrecognised user messages the bot.
func (t *TelegramTransport) handleUnknownUser(msg *tgbotapi.Message) {
	adminID := t.bot.cfg.Telegram.AdminUserID
	if adminID == 0 {
		return
	}
	from := msg.From
	name := strings.TrimSpace(from.FirstName + " " + from.LastName)
	username := from.UserName
	text := msg.Text
	if text == "" {
		text = "(no text)"
	}
	notif := fmt.Sprintf(
		"*Access request*\n\nName: %s\nUsername: @%s\nID: `%d`\n\nMessage: _%s_",
		name, username, from.ID, text,
	)
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Approve", fmt.Sprintf("approve:%d:%s:%s", from.ID, username, name)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Deny", fmt.Sprintf("deny:%d", from.ID)),
		),
	)
	m := tgbotapi.NewMessage(adminID, notif)
	m.ParseMode = "Markdown"
	m.ReplyMarkup = keyboard
	t.api.Send(m)
	t.Send(strconv.FormatInt(msg.Chat.ID, 10), "I don't recognise you yet. I've notified my owner — hang tight.")
}

// handleCallback processes Telegram inline keyboard button presses.
func (t *TelegramTransport) handleCallback(cb *tgbotapi.CallbackQuery) {
	b := t.bot
	t.api.Request(tgbotapi.NewCallback(cb.ID, ""))

	data := cb.Data

	// Callbacks available to all approved users (checked by session existence).
	if strings.HasPrefix(data, "projsetup:") {
		parts := strings.SplitN(strings.TrimPrefix(data, "projsetup:"), ":", 3)
		if len(parts) == 3 {
			t.handleProjectSetupCallback(cb, parts[0], parts[1], parts[2])
		}
		return
	}
	if strings.HasPrefix(data, "projswitch:") {
		t.handleProjSwitchCallback(cb, strings.TrimPrefix(data, "projswitch:"))
		return
	}
	if strings.HasPrefix(data, "projpath:") {
		absPath := strings.TrimPrefix(data, "projpath:")
		if sess := b.getSession(cb.From.ID); sess != nil {
			t.clearButtons(cb)
			b.handleWorkspace(tgChatID(cb.From.ID), sess, absPath)
		}
		return
	}
	if strings.HasPrefix(data, "projupdate:") {
		t.handleProjUpdateCallback(cb, strings.TrimPrefix(data, "projupdate:"))
		return
	}
	if strings.HasPrefix(data, "projstyleset:") {
		t.handleProjStyleSetCallback(cb, strings.TrimPrefix(data, "projstyleset:"))
		return
	}
	if strings.HasPrefix(data, "skillrun:") {
		t.handleSkillRunCallback(cb, strings.TrimPrefix(data, "skillrun:"))
		return
	}
	if strings.HasPrefix(data, "modelswitch:") {
		model := strings.TrimPrefix(data, "modelswitch:")
		if sess := b.getSession(cb.From.ID); sess != nil {
			t.clearButtons(cb)
			b.handleModelSwitch(tgChatID(cb.From.ID), sess, model)
		}
		return
	}
	if data == "modelsave" {
		if sess := b.getSession(cb.From.ID); sess != nil {
			t.clearButtons(cb)
			sess.mu.Lock()
			model := b.activeModelForSession(sess)
			sess.mu.Unlock()
			b.handleModelSwitch(tgChatID(cb.From.ID), sess, model+" --save")
		}
		return
	}
	if strings.HasPrefix(data, "projshare:") {
		t.handleProjShareCallback(cb, strings.TrimPrefix(data, "projshare:"))
		return
	}
	if strings.HasPrefix(data, "projunshare:") {
		t.handleProjUnshareCallback(cb, strings.TrimPrefix(data, "projunshare:"))
		return
	}
	if strings.HasPrefix(data, "attime:") {
		if sess := b.getSession(cb.From.ID); sess != nil {
			t.clearButtons(cb)
			t.handleAtTimeCallback(cb, sess, strings.TrimPrefix(data, "attime:"))
		}
		return
	}
	if strings.HasPrefix(data, "schedwiz:") {
		if sess := b.getSession(cb.From.ID); sess != nil {
			t.clearButtons(cb)
			t.handleSchedWizCallback(cb, sess, strings.TrimPrefix(data, "schedwiz:"))
		}
		return
	}

	// Admin-only callbacks.
	adminID := b.cfg.Telegram.AdminUserID
	if cb.From.ID != adminID {
		return
	}
	adminChatID := tgChatID(adminID)

	if strings.HasPrefix(data, "approve:") {
		parts := strings.SplitN(strings.TrimPrefix(data, "approve:"), ":", 3)
		if len(parts) != 3 {
			return
		}
		var userID int64
		fmt.Sscanf(parts[0], "%d", &userID)
		username := parts[1]
		name := parts[2]

		if err := b.mem.approveUser(userID, username, name); err != nil {
			b.reply(adminChatID, fmt.Sprintf("Failed to approve: %v", err))
			return
		}
		sess := b.addSession(userID)
		copyReportTemplate(b.cfg.Backend.WorkingDir, userID)
		sess.mu.Lock()
		sess.chatID = tgChatID(userID)
		sess.mu.Unlock()
		b.reply(adminChatID, fmt.Sprintf("✅ *%s* (@%s) approved.", name, username))
		b.reply(sess.chatID, "You're in! Send me a message to get started.")

	} else if strings.HasPrefix(data, "deny:") {
		var userID int64
		fmt.Sscanf(strings.TrimPrefix(data, "deny:"), "%d", &userID)
		b.reply(adminChatID, "❌ Request denied.")
		b.reply(tgChatID(userID), "Sorry, access not granted.")

	} else if strings.HasPrefix(data, "delschedule:") {
		var scheduleID int64
		fmt.Sscanf(strings.TrimPrefix(data, "delschedule:"), "%d", &scheduleID)
		sess := b.getSession(cb.From.ID)
		if sess == nil {
			return
		}
		if err := b.mem.deleteSchedule(sess.userID, scheduleID); err != nil {
			t.api.Request(tgbotapi.NewCallback(cb.ID, "Failed to remove"))
			return
		}
		b.cron.reload()
		if cb.Message != nil {
			edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID,
				cb.Message.Text+"\n\n~removed~")
			edit.ParseMode = "Markdown"
			t.api.Request(edit)
			t.api.Request(tgbotapi.NewEditMessageReplyMarkup(cb.Message.Chat.ID, cb.Message.MessageID, tgbotapi.InlineKeyboardMarkup{}))
		}
		t.api.Request(tgbotapi.NewCallback(cb.ID, "Removed ✓"))
		return

	} else if strings.HasPrefix(data, "wishdone:") {
		var id int64
		fmt.Sscanf(strings.TrimPrefix(data, "wishdone:"), "%d", &id)
		b.mem.markWishDone(id)
		if cb.Message != nil {
			edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID,
				cb.Message.Text+"\n\n~done~")
			edit.ParseMode = "Markdown"
			t.api.Request(edit)
			t.api.Request(tgbotapi.NewEditMessageReplyMarkup(cb.Message.Chat.ID, cb.Message.MessageID, tgbotapi.InlineKeyboardMarkup{}))
		}
		t.api.Request(tgbotapi.NewCallback(cb.ID, "Marked done ✓"))
		return
	}

	_ = adminChatID

	// Remove buttons from original message (for approve/deny).
	if cb.Message != nil {
		edit := tgbotapi.NewEditMessageReplyMarkup(adminID, cb.Message.MessageID, tgbotapi.InlineKeyboardMarkup{})
		t.api.Request(edit)
	}
}

func (t *TelegramTransport) clearButtons(cb *tgbotapi.CallbackQuery) {
	if cb.Message != nil {
		t.api.Request(tgbotapi.NewEditMessageReplyMarkup(
			cb.Message.Chat.ID, cb.Message.MessageID, tgbotapi.InlineKeyboardMarkup{}))
	}
}

func (t *TelegramTransport) handleProjSwitchCallback(cb *tgbotapi.CallbackQuery, name string) {
	b := t.bot
	sess := b.getSession(cb.From.ID)
	if sess == nil {
		return
	}
	t.clearButtons(cb)
	b.handleWorkspace(tgChatID(cb.From.ID), sess, name)
}

func (t *TelegramTransport) handleProjUpdateCallback(cb *tgbotapi.CallbackQuery, field string) {
	b := t.bot
	sess := b.getSession(cb.From.ID)
	if sess == nil {
		return
	}
	chatID := tgChatID(cb.From.ID)
	_, localChatID := splitChatID(chatID)
	t.clearButtons(cb)

	switch field {
	case "readme":
		go b.handleProjectUpdate(chatID, sess, "")
	case "style":
		t.SendButtonMenu(localChatID, "Choose a new agent style:", agentStyleButtons("projstyleset"))
	case "schedules":
		b.handleScheduleList(chatID, sess)
	}
}

func (t *TelegramTransport) handleProjStyleSetCallback(cb *tgbotapi.CallbackQuery, style string) {
	b := t.bot
	sess := b.getSession(cb.From.ID)
	if sess == nil {
		return
	}
	t.clearButtons(cb)
	go b.handleProjectStyleUpdate(tgChatID(cb.From.ID), sess, style)
}

func (t *TelegramTransport) handleSkillRunCallback(cb *tgbotapi.CallbackQuery, skillName string) {
	b := t.bot
	sess := b.getSession(cb.From.ID)
	if sess == nil {
		return
	}
	t.clearButtons(cb)
	b.skillsMu.RLock()
	skill, ok := b.skills[skillName]
	b.skillsMu.RUnlock()
	if ok {
		go b.runSkill(tgChatID(cb.From.ID), sess, skill, "")
	}
}

// handleFileUpload saves a Telegram document to the workspace and extracts it as markdown.
func (t *TelegramTransport) handleFileUpload(chatID string, sess *Session, doc *tgbotapi.Document) {
	b := t.bot
	sess.mu.Lock()
	wd := sess.workingDir
	ws := sess.workspace
	model := b.activeModelForSession(sess)
	sess.mu.Unlock()

	// Template and logo uploads are saved directly without Claude extraction.
	if doc.FileName == "template.yaml" || doc.FileName == "logo.png" {
		fileConfig := tgbotapi.FileConfig{FileID: doc.FileID}
		file, err := t.api.GetFile(fileConfig)
		if err != nil {
			b.reply(chatID, fmt.Sprintf("Failed to get file: %v", err))
			return
		}
		url := file.Link(b.cfg.Telegram.Token)
		destPath := filepath.Join(wd, doc.FileName)
		if _, err := downloadURL(url, destPath); err != nil {
			b.reply(chatID, fmt.Sprintf("Failed to download: %v", err))
			return
		}
		wsLabel := ws
		if wsLabel == "global" {
			wsLabel = "your default"
		}
		b.reply(chatID, fmt.Sprintf("Report template updated for project *%s* ✓", wsLabel))
		return
	}

	b.reply(chatID, fmt.Sprintf("Received *%s* — saving to project...", doc.FileName))

	fileConfig := tgbotapi.FileConfig{FileID: doc.FileID}
	file, err := t.api.GetFile(fileConfig)
	if err != nil {
		b.reply(chatID, fmt.Sprintf("Failed to get file: %v", err))
		return
	}

	url := file.Link(b.cfg.Telegram.Token)
	destPath := filepath.Join(wd, doc.FileName)
	if _, err := downloadURL(url, destPath); err != nil {
		b.reply(chatID, fmt.Sprintf("Failed to download: %v", err))
		return
	}

	ext := strings.ToLower(filepath.Ext(doc.FileName))
	mdName := strings.TrimSuffix(doc.FileName, filepath.Ext(doc.FileName)) + ".md"

	var extractHint string
	switch ext {
	case ".pdf":
		extractHint = fmt.Sprintf("Use pdftotext to extract text: run `pdftotext \"%s\" -` (prints to stdout). If unavailable, try `pandoc \"%s\" -t plain`.", doc.FileName, doc.FileName)
	case ".docx", ".doc":
		extractHint = fmt.Sprintf("Use pandoc: run `pandoc \"%s\" -t plain`.", doc.FileName)
	case ".xlsx", ".xls":
		extractHint = fmt.Sprintf("Use python3 with openpyxl or ssconvert to read \"%s\" as text.", doc.FileName)
	default:
		extractHint = fmt.Sprintf("Read \"%s\" directly as plain text.", doc.FileName)
	}

	prompt := fmt.Sprintf(`A file called "%s" has been saved to this workspace.

%s

Create a well-structured markdown file called "%s":
- # Heading: document title or filename
- ## Sections organised by topic
- Preserve all facts, numbers, dates, names, and data — be comprehensive
- Use tables for tabular data
- Write the file directly to disk.`,
		doc.FileName, extractHint, mdName)

	_, localChatID := splitChatID(chatID)
	stopTyping := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopTyping:
				return
			default:
				t.SendTyping(localChatID)
				time.Sleep(4 * time.Second)
			}
		}
	}()

	_, err = b.runClaude(sess.userID, sess.userID, prompt, ws, wd, model, nil)
	close(stopTyping)
	if err != nil {
		b.reply(chatID, fmt.Sprintf("File saved but extraction failed: %v", err))
		return
	}

	b.mem.recordFile(sess.userID, ws, doc.FileName, destPath, int64(doc.FileSize))

	// Populate memories from the extracted markdown.
	mdPath := filepath.Join(wd, mdName)
	if mdContent, readErr := os.ReadFile(mdPath); readErr == nil {
		go b.mem.extractAndSave(
			sess.userID, ws,
			fmt.Sprintf("File uploaded: %s", doc.FileName),
			string(mdContent),
			func(p string) (string, error) {
				return b.runClaude(sess.userID, sess.userID, p, ws, wd, b.cfg.Backend.ExtractModel, nil)
			},
		)
	}

	b.reply(chatID, fmt.Sprintf("*%s* extracted to `%s` ✓", doc.FileName, mdName))
}

// handleProjShareCallback dispatches projshare: sub-callbacks (step2, step3, confirm).
func (t *TelegramTransport) handleProjShareCallback(cb *tgbotapi.CallbackQuery, payload string) {
	b := t.bot
	sess := b.getSession(cb.From.ID)
	if sess == nil {
		return
	}
	chatID := tgChatID(cb.From.ID)
	_, localChatID := splitChatID(chatID)

	// payload is one of: "step2:<project>", "step3:<project>:<granteeID>", "confirm:<project>:<granteeID>:<access>"
	parts := strings.SplitN(payload, ":", 2)
	if len(parts) != 2 {
		return
	}
	step, rest := parts[0], parts[1]

	switch step {
	case "step2":
		// rest = projectName — show user picker
		t.clearButtons(cb)
		b.sendShareUserPicker(t, localChatID, sess.userID, rest)

	case "step3":
		// rest = "<project>:<granteeID>" — show access level picker
		idx := strings.LastIndex(rest, ":")
		if idx < 0 {
			return
		}
		project := rest[:idx]
		granteeIDStr := rest[idx+1:]
		var granteeID int64
		fmt.Sscanf(granteeIDStr, "%d", &granteeID)
		t.clearButtons(cb)
		t.SendWithButtons(localChatID,
			fmt.Sprintf("*Share _%s_* — choose access level:", project),
			[]Button{
				{Label: "Read", Data: fmt.Sprintf("projshare:confirm:%s:%d:read", project, granteeID)},
				{Label: "Read & Write", Data: fmt.Sprintf("projshare:confirm:%s:%d:write", project, granteeID)},
			})

	case "confirm":
		// rest = "<project>:<granteeID>:<access>"
		// Split from the right to get access first, then granteeID, then project.
		lastColon := strings.LastIndex(rest, ":")
		if lastColon < 0 {
			return
		}
		access := rest[lastColon+1:]
		mid := rest[:lastColon]
		midColon := strings.LastIndex(mid, ":")
		if midColon < 0 {
			return
		}
		project := mid[:midColon]
		granteeIDStr := mid[midColon+1:]
		var granteeID int64
		fmt.Sscanf(granteeIDStr, "%d", &granteeID)

		if err := b.mem.shareWorkspace(sess.userID, project, granteeID, access); err != nil {
			b.reply(chatID, fmt.Sprintf("Failed to share: %v", err))
			return
		}

		granteeUsername := b.mem.usernameFor(granteeID)
		accessLabel := access
		if access == "write" {
			accessLabel = "read & write"
		}
		ownerUsername := b.mem.usernameFor(sess.userID)

		// Edit confirmation into the message.
		if cb.Message != nil {
			confirmText := fmt.Sprintf("✓ Shared *%s* with @%s (%s access)", project, granteeUsername, accessLabel)
			edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, confirmText)
			edit.ParseMode = "Markdown"
			t.api.Request(edit)
			t.api.Request(tgbotapi.NewEditMessageReplyMarkup(cb.Message.Chat.ID, cb.Message.MessageID, tgbotapi.InlineKeyboardMarkup{}))
		}

		// Notify grantee.
		granteeChatID := b.chatIDForUser(granteeID)
		if granteeChatID != "" {
			b.reply(granteeChatID, fmt.Sprintf(
				"@%s shared project `%s` with you (%s access).\nUse /project to switch to it.",
				ownerUsername, project, accessLabel))
		}
	}
}

// handleProjUnshareCallback handles projunshare:<project>:<granteeID>.
func (t *TelegramTransport) handleProjUnshareCallback(cb *tgbotapi.CallbackQuery, payload string) {
	b := t.bot
	sess := b.getSession(cb.From.ID)
	if sess == nil {
		return
	}

	lastColon := strings.LastIndex(payload, ":")
	if lastColon < 0 {
		return
	}
	project := payload[:lastColon]
	granteeIDStr := payload[lastColon+1:]
	var granteeID int64
	fmt.Sscanf(granteeIDStr, "%d", &granteeID)

	if err := b.mem.unshareWorkspace(sess.userID, project, granteeID); err != nil {
		b.reply(tgChatID(cb.From.ID), fmt.Sprintf("Failed to revoke: %v", err))
		return
	}

	granteeUsername := b.mem.usernameFor(granteeID)

	// Edit the share message to show it's been revoked.
	if cb.Message != nil {
		revokedText := fmt.Sprintf("~*%s* → @%s — revoked~", project, granteeUsername)
		edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, revokedText)
		edit.ParseMode = "Markdown"
		t.api.Request(edit)
		t.api.Request(tgbotapi.NewEditMessageReplyMarkup(cb.Message.Chat.ID, cb.Message.MessageID, tgbotapi.InlineKeyboardMarkup{}))
	}
	t.api.Request(tgbotapi.NewCallback(cb.ID, "Access revoked ✓"))

	// Notify grantee.
	ownerUsername := b.mem.usernameFor(sess.userID)
	granteeChatID := b.chatIDForUser(granteeID)
	if granteeChatID != "" {
		b.reply(granteeChatID, fmt.Sprintf(
			"@%s has revoked your access to project `%s`.", ownerUsername, project))
	}
}

// handleAtTimeCallback handles the attime: callback — user picked a time for a /at reminder.
func (t *TelegramTransport) handleAtTimeCallback(cb *tgbotapi.CallbackQuery, sess *Session, timeStr string) {
	b := t.bot
	chatID := tgChatID(cb.From.ID)

	cronExpr, desc, err := parseAtTime(timeStr)
	if err != nil {
		b.reply(chatID, fmt.Sprintf("Couldn't parse time: %v", err))
		return
	}

	sess.mu.Lock()
	wiz := sess.scheduleWizard
	sess.mu.Unlock()

	if wiz != nil && wiz.IsAt && wiz.Prompt != "" {
		// Prompt was pre-supplied — schedule immediately.
		sess.mu.Lock()
		ws := sess.workspace
		wd := sess.workingDir
		cid := sess.chatID
		sess.scheduleWizard = nil
		sess.mu.Unlock()

		name := fmt.Sprintf("at:%s", timeStr)
		if err := b.mem.addSchedule(sess.userID, cid, name, cronExpr, wiz.Prompt, ws, wd, true); err != nil {
			b.reply(chatID, fmt.Sprintf("Failed to set reminder: %v", err))
			return
		}
		b.cron.reload()
		b.reply(chatID, fmt.Sprintf("Scheduled for *%s* ✓", desc))
		return
	}

	// Store the time and wait for the user to type the prompt.
	sess.mu.Lock()
	sess.scheduleWizard = &ScheduleWizard{
		IsAt:     true,
		CronExpr: cronExpr,
		CronDesc: desc,
		Step:     "awaiting_prompt",
	}
	sess.mu.Unlock()

	b.reply(chatID, fmt.Sprintf("Got it — *%s*.\nWhat should I do? Send me the task:", desc))
}

// handleSchedWizCallback handles the schedwiz: callback — user is in the /schedule wizard.
func (t *TelegramTransport) handleSchedWizCallback(cb *tgbotapi.CallbackQuery, sess *Session, data string) {
	b := t.bot
	chatID := tgChatID(cb.From.ID)
	_, localChatID := splitChatID(chatID)

	parts := strings.SplitN(data, ":", 2)
	if len(parts) != 2 {
		return
	}
	subCmd, value := parts[0], parts[1]

	switch subCmd {
	case "full":
		// Complete recurrence, no time selection needed.
		cronExpr, desc, err := parseRecurring(value)
		if err != nil {
			b.reply(chatID, fmt.Sprintf("Error: %v", err))
			return
		}
		sess.mu.Lock()
		if sess.scheduleWizard == nil {
			sess.scheduleWizard = &ScheduleWizard{}
		}
		sess.scheduleWizard.CronExpr = cronExpr
		sess.scheduleWizard.CronDesc = desc
		sess.scheduleWizard.Step = "awaiting_prompt"
		sess.mu.Unlock()
		b.reply(chatID, fmt.Sprintf("Got it — *%s*.\nWhat should I run? Send me the task prompt:", desc))

	case "base":
		// Recurrence needs a time — show time picker.
		sess.mu.Lock()
		if sess.scheduleWizard == nil {
			sess.scheduleWizard = &ScheduleWizard{}
		}
		sess.scheduleWizard.RecurrenceBase = value
		sess.scheduleWizard.Step = "time"
		sess.mu.Unlock()
		if rt, _, ok := b.richTransport(chatID); ok {
			rt.SendGrid(localChatID, "Pick a time:", timePickerGrid())
		}

	case "time":
		sess.mu.Lock()
		wiz := sess.scheduleWizard
		sess.mu.Unlock()
		if wiz == nil {
			return
		}
		fullRecurrence := wiz.RecurrenceBase + " " + value
		cronExpr, desc, err := parseRecurring(fullRecurrence)
		if err != nil {
			b.reply(chatID, fmt.Sprintf("Error: %v", err))
			return
		}
		sess.mu.Lock()
		sess.scheduleWizard.CronExpr = cronExpr
		sess.scheduleWizard.CronDesc = desc
		sess.scheduleWizard.Step = "awaiting_prompt"
		sess.mu.Unlock()
		b.reply(chatID, fmt.Sprintf("Got it — *%s*.\nWhat should I run? Send me the task prompt:", desc))
	}
}

// handleProjectSetupCallback processes button answers for interactive project creation.
func (t *TelegramTransport) handleProjectSetupCallback(cb *tgbotapi.CallbackQuery, token, field, value string) {
	b := t.bot
	t.api.Request(tgbotapi.NewCallback(cb.ID, ""))

	b.pendingProjectsMu.Lock()
	pending, ok := b.pendingProjects[token]
	b.pendingProjectsMu.Unlock()
	if !ok {
		return
	}

	// Clear buttons from the answered message.
	if cb.Message != nil {
		t.api.Request(tgbotapi.NewEditMessageReplyMarkup(
			cb.Message.Chat.ID, cb.Message.MessageID, tgbotapi.InlineKeyboardMarkup{}))
	}

	switch field {
	case "research":
		pending.IsResearch = value == "yes"
		pending.Step = 2
		_, localChatID := splitChatID(pending.ChatID)
		t.SendWithButtons(localChatID, "Auto-generate PDF reports after each run?", []Button{
			{Label: "Yes — send PDF", Data: fmt.Sprintf("projsetup:%s:report:yes", token)},
			{Label: "No thanks", Data: fmt.Sprintf("projsetup:%s:report:no", token)},
		})

	case "report":
		pending.AutoReport = value == "yes"
		pending.Step = 3
		_, localChatID := splitChatID(pending.ChatID)
		t.SendButtonMenu(localChatID, "Choose an agent style for this project:", agentStyleButtons(fmt.Sprintf("projsetup:%s:style", token)))

	case "style":
		pending.AgentStyle = value
		b.pendingProjectsMu.Lock()
		delete(b.pendingProjects, token)
		b.pendingProjectsMu.Unlock()

		b.reply(pending.ChatID, fmt.Sprintf("Writing README for *%s*...", pending.Name))
		sess := b.getSession(pending.UserID)
		if sess == nil {
			return
		}
		// Point the session at the project dir so generateWorkspaceReadme uses it.
		sess.mu.Lock()
		sess.workingDir = pending.WorkingDir
		sess.mu.Unlock()
		opts := &ProjectOptions{IsResearch: pending.IsResearch, AutoReport: pending.AutoReport, AgentStyle: pending.AgentStyle}
		go b.generateWorkspaceReadme(pending.ChatID, sess, pending.Name, pending.Description, opts)
	}
}
