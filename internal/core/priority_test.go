package core

import "testing"

func TestPriorityDivergenceUsesConfiguredScale(t *testing.T) {
	ptr := func(v int) *int { return &v }
	for _, tc := range []struct {
		name                 string
		scale                PriorityScale
		employee, supervisor *int
		want                 bool
	}{
		{"both unset", DefaultPriorityScale(), nil, nil, false},
		{"one unset", DefaultPriorityScale(), ptr(5), nil, false},
		{"equal", DefaultPriorityScale(), ptr(4), ptr(4), false},
		{"two apart", DefaultPriorityScale(), ptr(5), ptr(3), false},
		{"three apart", DefaultPriorityScale(), ptr(4), ptr(1), true},
		{"reverse", DefaultPriorityScale(), ptr(1), ptr(5), true},
		{"custom below threshold", PriorityScale{Min: 2, Max: 8}, ptr(2), ptr(5), false},
		{"custom threshold", PriorityScale{Min: 2, Max: 8}, ptr(2), ptr(6), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.scale.Diverges(Entry{EmployeePriority: tc.employee, SupervisorPriority: tc.supervisor})
			if got != tc.want {
				t.Fatalf("Diverges = %v, want %v", got, tc.want)
			}
		})
	}
}
