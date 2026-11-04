// Права на поля задачи описаны таблицей rules: роль в команде и отношение к
// задаче -> поля, которые можно менять. Пустая роль или пустое отношение
// означают «любое». Права всех подошедших строк складываются, поэтому автор,
// он же исполнитель, получает объединение. Новое правило — новая строка.

package task

import (
	"slices"

	"github.com/vzinkovsky95/task-service-secunda/internal/team"
)

type Relation string

const (
	RelationAuthor   Relation = "author"
	RelationAssignee Relation = "assignee"
)

type rule struct {
	Role     team.Role
	Relation Relation
	Fields   []Field
}

var editableFields = []Field{FieldTitle, FieldDescription, FieldStatus, FieldAssignee}

var rules = []rule{
	{Role: team.Owner, Fields: editableFields},
	{Role: team.Admin, Fields: editableFields},
	{Relation: RelationAuthor, Fields: editableFields},
	{Relation: RelationAssignee, Fields: []Field{FieldStatus}},
}

func (r rule) matches(role team.Role, relations []Relation) bool {
	roleMatches := r.Role == "" || r.Role == role
	relationMatches := r.Relation == "" || slices.Contains(relations, r.Relation)
	return roleMatches && relationMatches
}

func allowedFields(role team.Role, relations []Relation) map[Field]bool {
	allowed := map[Field]bool{}
	for _, r := range rules {
		if !r.matches(role, relations) {
			continue
		}
		for _, field := range r.Fields {
			allowed[field] = true
		}
	}
	return allowed
}

func deniedFields(role team.Role, relations []Relation, changed []Field) []Field {
	allowed := allowedFields(role, relations)
	var denied []Field
	for _, field := range changed {
		if !allowed[field] {
			denied = append(denied, field)
		}
	}
	return denied
}

func relationsOf(t Task, userID uint64) []Relation {
	var relations []Relation
	if t.CreatedBy == userID {
		relations = append(relations, RelationAuthor)
	}
	if t.AssigneeID != nil && *t.AssigneeID == userID {
		relations = append(relations, RelationAssignee)
	}
	return relations
}
