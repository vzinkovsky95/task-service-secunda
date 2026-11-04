// Права проверяются по реально изменившимся полям: PUT присылает задачу целиком,
// и прежние значения ничего не нарушают. diff строит эти изменения относительно
// строки, прочитанной под блокировкой.

package task

import (
	"strings"
	"unicode/utf8"

	"github.com/vzinkovsky95/task-service-secunda/internal/apperr"
)

const (
	maxTitleRunes       = 255
	maxDescriptionRunes = 10000
	maxCommentRunes     = 5000
)

func normalizeCreate(in CreateInput) (CreateInput, error) {
	title, err := normalizeTitle(in.Title)
	if err != nil {
		return CreateInput{}, err
	}
	if err := validateDescription(in.Description); err != nil {
		return CreateInput{}, err
	}
	if err := validateAssignee(in.AssigneeID); err != nil {
		return CreateInput{}, err
	}
	if in.TeamID == 0 {
		return CreateInput{}, invalid("team_id must be a positive integer")
	}
	in.Title = title
	return in, nil
}

func normalizeUpdate(in UpdateInput) (UpdateInput, error) {
	title, err := normalizeTitle(in.Title)
	if err != nil {
		return UpdateInput{}, err
	}
	if err := validateDescription(in.Description); err != nil {
		return UpdateInput{}, err
	}
	if !in.Status.valid() {
		return UpdateInput{}, invalid("status must be todo, in_progress or done")
	}
	if err := validateAssignee(in.AssigneeID); err != nil {
		return UpdateInput{}, err
	}
	if in.Version == 0 {
		return UpdateInput{}, invalid("version must be a positive integer")
	}
	in.Title = title
	return in, nil
}

func normalizeComment(raw string) (string, error) {
	content := strings.TrimSpace(raw)
	if content == "" || utf8.RuneCountInString(content) > maxCommentRunes {
		return "", invalid("content must be 1 to 5000 characters long")
	}
	return content, nil
}

func normalizeTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	if title == "" || utf8.RuneCountInString(title) > maxTitleRunes {
		return "", invalid("title must be 1 to 255 characters long")
	}
	return title, nil
}

func validateDescription(description string) error {
	if utf8.RuneCountInString(description) > maxDescriptionRunes {
		return invalid("description must be at most 10000 characters long")
	}
	return nil
}

func validateAssignee(assigneeID *uint64) error {
	if assigneeID != nil && *assigneeID == 0 {
		return invalid("assignee_id must be a positive integer or null")
	}
	return nil
}

func diff(current Task, in UpdateInput) Changes {
	changes := Changes{}
	if current.Title != in.Title {
		changes[FieldTitle] = Change{Old: current.Title, New: in.Title}
	}
	if current.Description != in.Description {
		changes[FieldDescription] = Change{Old: current.Description, New: in.Description}
	}
	if current.Status != in.Status {
		changes[FieldStatus] = Change{Old: current.Status, New: in.Status}
	}
	if !sameID(current.AssigneeID, in.AssigneeID) {
		changes[FieldAssignee] = Change{Old: idValue(current.AssigneeID), New: idValue(in.AssigneeID)}
	}
	return changes
}

func (c Changes) fields() []Field {
	fields := make([]Field, 0, len(c))
	for _, field := range editableFields {
		if _, ok := c[field]; ok {
			fields = append(fields, field)
		}
	}
	return fields
}

func sameID(a, b *uint64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func idValue(id *uint64) any {
	if id == nil {
		return nil
	}
	return *id
}

func invalid(message string) error {
	return apperr.New(apperr.Invalid, "validation_error", message)
}
