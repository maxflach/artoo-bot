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

// handleCallback processes Telegram inline keyboard button presses (approve/deny/delschedule).
func (t *TelegramTransport) handleCallback(cb *tgbotapi.CallbackQuery) {
	b := t.bot
	t.api.Request(tgbotapi.NewCallback(cb.ID, ""))

	adminID := b.cfg.Telegram.AdminUserID
	if cb.From.ID != adminID {
		return
	}

	data := cb.Data
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

	} else if strings.HasPrefix(data, "projsetup:") {
		// projsetup:<token>:<field>:<value>
		parts := strings.SplitN(strings.TrimPrefix(data, "projsetup:"), ":", 3)
		if len(parts) == 3 {
			t.handleProjectSetupCallback(cb, parts[0], parts[1], parts[2])
		}
		return

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
	}

	// Remove buttons from original message (for approve/deny)
	if cb.Message != nil {
		edit := tgbotapi.NewEditMessageReplyMarkup(adminID, cb.Message.MessageID, tgbotapi.InlineKeyboardMarkup{})
		t.api.Request(edit)
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

	_, err = b.runClaude(sess.userID, prompt, ws, wd, model, nil)
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
				return b.runClaude(sess.userID, p, ws, wd, b.cfg.Backend.ExtractModel, nil)
			},
		)
	}

	b.reply(chatID, fmt.Sprintf("*%s* extracted to `%s` ✓", doc.FileName, mdName))
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
		opts := &ProjectOptions{IsResearch: pending.IsResearch, AutoReport: pending.AutoReport}
		go b.generateWorkspaceReadme(pending.ChatID, sess, pending.Name, pending.Description, opts)
	}
}
