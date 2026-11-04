// Единый формат ответов: успех отдаёт свой JSON, любая ошибка отдаёт
// {"error":{"code","message","request_id"}}.

package httpx

import (
	"encoding/json"
	"net/http"
)

type errorBody struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	WriteJSON(w, status, errorBody{Error: errorPayload{
		Code:      code,
		Message:   message,
		RequestID: RequestID(r.Context()),
	}})
}
