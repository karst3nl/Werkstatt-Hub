package web

import (
	"strings"
	"testing"

	"werkstatt-hub/internal/core"
)

func TestPriorityWrenchesKeepStoredValueAndScale(t *testing.T) {
	for _, tc := range []struct {
		name       string
		scale      core.PriorityScale
		value      *int
		wantActive int
	}{
		{"unset", core.DefaultPriorityScale(), nil, 0},
		{"minimum", core.DefaultPriorityScale(), priorityPointer(1), 1},
		{"middle", core.DefaultPriorityScale(), priorityPointer(3), 3},
		{"maximum", core.DefaultPriorityScale(), priorityPointer(5), 5},
		{"custom minimum", core.PriorityScale{Min: 2, Max: 8}, priorityPointer(2), 1},
		{"custom middle", core.PriorityScale{Min: 2, Max: 8}, priorityPointer(5), 3},
		{"custom upper value", core.PriorityScale{Min: 2, Max: 8}, priorityPointer(7), 4},
		{"custom maximum", core.PriorityScale{Min: 1, Max: 100}, priorityPointer(100), 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := 0
			if tc.value != nil {
				original = *tc.value
			}
			rating := priorityRenderer(tc.scale)(tc.value, "Mitarbeiter-Priorität")
			if len(rating.Items) != 5 {
				t.Fatalf("icons = %d, want five", len(rating.Items))
			}
			active := 0
			for _, icon := range rating.Items {
				if icon.Active {
					active++
				}
			}
			if active != tc.wantActive {
				t.Fatalf("filled icons = %d, want %d", active, tc.wantActive)
			}
			if tc.value != nil && *tc.value != original {
				t.Fatal("display changed the stored priority")
			}
			if !strings.Contains(rating.Description, "Mitarbeiter-Priorität") || (tc.value == nil) != rating.Unset {
				t.Fatalf("incomplete accessible description: %#v", rating)
			}
			if tc.value == nil && rating.ValueText != "Noch nicht gesetzt" {
				t.Fatal("unset priority has no text equivalent")
			}
			if tc.value != nil && !strings.Contains(rating.Description, "Skala") {
				t.Fatal("configured scale is missing from the accessible description")
			}
		})
	}
}

func priorityPointer(value int) *int { return &value }
