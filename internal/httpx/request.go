package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/vzinkovsky95/task-service-secunda/internal/apperr"
)

const maxBodyBytes = 1 << 20

var statuses = map[apperr.Kind]int{
	apperr.Invalid:      http.StatusBadRequest,
	apperr.Unauthorized: http.StatusUnauthorized,
	apperr.Forbidden:    http.StatusForbidden,
	apperr.NotFound:     http.StatusNotFound,
	apperr.Conflict:     http.StatusConflict,
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return apperr.New(apperr.Invalid, "invalid_json", "request body is not valid JSON")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return apperr.New(apperr.Invalid, "invalid_json", "request body is not valid JSON")
	}
	return nil
}

func PathID(r *http.Request, name string) (uint64, error) {
	id, err := strconv.ParseUint(r.PathValue(name), 10, 64)
	if err != nil || id == 0 {
		return 0, apperr.New(apperr.Invalid, "invalid_id", name+" must be a positive integer")
	}
	return id, nil
}

func WriteServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		WriteError(w, r, statuses[appErr.Kind], appErr.Code, appErr.Message)
		return
	}
	Log(r.Context()).Error("request failed", "error", err)
	WriteError(w, r, http.StatusInternalServerError, "internal", "internal error")
}
