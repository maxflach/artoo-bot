package main

import (
	"log"

	"github.com/robfig/cron/v3"
)

type CronRunner struct {
	c   *cron.Cron
	bot *Bot
}

func newCronRunner(bot *Bot) *CronRunner {
	return &CronRunner{
		c:   cron.New(),
		bot: bot,
	}
}

func (cr *CronRunner) start() {
	schedules, err := cr.bot.mem.listAllSchedules()
	if err != nil {
		log.Printf("cron: failed to load schedules: %v", err)
		return
	}

	for _, s := range schedules {
		if !s.Enabled {
			continue
		}
		s := s // capture for closure

		// Look up the session for this user. If the user isn't in the allowed list, skip.
		sess := cr.bot.getSession(s.UserID)
		if sess == nil {
			log.Printf("cron: user %d not in allowed list, skipping schedule %q", s.UserID, s.Name)
			continue
		}

		_, err := cr.c.AddFunc(s.Schedule, func() {
			log.Printf("cron: running schedule %q for user %d in %s", s.Name, s.UserID, s.WorkingDir)
			cr.bot.runScheduledTask(s.ID, s.UserID, s.ChatID, s.Workspace, s.WorkingDir, s.Prompt)
			if s.OneShot {
				cr.bot.mem.deleteSchedule(s.UserID, s.ID)
				go cr.reload() // reload in background so we don't deadlock the cron runner
			}
		})
		if err != nil {
			log.Printf("cron: invalid schedule %q (%s): %v", s.Name, s.Schedule, err)
		} else {
			log.Printf("cron: registered %q (%s) for user %d", s.Name, s.Schedule, s.UserID)
		}
	}

	cr.c.Start()
}

func (cr *CronRunner) stop() {
	cr.c.Stop()
}

func (cr *CronRunner) reload() {
	cr.c.Stop()
	cr.c = cron.New()
	cr.start()
}
