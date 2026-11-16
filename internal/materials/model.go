// Package materials defines consumable stocks, signed checks and replenishment warnings.
package materials

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"werkstatt-hub/internal/core"
)

const (
	// Quantities use thousandths of the material's immutable unit, never floats.
	QuantityScale int64 = 1000
	MaxQuantity   int64 = 999999999999

	ModuleCheck     = "materialpruefung"
	CheckStatus     = "erfasst"
	FilterStatusLow = "niedrig"
)

type Material struct {
	ID          int64          `json:"id"`
	AuthorID    int64          `json:"author_id"`
	Name        string         `json:"name"`
	Unit        string         `json:"unit"`
	Location    string         `json:"location"`
	Stock       int64          `json:"stock"`
	Minimum     int64          `json:"minimum"`
	Active      bool           `json:"active"`
	OpenAlertID int64          `json:"open_alert_id"`
	Signature   core.Signature `json:"signature"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	Version     int64          `json:"version"`
}

type Input struct {
	Name, Unit, Location string
	Stock, Minimum       int64
}

// Update keeps the unit fixed and changes stock only through a signed check.
type Update struct {
	Name, Location string
	Minimum        int64
	Active         bool
	Version        int64
}

type Check struct {
	core.Entry
	MaterialID   int64  `json:"material_id"`
	MaterialName string `json:"material_name"`
	Unit         string `json:"unit"`
	Quantity     int64  `json:"quantity"`
}

type CheckInput struct {
	Quantity int64
	Note     string
	// Version is the material version displayed when entering this stock count.
	Version int64
}

type Alert struct {
	ID             int64           `json:"id"`
	MaterialID     int64           `json:"material_id"`
	MaterialName   string          `json:"material_name"`
	Unit           string          `json:"unit"`
	Quantity       int64           `json:"quantity"`
	Minimum        int64           `json:"minimum"`
	AuthorID       int64           `json:"author_id"`
	Signature      core.Signature  `json:"signature"`
	Acknowledgment *Acknowledgment `json:"acknowledgment"`
}

type Acknowledgment struct {
	UserID    int64          `json:"user_id"`
	Signature core.Signature `json:"signature"`
}

type Filter struct {
	Activity, Status, Search string
}

func ValidQuantity(value int64) bool { return value >= 0 && value <= MaxQuantity }

// ParseQuantity accepts a plain number with either decimal comma or decimal dot
// and at most three decimal places. Signs, grouping, exponents and whitespace
// are rejected so the quantity has the same meaning in every browser locale.
func ParseQuantity(value string) (int64, error) {
	if value == "" || strings.Count(value, ",")+strings.Count(value, ".") > 1 {
		return 0, core.ErrValidation
	}
	whole, fractional, hasFraction := strings.Cut(strings.ReplaceAll(value, ",", "."), ".")
	if whole == "" || hasFraction && (len(fractional) < 1 || len(fractional) > 3) {
		return 0, core.ErrValidation
	}
	var integer int64
	for _, digit := range whole {
		if digit < '0' || digit > '9' {
			return 0, core.ErrValidation
		}
		integer = integer*10 + int64(digit-'0')
		if integer > MaxQuantity/QuantityScale {
			return 0, core.ErrValidation
		}
	}
	var fraction int64
	for _, digit := range fractional {
		if digit < '0' || digit > '9' {
			return 0, core.ErrValidation
		}
		fraction = fraction*10 + int64(digit-'0')
	}
	for places := len(fractional); places < 3; places++ {
		fraction *= 10
	}
	result := integer*QuantityScale + fraction
	if !ValidQuantity(result) {
		return 0, core.ErrValidation
	}
	return result, nil
}

// FormatQuantity uses decimal comma and no grouping or insignificant zeroes.
// It also handles the full signed int64 range without negation overflow.
func FormatQuantity(value int64) string {
	prefix := ""
	magnitude := uint64(value)
	if value < 0 {
		prefix = "-"
		magnitude = uint64(-(value + 1)) + 1
	}
	whole := prefix + strconv.FormatUint(magnitude/uint64(QuantityScale), 10)
	fraction := magnitude % uint64(QuantityScale)
	if fraction == 0 {
		return whole
	}
	return whole + "," + strings.TrimRight(fmt.Sprintf("%03d", fraction), "0")
}

func IsLow(stock, minimum int64) bool {
	return ValidQuantity(stock) && ValidQuantity(minimum) && stock <= minimum
}

// ShouldWarn opens a new warning only for a later drop to a low stock, unless
// one is already open. Repeated equal or increasing low counts do not undo an
// acknowledgment. Stock recovery never closes an existing warning by itself.
// Initial low stock and a newly crossed minimum are handled during setup/edit.
func ShouldWarn(previous, current, minimum int64, hasOpen bool) bool {
	return ValidQuantity(previous) && IsLow(current, minimum) && !hasOpen && (current < previous || previous > minimum)
}

func CanManage(user core.User) bool { return core.CanAdmin(user) }
func CanCheck(user core.User) bool {
	return user.Active && !user.Deleted && user.Role.Valid()
}
func CanAcknowledge(user core.User) bool { return core.CanManage(user) }
