package store_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/deploy"
	"github.com/Wayy01/Just-Dashboard/backend/internal/store"
)

const fixtureMasterKey = "abababababababababababababababababababababababababababababababab"

func TestOpenMigratesPopulated066DeploymentsIdempotently(t *testing.T) {
	dir := install066Fixture(t)
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("open populated 0.6.6 fixture: %v", err)
	}

	assertCount(t, st.DB, "deploy_projects", 2)
	assertCount(t, st.DB, "deploy_environments", 2)
	assertCount(t, st.DB, "deploy_sources", 2)
	assertCount(t, st.DB, "deploy_build_plans", 2)
	assertCount(t, st.DB, "deploy_runtime_plans", 2)
	assertCount(t, st.DB, "deploy_variable_revisions", 2)
	assertCount(t, st.DB, "deploy_run_variable_snapshots", 4)
	assertCount(t, st.DB, "deploy_run_variable_revisions", 6)
	assertCount(t, st.DB, "deploy_run_plan_snapshots", 4)
	assertCount(t, st.DB, "deploy_triggers", 2)
	assertCount(t, st.DB, "deploy_steps", 4)
	assertCount(t, st.DB, "deploy_log_chunks", 4)
	assertCount(t, st.DB, "deploy_releases", 2)
	assertCount(t, st.DB, "deploy_release_artifacts", 2)

	var environmentID, liveReleaseID int64
	var environmentName, kind, strategy string
	err = st.DB.QueryRow(`
		SELECT id, name, kind, strategy, live_release_id
		  FROM deploy_environments
		 WHERE project_id = 1 AND slug = 'production'`).
		Scan(&environmentID, &environmentName, &kind, &strategy, &liveReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if environmentName != "Production" || kind != "production" || strategy != "stop_first" {
		t.Fatalf("production environment = %q/%q/%q", environmentName, kind, strategy)
	}

	var sourceKind, sourceConfig, buildMethod, runtimeConfig string
	if err := st.DB.QueryRow(`
		SELECT s.kind, s.config_json, b.method, r.config_json
		  FROM deploy_sources s
		  JOIN deploy_build_plans b ON b.environment_id = s.environment_id AND b.revision = s.revision
		  JOIN deploy_runtime_plans r ON r.environment_id = s.environment_id AND r.revision = s.revision
		 WHERE s.environment_id = ? AND s.revision = 1`, environmentID).
		Scan(&sourceKind, &sourceConfig, &buildMethod, &runtimeConfig); err != nil {
		t.Fatal(err)
	}
	if sourceKind != "local" || buildMethod != "legacy_compose" {
		t.Fatalf("legacy source/build = %q/%q", sourceKind, buildMethod)
	}
	for _, want := range []string{`"path":"/srv/alpha"`, `"ref":"main"`, `"managedInPlace":true`} {
		if !strings.Contains(sourceConfig, want) {
			t.Errorf("source config %s does not contain %s", sourceConfig, want)
		}
	}
	for _, want := range []string{`"compose.prod.yml"`, `"bun run migrate"`, `"bun run warm"`} {
		if !strings.Contains(runtimeConfig, want) {
			t.Errorf("runtime config %s does not contain %s", runtimeConfig, want)
		}
	}

	// Ciphertext is copied, not decrypted and resealed during migration. That
	// preserves the exact operator secret even when migration code has no key.
	var copied int
	if err := st.DB.QueryRow(`
		SELECT COUNT(*)
		  FROM deploy_env old
		  JOIN deploy_variable_revisions next
		    ON next.environment_id = ? AND next.key = old.key
		   AND next.value_enc = old.value_enc
		 WHERE old.project_id = 1`, environmentID).Scan(&copied); err != nil {
		t.Fatal(err)
	}
	if copied != 2 {
		t.Fatalf("copied ciphertext rows = %d, want 2", copied)
	}
	var digestedVariables, digestedSnapshots int
	if err := st.DB.QueryRow(`
		SELECT COUNT(*) FROM deploy_variable_revisions
		 WHERE value_digest LIKE 'sha256:%' AND length(value_digest) = 71`).Scan(&digestedVariables); err != nil {
		t.Fatal(err)
	}
	if err := st.DB.QueryRow(`
		SELECT COUNT(*) FROM deploy_run_variable_snapshots
		 WHERE digest LIKE 'sha256:%' AND length(digest) = 71`).Scan(&digestedSnapshots); err != nil {
		t.Fatal(err)
	}
	if digestedVariables != 2 || digestedSnapshots != 4 {
		t.Fatalf("migrated variable digests = %d revisions / %d run snapshots", digestedVariables, digestedSnapshots)
	}

	sealer, err := auth.NewSealer(fixtureMasterKey)
	if err != nil {
		t.Fatal(err)
	}
	legacy := deploy.NewStore(st, sealer, []string{"/srv", "/opt"})
	vars, err := legacy.ListEnv(context.Background(), 1, true)
	if err != nil {
		t.Fatalf("reveal migrated legacy variables: %v", err)
	}
	values := map[string]string{}
	for _, variable := range vars {
		values[variable.Key] = variable.Value
	}
	if values["DATABASE_URL"] != "postgres://app:fixture@db/app" ||
		values["API_KEY"] != "fixture-api-key" {
		t.Fatalf("legacy secrets did not decrypt after migration: %#v", values)
	}
	verifyLegacyHook(t, legacy, 1, "alpha-hook-secret")

	expectedRuns := []struct {
		id, release int64
		state, op   string
	}{
		{1, 1, "succeeded", "deploy"},
		{2, 0, "failed", "deploy"},
		{3, 2, "succeeded", "rollback"},
		{4, 0, "running", "deploy"},
	}
	for _, want := range expectedRuns {
		var state, operation, originalStatus string
		var envID, releaseID, requestedAt int64
		if err := st.DB.QueryRow(`
			SELECT state, operation, status, environment_id, release_id, requested_at
			  FROM deploy_runs WHERE id = ?`, want.id).
			Scan(&state, &operation, &originalStatus, &envID, &releaseID, &requestedAt); err != nil {
			t.Fatal(err)
		}
		if state != want.state || operation != want.op || releaseID != want.release || envID == 0 || requestedAt == 0 {
			t.Errorf("run %d = state %q op %q env %d release %d requested %d",
				want.id, state, operation, envID, releaseID, requestedAt)
		}
		if originalStatus == "" {
			t.Errorf("run %d lost its 0.6.6 status", want.id)
		}
	}

	var liveRevision, liveState string
	if err := st.DB.QueryRow(`
		SELECT source_revision, state FROM deploy_releases WHERE id = ?`, liveReleaseID).
		Scan(&liveRevision, &liveState); err != nil {
		t.Fatal(err)
	}
	if liveRevision != "aaaaaaaa" || liveState != "live" {
		t.Fatalf("live migrated release = %q/%q, want rollback commit/live", liveRevision, liveState)
	}
	var predecessor int64
	if err := st.DB.QueryRow(`SELECT predecessor_release_id FROM deploy_releases WHERE id = 2`).
		Scan(&predecessor); err != nil {
		t.Fatal(err)
	}
	if predecessor != 1 {
		t.Fatalf("rollback predecessor = %d, want release 1", predecessor)
	}
	var legacyArtifactState, legacyArtifactDigest, legacyArtifactMetadata string
	if err := st.DB.QueryRow(`
		SELECT state, digest, metadata_json
		  FROM deploy_release_artifacts
		 WHERE release_id = ? AND kind = 'compose'`, liveReleaseID).
		Scan(&legacyArtifactState, &legacyArtifactDigest, &legacyArtifactMetadata); err != nil {
		t.Fatal(err)
	}
	if legacyArtifactState != "unavailable" || legacyArtifactDigest != "" ||
		!strings.Contains(legacyArtifactMetadata, `"digestUnavailable":true`) ||
		!strings.Contains(legacyArtifactMetadata, "legacy runs did not record") {
		t.Fatalf("legacy release artifact = state %q digest %q metadata %s",
			legacyArtifactState, legacyArtifactDigest, legacyArtifactMetadata)
	}

	var oldLog, eventLog string
	if err := st.DB.QueryRow(`
		SELECT r.log, e.text
		  FROM deploy_runs r
		  JOIN deploy_log_chunks e ON e.run_id = r.id AND e.seq = 1
		 WHERE r.id = 2`).Scan(&oldLog, &eventLog); err != nil {
		t.Fatal(err)
	}
	if oldLog != "alpha failed build" || eventLog != oldLog {
		t.Fatalf("legacy transcript = %q / migrated event = %q", oldLog, eventLog)
	}

	for table, want := range map[string]int{
		"users": 1, "sessions": 1, "api_tokens": 1, "audit_log": 1,
		"backup_jobs": 1, "backup_runs": 1, "watched_domains": 1, "metric_samples": 1,
	} {
		assertCount(t, st.DB, table, want)
	}
	assertNoForeignKeyFailures(t, st.DB)

	countsBefore := normalizedCounts(t, st.DB)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(dir)
	if err != nil {
		t.Fatalf("second open after migration: %v", err)
	}
	defer st.Close()
	countsAfter := normalizedCounts(t, st.DB)
	for table, before := range countsBefore {
		if countsAfter[table] != before {
			t.Errorf("second open changed %s rows from %d to %d", table, before, countsAfter[table])
		}
	}
	assertNoForeignKeyFailures(t, st.DB)
}

func install066Fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script, err := os.ReadFile(filepath.Join("testdata", "0.6.6.sql"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, store.DatabaseFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(script)); err != nil {
		db.Close()
		t.Fatalf("install 0.6.6 fixture: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func verifyLegacyHook(t *testing.T, st *deploy.Store, projectID int64, secret string) {
	t.Helper()
	body := []byte(`{"fixture":true}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if err := st.VerifySignature(context.Background(), projectID, body, signature); err != nil {
		t.Fatalf("legacy hook no longer verifies: %v", err)
	}
}

func normalizedCounts(t *testing.T, db *sql.DB) map[string]int {
	t.Helper()
	tables := []string{
		"deploy_environments", "deploy_sources", "deploy_build_plans",
		"deploy_runtime_plans", "deploy_variable_revisions", "deploy_triggers",
		"deploy_run_variable_snapshots", "deploy_run_variable_revisions",
		"deploy_run_plan_snapshots",
		"deploy_steps", "deploy_log_chunks", "deploy_releases",
		"deploy_release_artifacts",
	}
	out := make(map[string]int, len(tables))
	for _, table := range tables {
		out[table] = rowCount(t, db, table)
	}
	return out
}

func assertCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	if got := rowCount(t, db, table); got != want {
		t.Errorf("%s rows = %d, want %d", table, got, want)
	}
}

func rowCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	// Every table name comes from a closed literal in this test, not fixture data.
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func assertNoForeignKeyFailures(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		var table string
		var rowID int64
		var parent string
		var foreignKey int
		if err := rows.Scan(&table, &rowID, &parent, &foreignKey); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("foreign key failure: %s row %d -> %s (%d)", table, rowID, parent, foreignKey)
	}
}
