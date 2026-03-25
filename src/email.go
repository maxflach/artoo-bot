package main

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log"
	"mime"
	"mime/multipart"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// EmailTemplate controls the visual style of outgoing emails.
type EmailTemplate struct {
	From      EmailFromConfig    `yaml:"from"`
	Subject   EmailSubjectConfig `yaml:"subject"`
	Signature string             `yaml:"signature"`
	HTML      EmailHTMLConfig    `yaml:"html"`
}

type EmailFromConfig struct {
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
}

type EmailSubjectConfig struct {
	Prefix string `yaml:"prefix"`
}

type EmailHTMLConfig struct {
	HeaderColor     string `yaml:"header_color"`
	AccentColor     string `yaml:"accent_color"`
	BackgroundColor string `yaml:"background_color"`
	FontFamily      string `yaml:"font_family"`
}

func defaultEmailTemplate() *EmailTemplate {
	return &EmailTemplate{
		From: EmailFromConfig{
			Name: "Artoo",
		},
		Subject: EmailSubjectConfig{
			Prefix: "[Artoo] ",
		},
		Signature: "--\nSent by Artoo",
		HTML: EmailHTMLConfig{
			HeaderColor:     "#2d3561",
			AccentColor:     "#4a9eff",
			BackgroundColor: "#ffffff",
			FontFamily:      "Helvetica, Arial, sans-serif",
		},
	}
}

// loadEmailTemplate resolves the email template in priority order:
//  1. projectDir/email-template.yaml
//  2. userBaseDir/email-template.yaml
//  3. ~/.config/bot/email-template/email-template.yaml
//
// Falls back to a hardcoded default if none found.
func loadEmailTemplate(projectDir, userBaseDir string) *EmailTemplate {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(projectDir, "email-template.yaml"),
		filepath.Join(userBaseDir, "email-template.yaml"),
		filepath.Join(home, ".config", "bot", "email-template", "email-template.yaml"),
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var tmpl EmailTemplate
		if err := yaml.Unmarshal(data, &tmpl); err != nil {
			continue
		}
		return &tmpl
	}
	return defaultEmailTemplate()
}

// smtpCredentials retrieves SMTP_USERNAME and SMTP_PASSWORD from the secrets store.
func smtpCredentials(b *Bot, userID int64, workspace string) (username, password string, err error) {
	secrets, err := b.mem.getSecretsForSkill(userID, workspace, "email")
	if err != nil {
		return "", "", fmt.Errorf("failed to load email secrets: %w", err)
	}

	encUser, ok := secrets["SMTP_USERNAME"]
	if !ok {
		return "", "", fmt.Errorf("SMTP_USERNAME not configured — run `/email setup`")
	}
	encPass, ok := secrets["SMTP_PASSWORD"]
	if !ok {
		return "", "", fmt.Errorf("SMTP_PASSWORD not configured — run `/email setup`")
	}

	username, err = decryptSecret(b.secretKey, encUser)
	if err != nil {
		return "", "", fmt.Errorf("decrypt SMTP_USERNAME: %w", err)
	}
	password, err = decryptSecret(b.secretKey, encPass)
	if err != nil {
		return "", "", fmt.Errorf("decrypt SMTP_PASSWORD: %w", err)
	}
	return username, password, nil
}

// smtpHost returns the SMTP host and port from config, with Gmail defaults.
func smtpHost(cfg *Config) (host string, port int, tlsMode string) {
	host = cfg.Email.SMTP.Host
	port = cfg.Email.SMTP.Port
	tlsMode = cfg.Email.SMTP.TLS

	if cfg.Email.Provider == "gmail" {
		if host == "" {
			host = "smtp.gmail.com"
		}
		if port == 0 {
			port = 587
		}
	}
	if port == 0 {
		port = 587
	}
	if tlsMode == "" {
		tlsMode = "starttls"
	}
	return
}

// sendEmailMessage sends an email via SMTP.
func sendEmailMessage(b *Bot, userID int64, workspace, to, subject, textBody string, attachments []string, projectDir, userBaseDir string) error {
	if b.cfg.Email.Provider == "" {
		return fmt.Errorf("email not configured — run `/email setup`")
	}

	username, password, err := smtpCredentials(b, userID, workspace)
	if err != nil {
		return err
	}

	tmpl := loadEmailTemplate(projectDir, userBaseDir)

	fromEmail := tmpl.From.Email
	if fromEmail == "" {
		fromEmail = username
	}
	fromName := tmpl.From.Name
	subjectLine := tmpl.Subject.Prefix + subject

	// Append signature to text body.
	fullText := textBody
	if tmpl.Signature != "" {
		fullText += "\n\n" + tmpl.Signature
	}

	htmlBody := textToHTML(fullText, tmpl)

	msg, err := buildMIME(fromName, fromEmail, to, subjectLine, htmlBody, fullText, attachments)
	if err != nil {
		return fmt.Errorf("build MIME: %w", err)
	}

	host, port, tlsMode := smtpHost(b.cfg)
	addr := fmt.Sprintf("%s:%d", host, port)
	auth := smtp.PlainAuth("", username, password, host)

	if tlsMode == "tls" {
		// Implicit TLS (port 465).
		return sendSMTPImplicitTLS(addr, host, auth, fromEmail, to, msg)
	}
	// STARTTLS (port 587).
	return smtp.SendMail(addr, auth, fromEmail, []string{to}, msg)
}

// sendSMTPImplicitTLS connects to an SMTP server using implicit TLS (port 465).
func sendSMTPImplicitTLS(addr, host string, auth smtp.Auth, from, to string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return fmt.Errorf("tls dial: %w", err)
	}
	defer conn.Close()

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Close()

	if err := c.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// buildMIME constructs a multipart MIME message with text + HTML + optional attachments.
func buildMIME(fromName, fromEmail, to, subject, htmlBody, textBody string, attachments []string) ([]byte, error) {
	var buf bytes.Buffer

	from := fromEmail
	if fromName != "" {
		from = fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("utf-8", fromName), fromEmail)
	}

	mainWriter := multipart.NewWriter(&buf)
	boundary := mainWriter.Boundary()

	// Headers
	buf.Reset()
	fmt.Fprintf(&buf, "From: %s\r\n", from)
	fmt.Fprintf(&buf, "To: %s\r\n", to)
	fmt.Fprintf(&buf, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")

	hasAttachments := len(attachments) > 0

	if hasAttachments {
		fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%q\r\n", boundary)
		fmt.Fprintf(&buf, "\r\n")

		// Alternative part (text + html)
		altWriter := multipart.NewWriter(nil)
		altBoundary := altWriter.Boundary()

		altHeader := make(textproto.MIMEHeader)
		altHeader.Set("Content-Type", fmt.Sprintf("multipart/alternative; boundary=%q", altBoundary))
		part, err := mainWriter.CreatePart(altHeader)
		if err != nil {
			return nil, err
		}

		// Text part
		fmt.Fprintf(part, "\r\n--%s\r\n", altBoundary)
		fmt.Fprintf(part, "Content-Type: text/plain; charset=utf-8\r\n")
		fmt.Fprintf(part, "Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		fmt.Fprintf(part, "%s\r\n", textBody)

		// HTML part
		fmt.Fprintf(part, "\r\n--%s\r\n", altBoundary)
		fmt.Fprintf(part, "Content-Type: text/html; charset=utf-8\r\n")
		fmt.Fprintf(part, "Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		fmt.Fprintf(part, "%s\r\n", htmlBody)
		fmt.Fprintf(part, "\r\n--%s--\r\n", altBoundary)

		// Attachments
		for _, path := range attachments {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			attHeader := make(textproto.MIMEHeader)
			attHeader.Set("Content-Type", "application/octet-stream")
			attHeader.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(path)))
			attHeader.Set("Content-Transfer-Encoding", "base64")
			att, err := mainWriter.CreatePart(attHeader)
			if err != nil {
				continue
			}
			encoded := base64.StdEncoding.EncodeToString(data)
			// Wrap at 76 chars
			for i := 0; i < len(encoded); i += 76 {
				end := i + 76
				if end > len(encoded) {
					end = len(encoded)
				}
				fmt.Fprintf(att, "%s\r\n", encoded[i:end])
			}
		}
		mainWriter.Close()
	} else {
		// No attachments — simple multipart/alternative
		altBoundary := mainWriter.Boundary()
		fmt.Fprintf(&buf, "Content-Type: multipart/alternative; boundary=%q\r\n", altBoundary)
		fmt.Fprintf(&buf, "\r\n")

		// Text part
		textHeader := make(textproto.MIMEHeader)
		textHeader.Set("Content-Type", "text/plain; charset=utf-8")
		tw, _ := mainWriter.CreatePart(textHeader)
		fmt.Fprintf(tw, "%s", textBody)

		// HTML part
		htmlHeader := make(textproto.MIMEHeader)
		htmlHeader.Set("Content-Type", "text/html; charset=utf-8")
		hw, _ := mainWriter.CreatePart(htmlHeader)
		fmt.Fprintf(hw, "%s", htmlBody)

		mainWriter.Close()
	}

	return buf.Bytes(), nil
}

// textToHTML wraps plain text in a styled HTML email template.
func textToHTML(text string, tmpl *EmailTemplate) string {
	// Escape HTML entities
	escaped := strings.ReplaceAll(text, "&", "&amp;")
	escaped = strings.ReplaceAll(escaped, "<", "&lt;")
	escaped = strings.ReplaceAll(escaped, ">", "&gt;")
	// Convert newlines to <br>
	escaped = strings.ReplaceAll(escaped, "\n", "<br>\n")

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"></head>
<body style="margin:0;padding:0;background-color:%s;font-family:%s;">
<table width="100%%" cellpadding="0" cellspacing="0" style="max-width:600px;margin:0 auto;padding:20px;">
<tr><td style="background-color:%s;padding:16px 20px;border-radius:4px 4px 0 0;">
<span style="color:#ffffff;font-size:14px;font-weight:bold;">%s</span>
</td></tr>
<tr><td style="padding:20px;background-color:#ffffff;border:1px solid #e0e0e0;border-radius:0 0 4px 4px;">
<div style="color:#333333;font-size:14px;line-height:1.6;">%s</div>
</td></tr>
</table>
</body>
</html>`,
		tmpl.HTML.BackgroundColor,
		tmpl.HTML.FontFamily,
		tmpl.HTML.HeaderColor,
		tmpl.From.Name,
		escaped,
	)
}

// sendReportEmail sends a PDF report as an email attachment.
func sendReportEmail(b *Bot, userID int64, workspace, to, subject, pdfPath, projectDir, userBaseDir string) error {
	body := "Please find the attached report."
	return sendEmailMessage(b, userID, workspace, to, subject, body, []string{pdfPath}, projectDir, userBaseDir)
}

// handleEmailCommand handles /email subcommands.
func (b *Bot) handleEmailCommand(chatID string, sess *Session, args string) {
	parts := strings.SplitN(args, " ", 2)
	sub := parts[0]
	rest := ""
	if len(parts) > 1 {
		rest = strings.TrimSpace(parts[1])
	}

	switch sub {
	case "", "status":
		b.emailStatus(chatID, sess)

	case "setup":
		b.emailSetup(chatID)

	case "test":
		to := rest
		if to == "" {
			to = b.cfg.Email.DefaultRecipient
		}
		if to == "" {
			b.reply(chatID, "Usage: `/email test <to>`")
			return
		}
		b.reply(chatID, "_Sending test email..._")
		sess.mu.Lock()
		ws := sess.workspace
		wd := sess.workingDir
		sess.mu.Unlock()
		userBaseDir := userWorkingDir(b.cfg.Backend.WorkingDir, sess.userID)
		err := sendEmailMessage(b, sess.userID, ws, to, "Test email", "This is a test email from Artoo.", nil, wd, userBaseDir)
		if err != nil {
			b.reply(chatID, fmt.Sprintf("Failed: %v", err))
			return
		}
		b.reply(chatID, fmt.Sprintf("Test email sent to `%s` ✓", to))

	case "send":
		// /email send to | subject | body
		sendParts := strings.SplitN(rest, "|", 3)
		if len(sendParts) < 3 {
			b.reply(chatID, "Usage: `/email send <to> | <subject> | <body>`")
			return
		}
		to := strings.TrimSpace(sendParts[0])
		subject := strings.TrimSpace(sendParts[1])
		body := strings.TrimSpace(sendParts[2])

		b.reply(chatID, "_Sending..._")
		sess.mu.Lock()
		ws := sess.workspace
		wd := sess.workingDir
		sess.mu.Unlock()
		userBaseDir := userWorkingDir(b.cfg.Backend.WorkingDir, sess.userID)
		err := sendEmailMessage(b, sess.userID, ws, to, subject, body, nil, wd, userBaseDir)
		if err != nil {
			b.reply(chatID, fmt.Sprintf("Failed: %v", err))
			return
		}
		b.reply(chatID, fmt.Sprintf("Email sent to `%s` ✓", to))

	case "report":
		to := rest
		if to == "" {
			to = b.cfg.Email.DefaultRecipient
		}
		if to == "" {
			b.reply(chatID, "Usage: `/email report <to>` or set `email.default_recipient` in config")
			return
		}
		sess.mu.Lock()
		wd := sess.workingDir
		ws := sess.workspace
		sess.mu.Unlock()

		reportPath := filepath.Join(wd, "report.md")
		if _, err := os.Stat(reportPath); os.IsNotExist(err) {
			b.reply(chatID, "No `report.md` found in the current project.")
			return
		}

		userBaseDir := userWorkingDir(b.cfg.Backend.WorkingDir, sess.userID)
		tmpl, _ := loadReportTemplate(wd, userBaseDir)
		outPath := strings.TrimSuffix(reportPath, ".md") + "_email.pdf"
		if err := RenderMarkdownReport(reportPath, outPath, tmpl); err != nil {
			b.reply(chatID, fmt.Sprintf("Report render failed: %v", err))
			return
		}

		b.reply(chatID, "_Emailing report..._")
		subject := "Report: " + ws
		err := sendReportEmail(b, sess.userID, ws, to, subject, outPath, wd, userBaseDir)
		if err != nil {
			b.reply(chatID, fmt.Sprintf("Failed: %v", err))
			return
		}
		b.reply(chatID, fmt.Sprintf("Report emailed to `%s` ✓", to))

	default:
		b.reply(chatID, "Usage:\n"+
			"`/email` — show status\n"+
			"`/email setup` — setup instructions\n"+
			"`/email test [to]` — send a test email\n"+
			"`/email send <to> | <subject> | <body>` — send an email\n"+
			"`/email report [to]` — email current report as PDF")
	}
}

func (b *Bot) emailStatus(chatID string, sess *Session) {
	if b.cfg.Email.Provider == "" {
		b.reply(chatID, "Email not configured.\n\nRun `/email setup` to get started.")
		return
	}

	sess.mu.Lock()
	ws := sess.workspace
	sess.mu.Unlock()

	username, _, err := smtpCredentials(b, sess.userID, ws)
	status := "configured"
	if err != nil {
		status = "missing credentials"
	}

	host, port, tlsMode := smtpHost(b.cfg)

	var lines []string
	lines = append(lines, "*Email Status*")
	lines = append(lines, fmt.Sprintf("Provider: `%s`", b.cfg.Email.Provider))
	lines = append(lines, fmt.Sprintf("SMTP: `%s:%d` (%s)", host, port, tlsMode))
	lines = append(lines, fmt.Sprintf("Auth: %s", status))
	if username != "" {
		lines = append(lines, fmt.Sprintf("Username: `%s`", username))
	}
	if b.cfg.Email.DefaultRecipient != "" {
		lines = append(lines, fmt.Sprintf("Default recipient: `%s`", b.cfg.Email.DefaultRecipient))
	}
	lines = append(lines, fmt.Sprintf("Auto-send reports: `%v`", b.cfg.Email.AutoSendReports))
	b.reply(chatID, strings.Join(lines, "\n"))
}

func (b *Bot) emailSetup(chatID string) {
	b.reply(chatID, "*Email Setup*\n\n"+
		"*1. Add to config.yaml:*\n"+
		"```\nemail:\n  provider: gmail\n  default_recipient: you@example.com\n  auto_send_reports: false\n```\n\n"+
		"For non-Gmail SMTP:\n"+
		"```\nemail:\n  provider: smtp\n  smtp:\n    host: mail.example.com\n    port: 587\n    tls: starttls\n```\n\n"+
		"*2. Store credentials:*\n"+
		"`/secret set --global SMTP_USERNAME your@gmail.com --skill email`\n"+
		"`/secret set --global SMTP_PASSWORD <app-password> --skill email`\n\n"+
		"For Gmail: generate an App Password at\n"+
		"_Google Account → Security → 2-Step Verification → App Passwords_\n\n"+
		"*3. Test:*\n"+
		"`/email test you@example.com`")
}

// autoEmailReport sends a report PDF via email in the background, if configured.
// Called from report.md auto-detection hooks.
func (b *Bot) autoEmailReport(userID int64, workspace, pdfPath, projectDir string) {
	if !b.cfg.Email.AutoSendReports || b.cfg.Email.Provider == "" {
		return
	}

	to := b.cfg.Email.DefaultRecipient

	// Check for project-level email template that overrides the recipient.
	tmpl := loadEmailTemplate(projectDir, userWorkingDir(b.cfg.Backend.WorkingDir, userID))
	if tmpl.From.Email != "" && to == "" {
		// No default recipient and template doesn't set one — skip.
		return
	}
	if to == "" {
		return
	}

	userBaseDir := userWorkingDir(b.cfg.Backend.WorkingDir, userID)
	subject := "Report: " + filepath.Base(projectDir)
	if err := sendReportEmail(b, userID, workspace, to, subject, pdfPath, projectDir, userBaseDir); err != nil {
		log.Printf("email: auto-send report failed: %v", err)
	} else {
		log.Printf("email: report sent to %s", to)
	}
}

