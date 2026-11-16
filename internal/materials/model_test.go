package materials

import (
	"errors"
	"math"
	"strings"
	"testing"

	"werkstatt-hub/internal/core"
)

func TestQuantityParsingKeepsExactThousandths(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int64
	}{
		{"0", 0}, {"0,000", 0}, {"1", 1000}, {"0.001", 1}, {"0,01", 10},
		{"0,1", 100}, {"1.125", 1125}, {"1,125", 1125}, {"12.03", 12030},
		{"0012,030", 12030}, {"999999999", 999999999000},
		{"999999999,999", MaxQuantity}, {"999999999.999", MaxQuantity},
	} {
		t.Run(tc.text, func(t *testing.T) {
			got, err := ParseQuantity(tc.text)
			if err != nil || got != tc.want {
				t.Fatalf("ParseQuantity(%q) = %d,%v; want %d", tc.text, got, err, tc.want)
			}
			back, err := ParseQuantity(FormatQuantity(got))
			if err != nil || back != tc.want {
				t.Fatalf("quantity lost precision after formatting: %d,%v", back, err)
			}
		})
	}
}

func TestQuantityParsingRejectsAmbiguousOrInvalidInput(t *testing.T) {
	for _, value := range []string{
		"", " ", " 1", "1 ", "1\n", "\t1", "+1", "-1", "-0", "−1", "NaN", "Inf", "1e3", "1E3",
		".5", ",5", "1.", "1,", "1,2345", "0.0001", "1.000.000", "1,000,000", "1,000.0", "1.000,0",
		"1 000", "1_000", "١", "１", "1\x00", "1000000000", "9999999999.999", "18446744073709551615",
		strings.Repeat("9", 1000), string([]byte{0xff}),
	} {
		t.Run(value, func(t *testing.T) {
			if got, err := ParseQuantity(value); !errors.Is(err, core.ErrValidation) || got != 0 {
				t.Fatalf("invalid quantity accepted: %q -> %d,%v", value, got, err)
			}
		})
	}
}

func TestQuantityFormatting(t *testing.T) {
	for _, tc := range []struct {
		quantity int64
		want     string
	}{
		{0, "0"}, {1, "0,001"}, {10, "0,01"}, {100, "0,1"}, {1000, "1"},
		{1050, "1,05"}, {12345, "12,345"}, {MaxQuantity, "999999999,999"},
		{-1, "-0,001"}, {-1000, "-1"}, {math.MinInt64, "-9223372036854775,808"},
	} {
		if got := FormatQuantity(tc.quantity); got != tc.want {
			t.Errorf("FormatQuantity(%d) = %q, want %q", tc.quantity, got, tc.want)
		}
	}
}

func TestQuantityBoundsAndMinimumBoundary(t *testing.T) {
	for _, value := range []int64{0, 1, MaxQuantity} {
		if !ValidQuantity(value) {
			t.Errorf("valid quantity rejected: %d", value)
		}
	}
	for _, value := range []int64{-1, MaxQuantity + 1, math.MinInt64, math.MaxInt64} {
		if ValidQuantity(value) {
			t.Errorf("invalid quantity accepted: %d", value)
		}
	}
	for _, tc := range []struct {
		stock, minimum int64
		want           bool
	}{
		{0, 0, true}, {999, 1000, true}, {1000, 1000, true}, {1001, 1000, false},
		{-1, 1000, false}, {1000, -1, false}, {MaxQuantity + 1, MaxQuantity, false},
	} {
		if got := IsLow(tc.stock, tc.minimum); got != tc.want {
			t.Errorf("IsLow(%d,%d) = %t, want %t", tc.stock, tc.minimum, got, tc.want)
		}
	}
}

func TestWarningTransitions(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		previous, current, minimum int64
		open, want                 bool
	}{
		{"reaches minimum", 2000, 1000, 1000, false, true},
		{"crosses minimum", 2000, 500, 1000, false, true},
		{"above minimum", 2000, 1500, 1000, false, false},
		{"equal after acknowledgment", 1000, 1000, 1000, false, false},
		{"higher low after acknowledgment", 500, 750, 1000, false, false},
		{"further drop after acknowledgment", 750, 500, 1000, false, true},
		{"recovery", 500, 2000, 1000, false, false},
		{"drop after recovery", 2000, 1000, 1000, false, true},
		{"open warning does not duplicate", 1000, 500, 1000, true, false},
		{"open warning not replaced on recovery", 500, 2000, 1000, true, false},
		{"zero minimum reached", 1, 0, 0, false, true},
		{"repeated zero", 0, 0, 0, false, false},
		{"invalid previous", -1, 0, 0, false, false},
		{"invalid current", 1000, -1, 1000, false, false},
		{"invalid minimum", 1000, 0, -1, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldWarn(tc.previous, tc.current, tc.minimum, tc.open); got != tc.want {
				t.Fatalf("ShouldWarn(%d,%d,%d,%t) = %t, want %t", tc.previous, tc.current, tc.minimum, tc.open, got, tc.want)
			}
		})
	}
}

func TestMaterialRolePolicy(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		user                       core.User
		manage, check, acknowledge bool
	}{
		{"employee", core.User{Role: core.RoleEmployee, Active: true}, false, true, false},
		{"supervisor", core.User{Role: core.RoleSupervisor, Active: true}, false, true, true},
		{"admin", core.User{Role: core.RoleAdmin, Active: true}, true, true, true},
		{"inactive employee", core.User{Role: core.RoleEmployee}, false, false, false},
		{"inactive supervisor", core.User{Role: core.RoleSupervisor}, false, false, false},
		{"inactive admin", core.User{Role: core.RoleAdmin}, false, false, false},
		{"deleted employee", core.User{Role: core.RoleEmployee, Active: true, Deleted: true}, false, false, false},
		{"deleted supervisor", core.User{Role: core.RoleSupervisor, Active: true, Deleted: true}, false, false, false},
		{"deleted admin", core.User{Role: core.RoleAdmin, Active: true, Deleted: true}, false, false, false},
		{"unknown role", core.User{Role: "fremd", Active: true}, false, false, false},
		{"anonymous", core.User{}, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if CanManage(tc.user) != tc.manage || CanCheck(tc.user) != tc.check || CanAcknowledge(tc.user) != tc.acknowledge {
				t.Errorf("unexpected rights: manage=%t check=%t acknowledge=%t", CanManage(tc.user), CanCheck(tc.user), CanAcknowledge(tc.user))
			}
		})
	}
}
