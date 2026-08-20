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
