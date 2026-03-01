package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// WhatsAppTransport implements Transport using the whatsmeow WhatsApp Web client.
type WhatsAppTransport struct {
	bot    *Bot
	client *whatsmeow.Client
}

func whatsappDBPath() string {
	return filepath.Join(configDir(), "whatsapp.db")
}

func newWhatsAppTransport(bot *Bot) (*WhatsAppTransport, error) {
	dbPath := whatsappDBPath()

	dbLog := waLog.Stdout("WA-DB", "WARN", true)
	container, err := sqlstore.New(context.Background(), "sqlite", "file:"+dbPath+"?_foreign_keys=on", dbLog)
	if err != nil {
		return nil, fmt.Errorf("sqlstore: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("get device: %w", err)
	}

	clientLog := waLog.Stdout("WA", "WARN", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	return &WhatsAppTransport{bot: bot, client: client}, nil
}

func (t *WhatsAppTransport) Name() string { return "wa" }

func (t *WhatsAppTransport) Start(handler func(IncomingMessage)) error {
	t.client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Connected:
			log.Println("whatsapp: connected")
		case *events.Message:
			t.handleMessage(v, handler)
		}
	})

	if t.client.Store.ID == nil {
		// No session stored — need to pair via QR.
		qrChan, _ := t.client.GetQRChannel(context.Background())
		if err := t.client.Connect(); err != nil {
			return fmt.Errorf("whatsapp connect: %w", err)
		}
		log.Println("whatsapp: scan the QR code below to link your device:")
		for evt := range qrChan {
			switch evt.Event {
			case "code":
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				t.forwardQRToAdmin(evt.Code)
			case "success":
				log.Println("whatsapp: paired successfully")
			case "timeout":
				return fmt.Errorf("whatsapp: QR scan timed out")
			}
		}
	} else {
		if err := t.client.Connect(); err != nil {
			return fmt.Errorf("whatsapp connect: %w", err)
		}
	}

	// Block until shutdown.
	<-make(chan struct{})
	return nil
}

func (t *WhatsAppTransport) Send(chatID string, text string) error {
	jid := types.NewJID(chatID, types.DefaultUserServer)
	msg := &waProto.Message{Conversation: proto.String(text)}
	_, err := t.client.SendMessage(context.Background(), jid, msg)
	return err
}

func (t *WhatsAppTransport) SendFile(chatID string, filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	jid := types.NewJID(chatID, types.DefaultUserServer)
	ext := strings.ToLower(filepath.Ext(filePath))
	filename := filepath.Base(filePath)

	mimeType := http.DetectContentType(data)
	if mt := mimeByExt(ext); mt != "" {
		mimeType = mt
	}

	isImage := ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp"

	if isImage {
		uploaded, err := t.client.Upload(context.Background(), data, whatsmeow.MediaImage)
		if err != nil {
			return fmt.Errorf("upload image: %w", err)
		}
		msg := &waProto.Message{
			ImageMessage: &waProto.ImageMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(data))),
				Mimetype:      proto.String(mimeType),
			},
		}
		_, err = t.client.SendMessage(context.Background(), jid, msg)
		return err
	}

	// Send as document.
	uploaded, err := t.client.Upload(context.Background(), data, whatsmeow.MediaDocument)
	if err != nil {
		return fmt.Errorf("upload document: %w", err)
	}
	msg := &waProto.Message{
		DocumentMessage: &waProto.DocumentMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(data))),
			Mimetype:      proto.String(mimeType),
			FileName:      proto.String(filename),
		},
	}
	_, err = t.client.SendMessage(context.Background(), jid, msg)
	return err
}

func (t *WhatsAppTransport) SendTyping(chatID string) {
	jid := types.NewJID(chatID, types.DefaultUserServer)
	t.client.SendChatPresence(context.Background(), jid, types.ChatPresenceComposing, types.ChatPresenceMediaText)
}

// handleMessage processes an incoming WhatsApp message event.
func (t *WhatsAppTransport) handleMessage(evt *events.Message, handler func(IncomingMessage)) {
	// Skip outgoing messages.
	if evt.Info.IsFromMe {
		return
	}
	// Skip group chats.
	if evt.Info.Chat.Server == "g.us" {
		return
	}
	// Skip status updates.
	if evt.Info.Chat.User == "status" {
		return
	}

	phoneStr := evt.Info.Sender.User
	uid, err := parsePhoneNumber(phoneStr)
	if err != nil {
		log.Printf("whatsapp: cannot parse phone %q: %v", phoneStr, err)
		return
	}

	// Check if this user is allowed.
	if t.bot.getSession(uid) == nil {
		log.Printf("whatsapp: message from unknown number %s — ignoring", phoneStr)
		return
	}

	// Extract text content.
	text := evt.Message.GetConversation()
	if text == "" {
		if ext := evt.Message.GetExtendedTextMessage(); ext != nil {
			text = ext.GetText()
		}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	chatID := makeChatID("wa", phoneStr)
	handler(IncomingMessage{
		UserID: uid,
		ChatID: chatID,
		Text:   text,
	})
}

// forwardQRToAdmin sends the QR code string to the Telegram admin as a notice.
// This is a best-effort notification; errors are logged and ignored.
func (t *WhatsAppTransport) forwardQRToAdmin(code string) {
	b := t.bot
	adminID := b.cfg.Telegram.AdminUserID
	if adminID == 0 {
		return
	}
	b.transportsMu.RLock()
	tg, ok := b.transports["tg"]
	b.transportsMu.RUnlock()
	if !ok {
		return
	}
	msg := fmt.Sprintf("WhatsApp QR code ready — scan it from the terminal to connect.\n\nCode: %s", code)
	if err := tg.Send(fmt.Sprintf("%d", adminID), msg); err != nil {
		log.Printf("whatsapp: could not forward QR to Telegram admin: %v", err)
	}
}

// mimeByExt returns a MIME type for common extensions, or "".
func mimeByExt(ext string) string {
	switch ext {
	case ".pdf":
		return "application/pdf"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".mp4":
		return "video/mp4"
	case ".mp3":
		return "audio/mpeg"
	case ".txt":
		return "text/plain"
	case ".csv":
		return "text/csv"
	default:
		return ""
	}
}
