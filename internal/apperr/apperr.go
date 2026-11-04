// Доменные ошибки без привязки к HTTP: сервисы возвращают вид ошибки,
// а в статус его переводит слой httpx.

package apperr

type Kind int

const (
	Invalid Kind = iota + 1
	Unauthorized
	Forbidden
	NotFound
	Conflict
)

type Error struct {
	Kind    Kind
	Code    string
	Message string
	Details map[string]any
}

func New(kind Kind, code, message string) *Error {
	return &Error{Kind: kind, Code: code, Message: message}
}

func (e *Error) WithDetails(details map[string]any) *Error {
	copied := *e
	copied.Details = details
	return &copied
}

func (e *Error) Error() string {
	return e.Code + ": " + e.Message
}
