package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Store struct {
	DB *sql.DB
}

// DatabaseFile is the SQLite file inside the data directory. It keeps its
// original name through the "Just Dashboard" rename: moving it would buy
// nothing and would strand every existing install's accounts, audit log and
// encrypted secrets. config.Load looks for it by this name when deciding
// whether a pre-rename data directory should be adopted.
const DatabaseFile = "vpsd.db"

const schema = `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS users (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  username       TEXT NOT NULL UNIQUE,
  password_hash  TEXT NOT NULL,
  role           TEXT NOT NULL,
  totp_secret    TEXT NOT NULL DEFAULT '',
  totp_enabled   INTEGER NOT NULL DEFAULT 0,
  disabled       INTEGER NOT NULL DEFAULT 0,
  must_change_pw INTEGER NOT NULL DEFAULT 0,
  failed_count   INTEGER NOT NULL DEFAULT 0,
  locked_until   INTEGER NOT NULL DEFAULT 0,
  last_login_at  INTEGER NOT NULL DEFAULT 0,
  created_at     INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS recovery_codes (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code_hash  TEXT NOT NULL,
  used_at    INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS sessions (
  id           TEXT PRIMARY KEY,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash   TEXT NOT NULL UNIQUE,
  twofa_passed INTEGER NOT NULL DEFAULT 0,
  ip           TEXT NOT NULL DEFAULT '',
  user_agent   TEXT NOT NULL DEFAULT '',
  created_at   INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

CREATE TABLE IF NOT EXISTS api_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  prefix       TEXT NOT NULL,
  token_hash   TEXT NOT NULL UNIQUE,
  role         TEXT NOT NULL,
  created_at   INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL DEFAULT 0,
  last_used_at INTEGER NOT NULL DEFAULT 0,
  revoked      INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS audit_log (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  ts        INTEGER NOT NULL,
  user_id   INTEGER NOT NULL DEFAULT 0,
  username  TEXT NOT NULL DEFAULT '',
  role      TEXT NOT NULL DEFAULT '',
  ip        TEXT NOT NULL DEFAULT '',
  actor     TEXT NOT NULL DEFAULT 'session',
  action    TEXT NOT NULL,
  target    TEXT NOT NULL DEFAULT '',
  method    TEXT NOT NULL DEFAULT '',
  path      TEXT NOT NULL DEFAULT '',
  status    INTEGER NOT NULL DEFAULT 0,
  success   INTEGER NOT NULL DEFAULT 1,
  detail    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_audit_ts ON audit_log(ts DESC);
CREATE INDEX IF NOT EXISTS idx_audit_user ON audit_log(user_id);

CREATE TABLE IF NOT EXISTS db_connections (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT NOT NULL UNIQUE,
  driver     TEXT NOT NULL,
  dsn_enc    TEXT NOT NULL,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS backup_jobs (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL UNIQUE,
  sources     TEXT NOT NULL,
  excludes    TEXT NOT NULL DEFAULT '',
  target_kind TEXT NOT NULL,
  target_cfg  TEXT NOT NULL DEFAULT '{}',
  secrets_enc TEXT NOT NULL DEFAULT '',
  schedule    TEXT NOT NULL DEFAULT '',
  retention   INTEGER NOT NULL DEFAULT 7,
  enabled     INTEGER NOT NULL DEFAULT 1,
  created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS backup_runs (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id     INTEGER NOT NULL REFERENCES backup_jobs(id) ON DELETE CASCADE,
  started_at INTEGER NOT NULL,
  ended_at   INTEGER NOT NULL DEFAULT 0,
  status     TEXT NOT NULL,
  artifact   TEXT NOT NULL DEFAULT '',
  size_bytes INTEGER NOT NULL DEFAULT 0,
  log        TEXT NOT NULL DEFAULT '',
  trigger    TEXT NOT NULL DEFAULT 'manual'
);
CREATE INDEX IF NOT EXISTS idx_backup_runs_job ON backup_runs(job_id, started_at DESC);

CREATE TABLE IF NOT EXISTS deploy_projects (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  name          TEXT NOT NULL UNIQUE,
  repo_path     TEXT NOT NULL,
  branch        TEXT NOT NULL DEFAULT 'main',
  compose_file  TEXT NOT NULL DEFAULT 'docker-compose.yml',
  pre_command   TEXT NOT NULL DEFAULT '',
  post_command  TEXT NOT NULL DEFAULT '',
  hook_secret   TEXT NOT NULL,
  hook_id       TEXT NOT NULL UNIQUE,
  enabled       INTEGER NOT NULL DEFAULT 1,
  created_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS deploy_env (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id INTEGER NOT NULL REFERENCES deploy_projects(id) ON DELETE CASCADE,
  key        TEXT NOT NULL,
  value_enc  TEXT NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE(project_id, key)
);

CREATE TABLE IF NOT EXISTS deploy_runs (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id   INTEGER NOT NULL REFERENCES deploy_projects(id) ON DELETE CASCADE,
  started_at   INTEGER NOT NULL,
  ended_at     INTEGER NOT NULL DEFAULT 0,
  status       TEXT NOT NULL,
  trigger      TEXT NOT NULL DEFAULT 'manual',
  actor        TEXT NOT NULL DEFAULT '',
  from_commit  TEXT NOT NULL DEFAULT '',
  to_commit    TEXT NOT NULL DEFAULT '',
  log          TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_deploy_runs_project ON deploy_runs(project_id, started_at DESC);

CREATE TABLE IF NOT EXISTS watched_domains (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  domain     TEXT NOT NULL UNIQUE,
  port       INTEGER NOT NULL DEFAULT 443,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- Host utilisation, sampled by the server on its own timer.
--
-- The charts used to exist only inside the browser tab that drew them, so the
-- history began when the page was opened and died with it: a spike at 03:00
-- was unobservable unless somebody happened to be watching at 03:00. That is
-- the opposite of what a monitoring page is for. Sampling here, driven by the
-- backend rather than by a client, is what makes "what happened while I was
-- asleep" answerable at all.
--
-- ts is the rowid, so range scans over a window are a b-tree walk and a
-- restarted sampler landing on the same second replaces rather than
-- duplicates. Rows are pruned past JD_METRICS_RETENTION.
-- Per-filesystem capacity, recorded by the same sampler.
--
-- The host table keeps only the fullest mount, which is a summary and not an
-- answer: when the fullest filesystem stops being the fullest, that single
-- line drops by whatever separates it from the runner-up, and reads as
-- somebody having freed a great deal of space on a disk that did not change.
-- Which filesystem grew is the actual question, so each one gets its own row.
--
-- Cardinality is small on purpose: pseudo filesystems are filtered out before
-- this is written, so a machine records a handful of rows per sample, not one
-- per cgroup mount.
CREATE TABLE IF NOT EXISTS metric_mount_samples (
  ts           INTEGER NOT NULL,
  mountpoint   TEXT NOT NULL,
  used_percent REAL NOT NULL DEFAULT 0,
  used_bytes   INTEGER NOT NULL DEFAULT 0,
  total_bytes  INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (mountpoint, ts)
);
-- Pruning walks by time across every mount, which the primary key cannot serve.
CREATE INDEX IF NOT EXISTS idx_mount_samples_ts ON metric_mount_samples(ts);

-- Per-container utilisation, recorded by the same sampler.
--
-- Keyed by container name rather than id on purpose: a compose redeploy
-- replaces a container with a new id under the same name, and seeing across
-- that restart is most of the reason to keep the history. An id would start a
-- fresh, empty series every deploy — exactly the amnesia this table exists to
-- cure.
CREATE TABLE IF NOT EXISTS metric_container_samples (
  ts          INTEGER NOT NULL,
  name        TEXT NOT NULL,
  cpu_percent REAL NOT NULL DEFAULT 0,
  mem_bytes   INTEGER NOT NULL DEFAULT 0,
  mem_limit   INTEGER NOT NULL DEFAULT 0,
  mem_percent REAL NOT NULL DEFAULT 0,
  net_rx      INTEGER NOT NULL DEFAULT 0,
  net_tx      INTEGER NOT NULL DEFAULT 0,
  pids        INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (name, ts)
);
-- Pruning walks by time across every container, which the primary key (name
-- first) cannot serve.
CREATE INDEX IF NOT EXISTS idx_container_samples_ts ON metric_container_samples(ts);

CREATE TABLE IF NOT EXISTS metric_samples (
  ts             INTEGER PRIMARY KEY,
  cpu_percent    REAL NOT NULL DEFAULT 0,
  load1          REAL NOT NULL DEFAULT 0,
  mem_percent    REAL NOT NULL DEFAULT 0,
  mem_used       INTEGER NOT NULL DEFAULT 0,
  mem_total      INTEGER NOT NULL DEFAULT 0,
  swap_percent   REAL NOT NULL DEFAULT 0,
  net_rx         REAL NOT NULL DEFAULT 0,
  net_tx         REAL NOT NULL DEFAULT 0,
  disk_read      REAL NOT NULL DEFAULT 0,
  disk_write     REAL NOT NULL DEFAULT 0,
  disk_percent   REAL NOT NULL DEFAULT 0,
  uptime_seconds INTEGER NOT NULL DEFAULT 0
);
`

// addedColumns are columns that arrived after the table they belong to had
// already shipped.
//
// A CREATE TABLE IF NOT EXISTS is a no-op against a database that predates
// them, so every one of these has to be applied separately — this is what
// "schema changes must be additive" means in practice, given there is no
// migration tool. Each entry must therefore carry a DEFAULT: an existing table
// has rows, and SQLite will not add a NOT NULL column to them without one.
//
// Never remove an entry. It is not a list of the current schema, it is the
// list of steps between every shipped schema and the current one, and dropping
// one strands whichever installs stopped at that version.
var addedColumns = []struct{ table, column, spec string }{
	// The CPU mode breakdown, added because one "busy" percentage cannot tell
	// apart a server doing work, a server waiting on a disk, and a hypervisor
	// running somebody else on the core you are paying for.
	{"metric_samples", "cpu_user", "REAL NOT NULL DEFAULT 0"},
	{"metric_samples", "cpu_system", "REAL NOT NULL DEFAULT 0"},
	{"metric_samples", "cpu_iowait", "REAL NOT NULL DEFAULT 0"},
	{"metric_samples", "cpu_steal", "REAL NOT NULL DEFAULT 0"},

	// Pressure stall information: the kernel's own measure of whether tasks
	// are waiting rather than running, which utilisation cannot express.
	{"metric_samples", "psi_cpu", "REAL NOT NULL DEFAULT 0"},
	{"metric_samples", "psi_mem", "REAL NOT NULL DEFAULT 0"},
	{"metric_samples", "psi_io", "REAL NOT NULL DEFAULT 0"},

	// Operations per second and service time. A disk saturated by small
	// random writes moves almost no bytes, so the byte rates alone report an
	// idle disk that is in fact unable to take another request.
	{"metric_samples", "disk_reads", "REAL NOT NULL DEFAULT 0"},
	{"metric_samples", "disk_writes", "REAL NOT NULL DEFAULT 0"},
	{"metric_samples", "disk_await", "REAL NOT NULL DEFAULT 0"},
	{"metric_samples", "disk_busy", "REAL NOT NULL DEFAULT 0"},

	// Sockets, so "we ran out of connections at 3am" leaves a trace.
	{"metric_samples", "tcp_conns", "INTEGER NOT NULL DEFAULT 0"},
	{"metric_samples", "tcp_timewait", "INTEGER NOT NULL DEFAULT 0"},

	// The other two load averages. One-minute load alone cannot distinguish a
	// spike that is ending from one that is building.
	{"metric_samples", "load5", "REAL NOT NULL DEFAULT 0"},
	{"metric_samples", "load15", "REAL NOT NULL DEFAULT 0"},
	{"metric_samples", "mem_available", "INTEGER NOT NULL DEFAULT 0"},
	{"metric_samples", "procs", "INTEGER NOT NULL DEFAULT 0"},

	// A container's network and block throughput, which was already being
	// sampled for the live view but never kept.
	{"metric_container_samples", "block_read", "INTEGER NOT NULL DEFAULT 0"},
	{"metric_container_samples", "block_write", "INTEGER NOT NULL DEFAULT 0"},

	// Inode exhaustion fills a filesystem that reports free space, and is
	// invisible in a used-bytes percentage.
	{"metric_mount_samples", "inodes_percent", "REAL NOT NULL DEFAULT 0"},
}

// applyAddedColumns adds any column the running binary expects and the file on
// disk does not have.
//
// It reads the table's current shape rather than trying the ALTER and
// swallowing the error: "duplicate column name" is a string comparison against
// a message SQLite is free to reword, and a real failure would look identical.
func applyAddedColumns(ctx context.Context, db *sql.DB) error {
	have := map[string]map[string]bool{}
	for _, c := range addedColumns {
		if have[c.table] != nil {
			continue
		}
		cols, err := tableColumns(ctx, db, c.table)
		if err != nil {
			return fmt.Errorf("read %s columns: %w", c.table, err)
		}
		have[c.table] = cols
	}
	for _, c := range addedColumns {
		if have[c.table][c.column] {
			continue
		}
		// The table name is a constant in this file, never user input, so
		// there is nothing here to parameterise — and SQLite does not accept
		// a placeholder for an identifier in any case.
		stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", c.table, c.column, c.spec)
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("add %s.%s: %w", c.table, c.column, err)
		}
		have[c.table][c.column] = true
	}
	return nil
}

func tableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(dataDir, DatabaseFile)
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// SQLite tolerates a single writer; the pool is kept small so the WAL
	// writer never contends with itself under WebSocket fan-out.
	db.SetMaxOpenConns(4)
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	// Run after the schema, never instead of it: a fresh database gets its
	// tables from the block above and finds nothing to add, while an existing
	// one gets only the columns it is missing.
	if err := applyAddedColumns(context.Background(), db); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{DB: db}, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) Setting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}
