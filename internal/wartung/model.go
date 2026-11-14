// Package wartung defines recurring workshop maintenance and calendar-based due dates.
package wartung

import (
	"time"
	_ "time/tzdata"

	"werkstatt-hub/internal/core"
)

const (
	UnitDays   = "tage"
	UnitWeeks  = "wochen"
	UnitMonths = "monate"
	UnitYears  = "jahre"

	StatusOK      = "in_ordnung"
	StatusSoon    = "bald"
	StatusOverdue = "ueberfaellig"

	FindingOK     = "in_ordnung"
	FindingDefect = "mangel"
)

type Object struct {
	ID                  int64          `json:"id"`
	Name                string         `json:"name"`
	Description         string         `json:"description"`
	Category            string         `json:"category"`
	Location            string         `json:"location"`
	IntervalValue       int            `json:"interval_value"`
	IntervalUnit        string         `json:"interval_unit"`
	LastMaintenanceDate string         `json:"last_maintenance_date"`
	WarningDays         *int           `json:"warning_days"`
	Instructions        string         `json:"instructions"`
	Active              bool           `json:"active"`
	AuthorID            int64          `json:"author_id"`
	Signature           core.Signature `json:"signature"`
	UpdatedAt           time.Time      `json:"updated_at"`
	Version             int64          `json:"version"`
}

type Input struct {
	Name, Description, Category, Location string
	IntervalValue                         int
	IntervalUnit                          string
	LastMaintenanceDate                   string
	WarningDays                           *int
	Instructions                          string
}

// Update deliberately excludes the initial maintenance date: dated records are
// appended to history instead of changing an object's original starting point.
type Update struct {
	Name, Description, Category, Location string
	IntervalValue                         int
	IntervalUnit                          string
	WarningDays                           *int
	Instructions                          string
	Active                                bool
	Version                               int64
}

type Record struct {
	ID            int64          `json:"id"`
	ObjectID      int64          `json:"object_id"`
	PerformedDate string         `json:"performed_date"`
	AuthorID      int64          `json:"author_id"`
	Signature     core.Signature `json:"signature"`
	Note          string         `json:"note"`
	Finding       string         `json:"finding"`
	Approval      *Approval      `json:"approval"`
}

type RecordInput struct {
	PerformedDate, Note, Finding string
}

type Approval struct {
	UserID    int64          `json:"user_id"`
	Signature core.Signature `json:"signature"`
}

type Settings struct {
	WarningDays int   `json:"warning_days"`
	Version     int64 `json:"version"`
}

func DefaultSettings() Settings { return Settings{WarningDays: 7, Version: 1} }

type Filter struct {
	Category, Status, Activity string
}

func ValidInterval(value int, unit string) bool {
	if value < 1 || value > 1000 {
		return false
	}
	switch unit {
	case UnitDays, UnitWeeks, UnitMonths, UnitYears:
		return true
	}
	return false
}

func ValidWarningDays(value int) bool { return value >= 0 && value <= 365 }

func ValidFinding(value string) bool {
	return value == "" || value == FindingOK || value == FindingDefect
}

func CanManageObjects(user core.User) bool { return core.CanAdmin(user) }
func CanRecord(user core.User) bool {
	return user.Active && !user.Deleted && user.Role.Valid()
}
func CanApprove(user core.User) bool { return core.CanManage(user) }

// ValidDate accepts one canonical calendar date, with neither a time nor a zone.
func ValidDate(value string) bool {
	date, err := time.Parse(time.DateOnly, value)
	return err == nil && date.Year() >= 1 && date.Year() <= 9999 && date.Format(time.DateOnly) == value
}

// Today uses the configured workshop location, independent of the server's zone.
// A nil location defaults to Europe/Berlin, including on an offline installation.
func Today(now time.Time, location *time.Location) string {
	return now.In(workshopLocation(location)).Format(time.DateOnly)
}

func workshopLocation(location *time.Location) *time.Location {
	if location != nil {
		return location
	}
	location, _ = time.LoadLocation("Europe/Berlin")
	return location
}

// ValidPerformedDate permits historical entries, but no future calendar dates.
func ValidPerformedDate(value string, now time.Time, location *time.Location) bool {
	return ValidDate(value) && value <= Today(now, location)
}

// NextDue adds calendar units to the actual last maintenance date. Month and
// year intervals clamp to the target month's last day, instead of rolling into
// the following month (31 January + one month is 28/29 February).
func NextDue(last string, value int, unit string) (string, error) {
	if !ValidDate(last) || !ValidInterval(value, unit) {
		return "", core.ErrValidation
	}
	date, _ := time.Parse(time.DateOnly, last)
	var next time.Time
	switch unit {
	case UnitDays:
		next = date.AddDate(0, 0, value)
	case UnitWeeks:
		next = date.AddDate(0, 0, value*7)
	case UnitMonths, UnitYears:
		months := value
		if unit == UnitYears {
			months *= 12
		}
		first := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, months, 0)
		lastDay := first.AddDate(0, 1, -1).Day()
		next = time.Date(first.Year(), first.Month(), min(date.Day(), lastDay), 0, 0, 0, 0, time.UTC)
	}
	if next.Year() < 1 || next.Year() > 9999 {
		return "", core.ErrValidation
	}
	return next.Format(time.DateOnly), nil
}

// Status classifies the due date using inclusive calendar-day warning bounds.
// A maintenance due today is "bald" even when the warning period is zero. An
// object's active flag is independent: inactive objects still have a due date,
// but callers exclude them from live reminders. Invalid data has no status.
func Status(object Object, defaultWarning int, now time.Time, location *time.Location) string {
	due, err := NextDue(object.LastMaintenanceDate, object.IntervalValue, object.IntervalUnit)
	if err != nil {
		return ""
	}
	warning := defaultWarning
	if object.WarningDays != nil {
		warning = *object.WarningDays
	}
	if !ValidWarningDays(warning) {
		return ""
	}
	today := Today(now, location)
	if due < today {
		return StatusOverdue
	}
	day, _ := time.Parse(time.DateOnly, today)
	dueDay, _ := time.Parse(time.DateOnly, due)
	if !dueDay.After(day.AddDate(0, 0, warning)) {
		return StatusSoon
	}
	return StatusOK
}
