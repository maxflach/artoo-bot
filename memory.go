package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type MemoryStore struct {
	db        *sql.DB
	claudeBin string
}

func dbPath() string {
	return filepath.Join(configDir(), "memory", "bot.db")
}

func newMemoryStore(claudeBin string) (*MemoryStore, error) {
	dir := filepath.Join(configDir(), "memory")
	os.MkdirAll(dir, 0755)

	db, err := sql.Open("sqlite", dbPath())
	if err != nil {
		return nil, err
	}

	// Create base tables (without user_id — migrations below will add it).
	// Using IF NOT EXISTS means existing tables are left untouched here.
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS memories (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			workspace  TEXT NOT NULL DEFAULT 'global',
			content    TEXT NOT NULL,
			source     TEXT NOT NULL DEFAULT 'auto',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS workspaces (
			name  TEXT PRIMARY KEY,
			path  TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS files (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			workspace  TEXT NOT NULL DEFAULT 'global',
			filename   TEXT NOT NULL,
			path       TEXT NOT NULL,
			size       INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS schedules (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			name       TEXT NOT NULL,
			schedule   TEXT NOT NULL,
			prompt     TEXT NOT NULL,
			workspace  TEXT NOT NULL DEFAULT 'global',
			enabled    INTEGER NOT NULL DEFAULT 1,
			last_run   DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return nil, err
	}

	// Additive column migrations — errors from "duplicate column name" are expected and ignored.
	for _, m := range []string{
		`ALTER TABLE memories  ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE files     ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE schedules ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE schedules ADD COLUMN chat_id INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE schedules ADD COLUMN one_shot    INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE schedules ADD COLUMN working_dir TEXT    NOT NULL DEFAULT ''`,
	} {
		db.Exec(m)
	}

	// Migrate workspaces table: old schema has `name TEXT PRIMARY KEY` (no user_id).
	// Recreate with user_id if needed.
	if !columnExists(db, "workspaces", "user_id") {
		db.Exec(`CREATE TABLE workspaces_v2 (
			user_id    INTEGER NOT NULL DEFAULT 0,
			name       TEXT NOT NULL,
			path       TEXT NOT NULL DEFAULT '',
			model      TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, name)
		)`)
		db.Exec(`INSERT OR IGNORE INTO workspaces_v2 (user_id, name, path, model, created_at)
			SELECT 0, name, path, model, created_at FROM workspaces`)
		db.Exec(`DROP TABLE workspaces`)
		db.Exec(`ALTER TABLE workspaces_v2 RENAME TO workspaces`)
	}

	// Idempotent indexes (safe to re-run, IF NOT EXISTS guards them)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_memories_user   ON memories(user_id, workspace)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_memories_ts     ON memories(created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_files_user      ON files(user_id, workspace)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_schedules_user  ON schedules(user_id)`)
	db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_workspaces_user_name ON workspaces(user_id, name)`)

	ms := &MemoryStore{db: db, claudeBin: claudeBin}
	ms.initUserStateTable()
	return ms, nil
}

// columnExists reports whether a column exists in a SQLite table.
func columnExists(db *sql.DB, table, column string) bool {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			continue
		}
		if name == column {
			return true
		}
	}
	return false
}

// --- Memories ---

func (m *MemoryStore) load(userID int64, workspace string, maxAgeDays int) string {
	workspaces := []string{"global"}
	if workspace != "global" {
		workspaces = append(workspaces, workspace)
	}
	placeholders := make([]string, len(workspaces))
	args := []interface{}{userID}
	for i, w := range workspaces {
		placeholders[i] = "?"
		args = append(args, w)
	}
	query := fmt.Sprintf(
		"SELECT workspace, content, created_at FROM memories"+
			" WHERE user_id = ? AND workspace IN (%s)"+
			" AND created_at > datetime('now', '-%d days')"+
			" ORDER BY workspace ASC, created_at ASC",
		strings.Join(placeholders, ","), maxAgeDays)

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var global, project []string
	for rows.Next() {
		var ws, content string
		var createdAt time.Time
		if err := rows.Scan(&ws, &content, &createdAt); err != nil {
			continue
		}
		entry := fmt.Sprintf("- %s (%s)", content, formatAge(createdAt))
		if ws == "global" {
			global = append(global, entry)
		} else {
			project = append(project, entry)
		}
	}

	var parts []string
	if len(global) > 0 {
		parts = append(parts, "### Global\n"+strings.Join(global, "\n"))
	}
	if len(project) > 0 {
		parts = append(parts, "### "+workspace+"\n"+strings.Join(project, "\n"))
	}
	if len(parts) == 0 {
		return ""
	}
	return "## Memory\n" + strings.Join(parts, "\n\n")
}

func (m *MemoryStore) save(userID int64, workspace, content, source string) error {
	_, err := m.db.Exec(
		"INSERT INTO memories (user_id, workspace, content, source) VALUES (?, ?, ?, ?)",
		userID, workspace, content, source,
	)
	return err
}

func (m *MemoryStore) list(userID int64, workspace string, maxAgeDays int) string {
	workspaces := []string{"global"}
	if workspace != "global" {
		workspaces = append(workspaces, workspace)
	}
	placeholders := make([]string, len(workspaces))
	args := []interface{}{userID}
	for i, w := range workspaces {
		placeholders[i] = "?"
		args = append(args, w)
	}
	query := fmt.Sprintf(
		"SELECT workspace, content, source, created_at FROM memories"+
			" WHERE user_id = ? AND workspace IN (%s)"+
			" AND created_at > datetime('now', '-%d days')"+
			" ORDER BY created_at DESC LIMIT 50",
		strings.Join(placeholders, ","), maxAgeDays)

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return "Error reading memories."
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var ws, content, source string
		var createdAt time.Time
		if err := rows.Scan(&ws, &content, &source, &createdAt); err != nil {
			continue
		}
		icon := "🤖"
		if source == "manual" {
			icon = "✏️"
		}
		lines = append(lines, fmt.Sprintf("%s [%s · %s] %s", icon, ws, formatAge(createdAt), content))
	}
	if len(lines) == 0 {
		return "No memories yet."
	}
	return strings.Join(lines, "\n")
}

func (m *MemoryStore) extractAndSave(userID int64, workspace, userMsg, assistantReply string) {
	prompt := "Extract factual memories from this conversation worth remembering for future sessions.\n" +
		"Focus on: preferences, decisions, project details, personal facts, patterns.\n" +
		"Each memory should be one concise sentence.\n" +
		"Return ONLY a valid JSON array of strings. If nothing is worth remembering, return [].\n\n" +
		"User: " + userMsg + "\nAssistant: " + assistantReply

	cmd := exec.Command(m.claudeBin, "-p", prompt, "--model", "claude-haiku-4-5")
	env := []string{}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "CLAUDECODE=") {
			env = append(env, e)
		}
	}
	cmd.Env = env

	out, err := cmd.Output()
	if err != nil {
		return
	}
	raw := strings.TrimSpace(string(out))
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start == -1 || end <= start {
		return
	}
	var facts []string
	if err := json.Unmarshal([]byte(raw[start:end+1]), &facts); err != nil {
		return
	}
	for _, fact := range facts {
		if f := strings.TrimSpace(fact); f != "" {
			if err := m.save(userID, workspace, f, "auto"); err == nil {
				log.Printf("memory [user=%d ws=%s]: %s", userID, workspace, f)
			}
		}
	}
}

// --- Workspace model ---

func (m *MemoryStore) getWorkspaceModel(userID int64, workspace string) string {
	var model string
	row := m.db.QueryRow("SELECT model FROM workspaces WHERE user_id = ? AND name = ?", userID, workspace)
	row.Scan(&model)
	return model
}

func (m *MemoryStore) setWorkspaceModel(userID int64, workspace, model string) error {
	_, err := m.db.Exec(
		"INSERT INTO workspaces (user_id, name, path, model) VALUES (?, ?, ?, ?)"+
			" ON CONFLICT(user_id, name) DO UPDATE SET model = excluded.model",
		userID, workspace, workspace, model,
	)
	return err
}

// --- Files ---

func (m *MemoryStore) recordFile(userID int64, workspace, filename, path string, size int64) {
	m.db.Exec(
		"INSERT INTO files (user_id, workspace, filename, path, size) VALUES (?, ?, ?, ?, ?)",
		userID, workspace, filename, path, size,
	)
}

func (m *MemoryStore) listFiles(userID int64, workspace string) string {
	rows, err := m.db.Query(
		"SELECT filename, size, created_at FROM files WHERE user_id = ? AND workspace = ?"+
			" ORDER BY created_at DESC LIMIT 20",
		userID, workspace,
	)
	if err != nil {
		return "Error."
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var filename string
		var size int64
		var createdAt time.Time
		if err := rows.Scan(&filename, &size, &createdAt); err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("📄 %s (%s · %s)", filename, humanSize(size), formatAge(createdAt)))
	}
	if len(lines) == 0 {
		return "No files created yet."
	}
	return strings.Join(lines, "\n")
}

// --- Schedules ---

type Schedule struct {
	ID         int64
	UserID     int64
	ChatID     int64
	Name       string
	Schedule   string
	Prompt     string
	Workspace  string
	WorkingDir string // directory to run the task in
	OneShot    bool
	Enabled    bool
	LastRun    *time.Time
}

func (m *MemoryStore) addSchedule(userID, chatID int64, name, schedule, prompt, workspace, workingDir string, oneShot bool) error {
	oneShotInt := 0
	if oneShot {
		oneShotInt = 1
	}
	_, err := m.db.Exec(
		"INSERT INTO schedules (user_id, chat_id, name, schedule, prompt, workspace, working_dir, one_shot) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		userID, chatID, name, schedule, prompt, workspace, workingDir, oneShotInt,
	)
	return err
}

// listAllSchedules returns all enabled schedules (used by cron runner).
func (m *MemoryStore) listAllSchedules() ([]Schedule, error) {
	rows, err := m.db.Query(
		"SELECT id, user_id, chat_id, name, schedule, prompt, workspace, working_dir, one_shot, enabled, last_run FROM schedules ORDER BY id",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schedules []Schedule
	for rows.Next() {
		var s Schedule
		var lastRun sql.NullTime
		if err := rows.Scan(&s.ID, &s.UserID, &s.ChatID, &s.Name, &s.Schedule, &s.Prompt, &s.Workspace, &s.WorkingDir, &s.OneShot, &s.Enabled, &lastRun); err != nil {
			continue
		}
		if lastRun.Valid {
			s.LastRun = &lastRun.Time
		}
		schedules = append(schedules, s)
	}
	return schedules, nil
}

// listSchedulesForUser returns schedules belonging to a specific user (used by /schedules command).
func (m *MemoryStore) listSchedulesForUser(userID int64) ([]Schedule, error) {
	rows, err := m.db.Query(
		"SELECT id, user_id, chat_id, name, schedule, prompt, workspace, working_dir, one_shot, enabled, last_run FROM schedules WHERE user_id = ? ORDER BY id",
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schedules []Schedule
	for rows.Next() {
		var s Schedule
		var lastRun sql.NullTime
		if err := rows.Scan(&s.ID, &s.UserID, &s.ChatID, &s.Name, &s.Schedule, &s.Prompt, &s.Workspace, &s.WorkingDir, &s.OneShot, &s.Enabled, &lastRun); err != nil {
			continue
		}
		if lastRun.Valid {
			s.LastRun = &lastRun.Time
		}
		schedules = append(schedules, s)
	}
	return schedules, nil
}

func (m *MemoryStore) deleteSchedule(userID, id int64) error {
	_, err := m.db.Exec("DELETE FROM schedules WHERE id = ? AND user_id = ?", id, userID)
	return err
}

func (m *MemoryStore) updateLastRun(id int64) {
	m.db.Exec("UPDATE schedules SET last_run = CURRENT_TIMESTAMP WHERE id = ?", id)
}

// --- User state (active workspace persistence) ---

func (m *MemoryStore) initUserStateTable() {
	m.db.Exec(`CREATE TABLE IF NOT EXISTS user_state (
		user_id     INTEGER PRIMARY KEY,
		workspace   TEXT NOT NULL DEFAULT 'global',
		working_dir TEXT NOT NULL DEFAULT ''
	)`)
	m.db.Exec(`CREATE TABLE IF NOT EXISTS approved_users (
		user_id    INTEGER PRIMARY KEY,
		username   TEXT NOT NULL DEFAULT '',
		name       TEXT NOT NULL DEFAULT '',
		approved_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
}

func (m *MemoryStore) approveUser(userID int64, username, name string) error {
	_, err := m.db.Exec(
		`INSERT INTO approved_users (user_id, username, name) VALUES (?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET username=excluded.username, name=excluded.name`,
		userID, username, name,
	)
	return err
}

func (m *MemoryStore) isApprovedUser(userID int64) bool {
	var count int
	m.db.QueryRow("SELECT COUNT(*) FROM approved_users WHERE user_id = ?", userID).Scan(&count)
	return count > 0
}

func (m *MemoryStore) listApprovedUsers() []struct {
	UserID   int64
	Username string
	Name     string
} {
	rows, err := m.db.Query("SELECT user_id, username, name FROM approved_users ORDER BY approved_at")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var users []struct {
		UserID   int64
		Username string
		Name     string
	}
	for rows.Next() {
		var u struct {
			UserID   int64
			Username string
			Name     string
		}
		rows.Scan(&u.UserID, &u.Username, &u.Name)
		users = append(users, u)
	}
	return users
}

func (m *MemoryStore) removeApprovedUser(userID int64) error {
	_, err := m.db.Exec("DELETE FROM approved_users WHERE user_id = ?", userID)
	return err
}

func (m *MemoryStore) saveUserState(userID int64, workspace, workingDir string) {
	m.db.Exec(
		`INSERT INTO user_state (user_id, workspace, working_dir) VALUES (?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET workspace = excluded.workspace, working_dir = excluded.working_dir`,
		userID, workspace, workingDir,
	)
}

func (m *MemoryStore) loadUserState(userID int64) (workspace, workingDir string) {
	row := m.db.QueryRow("SELECT workspace, working_dir FROM user_state WHERE user_id = ?", userID)
	row.Scan(&workspace, &workingDir)
	return
}

// --- Helpers ---

func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw ago", int(d.Hours()/(24*7)))
	default:
		return t.Format("Jan 2006")
	}
}

func humanSize(b int64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%dB", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(b)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(b)/(1024*1024))
	}
}
