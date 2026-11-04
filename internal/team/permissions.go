// Права на участников команды описаны данными: роль действующего -> роли, с
// которыми он может работать. Новое правило — новая строка таблицы.

package team

import "slices"

type Role string

const (
	Owner  Role = "owner"
	Admin  Role = "admin"
	Member Role = "member"
)

var invitable = map[Role][]Role{
	Owner: {Admin, Member},
	Admin: {Member},
}

var removable = map[Role][]Role{
	Owner: {Admin, Member},
	Admin: {Member},
}

var roleChangers = []Role{Owner}

var assignable = []Role{Admin, Member}

func canInvite(actor, invited Role) bool {
	return slices.Contains(invitable[actor], invited)
}

func canRemoveAnyone(actor Role) bool {
	return len(removable[actor]) > 0
}

func canRemove(actor, target Role) bool {
	return slices.Contains(removable[actor], target)
}

func canChangeRoles(actor Role) bool {
	return slices.Contains(roleChangers, actor)
}

func canChangeRoleOf(target Role) bool {
	return target != Owner
}

func isAssignable(role Role) bool {
	return slices.Contains(assignable, role)
}
