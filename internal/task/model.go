package task

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"
)

type Status string

const (
	StatusTodo       Status = "todo"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
)

func Statuses() []Status {
	return []Status{StatusTodo, StatusInProgress, StatusDone}
}

func (s Status) valid() bool {
	return slices.Contains(Statuses(), s)
}

type Field string

const (
	FieldTitle       Field = "title"
	FieldDescription Field = "description"
	FieldStatus      Field = "status"
	FieldAssignee    Field = "assignee_id"
	FieldClosedAt    Field = "closed_at"
)

type Task struct {
	ID          uint64
	TeamID      uint64
	Title       string
	Description string
	Status      Status
	CreatedBy   uint64
	AssigneeID  *uint64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ClosedAt    *time.Time
	Version     uint64
}

type Change struct {
	Old any `json:"old"`
	New any `json:"new"`
}

type Changes map[Field]Change

type HistoryEntry struct {
	ID        uint64
	TaskID    uint64
	ChangedBy uint64
	Changes   json.RawMessage
	CreatedAt time.Time
}

type Comment struct {
	ID        uint64
	TaskID    uint64
	UserID    uint64
	Content   string
	CreatedAt time.Time
}

type CreateInput struct {
	TeamID      uint64
	Title       string
	Description string
	AssigneeID  *uint64
}

type UpdateInput struct {
	Title       string
	Description string
	Status      Status
	AssigneeID  *uint64
	Version     uint64
}

type ListFilter struct {
	TeamID     uint64
	Status     Status
	AssigneeID *uint64
	Limit      int
	Offset     int
}

func (f ListFilter) cacheKey() string {
	status, assignee := "-", "-"
	if f.Status != "" {
		status = string(f.Status)
	}
	if f.AssigneeID != nil {
		assignee = strconv.FormatUint(*f.AssigneeID, 10)
	}
	return fmt.Sprintf("status=%s:assignee=%s:limit=%d:offset=%d", status, assignee, f.Limit, f.Offset)
}

type cachedList struct {
	Tasks []Task
	Total int
}
