package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/Wayy01/vps-dashboard/backend/internal/store"
)

// Entry is one immutable record of who did what, when, from where.
type Entry struct {
	ID       int64     `json:"id"`
	TS       time.Time `json:"ts"`
	UserID   int64     `json:"userId"`
	Username string    `json:"username"`
	Role     string    `json:"role"`
	IP       string    `json:"ip"`
	Actor    string    `json:"actor"`
	Action   string    `json:"action"`
	Target   string    `json:"target"`
	Method   string    `json:"method"`
	Path     string    `json:"path"`
	Status   int       `json:"status"`
	Success  bool      `json:"success"`
	Detail   string    `json:"detail"`
}

type Logger struct {
	st  *store.Store
	log *slog.Logger
}

func New(st *store.Store, log *slog.Logger) *Logger {
	return &Logger{st: st, log: log}
}

// Record writes the entry to the audit table and mirrors it to the process log,
// so an operator retains a trail even if the database is later tampered with.
func (l *Logger) Record(ctx context.Context, e Entry) {
	if e.TS.IsZero() {
		e.TS = time.Now()
	}
	if e.Actor == "" {
		e.Actor = "session"
	}
	success := 1
	if !e.Success {
		success = 0
	}
	if _, err := l.st.DB.ExecContext(ctx,
		`INSERT INTO audit_log(ts, user_id, username, role, ip, actor, action, target, method, path, status, success, detail)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.TS.Unix(), e.UserID, e.Username, e.Role, e.IP, e.Actor, e.Action, e.Target,
		e.Method, e.Path, e.Status, success, truncate(e.Detail, 4000)); err != nil {
		l.log.Error("audit write failed", "err", err, "action", e.Action)
	}
	l.log.Info("audit",
		"user", e.Username, "role", e.Role, "ip", e.IP, "actor", e.Actor,
		"action", e.Action, "target", e.Target, "status", e.Status, "success", e.Success)
}

type Filter struct {
	Username string
	Action   string
	Since    time.Time
	Until    time.Time
	OnlyFail bool
	Limit    int
	Offset   int
}

func (l *Logger) List(ctx context.Context, f Filter) ([]Entry, int, error) {
	where := []string{"1=1"}
	args := []any{}
	if f.Username != "" {
		where = append(where, "username LIKE ?")
		args = append(args, "%"+f.Username+"%")
	}
	if f.Action != "" {
		where = append(where, "action LIKE ?")
		args = append(args, "%"+f.Action+"%")
	}
	if !f.Since.IsZero() {
		where = append(where, "ts >= ?")
		args = append(args, f.Since.Unix())
	}
	if !f.Until.IsZero() {
		where = append(where, "ts <= ?")
		args = append(args, f.Until.Unix())
	}
	if f.OnlyFail {
		where = append(where, "success = 0")
	}
	clause := strings.Join(where, " AND ")

	var total int
	if err := l.st.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	rows, err := l.st.DB.QueryContext(ctx,
		`SELECT id, ts, user_id, username, role, ip, actor, action, target, method, path, status, success, detail
		 FROM audit_log WHERE `+clause+` ORDER BY ts DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var ts int64
		var success int
		if err := rows.Scan(&e.ID, &ts, &e.UserID, &e.Username, &e.Role, &e.IP, &e.Actor,
			&e.Action, &e.Target, &e.Method, &e.Path, &e.Status, &success, &e.Detail); err != nil {
			return nil, 0, err
		}
		e.TS = time.Unix(ts, 0).UTC()
		e.Success = success == 1
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// Detail renders structured context as compact JSON for the detail column.
func Detail(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
