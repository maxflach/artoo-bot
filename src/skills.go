package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Skill represents a custom /command loaded from a skills directory.
type Skill struct {
	Name        string
	Description string
	Type        string // "script" or "prompt"
	Path        string // absolute path to run.sh / executable / .md file
	Dir         string // working directory when executing the skill
}

// skillPaths returns the ordered list of directories to search for skills.
// Order: global → per-instance → per-project (later entries win on conflict).
func skillPaths(projectDir string) []string {
	home, _ := os.UserHomeDir()
	paths := []string{
		filepath.Join(home, ".config", "bot", "skills"),
		filepath.Join(home, ".config", "bot", instance, "skills"),
	}
	if projectDir != "" {
		paths = append(paths, filepath.Join(projectDir, "skills"))
	}
	return paths
}

// loadSkills scans each path in order, building a name→Skill map.
// Later paths override earlier entries with the same skill name.
func loadSkills(paths []string) map[string]*Skill {
	skills := make(map[string]*Skill)
	for _, dir := range paths {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			entryPath := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				discoverFolderSkill(skills, entry.Name(), entryPath)
			} else {
				discoverFileSkill(skills, entry.Name(), entryPath, dir)
			}
		}
	}
	return skills
}

func discoverFolderSkill(skills map[string]*Skill, name, dir string) {
	for _, candidate := range []string{"run.sh", "run"} {
		path := filepath.Join(dir, candidate)
		if isExecutable(path) {
			skills[name] = &Skill{
				Name:        name,
				Description: readScriptDescription(path),
				Type:        "script",
				Path:        path,
				Dir:         dir,
			}
			return
		}
	}
	promptPath := filepath.Join(dir, "prompt.md")
	if _, err := os.Stat(promptPath); err == nil {
		content, _ := os.ReadFile(promptPath)
		skills[name] = &Skill{
			Name:        name,
			Description: readPromptDescription(string(content)),
			Type:        "prompt",
			Path:        promptPath,
			Dir:         dir,
		}
	}
}

func discoverFileSkill(skills map[string]*Skill, filename, path, dir string) {
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filename, ext)
	if name == "" {
		return
	}
	if ext == ".md" {
		content, _ := os.ReadFile(path)
		skills[name] = &Skill{
			Name:        name,
			Description: readPromptDescription(string(content)),
			Type:        "prompt",
			Path:        path,
			Dir:         dir,
		}
	} else if isExecutable(path) {
		skills[name] = &Skill{
			Name:        name,
			Description: readScriptDescription(path),
			Type:        "script",
			Path:        path,
			Dir:         dir,
		}
	}
}

// dispatchSkill runs a skill and returns the output.
// For prompt-type skills, runFn is called with the assembled prompt.
func (b *Bot) dispatchSkill(skill *Skill, input string, runFn func(string) (string, error)) (string, error) {
	switch skill.Type {
	case "script":
		args := []string{}
		if input != "" {
			args = []string{input}
		}
		cmd := exec.Command(skill.Path, args...)
		cmd.Dir = skill.Dir
		out, err := cmd.Output()
		if err != nil {
			detail := ""
			if exitErr, ok := err.(*exec.ExitError); ok {
				detail = strings.TrimSpace(string(exitErr.Stderr))
			}
			if detail == "" {
				detail = err.Error()
			}
			return "", fmt.Errorf("%s", detail)
		}
		result := strings.TrimSpace(string(out))
		if result == "" {
			return "(no output)", nil
		}
		return result, nil

	case "prompt":
		if runFn == nil {
			return "", fmt.Errorf("no AI runner configured for prompt skill %s", skill.Name)
		}
		template, err := os.ReadFile(skill.Path)
		if err != nil {
			return "", fmt.Errorf("cannot read skill template: %w", err)
		}
		prompt := strings.TrimSpace(string(template))
		if input != "" {
			prompt += "\n\n" + input
		}
		return runFn(prompt)

	default:
		return "", fmt.Errorf("unknown skill type: %s", skill.Type)
	}
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode()&0111 != 0
}

// readScriptDescription reads the first non-shebang comment from a shell script.
func readScriptDescription(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#!") {
			continue
		}
		if strings.HasPrefix(line, "#") {
			return strings.TrimSpace(strings.TrimPrefix(line, "#"))
		}
		break
	}
	return ""
}

// readPromptDescription extracts the first meaningful line from a markdown template.
func readPromptDescription(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "---" {
			continue
		}
		return strings.TrimLeft(line, "# ")
	}
	return ""
}
