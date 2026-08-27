package api

import (
	"strings"
	"testing"

	"github.com/Wayy01/Just-Dashboard/backend/internal/dbx"
)

// What a reconcile says about a database it can see and cannot use.
//
// This pins the defect the reporting exists to fix, which was a silence: a
// Postgres on a compose network with no published port was detected, its
// credentials were read, Connectable() came back false, and the loop moved on.
// The operator pressed a button that appeared to do nothing about a database
// sitting in plain sight on their own Docker page.
//
// The candidates here come from dbx.Detect rather than being written by hand,
// so the test fails if detection ever stops explaining itself — the reason
// string is half the feature, and an empty one is the same silence wearing a
// different shape.

func TestSyncReportsADatabaseItCannotReach(t *testing.T) {
	cand, _ := dbx.Detect(
		"main-backend-postgres-1", "postgres:16-alpine",
		map[string]string{"POSTGRES_PASSWORD": "secret", "POSTGRES_DB": "main"},
		nil, nil, // nothing published and no address of its own: nowhere to dial
	)
	if cand == nil {
		t.Fatal("postgres:16-alpine should be recognised whether or not it publishes a port")
	}

	row, ok := unreachableFrom(cand)
	if !ok {
		t.Fatal("a container with no published port must be reported, not skipped")
	}
	if row.Container != "main-backend-postgres-1" {
		t.Errorf("container = %q, want the container's own name", row.Container)
	}
	if row.Driver != string(dbx.DriverPostgres) {
		t.Errorf("driver = %q, want postgres", row.Driver)
	}
	// The reason has to name the fix. An operator who is told only that
	// something failed is no better off than one who was told nothing.
	if !strings.Contains(row.Reason, "published port") {
		t.Errorf("reason = %q, want it to say the port is not published", row.Reason)
	}
}

func TestSyncSaysNothingAboutADatabaseItCanAdopt(t *testing.T) {
	cand, password := dbx.Detect(
		"pg", "postgres:16",
		map[string]string{"POSTGRES_PASSWORD": "secret"},
		[]dbx.PublishedPort{{ContainerPort: 5432, HostIP: "127.0.0.1", HostPort: 5432}},
		nil,
	)
	if cand == nil || password != "secret" {
		t.Fatalf("detect returned %+v / %q", cand, password)
	}
	if _, ok := unreachableFrom(cand); ok {
		t.Error("a connectable server must be adopted silently, not reported as a problem")
	}
}

// A candidate that is unconnectable for a reason detection did not name must
// still carry a sentence. This is the guard on the fallback, which exists so a
// future engine rule cannot reintroduce the blank-line version of the silence.
func TestSyncNeverReportsABlankReason(t *testing.T) {
	row, ok := unreachableFrom(&dbx.Candidate{
		Driver: dbx.DriverMySQL, Container: "db", Port: 0,
	})
	if !ok {
		t.Fatal("a candidate with no port is not connectable and must be reported")
	}
	if strings.TrimSpace(row.Reason) == "" {
		t.Error("an unreachable server reported without a reason is the same silence in another shape")
	}
}

func TestSyncIgnoresNothing(t *testing.T) {
	if _, ok := unreachableFrom(nil); ok {
		t.Error("a nil candidate is not a server to report")
	}
}
