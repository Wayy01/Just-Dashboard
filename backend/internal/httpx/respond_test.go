package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRequiresJSONAndOneValue(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantStatus  int
	}{
		{name: "json", contentType: "application/json; charset=utf-8", body: `{"name":"ok"}`},
		{name: "simple cross origin type", contentType: "text/plain", body: `{"name":"no"}`, wantStatus: http.StatusUnsupportedMediaType},
		{name: "trailing value", contentType: "application/json", body: `{"name":"one"} {"name":"two"}`, wantStatus: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			r.Header.Set("Content-Type", tt.contentType)
			var dst struct {
				Name string `json:"name"`
			}
			err := DecodeJSON(r, &dst)
			if tt.wantStatus == 0 {
				if err != nil || dst.Name != "ok" {
					t.Fatalf("DecodeJSON = (%q, %v), want (ok, nil)", dst.Name, err)
				}
				return
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Status != tt.wantStatus {
				t.Fatalf("DecodeJSON error = %v, want status %d", err, tt.wantStatus)
			}
		})
	}
}
