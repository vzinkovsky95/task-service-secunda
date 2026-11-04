package team

import (
	"net/http"
	"time"

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
	mux.HandleFunc("POST /api/v1/teams", authed(h.create))
	mux.HandleFunc("GET /api/v1/teams", authed(h.list))
	mux.HandleFunc("POST /api/v1/teams/{id}/invite", authed(h.invite))
	mux.HandleFunc("PATCH /api/v1/teams/{id}/members/{user_id}", authed(h.changeRole))
	mux.HandleFunc("DELETE /api/v1/teams/{id}/members/{user_id}", authed(h.removeMember))
}

type teamResponse struct {
	ID        uint64    `json:"id"`
	Name      string    `json:"name"`
	CreatedBy uint64    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	Role      Role      `json:"role"`
}

func toTeamResponse(m Membership) teamResponse {
	return teamResponse{ID: m.Team.ID, Name: m.Team.Name, CreatedBy: m.Team.CreatedBy, CreatedAt: m.Team.CreatedAt, Role: m.Role}
}

type memberResponse struct {
	TeamID uint64 `json:"team_id"`
	UserID uint64 `json:"user_id"`
	Role   Role   `json:"role"`
}

type createRequest struct {
	Name string `json:"name"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}

	membership, err := h.service.Create(r.Context(), auth.UserID(r.Context()), req.Name)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toTeamResponse(membership))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	memberships, err := h.service.List(r.Context(), auth.UserID(r.Context()))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}

	teams := make([]teamResponse, 0, len(memberships))
	for _, m := range memberships {
		teams = append(teams, toTeamResponse(m))
	}
	httpx.WriteJSON(w, http.StatusOK, teams)
}

type inviteRequest struct {
	Email string `json:"email"`
	Role  Role   `json:"role"`
}

func (h *Handler) invite(w http.ResponseWriter, r *http.Request) {
	teamID, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	var req inviteRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}

	member, err := h.service.Invite(r.Context(), auth.UserID(r.Context()), teamID, req.Email, req.Role)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, memberResponse(member))
}

type changeRoleRequest struct {
	Role Role `json:"role"`
}

func (h *Handler) changeRole(w http.ResponseWriter, r *http.Request) {
	teamID, targetID, err := memberPath(r)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	var req changeRoleRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}

	member, err := h.service.ChangeRole(r.Context(), auth.UserID(r.Context()), teamID, targetID, req.Role)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, memberResponse(member))
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	teamID, targetID, err := memberPath(r)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}

	if err := h.service.RemoveMember(r.Context(), auth.UserID(r.Context()), teamID, targetID); err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func memberPath(r *http.Request) (teamID, userID uint64, err error) {
	if teamID, err = httpx.PathID(r, "id"); err != nil {
		return 0, 0, err
	}
	userID, err = httpx.PathID(r, "user_id")
	return teamID, userID, err
}
