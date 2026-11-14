// Package tooling defines workshop inventory, condition reports and purchase wishes.
package tooling

import (
	"time"
	"werkstatt-hub/internal/core"
)

type Tool struct {
	ID          int64          `json:"id"`
	AuthorID    int64          `json:"author_id"`
	Name        string         `json:"name"`
	Category    string         `json:"category"`
	Location    string         `json:"location"`
	Condition   string         `json:"condition"`
	Signature   core.Signature `json:"signature"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	Version     int64          `json:"version"`
	OpenReports int            `json:"open_reports"`
}
type ToolInput struct {
	Name, Category, Location, Condition string
	AuthorID                            int64
}
type ToolUpdate struct {
	Name, Category, Location, Condition string
	Version                             int64
}
type ToolFilter struct{ Category, Condition, Search string }

type Report struct {
	core.Entry
	ToolID            int64  `json:"tool_id"`
	ToolName          string `json:"tool_name"`
	ObservedCondition string `json:"observed_condition"`
}
type ReportInput struct {
	ToolID                         int64
	Title, Body, ObservedCondition string
}
type ReportUpdate struct {
	Title, Body, ObservedCondition, Status string
	Version                                int64
}
type ReportFilter struct {
	ToolID                                        int64
	Status, Sort                                  string
	EmployeePriority, SupervisorPriority          *int
	EmployeeUnset, SupervisorUnset, OnlyDivergent bool
}
type Purchase struct {
	core.Entry
	ToolID   int64  `json:"tool_id"`
	ToolName string `json:"tool_name"`
	Category string `json:"category"`
	Location string `json:"location"`
}
type PurchaseInput struct {
	Title, Body, Category, Location string
	ToolID                          int64
}
type PurchaseUpdate struct {
	Title, Body, Category, Location, Status string
	Version                                 int64
}
type PurchaseFilter struct {
	ToolID                                        int64
	Category, Status, Sort                        string
	EmployeePriority, SupervisorPriority          *int
	EmployeeUnset, SupervisorUnset, OnlyDivergent bool
}

func ValidCondition(value string) bool {
	switch value {
	case "ok", "verschleisst", "defekt", "ersetzt":
		return true
	}
	return false
}
func ValidReportStatus(value string) bool {
	switch value {
	case "neu", "in_pruefung", "erledigt":
		return true
	}
	return false
}
func ValidPurchaseStatus(value string) bool {
	switch value {
	case "gewuenscht", "genehmigt", "bestellt", "geliefert":
		return true
	}
	return false
}
func CanEditPurchase(u core.User, p Purchase) bool {
	return u.Active && !u.Deleted && u.Role.Valid() && (core.CanManage(u) || u.ID == p.AuthorID && p.Status == "gewuenscht")
}
