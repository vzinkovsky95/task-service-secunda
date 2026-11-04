package stats

import (
	"time"

	"github.com/vzinkovsky95/task-service-secunda/internal/task"
)

const (
	topAssigneesLimit = 3
	windowDays        = 30
	window            = windowDays * 24 * time.Hour
)

type Assignee struct {
	UserID      uint64
	Name        string
	ClosedTasks int
}

type Report struct {
	TeamID          uint64
	TasksByStatus   map[task.Status]int
	TopAssignees    []Assignee
	AvgCloseSeconds float64
	CommentsTotal   int
}
