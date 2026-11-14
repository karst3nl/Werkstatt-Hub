package tasks

import (
	"testing"
	"time"

	"werkstatt-hub/internal/core"
)

func TestReminderUsesWorkshopCalendarAndInclusiveSoonWindow(t *testing.T) {
	settings := DefaultSettings()
	for _, tc := range []struct{ name, now, due, status, want string }{
		{"overdue", "2026-11-14T12:00:00Z", "2026-11-13", "offen", "ueberfaellig"},
		{"today", "2026-11-14T12:00:00Z", "2026-11-14", "in_arbeit", "heute"},
		{"awaiting approval remains due", "2026-11-14T12:00:00Z", "2026-11-14", "zur_kontrolle", "heute"},
		{"tomorrow", "2026-11-14T12:00:00Z", "2026-11-15", "offen", "bald"},
		{"inclusive third calendar day", "2026-11-14T12:00:00Z", "2026-11-17", "offen", "bald"},
		{"fourth calendar day", "2026-11-14T12:00:00Z", "2026-11-18", "offen", ""},
		{"done", "2026-11-14T12:00:00Z", "2026-11-10", "erledigt", ""},
		{"local midnight today", "2026-11-14T23:30:00Z", "2026-11-15", "offen", "heute"},
		{"local midnight yesterday", "2026-11-14T23:30:00Z", "2026-11-14", "offen", "ueberfaellig"},
		{"spring DST inclusive day", "2026-03-28T11:00:00Z", "2026-03-31", "offen", "bald"},
		{"autumn DST inclusive day", "2026-10-24T10:00:00Z", "2026-10-27", "offen", "bald"},
		{"year boundary", "2026-12-31T12:00:00Z", "2027-01-03", "offen", "bald"},
		{"invalid date", "2026-11-14T12:00:00Z", "2026-02-30", "offen", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.now)
			if err != nil {
				t.Fatal(err)
			}
			task := Task{Entry: core.Entry{Status: tc.status}, DueDate: tc.due}
			if got := settings.Reminder(task, now); got != tc.want {
				t.Fatalf("Reminder = %q, want %q", got, tc.want)
			}
		})
	}
	settings.SoonDays = 1
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	if got := settings.Reminder(Task{Entry: core.Entry{Status: "offen"}, DueDate: "2026-11-16"}, now); got != "" {
		t.Fatalf("custom reminder window ignored: %q", got)
	}
}

func TestValidDueDateRequiresCanonicalExistingDate(t *testing.T) {
	for _, date := range []string{"2026-11-14", "2028-02-29", "0001-01-01"} {
		if !ValidDueDate(date) {
			t.Fatalf("valid date rejected: %q", date)
		}
	}
	for _, date := range []string{"", "2026-2-01", "2026-02-29", "2026-02-30", "2026-13-01", "2026-11-14T12:00:00Z", " 2026-11-14", "0000-01-01"} {
		if ValidDueDate(date) {
			t.Fatalf("invalid date accepted: %q", date)
		}
	}
}

func TestTaskStatusPermissionsAndApprovalBoundary(t *testing.T) {
	employee := core.User{ID: 2, Role: core.RoleEmployee, Active: true}
	supervisor := core.User{ID: 3, Role: core.RoleSupervisor, Active: true}
	admin := core.User{ID: 1, Role: core.RoleAdmin, Active: true}
	peer := core.User{ID: 4, Role: core.RoleEmployee, Active: true}
	task := Task{Entry: core.Entry{Status: "offen"}, AssigneeID: employee.ID}
	for _, u := range []core.User{employee, supervisor, admin} {
		for _, target := range []string{"offen", "in_arbeit", "zur_kontrolle"} {
			if !CanChangeStatus(u, task, target) {
				t.Fatalf("%s cannot use open status %s", u.Role, target)
			}
		}
		if CanChangeStatus(u, task, "erledigt") {
			t.Fatalf("%s skipped the review stage", u.Role)
		}
	}
	for _, u := range []core.User{peer, {}, {ID: 2, Role: core.RoleEmployee, Active: false}, {ID: 2, Role: "unknown", Active: true}} {
		if CanChangeStatus(u, task, "in_arbeit") {
			t.Fatalf("unauthorized actor accepted: %#v", u)
		}
	}
	task.Status = "zur_kontrolle"
	if CanChangeStatus(employee, task, "erledigt") {
		t.Fatal("assignee can approve their own task")
	}
	for _, u := range []core.User{supervisor, admin} {
		if !CanChangeStatus(u, task, "erledigt") {
			t.Fatalf("%s cannot approve review-ready task", u.Role)
		}
	}
	task.Status = "erledigt"
	for _, target := range []string{"offen", "in_arbeit", "zur_kontrolle", "erledigt"} {
		if CanChangeStatus(employee, task, target) {
			t.Fatal("employee can modify an approved task")
		}
		if !CanChangeStatus(supervisor, task, target) {
			t.Fatal("manager cannot reopen or retain an approved task")
		}
	}
	if CanChangeStatus(admin, task, "unknown") {
		t.Fatal("unknown status accepted")
	}
}

func TestReminderSettingsValidation(t *testing.T) {
	if !DefaultSettings().Valid() {
		t.Fatal("default task settings invalid")
	}
	for _, s := range []Settings{{}, {Location: time.UTC, SoonDays: 0}, {Location: time.UTC, SoonDays: 31}, {SoonDays: 3}} {
		if s.Valid() {
			t.Fatalf("invalid settings accepted: %#v", s)
		}
	}
}
