// Package core contains the shared domain model and role policy for all modules.
package core

import (
	"errors"
	"time"
)

type Role string

const (
	RoleEmployee   Role = "mitarbeiter"
	RoleSupervisor Role = "vorgesetzter"
	RoleAdmin      Role = "admin"
)

func (r Role) Valid() bool { return r == RoleEmployee || r == RoleSupervisor || r == RoleAdmin }

type User struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Role   Role   `json:"role"`
	Active bool   `json:"active"`
}
type Signature struct {
	Name string    `json:"name"`
	At   time.Time `json:"at"`
}
type Entry struct {
	Module             string    `json:"module"`
	ID                 int64     `json:"id"`
	AuthorID           int64     `json:"author_id"`
	Title              string    `json:"title"`
	Body               string    `json:"body"`
	Status             string    `json:"status"`
	Signature          Signature `json:"signature"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	Version            int64     `json:"version"`
	EmployeePriority   *int      `json:"employee_priority"`
	SupervisorPriority *int      `json:"supervisor_priority"`
}
type EntryInput struct{ Title, Body string }
type EntryUpdate struct {
	Title, Body, Status string
	Version             int64
}
type Comment struct {
	ID        int64     `json:"id"`
	EntryID   int64     `json:"entry_id"`
	AuthorID  int64     `json:"author_id"`
	Body      string    `json:"body"`
	Signature Signature `json:"signature"`
}
type AuditEvent struct {
	ID         int64
	ActorID    int64
	ActorName  string
	Action     string
	EntityType string
	EntityID   int64
	At         time.Time
	Before     string
	After      string
}

var (
	ErrForbidden          = errors.New("keine Berechtigung")
	ErrNotFound           = errors.New("nicht gefunden")
	ErrValidation         = errors.New("ungültige Eingabe")
	ErrConflict           = errors.New("zwischenzeitlich geändert")
	ErrAlreadyInitialized = errors.New("bereits eingerichtet")
)

func CanManage(u User) bool { return u.Active && (u.Role == RoleSupervisor || u.Role == RoleAdmin) }
func CanAdmin(u User) bool  { return u.Active && u.Role == RoleAdmin }
func CanEditEntry(u User, e Entry) bool {
	return u.Active && u.Role.Valid() && (u.ID == e.AuthorID || CanManage(u))
}
