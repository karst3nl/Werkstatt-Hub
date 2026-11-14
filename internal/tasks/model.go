// Package tasks defines apprentice assignments and calendar-based reminders.
package tasks

import (
	"time"
	_ "time/tzdata"

	"werkstatt-hub/internal/core"
)

type Task struct {
	core.Entry
	AssigneeID   int64     `json:"assignee_id"`
	AssigneeName string    `json:"assignee_name"`
	DueDate      string    `json:"due_date"`
	Approval     *Approval `json:"approval"`
}
type Approval struct {
	UserID    int64          `json:"user_id"`
	Signature core.Signature `json:"signature"`
}
type Input struct {
	Title, Body, DueDate string
	AssigneeID, AuthorID int64
}
type Update struct {
	Title, Body, DueDate string
	AssigneeID, Version  int64
}
type StatusChange struct {
	Status  string
	Version int64
}
type Filter struct {
	AssigneeID int64
	Status     string
}
type Settings struct {
	Location *time.Location
	SoonDays int
}

func DefaultSettings() Settings {
	// tzdata is embedded so this location is available on an offline Pi too.
	loc, _ := time.LoadLocation("Europe/Berlin")
	return Settings{Location: loc, SoonDays: 3}
}
func (s Settings) Valid() bool { return s.Location != nil && s.SoonDays >= 1 && s.SoonDays <= 30 }
func ValidStatus(status string) bool {
	switch status {
	case "offen", "in_arbeit", "zur_kontrolle", "erledigt":
		return true
	}
	return false
}

// ValidDueDate accepts one canonical calendar date, without a time of day.
func ValidDueDate(value string) bool {
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Year() >= 1 && parsed.Format("2006-01-02") == value
}
func (s Settings) Reminder(t Task, now time.Time) string {
	if !s.Valid() || t.Status == "erledigt" || !ValidDueDate(t.DueDate) {
		return ""
	}
	today := now.In(s.Location)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, s.Location)
	date, _ := time.ParseInLocation("2006-01-02", t.DueDate, s.Location)
	if date.Before(today) {
		return "ueberfaellig"
	}
	if date.Equal(today) {
		return "heute"
	}
	if !date.After(today.AddDate(0, 0, s.SoonDays)) {
		return "bald"
	}
	return ""
}
func CanChangeStatus(u core.User, t Task, target string) bool {
	if !u.Active || !u.Role.Valid() || !ValidStatus(t.Status) || !ValidStatus(target) {
		return false
	}
	if core.CanManage(u) {
		return target != "erledigt" || t.Status == "zur_kontrolle" || t.Status == "erledigt"
	}
	return u.Role == core.RoleEmployee && u.ID == t.AssigneeID && t.Status != "erledigt" && target != "erledigt"
}
