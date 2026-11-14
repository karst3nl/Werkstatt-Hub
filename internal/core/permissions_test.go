package core

import "testing"

func TestRolePermissions(t *testing.T) {
	entry := Entry{AuthorID: 7}
	for _, tc := range []struct {
		name                string
		user                User
		edit, manage, admin bool
	}{
		{"employee owns entry", User{ID: 7, Role: RoleEmployee, Active: true}, true, false, false},
		{"employee other entry", User{ID: 8, Role: RoleEmployee, Active: true}, false, false, false},
		{"supervisor", User{ID: 8, Role: RoleSupervisor, Active: true}, true, true, false},
		{"admin", User{ID: 8, Role: RoleAdmin, Active: true}, true, true, true},
		{"inactive employee", User{ID: 7, Role: RoleEmployee}, false, false, false},
		{"inactive supervisor", User{ID: 8, Role: RoleSupervisor}, false, false, false},
		{"inactive admin", User{ID: 8, Role: RoleAdmin}, false, false, false},
		{"unknown role", User{ID: 7, Role: Role("owner"), Active: true}, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanEditEntry(tc.user, entry); got != tc.edit {
				t.Errorf("CanEditEntry = %v, want %v", got, tc.edit)
			}
			if got := CanManage(tc.user); got != tc.manage {
				t.Errorf("CanManage = %v, want %v", got, tc.manage)
			}
			if got := CanAdmin(tc.user); got != tc.admin {
				t.Errorf("CanAdmin = %v, want %v", got, tc.admin)
			}
		})
	}
}

func TestValidRoles(t *testing.T) {
	for _, role := range []Role{RoleEmployee, RoleSupervisor, RoleAdmin} {
		if !role.Valid() {
			t.Errorf("role %q should be valid", role)
		}
	}
	for _, role := range []Role{"", "ADMIN", "admin ", "owner"} {
		if role.Valid() {
			t.Errorf("role %q should be invalid", role)
		}
	}
}
