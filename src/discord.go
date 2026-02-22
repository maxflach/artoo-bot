package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// DiscordTransport implements Transport using the Discord Bot API.
type DiscordTransport struct {
	bot     *Bot
	session *discordgo.Session
}

func newDiscordTransport(bot *Bot) (*DiscordTransport, error) {
	dg, err := discordgo.New("Bot " + bot.cfg.Discord.Token)
	if err != nil {
		return nil, fmt.Errorf("discordgo: %w", err)
	}
	dg.Identify.Intents = discordgo.IntentsDirectMessages | discordgo.IntentsGuildMessages
	return &DiscordTransport{bot: bot, session: dg}, nil
}

func (d *DiscordTransport) Name() string { return "dc" }

func (d *DiscordTransport) Send(chatID string, text string) error {
	// Discord has a 2000-character limit per message.
	for _, chunk := range splitMessage(text, 2000) {
		if _, err := d.session.ChannelMessageSend(chatID, chunk); err != nil {
			return fmt.Errorf("discord send: %w", err)
		}
	}
	return nil
}

func (d *DiscordTransport) SendFile(chatID string, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", filePath, err)
	}
	defer f.Close()
	_, err = d.session.ChannelFileSend(chatID, filepath.Base(filePath), f)
	return err
}

func (d *DiscordTransport) SendTyping(chatID string) {
	d.session.ChannelTyping(chatID)
}

// Start registers the message handler and opens the Discord WebSocket connection.
// Blocks until the session is closed.
func (d *DiscordTransport) Start(handler func(IncomingMessage)) error {
	bot := d.bot

	d.session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// Ignore messages from the bot itself.
		if m.Author.ID == s.State.User.ID {
			return
		}

		// Parse Discord user ID (snowflake) as int64 for session lookup.
		userID, err := strconv.ParseInt(m.Author.ID, 10, 64)
		if err != nil {
			log.Printf("discord: cannot parse user ID %q: %v", m.Author.ID, err)
			return
		}

		// Check if this Discord user is in the allowed list.
		if bot.getSession(userID) == nil {
			// Unknown user — notify admin if configured.
			if bot.cfg.Discord.AdminUserID != 0 && userID != bot.cfg.Discord.AdminUserID {
				adminChatID := d.adminChatID()
				if adminChatID != "" {
					msg := fmt.Sprintf("Unknown Discord user: %s#%s (ID: %s) — message: %s",
						m.Author.Username, m.Author.Discriminator, m.Author.ID, m.Content)
					d.Send(adminChatID, msg)
				}
			}
			return
		}

		text := strings.TrimSpace(m.Content)
		if text == "" {
			return
		}

		chatID := makeChatID("dc", m.ChannelID)
		handler(IncomingMessage{
			UserID: userID,
			ChatID: chatID,
			Text:   text,
		})
	})

	if err := d.session.Open(); err != nil {
		return fmt.Errorf("discord open: %w", err)
	}
	log.Printf("discord transport connected")

	// Block until the session closes.
	<-make(chan struct{})
	return nil
}

// adminChatID returns the DM channel ID for the admin user, or "".
func (d *DiscordTransport) adminChatID() string {
	adminID := d.bot.cfg.Discord.AdminUserID
	if adminID == 0 {
		return ""
	}
	ch, err := d.session.UserChannelCreate(strconv.FormatInt(adminID, 10))
	if err != nil {
		return ""
	}
	return ch.ID
}
