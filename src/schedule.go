package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// atTimeButtons returns a grid of quick-pick times for the /at wizard.
func atTimeButtons() [][]Button {
	now := time.Now()
	rows := [][]Button{
		{
			{Label: "in 30 min", Data: "attime:in 30m"},
			{Label: "in 1 hour", Data: "attime:in 1h"},
			{Label: "in 2 hours", Data: "attime:in 2h"},
		},
	}
	// Today: only show times that are still in the future.
	todayTimes := []string{"12:00", "18:00", "20:00", "22:00"}
	var todayBtns []Button
	for _, t := range todayTimes {
		hh, mm, err := parseHHMM(t)
		if err != nil {
			continue
		}
		target := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, now.Location())
		if target.After(now) {
			todayBtns = append(todayBtns, Button{Label: "Today " + t, Data: "attime:today " + t})
		}
	}
	if len(todayBtns) > 0 {
		rows = append(rows, todayBtns)
	}
	rows = append(rows,
		[]Button{
			{Label: "Tomorrow 08:00", Data: "attime:tomorrow 08:00"},
			{Label: "Tomorrow 12:00", Data: "attime:tomorrow 12:00"},
		},
		[]Button{
			{Label: "Tomorrow 18:00", Data: "attime:tomorrow 18:00"},
			{Label: "Tomorrow 20:00", Data: "attime:tomorrow 20:00"},
		},
	)
	return rows
}

// recurrenceButtons returns a grid of recurrence options for the /schedule wizard.
func recurrenceButtons() [][]Button {
	return [][]Button{
		{
			{Label: "Every morning", Data: "schedwiz:full:every morning"},
			{Label: "Every noon", Data: "schedwiz:full:every noon"},
			{Label: "Every evening", Data: "schedwiz:full:every evening"},
			{Label: "Every night", Data: "schedwiz:full:every night"},
		},
		{
			{Label: "Daily (pick time)", Data: "schedwiz:base:every day"},
			{Label: "Weekdays (pick time)", Data: "schedwiz:base:every weekday"},
			{Label: "Weekends (pick time)", Data: "schedwiz:base:every weekend"},
		},
		{
			{Label: "Every hour", Data: "schedwiz:full:every hour"},
			{Label: "Every 6h", Data: "schedwiz:full:every 6 hours"},
			{Label: "Every 12h", Data: "schedwiz:full:every 12 hours"},
		},
		{
			{Label: "Mon", Data: "schedwiz:base:every monday"},
			{Label: "Tue", Data: "schedwiz:base:every tuesday"},
			{Label: "Wed", Data: "schedwiz:base:every wednesday"},
			{Label: "Thu", Data: "schedwiz:base:every thursday"},
			{Label: "Fri", Data: "schedwiz:base:every friday"},
			{Label: "Sat", Data: "schedwiz:base:every saturday"},
			{Label: "Sun", Data: "schedwiz:base:every sunday"},
		},
	}
}

// timePickerGrid returns a grid of time options for the schedule wizard's time-selection step.
func timePickerGrid() [][]Button {
	return [][]Button{
		{
			{Label: "06:00", Data: "schedwiz:time:06:00"},
			{Label: "07:00", Data: "schedwiz:time:07:00"},
			{Label: "08:00", Data: "schedwiz:time:08:00"},
			{Label: "09:00", Data: "schedwiz:time:09:00"},
		},
		{
			{Label: "10:00", Data: "schedwiz:time:10:00"},
			{Label: "12:00", Data: "schedwiz:time:12:00"},
			{Label: "14:00", Data: "schedwiz:time:14:00"},
			{Label: "16:00", Data: "schedwiz:time:16:00"},
		},
		{
			{Label: "18:00", Data: "schedwiz:time:18:00"},
			{Label: "20:00", Data: "schedwiz:time:20:00"},
			{Label: "21:00", Data: "schedwiz:time:21:00"},
			{Label: "22:00", Data: "schedwiz:time:22:00"},
		},
	}
}

func (b *Bot) handleScheduleAdd(chatID string, sess *Session, args string) {
	parts := strings.SplitN(args, "|", 3)

	switch len(parts) {
	case 3:
		// Full format: name | when | prompt
		name := strings.TrimSpace(parts[0])
		whenStr := strings.TrimSpace(parts[1])
		prompt := strings.TrimSpace(parts[2])

		schedule, desc, err := parseRecurring(whenStr)
		if err != nil {
			b.reply(chatID, fmt.Sprintf("%v", err))
			return
		}

		sess.mu.Lock()
		ws := sess.workspace
		wd := sess.workingDir
		cid := sess.chatID
		sess.mu.Unlock()

		if err := b.mem.addSchedule(sess.userID, cid, name, schedule, prompt, ws, wd, false); err != nil {
			b.reply(chatID, fmt.Sprintf("Failed to add schedule: %v", err))
			return
		}
		b.cron.reload()
		b.reply(chatID, fmt.Sprintf("Schedule *%s* added — %s ✓", name, desc))

	case 2:
		// Partial: name | when — missing prompt.
		name := strings.TrimSpace(parts[0])
		whenStr := strings.TrimSpace(parts[1])
		cronExpr, desc, err := parseRecurring(whenStr)
		if err != nil {
			b.reply(chatID, fmt.Sprintf("%v", err))
			return
		}
		if _, _, ok := b.richTransport(chatID); ok {
			sess.mu.Lock()
			sess.scheduleWizard = &ScheduleWizard{
				Name:     name,
				CronExpr: cronExpr,
				CronDesc: desc,
				Step:     "awaiting_prompt",
			}
			sess.mu.Unlock()
			b.reply(chatID, fmt.Sprintf("Got it — *%s* · *%s*.\nWhat should I run? Reply with the task prompt:", name, desc))
		} else {
			b.reply(chatID, "Usage: `/schedule <name> | <when> | <prompt>`")
		}

	default:
		// No | separator — start the recurrence-picker wizard.
		if rt, localChatID, ok := b.richTransport(chatID); ok {
			name := strings.TrimSpace(args)
			sess.mu.Lock()
			sess.scheduleWizard = &ScheduleWizard{Name: name, Step: "rec"}
			sess.mu.Unlock()
			rt.SendGrid(localChatID, "Pick a recurrence pattern:", recurrenceButtons())
		} else {
			b.reply(chatID, "Usage: `/schedule <name> | <when> | <prompt>`\n\nExamples:\n`/schedule morning-news | every day 08:00 | Fetch top headlines`\n`/schedule standup | every weekday 09:00 | What should I focus on today?`\n`/schedule weekly | every monday 08:00 | Summarize my week ahead`")
		}
	}
}

func (b *Bot) handleAt(chatID string, sess *Session, args string) {
	if !strings.Contains(args, "|") {
		// No complete "time | prompt" format — show time picker if buttons are available.
		if rt, localChatID, ok := b.richTransport(chatID); ok {
			var pendingPrompt string
			if args != "" {
				// Treat bare args as a pre-supplied prompt.
				pendingPrompt = strings.TrimSpace(args)
			}
			sess.mu.Lock()
			sess.scheduleWizard = &ScheduleWizard{IsAt: true, Prompt: pendingPrompt, Step: "time"}
			sess.mu.Unlock()
			rt.SendGrid(localChatID, "Pick a time for your reminder:", atTimeButtons())
			return
		}
		b.reply(chatID, "Usage: `/at <time> | <prompt>`\n\nExamples:\n`/at tomorrow 18:00 | give me a pasta recipe`\n`/at friday 09:00 | summarize my week`\n`/at in 2h | remind me to take a break`")
		return
	}

	parts := strings.SplitN(args, "|", 2)
	timeStr := strings.TrimSpace(parts[0])
	prompt := strings.TrimSpace(parts[1])

	cronExpr, desc, err := parseAtTime(timeStr)
	if err != nil {
		b.reply(chatID, fmt.Sprintf("Couldn't parse time: %v", err))
		return
	}

	sess.mu.Lock()
	ws := sess.workspace
	wd := sess.workingDir
	cid := sess.chatID
	sess.mu.Unlock()

	name := fmt.Sprintf("at:%s", timeStr)
	if err := b.mem.addSchedule(sess.userID, cid, name, cronExpr, prompt, ws, wd, true); err != nil {
		b.reply(chatID, fmt.Sprintf("Failed to set reminder: %v", err))
		return
	}
	b.cron.reload()
	b.reply(chatID, fmt.Sprintf("Scheduled for *%s* ✓", desc))
}

// handleScheduleWizardPrompt completes a /schedule or /at wizard with the user's task prompt.
func (b *Bot) handleScheduleWizardPrompt(chatID string, sess *Session, prompt string) {
	sess.mu.Lock()
	wiz := sess.scheduleWizard
	sess.scheduleWizard = nil
	ws := sess.workspace
	wd := sess.workingDir
	cid := sess.chatID
	sess.mu.Unlock()

	if wiz == nil || wiz.CronExpr == "" {
		// Wizard state is invalid — fall back to normal message handling.
		b.runUserMessage(chatID, sess, prompt)
		return
	}

	name := wiz.Name
	if name == "" {
		// Auto-derive name from the first 40 chars of the prompt.
		name = strings.ReplaceAll(prompt, "\n", " ")
		if len(name) > 40 {
			name = name[:40]
		}
	}

	if err := b.mem.addSchedule(sess.userID, cid, name, wiz.CronExpr, prompt, ws, wd, wiz.IsAt); err != nil {
		b.reply(chatID, fmt.Sprintf("Failed to save schedule: %v", err))
		return
	}
	b.cron.reload()

	if wiz.IsAt {
		b.reply(chatID, fmt.Sprintf("Scheduled for *%s* ✓", wiz.CronDesc))
	} else {
		b.reply(chatID, fmt.Sprintf("Schedule *%s* added — %s ✓", name, wiz.CronDesc))
	}
}

func (b *Bot) handleScheduleList(chatID string, sess *Session) {
	schedules, err := b.mem.listSchedulesForUser(sess.userID)
	if err != nil || len(schedules) == 0 {
		b.reply(chatID, "No schedules configured.")
		return
	}

	transportName, localChatID := splitChatID(chatID)
	b.transportsMu.RLock()
	tp := b.transports[transportName]
	b.transportsMu.RUnlock()

	for _, s := range schedules {
		status := "✅"
		if !s.Enabled {
			status = "⏸"
		}
		if s.OneShot {
			status = "⏰"
		}
		kind := "recurring"
		if s.OneShot {
			kind = "one-off"
		}
		last := "never"
		if s.LastRun != nil {
			last = formatAge(*s.LastRun)
		}
		project := s.Workspace
		if project == "" {
			project = "global"
		}
		text := fmt.Sprintf(
			"%s *%s* `#%d`\nProject: _%s_ · %s\n`%s` — last: %s\n_%s_",
			status, s.Name, s.ID, project, kind, s.Schedule, last, s.Prompt,
		)

		if rt, ok := tp.(RichTransport); ok {
			rt.SendWithButtons(localChatID, text, []Button{
				{Label: "🗑 Remove", Data: fmt.Sprintf("delschedule:%d", s.ID)},
			})
		} else {
			b.reply(chatID, text+fmt.Sprintf("\n`/unschedule %d` to remove", s.ID))
		}
	}
}

func (b *Bot) handleScheduleDelete(chatID string, sess *Session, arg string) {
	var id int64
	if _, err := fmt.Sscanf(strings.TrimSpace(arg), "%d", &id); err != nil {
		b.reply(chatID, "Usage: `/unschedule <id>` — get IDs with /schedules")
		return
	}
	if err := b.mem.deleteSchedule(sess.userID, id); err != nil {
		b.reply(chatID, fmt.Sprintf("Failed: %v", err))
		return
	}
	b.cron.reload()
	b.reply(chatID, fmt.Sprintf("Schedule %d removed.", id))
}

// runScheduledTask is called by the cron runner.
func (b *Bot) runScheduledTask(id, userID int64, chatID string, workspace, workingDir, prompt string) {
	// Verify the schedule still exists in DB — it may have been deleted
	// by an agent (via SQL) without triggering an in-process cron reload.
	if !b.mem.scheduleExists(id) {
		log.Printf("cron: schedule %d no longer exists, skipping and reloading", id)
		b.cron.reload()
		return
	}

	sess := b.getSession(userID)
	if sess == nil {
		return
	}

	sess.mu.Lock()
	model := b.activeModelForSession(sess)
	hist := make([]Message, len(sess.history))
	copy(hist, sess.history)
	sess.mu.Unlock()

	wd := workingDir
	if wd == "" {
		sess.mu.Lock()
		wd = sess.workingDir
		sess.mu.Unlock()
	}

	before := snapshotFiles(wd)

	response, err := b.runClaude(userID, userID, prompt, workspace, wd, model, hist)
	b.mem.updateLastRun(id)

	if err != nil {
		log.Printf("cron task %d error: %v", id, err)
		b.reply(chatID, fmt.Sprintf("⚠️ Scheduled task failed: %s", err))
		return
	}

	for _, chunk := range splitMessage(response, maxMsgLen) {
		b.reply(chatID, chunk)
	}

	for _, path := range newFiles(wd, before) {
		if filepath.Base(path) == "report.md" {
			userBaseDir := userWorkingDir(b.cfg.Backend.WorkingDir, userID)
			rtmpl, _ := loadReportTemplate(wd, userBaseDir)
			outPath := strings.TrimSuffix(path, ".md") + "_" + time.Now().Format("2006-01-02") + ".pdf"
			if err := RenderMarkdownReport(path, outPath, rtmpl); err != nil {
				b.reply(chatID, fmt.Sprintf("Report render failed: %v", err))
			} else {
				b.sendFile(chatID, outPath)
				if info, err := os.Stat(outPath); err == nil {
					b.mem.recordFile(userID, workspace, filepath.Base(outPath), outPath, info.Size())
				}
				go b.autoEmailReport(userID, workspace, outPath, wd)
			}
			continue
		}
		b.sendFile(chatID, path)
		if info, err := os.Stat(path); err == nil {
			b.mem.recordFile(userID, workspace, filepath.Base(path), path, info.Size())
		}
	}
}
