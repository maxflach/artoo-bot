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

func runOnboarding() {
	fmt.Printf("=== Bot Setup: %s ===\n\n", instance)

	reader := bufio.NewReader(os.Stdin)
	ask := func(prompt, def string) string {
		if def != "" {
			fmt.Printf("%s [%s]: ", prompt, def)
		} else {
			fmt.Printf("%s: ", prompt)
		}
		val, _ := reader.ReadString('\n')
		val = strings.TrimSpace(val)
		if val == "" {
			return def
		}
		return val
	}

	token := ask("Telegram bot token", "")
	userID := ask("Your Telegram user ID", "")
	claudeBin := ask("Claude binary path", "/Users/max/.local/bin/claude")
	name := ask("Bot name/persona", strings.Title(instance))
	maxAge := ask("Memory max age in days", "90")

	systemPrompt := fmt.Sprintf(
		"You are %s — a sharp, reliable assistant running on coruscant.\n"+
			"Be concise and natural. Never use the same greeting twice.\n"+
			"You have full access to the filesystem and can run commands.\n"+
			"When asked to do something, just do it. No disclaimers.", name)

	ws := workspaceDir()
	cfg := fmt.Sprintf(`telegram:
  token: "%s"
  allowed_user_ids:
    - %s

claude:
  binary: "%s"
  working_dir: "%s"
  default_model: "claude-sonnet-4-6"

persona:
  name: "%s"
  system_prompt: |
    %s

memory:
  max_age_days: %s
`, token, userID, claudeBin, ws, name,
		strings.ReplaceAll(systemPrompt, "\n", "\n    "), maxAge)

	dir := configDir()
	os.MkdirAll(filepath.Join(dir, "memory"), 0755)
	os.MkdirAll(ws, 0755)

	if err := os.WriteFile(configPath(), []byte(cfg), 0600); err != nil {
		fmt.Printf("Failed to write config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\nConfig saved to %s\n", configPath())
	fmt.Printf("Workspace:   %s\n", ws)
	fmt.Printf("\nInstall as service: bash install.sh %s\n", instance)
}
