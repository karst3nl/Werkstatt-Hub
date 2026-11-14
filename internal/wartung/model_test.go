package wartung

import (
	"errors"
	"testing"
	"time"

	"werkstatt-hub/internal/core"
)

func TestNextDueCalendarIntervals(t *testing.T) {
	for _, tc := range []struct {
		name, last, unit, want string
		value                  int
	}{
		{"day across month", "2026-01-31", UnitDays, "2026-02-01", 1},
		{"week across year", "2026-12-28", UnitWeeks, "2027-01-04", 1},
		{"multiple weeks", "2026-11-01", UnitWeeks, "2026-11-22", 3},
		{"month clamps February", "2026-01-31", UnitMonths, "2026-02-28", 1},
		{"month clamps leap February", "2024-01-31", UnitMonths, "2024-02-29", 1},
		{"multiple months from actual date", "2026-01-31", UnitMonths, "2026-03-31", 2},
		{"month crosses year", "2026-12-31", UnitMonths, "2027-02-28", 2},
		{"month from November", "2026-11-30", UnitMonths, "2026-12-30", 1},
		{"year clamps leap day", "2024-02-29", UnitYears, "2025-02-28", 1},
		{"four years preserves leap day", "2024-02-29", UnitYears, "2028-02-29", 4},
		{"century not leap", "2096-02-29", UnitYears, "2100-02-28", 4},
		{"400 year leap", "1996-02-29", UnitYears, "2000-02-29", 4},
		{"spring daylight saving", "2026-03-28", UnitDays, "2026-03-30", 2},
		{"autumn daylight saving", "2026-10-24", UnitDays, "2026-10-26", 2},
		{"maximum interval", "2026-01-01", UnitYears, "3026-01-01", 1000},
		{"first calendar year", "0001-01-01", UnitDays, "0001-01-02", 1},
		{"last accepted calendar date", "9999-12-30", UnitDays, "9999-12-31", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NextDue(tc.last, tc.value, tc.unit)
			if err != nil || got != tc.want {
				t.Fatalf("NextDue(%q,%d,%q) = %q, %v; want %q", tc.last, tc.value, tc.unit, got, err, tc.want)
			}
		})
	}
}

func TestNextDueRejectsInvalidInputs(t *testing.T) {
	for _, tc := range []struct {
		last, unit string
		value      int
	}{
		{"2026-02-29", UnitMonths, 1}, {"2026-01-01 ", UnitDays, 1}, {"2026-1-01", UnitDays, 1},
		{"0000-01-01", UnitDays, 1}, {"10000-01-01", UnitDays, 1}, {"", UnitDays, 1},
		{"2026-01-01T00:00:00Z", UnitDays, 1}, {"2026-01-01", "month", 1},
		{"2026-01-01", UnitDays, 0}, {"2026-01-01", UnitDays, -1}, {"2026-01-01", UnitDays, 1001},
		{"9999-12-31", UnitDays, 1}, {"9999-12-31", UnitWeeks, 1},
		{"9999-12-31", UnitMonths, 1}, {"9999-12-31", UnitYears, 1},
	} {
		if got, err := NextDue(tc.last, tc.value, tc.unit); !errors.Is(err, core.ErrValidation) || got != "" {
			t.Errorf("NextDue(%q,%d,%q) = %q,%v; want validation failure", tc.last, tc.value, tc.unit, got, err)
		}
	}
}

func TestStatusUsesInclusiveWarningAndCalendarDays(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	object := Object{LastMaintenanceDate: "2026-03-24", IntervalValue: 1, IntervalUnit: UnitWeeks, Active: true}
	for _, tc := range []struct {
		name, now, want string
		warning         int
	}{
		{"before warning", "2026-03-23T12:00:00Z", StatusOK, 7},
		{"first warning day", "2026-03-24T12:00:00Z", StatusSoon, 7},
		{"warning crosses spring DST", "2026-03-27T12:00:00Z", StatusSoon, 4},
		{"due today", "2026-03-31T12:00:00Z", StatusSoon, 0},
		{"not overdue at end of day", "2026-03-31T21:59:59Z", StatusSoon, 7},
		{"overdue at local midnight", "2026-03-31T22:00:00Z", StatusOverdue, 7},
		{"zero warning before due", "2026-03-30T12:00:00Z", StatusOK, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.now)
			if err != nil {
				t.Fatal(err)
			}
			if got := Status(object, tc.warning, now, berlin); got != tc.want {
				t.Errorf("Status = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStatusWarningOverrideAndInvalidData(t *testing.T) {
	now := time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)
	object := Object{LastMaintenanceDate: "2026-11-01", IntervalValue: 10, IntervalUnit: UnitDays}
	if got := Status(object, 7, now, nil); got != StatusOK {
		t.Fatalf("global warning status = %q", got)
	}
	object.WarningDays = new(10)
	if got := Status(object, 7, now, nil); got != StatusSoon {
		t.Fatalf("object override status = %q", got)
	}
	object.WarningDays = new(0)
	if got := Status(object, 365, now, nil); got != StatusOK {
		t.Fatalf("zero-day override ignored: %q", got)
	}
	object.WarningDays = new(-1)
	if got := Status(object, 7, now, nil); got != "" {
		t.Fatalf("invalid object warning status = %q", got)
	}
	object.WarningDays = nil
	if got := Status(object, 366, now, nil); got != "" {
		t.Fatalf("invalid default warning status = %q", got)
	}
	object.LastMaintenanceDate = "2026-02-30"
	if got := Status(object, 7, now, nil); got != "" {
		t.Fatalf("invalid date status = %q", got)
	}
}

func TestWarningBoundaryAcrossAutumnDSTAndYearLimit(t *testing.T) {
	for _, tc := range []struct {
		name, last, instant string
		warning             int
	}{
		{"autumn DST", "2026-10-31", "2026-10-25T00:30:00Z", 7},
		{"upper year limit", "9999-12-30", "9999-12-30T12:00:00Z", 365},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, _ := time.Parse(time.RFC3339, tc.instant)
			object := Object{LastMaintenanceDate: tc.last, IntervalValue: 1, IntervalUnit: UnitDays}
			if got := Status(object, tc.warning, now, nil); got != StatusSoon {
				t.Fatalf("warning boundary = %q, want %q", got, StatusSoon)
			}
		})
	}
}

func TestTodayAndPerformedDateRespectWorkshopTimezone(t *testing.T) {
	for _, tc := range []struct{ instant, today string }{
		{"2026-01-01T22:59:59Z", "2026-01-01"},
		{"2026-01-01T23:00:00Z", "2026-01-02"},
		{"2026-07-01T22:00:00Z", "2026-07-02"},
		{"2026-03-29T00:30:00Z", "2026-03-29"},
		{"2026-10-25T01:30:00Z", "2026-10-25"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.instant)
		if got := Today(now, nil); got != tc.today {
			t.Errorf("Today(%s) = %q, want %q", tc.instant, got, tc.today)
		}
		if !ValidPerformedDate(tc.today, now, nil) || !ValidPerformedDate("2020-01-01", now, nil) || ValidPerformedDate("2027-01-01", now, nil) {
			t.Errorf("performed date validation failed at %s", tc.instant)
		}
	}
	now := time.Date(2026, 1, 1, 23, 30, 0, 0, time.UTC)
	if Today(now, time.UTC) != "2026-01-01" || ValidPerformedDate("2026-01-02", now, time.UTC) {
		t.Fatal("explicit configured timezone ignored")
	}
	for _, invalid := range []string{"", "2026-02-29", "2026-01-01 ", "0000-01-01", "2026-01-01T00:00:00Z"} {
		if ValidPerformedDate(invalid, now, nil) {
			t.Errorf("invalid performed date accepted: %q", invalid)
		}
	}
}

func TestMaintenanceRolePolicy(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		user                    core.User
		manage, record, approve bool
	}{
		{"employee", core.User{Role: core.RoleEmployee, Active: true}, false, true, false},
		{"supervisor", core.User{Role: core.RoleSupervisor, Active: true}, false, true, true},
		{"admin", core.User{Role: core.RoleAdmin, Active: true}, true, true, true},
		{"inactive admin", core.User{Role: core.RoleAdmin}, false, false, false},
		{"deleted admin", core.User{Role: core.RoleAdmin, Active: true, Deleted: true}, false, false, false},
		{"deleted employee", core.User{Role: core.RoleEmployee, Active: true, Deleted: true}, false, false, false},
		{"unknown role", core.User{Role: "unbekannt", Active: true}, false, false, false},
		{"anonymous", core.User{}, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if CanManageObjects(tc.user) != tc.manage || CanRecord(tc.user) != tc.record || CanApprove(tc.user) != tc.approve {
				t.Errorf("unexpected rights: manage=%t record=%t approve=%t", CanManageObjects(tc.user), CanRecord(tc.user), CanApprove(tc.user))
			}
		})
	}
}

func TestWarningAndFindingValidation(t *testing.T) {
	if settings := DefaultSettings(); settings.WarningDays != 7 || settings.Version != 1 {
		t.Fatalf("unexpected defaults: %#v", settings)
	}
	for _, value := range []int{0, 7, 365} {
		if !ValidWarningDays(value) {
			t.Errorf("valid warning rejected: %d", value)
		}
	}
	for _, value := range []int{-1, 366} {
		if ValidWarningDays(value) {
			t.Errorf("invalid warning accepted: %d", value)
		}
	}
	for _, value := range []string{"", FindingOK, FindingDefect} {
		if !ValidFinding(value) {
			t.Errorf("valid finding rejected: %q", value)
		}
	}
	if ValidFinding("unknown") {
		t.Fatal("unknown finding accepted")
	}
}
