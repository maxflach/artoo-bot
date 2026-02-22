package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Telegram struct {
		Token          string  `yaml:"token"`
		AllowedUserIDs []int64 `yaml:"allowed_user_ids"`
		AdminUserID    int64   `yaml:"admin_user_id"`
	} `yaml:"telegram"`
	Discord struct {
		Token          string  `yaml:"token"`           // Discord bot token (empty = disabled)
		AllowedUserIDs []int64 `yaml:"allowed_user_ids"` // Discord user IDs (snowflakes as int64)
		AdminUserID    int64   `yaml:"admin_user_id"`
	} `yaml:"discord"`
	WebChat struct {
		Enabled bool `yaml:"enabled"` // serve /chat and /chat/sse endpoints
	} `yaml:"webchat"`
	Backend struct {
		Type         string `yaml:"type"`          // "claude-code" or "opencode"
		Binary       string `yaml:"binary"`        // path to the binary
		WorkingDir   string `yaml:"working_dir"`   // base working dir for all users
		DefaultModel string `yaml:"default_model"` // default model name
		ExtractModel string `yaml:"extract_model"` // model used for memory extraction (optional)
	} `yaml:"backend"`
	Persona struct {
		Name         string `yaml:"name"`
		SystemPrompt string `yaml:"system_prompt"`
	} `yaml:"persona"`
	Memory struct {
		MaxAgeDays int `yaml:"max_age_days"`
	} `yaml:"memory"`
	API struct {
		Port int `yaml:"port"` // 0 = disabled
	} `yaml:"api"`
}

// instance is the bot's name (e.g. "rex", "sara") set via --instance flag
var instance = "default"

func configDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "bot", instance)
}

func configPath() string {
	return filepath.Join(configDir(), "config.yaml")
}

func workspaceDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "bot-workspace", instance)
}

func loadConfig() (*Config, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Memory.MaxAgeDays == 0 {
		cfg.Memory.MaxAgeDays = 90
	}
	if cfg.Backend.Type == "" {
		cfg.Backend.Type = "claude-code"
	}
	if cfg.Backend.DefaultModel == "" {
		cfg.Backend.DefaultModel = "claude-sonnet-4-6"
	}
	if cfg.Backend.WorkingDir == "" {
		cfg.Backend.WorkingDir = workspaceDir()
	}
	if cfg.Backend.ExtractModel == "" {
		cfg.Backend.ExtractModel = cfg.Backend.DefaultModel
	}
	return &cfg, nil
}

// ANSI helpers
const (
	bold  = "\033[1m"
	dim   = "\033[2m"
	cyan  = "\033[36m"
	green = "\033[32m"
	reset = "\033[0m"
)

func runOnboarding() {
	reader := bufio.NewReader(os.Stdin)

	ask := func(label, def string) string {
		if def != "" {
			fmt.Printf("  %s%s%s [%s%s%s]: ", bold, label, reset, dim, def, reset)
		} else {
			fmt.Printf("  %s%s%s: ", bold, label, reset)
		}
		val, _ := reader.ReadString('\n')
		val = strings.TrimSpace(val)
		if val == "" {
			return def
		}
		return val
	}

	choose := func(label string, options []string) string {
		fmt.Printf("\n  %s%s%s\n", bold, label, reset)
		for i, o := range options {
			fmt.Printf("  %s[%d]%s %s\n", cyan, i+1, reset, o)
		}
		for {
			fmt.Printf("  Choice [1]: ")
			val, _ := reader.ReadString('\n')
			val = strings.TrimSpace(val)
			if val == "" {
				return options[0]
			}
			for i := range options {
				if val == fmt.Sprintf("%d", i+1) {
					return options[i]
				}
			}
			fmt.Println("  Invalid choice, try again.")
		}
	}

	confirm := func(label string) bool {
		fmt.Printf("  %s [Y/n]: ", label)
		val, _ := reader.ReadString('\n')
		val = strings.ToLower(strings.TrimSpace(val))
		return val == "" || val == "y" || val == "yes"
	}

	section := func(title string) {
		fmt.Printf("\n%s── %s %s─────────────────────────%s\n\n", cyan, title, dim, reset)
	}

	// ── Header ──
	fmt.Printf("\n%s%s", bold, cyan)
	fmt.Println("  ╭─────────────────────────────────╮")
	fmt.Println("  │         Bot Setup Wizard         │")
	fmt.Printf("  ╰─────────────────────────────────╯%s\n", reset)

	// ── Step 1: Backend ──
	section("Step 1 · AI Backend")

	backendChoice := choose("Which backend do you want to use?", []string{
		"Claude Code  (claude CLI — best tool use)",
		"OpenCode     (opencode CLI — open source)",
	})
	backendType := "claude-code"
	if strings.HasPrefix(backendChoice, "OpenCode") {
		backendType = "opencode"
	}

	defaultBinary := "/usr/local/bin/claude"
	if backendType == "opencode" {
		defaultBinary = "/usr/local/bin/opencode"
	}
	// Try to detect the binary in PATH
	for _, candidate := range []string{
		os.ExpandEnv("$HOME/.local/bin/claude"),
		"/usr/local/bin/claude",
		os.ExpandEnv("$HOME/.local/bin/opencode"),
		"/usr/local/bin/opencode",
	} {
		if _, err := os.Stat(candidate); err == nil {
			if (backendType == "claude-code" && strings.Contains(candidate, "claude")) ||
				(backendType == "opencode" && strings.Contains(candidate, "opencode")) {
				defaultBinary = candidate
				break
			}
		}
	}

	fmt.Println()
	binary := ask("Binary path", defaultBinary)

	var defaultModel, defaultExtractModel string
	switch backendType {
	case "opencode":
		defaultModel = "anthropic/claude-sonnet-4-6"
		defaultExtractModel = "anthropic/claude-haiku-4-5"
	default:
		defaultModel = "claude-sonnet-4-6"
		defaultExtractModel = "claude-haiku-4-5"
	}
	model := ask("Default model", defaultModel)
	extractModel := ask("Extract model (memory, cheaper)", defaultExtractModel)

	// ── Step 2: Telegram ──
	section("Step 2 · Telegram")
	fmt.Printf("  %sGet your token from @BotFather → /newbot%s\n\n", dim, reset)

	token := ask("Bot token", "")
	for token == "" {
		fmt.Printf("  %sToken is required.%s\n", dim, reset)
		token = ask("Bot token", "")
	}

	fmt.Printf("\n  %sYour Telegram user ID — message @userinfobot to find it%s\n\n", dim, reset)
	userID := ask("Your Telegram user ID", "")
	for userID == "" {
		fmt.Printf("  %sUser ID is required.%s\n", dim, reset)
		userID = ask("Your Telegram user ID", "")
	}
	adminID := ask("Admin user ID (approves new users)", userID)

	// ── Step 3: Persona ──
	section("Step 3 · Persona")

	name := ask("Bot name", "Artoo")
	hostname, _ := os.Hostname()
	defaultPrompt := fmt.Sprintf(
		"You are %s — a sharp, reliable personal assistant running on %s.\n"+
			"    Be concise and natural. Never use the same greeting twice.\n"+
			"    You have full access to the filesystem and can run commands.\n"+
			"    When asked to do something, just do it. No disclaimers, no fluff.",
		name, hostname)
	fmt.Printf("\n  %sDefault system prompt (press enter to accept):%s\n", dim, reset)
	fmt.Printf("  %s%s%s\n\n", dim, strings.ReplaceAll(defaultPrompt, "\n    ", "\n  "), reset)
	customPrompt := ask("Custom system prompt (or press enter)", "")
	systemPrompt := defaultPrompt
	if customPrompt != "" {
		systemPrompt = customPrompt
	}

	// ── Step 4: Memory ──
	section("Step 4 · Memory")
	maxAge := ask("Memory retention in days", "90")

	// ── Step 5: API ──
	section("Step 5 · REST API")
	fmt.Printf("  %sThe bot can expose an HTTP API so scripts and services on your machine\n  can trigger messages and run tasks. Set port to 0 to disable.%s\n\n", dim, reset)
	apiPort := ask("API port", "8088")

	// ── Summary ──
	section("Summary")
	fmt.Printf("  Backend:      %s%s%s (%s)\n", bold, backendType, reset, binary)
	fmt.Printf("  Model:        %s\n", model)
	fmt.Printf("  Extract:      %s\n", extractModel)
	fmt.Printf("  Telegram:     %s****%s\n", token[:10], token[len(token)-4:])
	fmt.Printf("  User ID:      %s\n", userID)
	fmt.Printf("  Admin ID:     %s\n", adminID)
	fmt.Printf("  Persona:      %s\n", name)
	fmt.Printf("  Memory:       %s days\n", maxAge)
	apiPortLabel := apiPort
	if apiPort == "0" {
		apiPortLabel = "disabled"
	}
	fmt.Printf("  API:          port %s\n", apiPortLabel)
	fmt.Println()

	if !confirm("Save config and set up directories?") {
		fmt.Println("\n  Aborted.")
		return
	}

	ws := workspaceDir()
	yamlContent := fmt.Sprintf(`telegram:
  token: "%s"
  allowed_user_ids:
    - %s
  admin_user_id: %s

backend:
  type: "%s"
  binary: "%s"
  working_dir: "%s"
  default_model: "%s"
  extract_model: "%s"

persona:
  name: "%s"
  system_prompt: |
    %s

memory:
  max_age_days: %s

api:
  port: %s
`, token, userID, adminID, backendType, binary, ws, model, extractModel, name,
		strings.ReplaceAll(systemPrompt, "\n", "\n    "), maxAge, apiPort)

	dir := configDir()
	os.MkdirAll(filepath.Join(dir, "memory"), 0755)
	os.MkdirAll(ws, 0755)

	if err := os.WriteFile(configPath(), []byte(yamlContent), 0600); err != nil {
		fmt.Printf("\n  %sFailed to write config: %v%s\n", dim, err, reset)
		os.Exit(1)
	}

	fmt.Printf("\n  %s✓ Config saved%s → %s\n", green, reset, configPath())
	fmt.Printf("  %s✓ Workspace%s   → %s\n", green, reset, ws)
	fmt.Printf("\n  %sInstall as background service:%s\n", dim, reset)
	fmt.Printf("  bash install.sh %s\n\n", instance)
}
