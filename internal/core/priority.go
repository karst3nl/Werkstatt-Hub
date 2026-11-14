package core

// PriorityScale is shared by both independent priority fields.
type PriorityScale struct{ Min, Max int }

func DefaultPriorityScale() PriorityScale { return PriorityScale{Min: 1, Max: 5} }
func (s PriorityScale) Valid() bool       { return s.Min >= 1 && s.Min < s.Max && s.Max <= 100 }
func (s PriorityScale) Values() []int {
	if !s.Valid() {
		return nil
	}
	result := make([]int, 0, s.Max-s.Min+1)
	for n := s.Min; n <= s.Max; n++ {
		result = append(result, n)
	}
	return result
}
func (s PriorityScale) Diverges(e Entry) bool {
	if !s.Valid() || e.EmployeePriority == nil || e.SupervisorPriority == nil {
		return false
	}
	gap := *e.EmployeePriority - *e.SupervisorPriority
	if gap < 0 {
		gap = -gap
	}
	return gap >= (s.Max-s.Min)/2+1
}

type PrioritySide string

const (
	PriorityEmployee   PrioritySide = "mitarbeiter"
	PrioritySupervisor PrioritySide = "vorgesetzter"
)

type PriorityChange struct {
	Side    PrioritySide
	Value   *int
	Version int64
}

func CanSetEmployeePriority(u User, e Entry) bool {
	return u.Active && u.Role.Valid() && (u.Role == RoleEmployee || u.ID == e.AuthorID)
}
func CanSetSupervisorPriority(u User) bool { return CanManage(u) }
