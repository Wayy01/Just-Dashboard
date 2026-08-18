package httpx

import (
	"net/http"
	"strings"
)

// ConfirmHeader carries the phrase a client must echo back before an
// irreversible action runs. Requiring it in a header (rather than a body
// field) means the same guard applies uniformly to DELETE and POST alike,
// and a replayed URL alone can never trigger destruction.
const ConfirmHeader = "X-Confirm"

// RequireTypedConfirmation refuses the request unless the caller repeats the
// exact phrase. The frontend renders this as a "type <name> to confirm" input;
// scripted callers must send the header deliberately.
func RequireTypedConfirmation(w http.ResponseWriter, r *http.Request, phrase string) error {
	got := strings.TrimSpace(r.Header.Get(ConfirmHeader))
	if got == "" {
		return &APIError{
			Status:  http.StatusPreconditionRequired,
			Code:    "confirmation_required",
			Message: "type \"" + phrase + "\" to confirm this irreversible action",
		}
	}
	if got != phrase {
		return &APIError{
			Status:  http.StatusPreconditionFailed,
			Code:    "confirmation_mismatch",
			Message: "confirmation text does not match \"" + phrase + "\"",
		}
	}
	return nil
}
