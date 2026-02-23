package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ClaudeProcess wraps a long-running claude subprocess using stream-json mode.
type ClaudeProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner

	mu       sync.Mutex // protects state fields
	alive    bool
	inUse    bool
	lastUsed time.Time

	ioMu sync.Mutex // serializes send operations

	// Config snapshot for drift detection
	model      string
	workingDir string
	workspace  string
}

// streamMessage represents a line of NDJSON output from Claude.
type streamMessage struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`
	Result  string `json:"result,omitempty"`
}

// userMessage is the input format for stream-json stdin.
type userMessage struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// startClaudeProcess spawns a persistent claude subprocess in stream-json mode.
func (b *Bot) startClaudeProcess(sess *Session, systemPrompt, model, workingDir, workspace string) (*ClaudeProcess, error) {
	bin := b.cfg.Backend.Binary

	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--model", model,
		"--system-prompt", systemPrompt,
		"--dangerously-skip-permissions",
		"--allowedTools", "Bash,Read,Write,Edit,Glob,Grep,WebSearch,WebFetch",
	}

	cmd := exec.Command(bin, args...)
	cmd.Dir = workingDir

	// Filter out CLAUDECODE env var
	env := []string{}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "CLAUDECODE=") {
			env = append(env, e)
		}
	}
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	cmd.Stderr = nil // discard stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB buffer

	// Read the init message
	if !scanner.Scan() {
		cmd.Process.Kill()
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read init: %w", err)
		}
		return nil, fmt.Errorf("process exited before init message")
	}

	var init streamMessage
	if err := json.Unmarshal(scanner.Bytes(), &init); err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("parse init: %w", err)
	}
	if init.Type != "system" {
		cmd.Process.Kill()
		return nil, fmt.Errorf("unexpected init type: %s", init.Type)
	}

	proc := &ClaudeProcess{
		cmd:        cmd,
		stdin:      stdin,
		stdout:     scanner,
		alive:      true,
		lastUsed:   time.Now(),
		model:      model,
		workingDir: workingDir,
		workspace:  workspace,
	}

	log.Printf("repl: started claude process (pid=%d, model=%s, workspace=%s)", cmd.Process.Pid, model, workspace)
	return proc, nil
}

// sendMessage writes a user message to the REPL process and reads the result.
func (p *ClaudeProcess) sendMessage(text string) (string, error) {
	p.ioMu.Lock()
	defer p.ioMu.Unlock()

	p.mu.Lock()
	if !p.alive {
		p.mu.Unlock()
		return "", fmt.Errorf("process not alive")
	}
	p.inUse = true
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		p.inUse = false
		p.lastUsed = time.Now()
		p.mu.Unlock()
	}()

	msg := userMessage{Type: "user", Content: text}
	data, err := json.Marshal(msg)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')

	if _, err := p.stdin.Write(data); err != nil {
		p.mu.Lock()
		p.alive = false
		p.mu.Unlock()
		return "", fmt.Errorf("write: %w", err)
	}

	// Read NDJSON lines until we get a result
	for p.stdout.Scan() {
		line := p.stdout.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg streamMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue // skip unparseable lines
		}

		if msg.Type == "result" {
			if msg.Subtype == "error" {
				errMsg := msg.Result
				if errMsg == "" {
					errMsg = "unknown error from REPL"
				}
				return "", fmt.Errorf("%s", errMsg)
			}
			result := strings.TrimSpace(msg.Result)
			if result == "" {
				return "(no output)", nil
			}
			return result, nil
		}
	}

	// Scanner ended — process died
	p.mu.Lock()
	p.alive = false
	p.mu.Unlock()
	if err := p.stdout.Err(); err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	return "", fmt.Errorf("process exited unexpectedly")
}

// kill terminates the REPL process gracefully (SIGINT then SIGKILL after 3s).
func (p *ClaudeProcess) kill() {
	p.mu.Lock()
	if !p.alive {
		p.mu.Unlock()
		return
	}
	p.alive = false
	p.mu.Unlock()

	pid := p.cmd.Process.Pid

	// Close stdin to signal the process
	p.stdin.Close()

	// Try SIGINT first
	p.cmd.Process.Signal(os.Interrupt)

	// Wait up to 3 seconds for graceful exit
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()

	select {
	case <-done:
		log.Printf("repl: process %d exited gracefully", pid)
	case <-time.After(3 * time.Second):
		p.cmd.Process.Kill()
		<-done
		log.Printf("repl: process %d killed after timeout", pid)
	}
}

// getOrStartProcess returns the existing REPL process or starts a new one.
// It detects config drift and restarts if needed.
func (b *Bot) getOrStartProcess(sess *Session) (*ClaudeProcess, error) {
	sess.mu.Lock()
	model := b.activeModelForSession(sess)
	wd := sess.workingDir
	ws := sess.workspace
	proc := sess.proc
	sess.mu.Unlock()

	// Check if existing process is still valid
	if proc != nil {
		proc.mu.Lock()
		alive := proc.alive
		drifted := proc.model != model || proc.workingDir != wd || proc.workspace != ws
		proc.mu.Unlock()

		if alive && !drifted {
			return proc, nil
		}

		// Kill stale or drifted process
		if alive {
			log.Printf("repl: restarting due to config drift (model=%s→%s, ws=%s→%s)", proc.model, model, proc.workspace, ws)
		}
		proc.kill()
	}

	// Build system prompt without history — REPL manages context natively
	systemPrompt := b.buildSystemPrompt(sess.userID, ws, wd)

	newProc, err := b.startClaudeProcess(sess, systemPrompt, model, wd, ws)
	if err != nil {
		return nil, err
	}

	sess.mu.Lock()
	sess.proc = newProc
	sess.mu.Unlock()

	return newProc, nil
}

// runClaudeREPL sends a message through the persistent REPL process.
func (b *Bot) runClaudeREPL(sess *Session, text string) (string, error) {
	proc, err := b.getOrStartProcess(sess)
	if err != nil {
		return "", err
	}

	result, err := proc.sendMessage(text)
	if err != nil {
		// Process is likely dead; clear it so next call starts fresh
		sess.mu.Lock()
		if sess.proc == proc {
			sess.proc = nil
		}
		sess.mu.Unlock()
		return "", err
	}

	return result, nil
}

// killProc kills the session's REPL process if one exists.
func (sess *Session) killProc() {
	sess.mu.Lock()
	proc := sess.proc
	sess.proc = nil
	sess.mu.Unlock()
	if proc != nil {
		proc.kill()
	}
}

// reapIdleProcesses periodically kills REPL processes that have been idle too long.
func (b *Bot) reapIdleProcesses() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		b.sessionsMu.RLock()
		sessions := make([]*Session, 0, len(b.sessions))
		for _, s := range b.sessions {
			sessions = append(sessions, s)
		}
		b.sessionsMu.RUnlock()

		for _, sess := range sessions {
			sess.mu.Lock()
			proc := sess.proc
			sess.mu.Unlock()

			if proc == nil {
				continue
			}

			proc.mu.Lock()
			shouldKill := proc.alive && !proc.inUse && time.Since(proc.lastUsed) > 20*time.Minute
			proc.mu.Unlock()

			if shouldKill {
				log.Printf("repl: reaping idle process for user %d", sess.userID)
				proc.kill()
			}
		}
	}
}

// killAllProcesses kills all active REPL processes (for graceful shutdown).
func (b *Bot) killAllProcesses() {
	b.sessionsMu.RLock()
	defer b.sessionsMu.RUnlock()

	for _, sess := range b.sessions {
		sess.mu.Lock()
		proc := sess.proc
		sess.proc = nil
		sess.mu.Unlock()

		if proc != nil {
			proc.kill()
		}
	}
}
