// В PUT обязательны все поля, включая assignee_id (null снимает исполнителя):
// пропущенное поле не должно молча превращаться в «снять исполнителя».

package task

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/vzinkovsky95/task-service-secunda/internal/apperr"
	"github.com/vzinkovsky95/task-service-secunda/internal/auth"
	"github.com/vzinkovsky95/task-service-secunda/internal/httpx"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Routes(mux *http.ServeMux, authed func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("POST /api/v1/tasks", authed(h.create))
	mux.HandleFunc("GET /api/v1/tasks", authed(h.list))
	mux.HandleFunc("PUT /api/v1/tasks/{id}", authed(h.update))
	mux.HandleFunc("GET /api/v1/tasks/{id}/history", authed(h.history))
	mux.HandleFunc("POST /api/v1/tasks/{id}/comments", authed(h.addComment))
	mux.HandleFunc("GET /api/v1/tasks/{id}/comments", authed(h.comments))
}

type taskResponse struct {
	ID          uint64     `json:"id"`
	TeamID      uint64     `json:"team_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      Status     `json:"status"`
	CreatedBy   uint64     `json:"created_by"`
	AssigneeID  *uint64    `json:"assignee_id"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ClosedAt    *time.Time `json:"closed_at"`
	Version     uint64     `json:"version"`
}

func toTaskResponse(t Task) taskResponse {
	return taskResponse(t)
}

type optionalID struct {
	set   bool
	value *uint64
}

func (o *optionalID) UnmarshalJSON(data []byte) error {
	o.set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		o.value = nil
		return nil
	}
	var id uint64
	if err := json.Unmarshal(data, &id); err != nil {
		return err
	}
	o.value = &id
	return nil
}

type createRequest struct {
	TeamID      uint64  `json:"team_id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	AssigneeID  *uint64 `json:"assignee_id"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}

	created, err := h.service.Create(r.Context(), auth.UserID(r.Context()), CreateInput(req))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toTaskResponse(created))
}

type updateRequest struct {
	Title       *string    `json:"title"`
	Description *string    `json:"description"`
	Status      *Status    `json:"status"`
	AssigneeID  optionalID `json:"assignee_id"`
	Version     *uint64    `json:"version"`
}

func (u updateRequest) toInput() (UpdateInput, error) {
	switch {
	case u.Title == nil:
		return UpdateInput{}, missing("title")
	case u.Description == nil:
		return UpdateInput{}, missing("description")
	case u.Status == nil:
		return UpdateInput{}, missing("status")
	case !u.AssigneeID.set:
		return UpdateInput{}, missing("assignee_id")
	case u.Version == nil:
		return UpdateInput{}, missing("version")
	}
	return UpdateInput{
		Title:       *u.Title,
		Description: *u.Description,
		Status:      *u.Status,
		AssigneeID:  u.AssigneeID.value,
		Version:     *u.Version,
	}, nil
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	taskID, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	var req updateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	input, err := req.toInput()
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}

	updated, err := h.service.Update(r.Context(), auth.UserID(r.Context()), taskID, input)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toTaskResponse(updated))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	filter, params, err := parseListQuery(r)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}

	tasks, total, err := h.service.List(r.Context(), auth.UserID(r.Context()), filter)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}

	items := make([]taskResponse, 0, len(tasks))
	for _, t := range tasks {
		items = append(items, toTaskResponse(t))
	}
	httpx.WriteJSON(w, http.StatusOK, taskPageResponse{
		Page:       httpx.NewPage(items, params, total),
		NextCursor: nextCursor(tasks, params.Offset, total),
	})
}

type taskPageResponse struct {
	httpx.Page[taskResponse]
	NextCursor *uint64 `json:"next_cursor"`
}

func nextCursor(tasks []Task, offset, total int) *uint64 {
	if len(tasks) == 0 || offset+len(tasks) >= total {
		return nil
	}
	last := tasks[len(tasks)-1].ID
	return &last
}

func parseListQuery(r *http.Request) (ListFilter, httpx.PageParams, error) {
	params, err := httpx.ParsePage(r)
	if err != nil {
		return ListFilter{}, params, err
	}

	query := r.URL.Query()
	teamID, err := strconv.ParseUint(query.Get("team_id"), 10, 64)
	if err != nil || teamID == 0 {
		return ListFilter{}, params, invalid("team_id is required and must be a positive integer")
	}

	filter := ListFilter{TeamID: teamID, Limit: params.Limit, Offset: params.Offset}
	if raw := query.Get("status"); raw != "" {
		filter.Status = Status(raw)
		if !filter.Status.valid() {
			return ListFilter{}, params, invalid("status must be todo, in_progress or done")
		}
	}
	if raw := query.Get("assignee_id"); raw != "" {
		assigneeID, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || assigneeID == 0 {
			return ListFilter{}, params, invalid("assignee_id must be a positive integer")
		}
		filter.AssigneeID = &assigneeID
	}
	if raw := query.Get("cursor"); raw != "" {
		cursor, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || cursor == 0 {
			return ListFilter{}, params, invalid("cursor must be a positive integer")
		}
		filter.Cursor = &cursor
		if query.Has("offset") {
			return ListFilter{}, params, invalid("cursor and offset cannot be used together")
		}
	}
	return filter, params, nil
}

type historyResponse struct {
	ID        uint64          `json:"id"`
	TaskID    uint64          `json:"task_id"`
	ChangedBy uint64          `json:"changed_by"`
	Changes   json.RawMessage `json:"changes"`
	CreatedAt time.Time       `json:"created_at"`
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	taskID, params, err := taskAndPage(r)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}

	entries, total, err := h.service.History(r.Context(), auth.UserID(r.Context()), taskID, params.Limit, params.Offset)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}

	items := make([]historyResponse, 0, len(entries))
	for _, e := range entries {
		items = append(items, historyResponse(e))
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(items, params, total))
}

type commentResponse struct {
	ID        uint64    `json:"id"`
	TaskID    uint64    `json:"task_id"`
	UserID    uint64    `json:"user_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type commentRequest struct {
	Content string `json:"content"`
}

func (h *Handler) addComment(w http.ResponseWriter, r *http.Request) {
	taskID, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	var req commentRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}

	comment, err := h.service.AddComment(r.Context(), auth.UserID(r.Context()), taskID, req.Content)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, commentResponse(comment))
}

func (h *Handler) comments(w http.ResponseWriter, r *http.Request) {
	taskID, params, err := taskAndPage(r)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}

	comments, total, err := h.service.Comments(r.Context(), auth.UserID(r.Context()), taskID, params.Limit, params.Offset)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}

	items := make([]commentResponse, 0, len(comments))
	for _, c := range comments {
		items = append(items, commentResponse(c))
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(items, params, total))
}

func taskAndPage(r *http.Request) (uint64, httpx.PageParams, error) {
	taskID, err := httpx.PathID(r, "id")
	if err != nil {
		return 0, httpx.PageParams{}, err
	}
	params, err := httpx.ParsePage(r)
	return taskID, params, err
}

func missing(field string) error {
	return apperr.New(apperr.Invalid, "validation_error", field+" is required")
}
