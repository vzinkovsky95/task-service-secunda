package stats

import (
	"net/http"

	"github.com/vzinkovsky95/task-service-secunda/internal/auth"
	"github.com/vzinkovsky95/task-service-secunda/internal/httpx"
	"github.com/vzinkovsky95/task-service-secunda/internal/task"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Routes(mux httpx.Router, authed func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/teams/{team_id}/stats", authed(h.get))
}

type assigneeResponse struct {
	UserID      uint64 `json:"user_id"`
	Name        string `json:"name"`
	ClosedTasks int    `json:"closed_tasks"`
}

type reportResponse struct {
	TeamID          uint64              `json:"team_id"`
	TasksByStatus   map[task.Status]int `json:"tasks_by_status"`
	TopAssignees    []assigneeResponse  `json:"top_assignees_30d"`
	AvgCloseSeconds float64             `json:"avg_close_time_seconds"`
	CommentsTotal   int                 `json:"comments_total"`
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	teamID, err := httpx.PathID(r, "team_id")
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}

	report, err := h.service.Get(r.Context(), auth.UserID(r.Context()), teamID)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}

	top := make([]assigneeResponse, 0, len(report.TopAssignees))
	for _, a := range report.TopAssignees {
		top = append(top, assigneeResponse(a))
	}
	httpx.WriteJSON(w, http.StatusOK, reportResponse{
		TeamID:          report.TeamID,
		TasksByStatus:   report.TasksByStatus,
		TopAssignees:    top,
		AvgCloseSeconds: report.AvgCloseSeconds,
		CommentsTotal:   report.CommentsTotal,
	})
}
