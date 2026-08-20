package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/metrics"
)

// The history endpoint is what makes the Overview charts outlive the tab that
// drew them, so its window arithmetic is worth pinning: a client that asks for
// a week and a client that asks for a minute must both get a series they can
// draw, and a malformed window must be refused rather than silently widened.
func TestMetricsHistoryWindow(t *testing.T) {
	s := testServer(t)

	for _, query := range []string{"range=1h", "range=7d&points=300", "range=15m&points=2"} {
		req := httptest.NewRequest(http.MethodGet, "/?"+query, nil)
		w := httptest.NewRecorder()
		if err := s.handleMetricsHistory(w, req); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		var series metrics.Series
		if err := json.Unmarshal(w.Body.Bytes(), &series); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		if series.StepSeconds < series.IntervalSeconds {
			t.Errorf("%s: bucket %ds is finer than the %ds sampling interval",
				query, series.StepSeconds, series.IntervalSeconds)
		}
		if series.Points == nil {
			t.Errorf("%s: points is null; an empty window is not a failed one", query)
		}
		if !series.To.After(series.From) {
			t.Errorf("%s: window runs backwards (%s → %s)", query, series.From, series.To)
		}
	}
}

func TestMetricsHistoryRefusesAnUnreadableWindow(t *testing.T) {
	s := testServer(t)
	for _, query := range []string{"range=banana", "range=-1h", "from=nonsense", "from=2000&to=1000"} {
		req := httptest.NewRequest(http.MethodGet, "/?"+query, nil)
		w := httptest.NewRecorder()
		err := s.handleMetricsHistory(w, req)
		var apiErr *httpx.APIError
		if !errors.As(err, &apiErr) {
			t.Errorf("%s: accepted, or failed with something the renderer cannot use: %v", query, err)
			continue
		}
		if apiErr.Status != http.StatusBadRequest {
			t.Errorf("%s: answered %d, want 400", query, apiErr.Status)
		}
	}
}

// Turning recording off is a configuration choice, not a fault, and the
// frontend distinguishes the two by this code — it renders the disabled case
// as an explanation rather than as a broken page.
func TestMetricsHistorySaysWhenItIsNotRecording(t *testing.T) {
	s := testServer(t)
	s.modules.metrics = metrics.New(s.Store, slog.New(slog.NewTextHandler(io.Discard, nil)), 0, 0)

	req := httptest.NewRequest(http.MethodGet, "/?range=1h", nil)
	w := httptest.NewRecorder()
	err := s.handleMetricsHistory(w, req)
	if err == nil {
		t.Fatal("a server that records nothing answered as though it had history")
	}
	if !strings.Contains(err.Error(), "metrics history is not being recorded") {
		t.Fatalf("unexpected error: %v", err)
	}
}
