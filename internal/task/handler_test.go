package task

import (
	"encoding/json"
	"testing"
)

func decodeUpdate(t *testing.T, body string) (UpdateInput, error) {
	t.Helper()
	var req updateRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	return req.toInput()
}

func TestUpdateRequestRequiresEveryField(t *testing.T) {
	full := `{"title":"T","description":"","status":"todo","assignee_id":null,"version":1}`
	input, err := decodeUpdate(t, full)
	if err != nil || input.AssigneeID != nil || input.Version != 1 {
		t.Fatalf("input = %+v, err = %v", input, err)
	}

	withAssignee := `{"title":"T","description":"","status":"todo","assignee_id":7,"version":1}`
	input, err = decodeUpdate(t, withAssignee)
	if err != nil || input.AssigneeID == nil || *input.AssigneeID != 7 {
		t.Fatalf("input = %+v, err = %v", input, err)
	}

	missingField := map[string]string{
		"title":       `{"description":"","status":"todo","assignee_id":null,"version":1}`,
		"description": `{"title":"T","status":"todo","assignee_id":null,"version":1}`,
		"status":      `{"title":"T","description":"","assignee_id":null,"version":1}`,
		"assignee_id": `{"title":"T","description":"","status":"todo","version":1}`,
		"version":     `{"title":"T","description":"","status":"todo","assignee_id":null}`,
	}
	for field, body := range missingField {
		if _, err := decodeUpdate(t, body); err == nil {
			t.Errorf("missing %s accepted", field)
		}
	}
}
