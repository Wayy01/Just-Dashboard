PRAGMA foreign_keys = ON;

CREATE TABLE users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL,
  totp_secret TEXT NOT NULL DEFAULT '',
  totp_enabled INTEGER NOT NULL DEFAULT 0,
  disabled INTEGER NOT NULL DEFAULT 0,
  must_change_pw INTEGER NOT NULL DEFAULT 0,
  failed_count INTEGER NOT NULL DEFAULT 0,
  locked_until INTEGER NOT NULL DEFAULT 0,
  last_login_at INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);

CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  twofa_passed INTEGER NOT NULL DEFAULT 0,
  ip TEXT NOT NULL DEFAULT '',
  user_agent TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);

CREATE TABLE api_tokens (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  prefix TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  role TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL DEFAULT 0,
  last_used_at INTEGER NOT NULL DEFAULT 0,
  revoked INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE audit_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  user_id INTEGER NOT NULL DEFAULT 0,
  username TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL DEFAULT '',
  ip TEXT NOT NULL DEFAULT '',
  actor TEXT NOT NULL DEFAULT 'session',
  action TEXT NOT NULL,
  target TEXT NOT NULL DEFAULT '',
  method TEXT NOT NULL DEFAULT '',
  path TEXT NOT NULL DEFAULT '',
  status INTEGER NOT NULL DEFAULT 0,
  success INTEGER NOT NULL DEFAULT 1,
  detail TEXT NOT NULL DEFAULT ''
);

CREATE TABLE backup_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  sources TEXT NOT NULL,
  excludes TEXT NOT NULL DEFAULT '',
  target_kind TEXT NOT NULL,
  target_cfg TEXT NOT NULL DEFAULT '{}',
  secrets_enc TEXT NOT NULL DEFAULT '',
  schedule TEXT NOT NULL DEFAULT '',
  retention INTEGER NOT NULL DEFAULT 7,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL
);

CREATE TABLE backup_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id INTEGER NOT NULL REFERENCES backup_jobs(id) ON DELETE CASCADE,
  started_at INTEGER NOT NULL,
  ended_at INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  artifact TEXT NOT NULL DEFAULT '',
  size_bytes INTEGER NOT NULL DEFAULT 0,
  log TEXT NOT NULL DEFAULT '',
  trigger TEXT NOT NULL DEFAULT 'manual'
);

CREATE TABLE deploy_projects (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  repo_path TEXT NOT NULL,
  branch TEXT NOT NULL DEFAULT 'main',
  compose_file TEXT NOT NULL DEFAULT 'docker-compose.yml',
  pre_command TEXT NOT NULL DEFAULT '',
  post_command TEXT NOT NULL DEFAULT '',
  hook_secret TEXT NOT NULL,
  hook_id TEXT NOT NULL UNIQUE,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL
);

CREATE TABLE deploy_env (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id INTEGER NOT NULL REFERENCES deploy_projects(id) ON DELETE CASCADE,
  key TEXT NOT NULL,
  value_enc TEXT NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE(project_id, key)
);

CREATE TABLE deploy_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id INTEGER NOT NULL REFERENCES deploy_projects(id) ON DELETE CASCADE,
  started_at INTEGER NOT NULL,
  ended_at INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  trigger TEXT NOT NULL DEFAULT 'manual',
  actor TEXT NOT NULL DEFAULT '',
  from_commit TEXT NOT NULL DEFAULT '',
  to_commit TEXT NOT NULL DEFAULT '',
  log TEXT NOT NULL DEFAULT ''
);

CREATE TABLE watched_domains (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  domain TEXT NOT NULL UNIQUE,
  port INTEGER NOT NULL DEFAULT 443,
  created_at INTEGER NOT NULL
);

CREATE TABLE metric_samples (
  ts INTEGER PRIMARY KEY,
  cpu_percent REAL NOT NULL DEFAULT 0,
  load1 REAL NOT NULL DEFAULT 0,
  mem_percent REAL NOT NULL DEFAULT 0,
  mem_used INTEGER NOT NULL DEFAULT 0,
  mem_total INTEGER NOT NULL DEFAULT 0,
  swap_percent REAL NOT NULL DEFAULT 0,
  net_rx REAL NOT NULL DEFAULT 0,
  net_tx REAL NOT NULL DEFAULT 0,
  disk_read REAL NOT NULL DEFAULT 0,
  disk_write REAL NOT NULL DEFAULT 0,
  disk_percent REAL NOT NULL DEFAULT 0,
  uptime_seconds INTEGER NOT NULL DEFAULT 0,
  cpu_user REAL NOT NULL DEFAULT 0,
  cpu_system REAL NOT NULL DEFAULT 0,
  cpu_iowait REAL NOT NULL DEFAULT 0,
  cpu_steal REAL NOT NULL DEFAULT 0,
  psi_cpu REAL NOT NULL DEFAULT 0,
  psi_mem REAL NOT NULL DEFAULT 0,
  psi_io REAL NOT NULL DEFAULT 0,
  disk_reads REAL NOT NULL DEFAULT 0,
  disk_writes REAL NOT NULL DEFAULT 0,
  disk_await REAL NOT NULL DEFAULT 0,
  disk_busy REAL NOT NULL DEFAULT 0,
  tcp_conns INTEGER NOT NULL DEFAULT 0,
  tcp_timewait INTEGER NOT NULL DEFAULT 0,
  load5 REAL NOT NULL DEFAULT 0,
  load15 REAL NOT NULL DEFAULT 0,
  mem_available INTEGER NOT NULL DEFAULT 0,
  procs INTEGER NOT NULL DEFAULT 0
);

INSERT INTO users(id, username, password_hash, role, totp_secret, totp_enabled,
  created_at) VALUES(1, 'fixture-admin', 'fixture-hash', 'admin',
  'fixture-sealed-totp', 1, 1699999000);
INSERT INTO sessions(id, user_id, token_hash, twofa_passed, ip, created_at,
  last_seen_at, expires_at) VALUES('fixture-session', 1, 'fixture-session-hash',
  1, '127.0.0.1', 1700000000, 1700000100, 1800000000);
INSERT INTO api_tokens(id, user_id, name, prefix, token_hash, role, created_at)
  VALUES(1, 1, 'fixture-ci', 'jd_fixture', 'fixture-token-hash', 'limited', 1700000000);
INSERT INTO audit_log(id, ts, user_id, username, role, action, target, method,
  path, status, success) VALUES(1, 1700000200, 1, 'fixture-admin', 'admin',
  'deploy.run', 'alpha', 'POST', '/api/v1/deploy/1/run', 202, 1);

INSERT INTO backup_jobs(id, name, sources, target_kind, target_cfg, secrets_enc,
  created_at) VALUES(1, 'fixture-backup', '/srv/alpha/data', 'local', '{}',
  '4ed+JBBbqMBUnn/kuwgnURwcjnSO6fPLgB1KEiytvxlkBsHLJmzkoX1KIgFdxscQQA==',
  1700000000);
INSERT INTO backup_runs(id, job_id, started_at, ended_at, status, artifact,
  size_bytes, log) VALUES(1, 1, 1700000250, 1700000260, 'success',
  '/backup/fixture.tar', 2048, 'fixture backup complete');

INSERT INTO deploy_projects(id, name, repo_path, branch, compose_file,
  pre_command, post_command, hook_secret, hook_id, enabled, created_at) VALUES
  (1, 'alpha', '/srv/alpha', 'main', 'compose.prod.yml', 'bun run migrate',
   'bun run warm',
   'JWnhzkoOm1AUR0Df/NiPR/nowPIspgjud8m/CFkgcUlrZTKYVHSZOaIUrofH',
   'hook-alpha', 1, 1700000000),
  (2, 'beta', '/opt/beta', 'stable', 'docker-compose.yml', '', '',
   'Hha0CE8IpvdmwlLfjOvBHpUOBUUreFv9BTW5Dk7ZUAm0EJWp+xr519UQ8t0=',
   'hook-beta', 0, 1700001000);

INSERT INTO deploy_env(id, project_id, key, value_enc, updated_at) VALUES
  (1, 1, 'DATABASE_URL',
   '/I1jFc7WzBgwn+ph525oSTDYNaqxaTb6NbBQdOQaoE+3u8XfDFDfBZanUM45lbosMO/WPy4um9kq',
   1700000100),
  (2, 1, 'API_KEY',
   '/auYAzdJ0C8CpXmZgMCTGr1VXJ+ZLPD/4GUMWupRya+B+BzNpESWOFWvaQ==',
   1700000110);

INSERT INTO deploy_runs(id, project_id, started_at, ended_at, status, trigger,
  actor, from_commit, to_commit, log) VALUES
  (1, 1, 1700000200, 1700000230, 'success', 'manual', 'fixture-admin',
   'aaaaaaaa', 'bbbbbbbb', 'alpha first release'),
  (2, 1, 1700000300, 1700000310, 'failed', 'webhook', 'hook',
   'bbbbbbbb', 'cccccccc', 'alpha failed build'),
  (3, 1, 1700000400, 1700000420, 'success', 'rollback', 'fixture-admin',
   'bbbbbbbb', 'aaaaaaaa', 'alpha rollback'),
  (4, 2, 1700001100, 0, 'running', 'webhook', 'hook',
   'dddddddd', '', 'beta was interrupted');

INSERT INTO watched_domains(id, domain, port, created_at)
  VALUES(1, 'fixture.example.test', 443, 1700000000);
INSERT INTO metric_samples(ts, cpu_percent, mem_percent, uptime_seconds,
  cpu_user, mem_available) VALUES(1700000200, 42.5, 51.0, 12345, 31.0, 4096);
