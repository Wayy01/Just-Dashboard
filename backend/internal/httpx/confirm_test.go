package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Invariant 3 rests entirely on this function: an irreversible action runs
// only if the caller echoed the exact phrase. The phrase travels as its own
// field so the client never has to parse it back out of the message.
func TestRequireTypedConfirmation(t *testing.T) {
	cases := []struct {
		name   string
		header string
		phrase string
		status int
		code   string
	}{
		{"missing", "", "prod-db", http.StatusPreconditionRequired, "confirmation_required"},
		{"wrong", "prod", "prod-db", http.StatusPreconditionFailed, "confirmation_mismatch"},
		{"case differs", "PROD-DB", "prod-db", http.StatusPreconditionFailed, "confirmation_mismatch"},
		{"exact", "prod-db", "prod-db", 0, ""},
		{"surrounding space is forgiven", "  prod-db\t", "prod-db", 0, ""},
		// The phrase is a file or object name, so it can contain anything a
		// name can contain — including the quotes the message wraps it in.
		{"quotes in the phrase", `my"file`, `my"file`, 0, ""},
		{"quotes truncated", "my", `my"file`, http.StatusPreconditionFailed, "confirmation_mismatch"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodDelete, "/api/v1/files", nil)
			if c.header != "" {
				r.Header.Set(ConfirmHeader, c.header)
			}
			err := RequireTypedConfirmation(httptest.NewRecorder(), r, c.phrase)
			if c.status == 0 {
				if err != nil {
					t.Fatalf("expected the confirmation to pass, got %v", err)
				}
				return
			}
			apiErr, ok := err.(*APIError)
			if !ok {
				t.Fatalf("expected an *APIError, got %T", err)
			}
			if apiErr.Status != c.status || apiErr.Code != c.code {
				t.Fatalf("got %d/%s, want %d/%s", apiErr.Status, apiErr.Code, c.status, c.code)
			}
			if apiErr.Phrase != c.phrase {
				t.Fatalf("the client cannot ask for the right text: phrase = %q, want %q",
					apiErr.Phrase, c.phrase)
			}
		})
	}
}
