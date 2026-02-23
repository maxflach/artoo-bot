package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/google/uuid"
)

// runClaudeSession runs Claude with session persistence.
// First call for a session creates it with --session-id; follow-ups use --resume.
// This gives native multi-turn context without re-sending history.
// memUserID is used for memory loading (may differ from sess.userID in shared projects).
func (b *Bot) runClaudeSession(sess *Session, memUserID int64, prompt string) (string, error) {
	sess.mu.Lock()
	sessionID := sess.claudeSessionID
	ws := sess.workspace
	wd := sess.workingDir
	model := b.activeModelForSession(sess)
	sess.mu.Unlock()

	isNew := sessionID == ""
	if isNew {
		sessionID = uuid.New().String()
	}

	systemPrompt := b.buildSystemPrompt(sess.userID, memUserID, ws, wd)

	cmd := b.buildSessionCommand(prompt, model, systemPrompt, sessionID, isNew)
	cmd.Dir = wd

	out, err := cmd.Output()
	if err != nil {
		// On resume failure, clear session and return error for fallback
		if !isNew {
			log.Printf("session: resume failed (session=%s), clearing: %v", sessionID[:8], err)
			sess.mu.Lock()
			sess.claudeSessionID = ""
			sess.mu.Unlock()
		}

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
		return "", fmt.Errorf("%s", detail)
	}

	// Store session ID for follow-ups
	sess.mu.Lock()
	sess.claudeSessionID = sessionID
	sess.mu.Unlock()

	if isNew {
		log.Printf("session: created %s (model=%s, workspace=%s)", sessionID[:8], model, ws)
	}

	result := strings.TrimSpace(string(out))
	if result == "" {
		return "(no output)", nil
	}
	return result, nil
}

// buildSessionCommand builds the exec.Cmd with session support.
func (b *Bot) buildSessionCommand(prompt, model, systemPrompt, sessionID string, isNew bool) *exec.Cmd {
	bin := b.cfg.Backend.Binary

	var args []string
	if isNew {
		args = []string{
			"-p", prompt,
			"--model", model,
			"--system-prompt", systemPrompt,
			"--session-id", sessionID,
			"--dangerously-skip-permissions",
			"--allowedTools", "Bash,Read,Write,Edit,Glob,Grep,WebSearch,WebFetch",
		}
	} else {
		args = []string{
			"-p", prompt,
			"--model", model,
			"--resume", sessionID,
			"--dangerously-skip-permissions",
			"--allowedTools", "Bash,Read,Write,Edit,Glob,Grep,WebSearch,WebFetch",
		}
	}

	cmd := exec.Command(bin, args...)
	env := []string{}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "CLAUDECODE=") {
			env = append(env, e)
		}
	}
	cmd.Env = env
	return cmd
}

// resetSession clears the Claude session ID so the next message starts fresh.
func (sess *Session) resetSession() {
	sess.mu.Lock()
	sess.claudeSessionID = ""
	sess.mu.Unlock()
}
