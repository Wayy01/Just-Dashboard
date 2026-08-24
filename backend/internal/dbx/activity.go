package dbx

import (
	"context"
	"database/sql"
	"fmt"
)

// Activity is one thing the server is currently doing.
//
// This exists for one moment: the application has gone unresponsive and nobody
// knows whether the database is the cause. Every engine can answer that, and
// none of them answer it the same way — so the shape below is the question
// ("what is running, for how long, and is it stuck behind something else?")
// rather than any one engine's table.
type Activity struct {
	// PID is whatever handle this engine's kill command takes. It is a string
	// because they are not all integers.
	PID      string  `json:"pid"`
	User     string  `json:"user,omitempty"`
	Database string  `json:"database,omitempty"`
	State    string  `json:"state,omitempty"`
	Seconds  float64 `json:"seconds"`
	Query    string  `json:"query,omitempty"`
	Client   string  `json:"client,omitempty"`
	// Wait is what the session is blocked on, where the engine reports it. This
	// is the field that turns "a query is slow" into "a query is waiting for a
	// lock", which are different problems with different fixes.
	Wait string `json:"wait,omitempty"`
	// BlockedBy names the session holding what this one wants, where the engine
	// can say. It is what makes a pile-up readable: fifty blocked sessions and
	// one culprit.
	BlockedBy string `json:"blockedBy,omitempty"`
	// Self marks the connection that answered this request.
	//
	// The list deliberately includes the dashboard's own sessions rather than
	// filtering them out. Hiding them showed an operator a partially true
	// picture — a long browse of a big table is the dashboard holding the lock,
	// and a session list that cannot say so sends them looking somewhere else —
	// and it also meant a server with nothing else connected reported an empty
	// table, which reads as "the query is broken" rather than "nothing is
	// running". The flag is per-connection and means exactly what it says: this
	// row is the connection that answered. The pool holds others, and they show
	// up unmarked, which is fine — killing one costs a reconnect that
	// database/sql does silently.
	Self bool `json:"self,omitempty"`
}

// ErrNoActivityView is returned by engines that have no server-side session
// concept at all. It is not a failure — SQLite genuinely has nothing to show —
// so the handler renders it as information rather than an error.
var ErrNoActivityView = fmt.Errorf("this engine has no server-side session list")

func scanActivity(rows *sql.Rows) ([]Activity, error) {
	defer rows.Close()
	out := []Activity{}
	for rows.Next() {
		var a Activity
		// Self arrives as 1/0 rather than a boolean: only Postgres has a real
		// boolean type here, and scanning the other five engines' integer
		// through database/sql's bool conversion depends on each driver's
		// choice of Go type for a one-bit column.
		var self int
		if err := rows.Scan(&a.PID, &a.User, &a.Database, &a.State,
			&a.Seconds, &a.Query, &a.Client, &a.Wait, &a.BlockedBy, &self); err != nil {
			return nil, err
		}
		a.Self = self != 0
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListActivity reports what the server is currently running.
func ListActivity(ctx context.Context, db *sql.DB, driver Driver) ([]Activity, error) {
	d, err := DialectFor(driver)
	if err != nil {
		return nil, err
	}
	return d.Activity(ctx, db)
}

// KillQuery terminates a session or statement.
//
// It is destructive in the sense the route map means: something running is
// stopped, and whatever it had done so far rolls back. The handler in front
// demands the capability and pauses on a confirmation naming the session, but
// asks for no phrase to be typed — this is pressed repeatedly under exactly the
// time pressure that makes a typing exercise counterproductive, and the rollback
// means nothing is lost that was not already going.
func KillQuery(ctx context.Context, db *sql.DB, driver Driver, pid string) error {
	d, err := DialectFor(driver)
	if err != nil {
		return err
	}
	return d.Kill(ctx, db, pid)
}

// pidRe bounds what can reach a kill statement. None of these engines can bind
// a session id as a parameter in their kill syntax, so the value is checked
// rather than escaped — and a session id is always digits or, on ClickHouse, a
// query UUID.
func validatePID(pid string) error {
	if pid == "" {
		return fmt.Errorf("a session id is required")
	}
	if len(pid) > 64 {
		return fmt.Errorf("session id is too long")
	}
	for _, r := range pid {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') && !(r >= 'A' && r <= 'F') && r != '-' {
			return fmt.Errorf("session id %q is not a number or uuid", pid)
		}
	}
	return nil
}
