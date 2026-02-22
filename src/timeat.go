package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseAtTime parses natural time expressions into a cron expression and human description.
// Supported formats:
//   - "tomorrow 18:00"
//   - "today 18:00"
//   - "monday 09:30"
//   - "friday 14:00"
//   - "2026-02-23 09:00"
//   - "in 2h" / "in 30m" / "in 90m"
func parseAtTime(s string) (cronExpr string, desc string, err error) {
	s = strings.TrimSpace(strings.ToLower(s))
	now := time.Now()

	// "in Xh" or "in Xm"
	if strings.HasPrefix(s, "in ") {
		dur, e := parseDuration(strings.TrimPrefix(s, "in "))
		if e != nil {
			return "", "", fmt.Errorf("couldn't parse duration %q", s)
		}
		t := now.Add(dur)
		return timeToCron(t), t.Format("Mon Jan 2 at 15:04"), nil
	}

	// Split into day-part and time-part: "tomorrow 18:00", "monday 09:30", "2026-02-23 09:00"
	parts := strings.Fields(s)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("use format like \"tomorrow 18:00\" or \"in 2h\"")
	}
	dayPart, timePart := parts[0], parts[1]

	hh, mm, e := parseHHMM(timePart)
	if e != nil {
		return "", "", e
	}

	var target time.Time
	switch dayPart {
	case "today":
		target = time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, now.Location())
		if !target.After(now) {
			return "", "", fmt.Errorf("that time has already passed today")
		}
	case "tomorrow":
		d := now.AddDate(0, 0, 1)
		target = time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, now.Location())
	default:
		// Try weekday name
		if wd, ok := weekday(dayPart); ok {
			daysUntil := int(wd) - int(now.Weekday())
			if daysUntil <= 0 {
				daysUntil += 7
			}
			d := now.AddDate(0, 0, daysUntil)
			target = time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, now.Location())
		} else {
			// Try YYYY-MM-DD
			t, e := time.ParseInLocation("2006-01-02", dayPart, now.Location())
			if e != nil {
				return "", "", fmt.Errorf("couldn't parse day %q — use tomorrow, monday, or 2006-01-02", dayPart)
			}
			target = time.Date(t.Year(), t.Month(), t.Day(), hh, mm, 0, 0, now.Location())
			if !target.After(now) {
				return "", "", fmt.Errorf("that date and time is in the past")
			}
		}
	}

	return timeToCron(target), target.Format("Mon Jan 2 at 15:04"), nil
}

// parseRecurring parses natural language recurring schedule expressions into a cron expression.
// If the input already looks like a cron expression (5 fields), it passes through unchanged.
//
// Supported natural language:
//   - "every day 08:00"
//   - "every weekday 08:00"
//   - "every weekend 10:00"
//   - "every monday 09:00"  (any weekday name)
//   - "every hour"
//   - "every 6 hours"
//   - "every 30 minutes"
//   - "every morning"       → 08:00 daily
//   - "every evening"       → 18:00 daily
//   - "every night"         → 22:00 daily
func parseRecurring(s string) (cronExpr string, desc string, err error) {
	s = strings.TrimSpace(s)

	// Pass through raw cron expressions (5 space-separated fields)
	if looksLikeCron(s) {
		return s, s, nil
	}

	lower := strings.ToLower(s)

	// Named times of day (no explicit time given)
	namedTimes := map[string]string{
		"morning": "08:00",
		"noon":    "12:00",
		"evening": "18:00",
		"night":   "22:00",
	}

	// "every hour"
	if lower == "every hour" {
		return "0 * * * *", "every hour", nil
	}

	// "every N hours" / "every N minutes"
	if strings.HasPrefix(lower, "every ") {
		rest := strings.TrimPrefix(lower, "every ")
		fields := strings.Fields(rest)

		if len(fields) == 2 {
			n, nerr := strconv.Atoi(fields[0])
			unit := fields[1]
			if nerr == nil {
				switch {
				case strings.HasPrefix(unit, "hour"):
					return fmt.Sprintf("0 */%d * * *", n), fmt.Sprintf("every %d hours", n), nil
				case strings.HasPrefix(unit, "minute"):
					return fmt.Sprintf("*/%d * * * *", n), fmt.Sprintf("every %d minutes", n), nil
				}
			}
		}

		if len(fields) >= 1 {
			firstWord := fields[0]

			// Named time shortcuts: "every morning", "every evening", etc.
			if t, ok := namedTimes[firstWord]; ok {
				hh, mm, e := parseHHMM(t)
				if e == nil {
					return fmt.Sprintf("%d %d * * *", mm, hh), fmt.Sprintf("every day at %s", t), nil
				}
			}

			// "every day HH:MM"
			if firstWord == "day" && len(fields) == 2 {
				hh, mm, e := parseHHMM(fields[1])
				if e != nil {
					return "", "", e
				}
				return fmt.Sprintf("%d %d * * *", mm, hh), fmt.Sprintf("every day at %s", fields[1]), nil
			}

			// "every weekday HH:MM"
			if firstWord == "weekday" && len(fields) == 2 {
				hh, mm, e := parseHHMM(fields[1])
				if e != nil {
					return "", "", e
				}
				return fmt.Sprintf("%d %d * * 1-5", mm, hh), fmt.Sprintf("weekdays at %s", fields[1]), nil
			}

			// "every weekend HH:MM"
			if firstWord == "weekend" && len(fields) == 2 {
				hh, mm, e := parseHHMM(fields[1])
				if e != nil {
					return "", "", e
				}
				return fmt.Sprintf("%d %d * * 0,6", mm, hh), fmt.Sprintf("weekends at %s", fields[1]), nil
			}

			// "every monday HH:MM" etc.
			if wd, ok := weekday(firstWord); ok {
				if len(fields) == 2 {
					hh, mm, e := parseHHMM(fields[1])
					if e != nil {
						return "", "", e
					}
					return fmt.Sprintf("%d %d * * %d", mm, hh, int(wd)),
						fmt.Sprintf("every %s at %s", strings.Title(firstWord), fields[1]), nil
				}
			}
		}
	}

	return "", "", fmt.Errorf(
		"couldn't parse %q\n\nExamples:\n"+
			"`every day 08:00`\n"+
			"`every weekday 09:00`\n"+
			"`every monday 08:00`\n"+
			"`every weekend 10:00`\n"+
			"`every hour`\n"+
			"`every 6 hours`\n"+
			"`every morning` / `every evening` / `every night`\n"+
			"Or raw cron: `0 8 * * *`", s)
}

// looksLikeCron returns true if s looks like a 5-field cron expression.
func looksLikeCron(s string) bool {
	fields := strings.Fields(s)
	if len(fields) != 5 {
		return false
	}
	for _, f := range fields {
		for _, c := range f {
			if c != '*' && c != '/' && c != ',' && c != '-' && (c < '0' || c > '9') {
				return false
			}
		}
	}
	return true
}

// timeToCron converts a specific time to a cron expression (fires once at that minute).
func timeToCron(t time.Time) string {
	return fmt.Sprintf("%d %d %d %d *", t.Minute(), t.Hour(), t.Day(), int(t.Month()))
}

func parseHHMM(s string) (hh, mm int, err error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("time must be HH:MM, got %q", s)
	}
	hh, err = strconv.Atoi(parts[0])
	if err != nil || hh < 0 || hh > 23 {
		return 0, 0, fmt.Errorf("invalid hour in %q", s)
	}
	mm, err = strconv.Atoi(parts[1])
	if err != nil || mm < 0 || mm > 59 {
		return 0, 0, fmt.Errorf("invalid minute in %q", s)
	}
	return hh, mm, nil
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "h") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "h"))
		if err != nil {
			return 0, err
		}
		return time.Duration(n) * time.Hour, nil
	}
	if strings.HasSuffix(s, "m") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "m"))
		if err != nil {
			return 0, err
		}
		return time.Duration(n) * time.Minute, nil
	}
	return 0, fmt.Errorf("use Xh or Xm (e.g. 2h, 30m)")
}

func weekday(s string) (time.Weekday, bool) {
	days := map[string]time.Weekday{
		"sunday": time.Sunday, "sun": time.Sunday,
		"monday": time.Monday, "mon": time.Monday,
		"tuesday": time.Tuesday, "tue": time.Tuesday,
		"wednesday": time.Wednesday, "wed": time.Wednesday,
		"thursday": time.Thursday, "thu": time.Thursday,
		"friday": time.Friday, "fri": time.Friday,
		"saturday": time.Saturday, "sat": time.Saturday,
	}
	wd, ok := days[s]
	return wd, ok
}
