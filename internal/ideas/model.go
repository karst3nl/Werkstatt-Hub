// Package ideas defines the workshop's ideas board using the shared entry core.
package ideas

import "werkstatt-hub/internal/core"

type Idea struct {
	core.Entry
	Category string `json:"category"`
}
type Input struct{ Title, Body, Category string }
type Update struct {
	Title, Body, Category, Status string
	Version                       int64
}
type Filter struct {
	Category, Status, Sort                        string
	EmployeePriority, SupervisorPriority          *int
	EmployeeUnset, SupervisorUnset, OnlyDivergent bool
}

func ValidStatus(status string) bool {
	switch status {
	case "neu", "in_pruefung", "geplant", "umgesetzt", "abgelehnt":
		return true
	}
	return false
}
