package activity

import (
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Totals struct {
	Messages  int64 `json:"messages"`
	Sessions  int64 `json:"sessions"`
	ToolCalls int64 `json:"toolCalls"`
	Tokens    int64 `json:"tokens"`
}

func (t *Totals) add(c Counts) {
	t.Messages += c.Messages
	t.Sessions += c.Sessions
	t.ToolCalls += c.ToolCalls
	t.Tokens += c.Tokens()
}

type Day struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

type Share struct {
	Driver   string `json:"driver"`
	Model    string `json:"model,omitempty"`
	Messages int64  `json:"messages"`
	Tokens   int64  `json:"tokens"`
}

type Project struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Messages int64  `json:"messages"`
}

type Overview struct {
	Period                 string    `json:"period"`
	Project                string    `json:"project"`
	Current                Totals    `json:"current"`
	Previous               Totals    `json:"previous"`
	Days                   []Day     `json:"days"`
	YearMessages           int64     `json:"yearMessages"`
	ActiveDays             int       `json:"activeDays"`
	CurrentStreak          int       `json:"currentStreak"`
	LongestStreak          int       `json:"longestStreak"`
	BusiestMonth           string    `json:"busiestMonth"`
	BusiestMonthMessages   int64     `json:"busiestMonthMessages"`
	BusiestWeekday         string    `json:"busiestWeekday"`
	BusiestWeekdayMessages int64     `json:"busiestWeekdayMessages"`
	Providers              []Share   `json:"providers"`
	Models                 []Share   `json:"models"`
	Projects               []Project `json:"projects"`
}

const maxModels = 5

func midnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func windows(today time.Time, period string) (string, string, string) {
	var start, prevStart, prevEnd time.Time
	switch period {
	case "month":
		start = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
		prevStart = start.AddDate(0, -1, 0)
		prevEnd = prevStart.AddDate(0, 0, today.Day()-1)
		if !prevEnd.Before(start) {
			prevEnd = start.AddDate(0, 0, -1)
		}
	case "year":
		start = time.Date(today.Year(), 1, 1, 0, 0, 0, 0, today.Location())
		prevStart = start.AddDate(-1, 0, 0)
		prevEnd = today.AddDate(-1, 0, 0)
	default:
		start = today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
		prevStart = start.AddDate(0, 0, -7)
		prevEnd = today.AddDate(0, 0, -7)
	}
	return start.Format(dayLayout), prevStart.Format(dayLayout), prevEnd.Format(dayLayout)
}

func normalizePeriod(period string) string {
	switch period {
	case "month", "year":
		return period
	}
	return "week"
}

func Build(rows []Row, now time.Time, period, project string) Overview {
	period = normalizePeriod(period)
	today := midnight(now)
	todayKey := today.Format(dayLayout)
	start, prevStart, prevEnd := windows(today, period)
	gridStart := today.AddDate(0, 0, -((int(today.Weekday())+6)%7)-52*7)
	gridKey := gridStart.Format(dayLayout)

	out := Overview{Period: period, Project: project}
	projects := map[string]int64{}
	perDay := map[string]int64{}
	providers := map[string]*Share{}
	models := map[string]*Share{}

	for _, row := range rows {
		if row.Project != "" {
			projects[row.Project] += row.Messages
		}
		if project != "" && !sameProject(row.Project, project) {
			continue
		}
		if row.Day > todayKey {
			continue
		}
		perDay[row.Day] += row.Messages
		if row.Day >= start {
			out.Current.add(row.Counts)
			if row.Driver != "" {
				p := providers[row.Driver]
				if p == nil {
					p = &Share{Driver: row.Driver}
					providers[row.Driver] = p
				}
				p.Messages += row.Messages
				p.Tokens += row.Tokens()
				if row.Model != "" {
					key := row.Driver + "\x00" + row.Model
					m := models[key]
					if m == nil {
						m = &Share{Driver: row.Driver, Model: row.Model}
						models[key] = m
					}
					m.Messages += row.Messages
					m.Tokens += row.Tokens()
				}
			}
		} else if row.Day >= prevStart && row.Day <= prevEnd {
			out.Previous.add(row.Counts)
		}
	}

	months := map[string]int64{}
	weekdays := map[time.Weekday]int64{}
	for d := gridStart; !d.After(today); d = d.AddDate(0, 0, 1) {
		key := d.Format(dayLayout)
		count := perDay[key]
		out.Days = append(out.Days, Day{Date: key, Count: count})
		if count == 0 {
			continue
		}
		out.YearMessages += count
		out.ActiveDays++
		months[key[:7]] += count
		weekdays[d.Weekday()] += count
	}
	for month, count := range months {
		if count > out.BusiestMonthMessages || (count == out.BusiestMonthMessages && month > out.BusiestMonth) {
			out.BusiestMonth, out.BusiestMonthMessages = month, count
		}
	}
	for day := time.Sunday; day <= time.Saturday; day++ {
		if weekdays[day] > out.BusiestWeekdayMessages {
			out.BusiestWeekday, out.BusiestWeekdayMessages = day.String(), weekdays[day]
		}
	}

	out.CurrentStreak, out.LongestStreak = streaks(perDay, today, gridKey)
	out.Providers = rank(providers, 0)
	out.Models = rank(models, maxModels)
	out.Projects = make([]Project, 0, len(projects))
	for path, messages := range projects {
		out.Projects = append(out.Projects, Project{Path: path, Name: projectName(path), Messages: messages})
	}
	sort.Slice(out.Projects, func(i, j int) bool {
		if out.Projects[i].Messages != out.Projects[j].Messages {
			return out.Projects[i].Messages > out.Projects[j].Messages
		}
		return out.Projects[i].Name < out.Projects[j].Name
	})
	return out
}

func streaks(perDay map[string]int64, today time.Time, floor string) (int, int) {
	current := 0
	cursor := today
	if perDay[cursor.Format(dayLayout)] == 0 {
		cursor = cursor.AddDate(0, 0, -1)
	}
	for perDay[cursor.Format(dayLayout)] > 0 {
		current++
		cursor = cursor.AddDate(0, 0, -1)
	}

	earliest := floor
	for day, count := range perDay {
		if count > 0 && day < earliest {
			earliest = day
		}
	}
	first, err := time.ParseInLocation(dayLayout, earliest, today.Location())
	if err != nil {
		return current, current
	}
	longest, run := 0, 0
	for d := first; !d.After(today); d = d.AddDate(0, 0, 1) {
		if perDay[d.Format(dayLayout)] > 0 {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	return current, longest
}

func rank(shares map[string]*Share, limit int) []Share {
	out := make([]Share, 0, len(shares))
	for _, share := range shares {
		if share.Tokens == 0 && share.Messages == 0 {
			continue
		}
		out = append(out, *share)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tokens != out[j].Tokens {
			return out[i].Tokens > out[j].Tokens
		}
		if out[i].Messages != out[j].Messages {
			return out[i].Messages > out[j].Messages
		}
		return out[i].Driver+out[i].Model < out[j].Driver+out[j].Model
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func sameProject(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func projectName(path string) string {
	name := filepath.Base(filepath.Clean(path))
	if name == "." || name == string(filepath.Separator) {
		return path
	}
	return name
}
