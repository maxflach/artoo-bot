package main

import "strings"

// IncomingMessage is a transport-agnostic incoming message from a user.
type IncomingMessage struct {
	UserID   int64
	ChatID   string // prefixed: "tg:123", "dc:456", "wc:abc"
	Text     string
	// File upload fields (only set for file messages)
	FileID   string
	FileName string
	FileSize int
}

// Button is a clickable action button in a message.
type Button struct {
	Label string
	Data  string // callback data
}

// Transport is the interface all messaging backends must implement.
type Transport interface {
	// Name returns the short, stable prefix used in chatID strings (e.g. "tg").
	Name() string
	// Send delivers a text message to the given transport-local chat ID.
	Send(chatID string, text string) error
	// SendFile delivers a local file to the given chat ID.
	SendFile(chatID string, filePath string) error
	// SendTyping sends a typing indicator (no-op if unsupported).
	SendTyping(chatID string)
	// Start begins receiving messages, calling handler for each one.
	// Blocks until the transport shuts down or returns an error.
	Start(handler func(IncomingMessage)) error
}

// RichTransport is an optional extension for transports that support interactive buttons.
type RichTransport interface {
	Transport
	SendWithButtons(chatID string, text string, buttons []Button) error
}

// makeChatID builds a prefixed chatID string from transport name and local ID.
func makeChatID(transport, local string) string {
	return transport + ":" + local
}

// splitChatID splits "tg:123" → ("tg", "123").
func splitChatID(chatID string) (transport, local string) {
	if i := strings.IndexByte(chatID, ':'); i != -1 {
		return chatID[:i], chatID[i+1:]
	}
	return "", chatID
}
