package task

import (
	"slices"
	"testing"

	"github.com/vzinkovsky95/task-service-secunda/internal/team"
)

var allEditable = []Field{FieldTitle, FieldDescription, FieldStatus, FieldAssignee}

func TestDeniedFields(t *testing.T) {
	cases := []struct {
		name      string
		role      team.Role
		relations []Relation
		changed   []Field
		denied    []Field
	}{
		{"owner edits anything", team.Owner, nil, allEditable, nil},
		{"admin edits anything", team.Admin, nil, allEditable, nil},
		{"member author edits anything", team.Member, []Relation{RelationAuthor}, allEditable, nil},
		{"assignee changes status", team.Member, []Relation{RelationAssignee}, []Field{FieldStatus}, nil},
		{"assignee cannot change title", team.Member, []Relation{RelationAssignee}, []Field{FieldTitle}, []Field{FieldTitle}},
		{"assignee cannot reassign", team.Member, []Relation{RelationAssignee}, []Field{FieldAssignee}, []Field{FieldAssignee}},
		{
			"assignee mixed change denies only the forbidden field",
			team.Member, []Relation{RelationAssignee},
			[]Field{FieldStatus, FieldDescription}, []Field{FieldDescription},
		},
		{"author and assignee get the union", team.Member, []Relation{RelationAuthor, RelationAssignee}, allEditable, nil},
		{"unrelated member edits nothing", team.Member, nil, []Field{FieldStatus}, []Field{FieldStatus}},
		{"unrelated member denied every field", team.Member, nil, allEditable, allEditable},
		{"empty change is never denied", team.Member, nil, nil, nil},
		{"unknown role gets nothing", "", nil, []Field{FieldTitle}, []Field{FieldTitle}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deniedFields(tc.role, tc.relations, tc.changed)
			if !slices.Equal(got, tc.denied) {
				t.Errorf("denied = %v, want %v", got, tc.denied)
			}
		})
	}
}

func TestRelationsOf(t *testing.T) {
	assignee := uint64(5)
	cases := []struct {
		name string
		task Task
		user uint64
		want []Relation
	}{
		{"author only", Task{CreatedBy: 1, AssigneeID: &assignee}, 1, []Relation{RelationAuthor}},
		{"assignee only", Task{CreatedBy: 1, AssigneeID: &assignee}, 5, []Relation{RelationAssignee}},
		{"both", Task{CreatedBy: 5, AssigneeID: &assignee}, 5, []Relation{RelationAuthor, RelationAssignee}},
		{"neither", Task{CreatedBy: 1, AssigneeID: &assignee}, 9, nil},
		{"no assignee", Task{CreatedBy: 1}, 9, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := relationsOf(tc.task, tc.user); !slices.Equal(got, tc.want) {
				t.Errorf("relations = %v, want %v", got, tc.want)
			}
		})
	}
}
