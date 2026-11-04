package team

import "testing"

func TestCanInvite(t *testing.T) {
	cases := []struct {
		actor, invited Role
		want           bool
	}{
		{Owner, Admin, true},
		{Owner, Member, true},
		{Owner, Owner, false},
		{Admin, Member, true},
		{Admin, Admin, false},
		{Admin, Owner, false},
		{Member, Member, false},
		{Member, Admin, false},
		{"", Member, false},
	}
	for _, tc := range cases {
		if got := canInvite(tc.actor, tc.invited); got != tc.want {
			t.Errorf("canInvite(%s, %s) = %v", tc.actor, tc.invited, got)
		}
	}
}

func TestCanRemove(t *testing.T) {
	cases := []struct {
		actor, target Role
		want          bool
	}{
		{Owner, Admin, true},
		{Owner, Member, true},
		{Owner, Owner, false},
		{Admin, Member, true},
		{Admin, Admin, false},
		{Admin, Owner, false},
		{Member, Member, false},
	}
	for _, tc := range cases {
		if got := canRemove(tc.actor, tc.target); got != tc.want {
			t.Errorf("canRemove(%s, %s) = %v", tc.actor, tc.target, got)
		}
	}
}

func TestCanRemoveAnyone(t *testing.T) {
	want := map[Role]bool{Owner: true, Admin: true, Member: false}
	for role, expected := range want {
		if got := canRemoveAnyone(role); got != expected {
			t.Errorf("canRemoveAnyone(%s) = %v", role, got)
		}
	}
}

func TestRoleChanges(t *testing.T) {
	if !canChangeRoles(Owner) || canChangeRoles(Admin) || canChangeRoles(Member) {
		t.Error("only owner may change roles")
	}
	if canChangeRoleOf(Owner) || !canChangeRoleOf(Admin) || !canChangeRoleOf(Member) {
		t.Error("owner role must be immutable")
	}
	if isAssignable(Owner) || !isAssignable(Admin) || !isAssignable(Member) || isAssignable("root") {
		t.Error("only admin and member are assignable")
	}
}

func TestCanViewStats(t *testing.T) {
	want := map[Role]bool{Owner: true, Admin: true, Member: false, "": false}
	for role, expected := range want {
		if got := CanViewStats(role); got != expected {
			t.Errorf("CanViewStats(%q) = %v", role, got)
		}
	}
}
