package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// migrateLegacyDeployments maps every shipped 0.6.6 project into the
// normalized deployment model without replacing the rows old clients and
// metrics joins still address. Unique environment, plan, run, and trigger
// constraints are the migration markers, so an interrupted boot can rerun the
// whole transaction rather than trying to infer how far it got.
func migrateLegacyDeployments(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	type legacyProject struct {
		id                            int64
		repoPath, branch, composeFile string
		preCommand, postCommand       string
		hookSecret, hookID            string
		enabled, createdAt            int64
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, repo_path, branch, compose_file, pre_command, post_command,
		       hook_secret, hook_id, enabled, created_at
		  FROM deploy_projects
		 ORDER BY id`)
	if err != nil {
		return fmt.Errorf("list legacy deploy projects: %w", err)
	}
	var projects []legacyProject
	for rows.Next() {
		var p legacyProject
		if err := rows.Scan(&p.id, &p.repoPath, &p.branch, &p.composeFile,
			&p.preCommand, &p.postCommand, &p.hookSecret, &p.hookID, &p.enabled,
			&p.createdAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan legacy deploy project: %w", err)
		}
		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, p := range projects {
		if err := migrateLegacyProject(ctx, tx, p.id, p.repoPath, p.branch,
			p.composeFile, p.preCommand, p.postCommand, p.hookSecret, p.hookID,
			p.enabled, p.createdAt); err != nil {
			return fmt.Errorf("migrate deploy project %d: %w", p.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit legacy deploy migration: %w", err)
	}
	return nil
}

// NormalizeLegacyDeployments is the compatibility hook for a 0.6.6 project
// created after startup. Open performs the same mapper for upgrades; keeping
// this operation idempotent means the legacy create route can immediately
// enqueue into the persistent engine without waiting for a restart.
func (s *Store) NormalizeLegacyDeployments(ctx context.Context) error {
	return migrateLegacyDeployments(ctx, s.DB)
}

func migrateLegacyProject(
	ctx context.Context,
	tx *sql.Tx,
	projectID int64,
	repoPath, branch, composeFile, preCommand, postCommand, hookSecret, hookID string,
	enabled, createdAt int64,
) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE deploy_projects
		   SET profile = CASE WHEN profile = '' THEN 'compose' ELSE profile END,
		       updated_at = CASE WHEN updated_at = 0 THEN created_at ELSE updated_at END
		 WHERE id = ?`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO deploy_environments(
		  project_id, name, slug, kind, desired_revision, strategy, expected_downtime,
		  protected, created_at, updated_at)
		VALUES(?, 'Production', 'production', 'production', 1, 'stop_first', 1, 1, ?, ?)
		ON CONFLICT(project_id, slug) DO NOTHING`, projectID, createdAt, createdAt); err != nil {
		return err
	}

	var environmentID, existingLiveReleaseID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id, live_release_id
		  FROM deploy_environments
		 WHERE project_id = ? AND slug = 'production'`, projectID).
		Scan(&environmentID, &existingLiveReleaseID); err != nil {
		return err
	}

	sourceConfig, err := deploymentJSON(map[string]any{
		"path": repoPath, "ref": branch, "managedInPlace": true, "legacy": true,
	})
	if err != nil {
		return err
	}
	buildConfig, err := deploymentJSON(map[string]any{
		"composeFiles": []string{composeFile}, "legacy": true,
	})
	if err != nil {
		return err
	}
	runtimeConfig, err := deploymentJSON(map[string]any{
		"composeFiles": []string{composeFile}, "preCommand": preCommand,
		"postCommand": postCommand, "legacy": true,
	})
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO deploy_sources(environment_id, revision, kind, config_json, created_at)
		VALUES(?, 1, 'local', ?, ?)
		ON CONFLICT(environment_id, revision) DO NOTHING`,
		environmentID, sourceConfig, createdAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO deploy_build_plans(
		  environment_id, revision, method, config_json, evidence_json, created_at)
		VALUES(?, 1, 'legacy_compose', ?,
		       '[{"kind":"migration","detail":"Imported from 0.6.6"}]', ?)
		ON CONFLICT(environment_id, revision) DO NOTHING`,
		environmentID, buildConfig, createdAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO deploy_runtime_plans(environment_id, revision, config_json, created_at)
		VALUES(?, 1, ?, ?)
		ON CONFLICT(environment_id, revision) DO NOTHING`,
		environmentID, runtimeConfig, createdAt); err != nil {
		return err
	}

	var sourceID, buildPlanID, runtimePlanID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM deploy_sources WHERE environment_id = ? AND revision = 1`,
		environmentID).Scan(&sourceID); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM deploy_build_plans WHERE environment_id = ? AND revision = 1`,
		environmentID).Scan(&buildPlanID); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM deploy_runtime_plans WHERE environment_id = ? AND revision = 1`,
		environmentID).Scan(&runtimePlanID); err != nil {
		return err
	}

	if err := migrateLegacyVariables(ctx, tx, projectID, environmentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO deploy_triggers(
		  environment_id, name, kind, config_json, secret_enc, hook_id, enabled,
		  created_at, updated_at)
		VALUES(?, 'Legacy deployment hook', 'legacy_hook', '{}', ?, ?, ?, ?, ?)
		ON CONFLICT(environment_id, kind, name) DO NOTHING`,
		environmentID, hookSecret, hookID, enabled, createdAt, createdAt); err != nil {
		return err
	}

	liveReleaseID, err := migrateLegacyRuns(ctx, tx, projectID, environmentID,
		sourceID, buildPlanID, runtimePlanID)
	if err != nil {
		return err
	}
	// A compatibility run appearing after a normalized release must not turn
	// into a second live release. On a pure 0.6.6 upgrade the pointer is zero,
	// and the latest successful legacy run becomes the live release exactly once.
	if liveReleaseID != 0 && existingLiveReleaseID == 0 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE deploy_releases SET state = 'live' WHERE id = ?`, liveReleaseID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE deploy_environments
			   SET live_release_id = ?, updated_at = ?
			 WHERE id = ? AND live_release_id = 0`,
			liveReleaseID, createdAt, environmentID); err != nil {
			return err
		}
	}
	return nil
}

func migrateLegacyVariables(ctx context.Context, tx *sql.Tx, projectID, environmentID int64) error {
	type legacyVariable struct {
		key, value string
		updatedAt  int64
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT key, value_enc, updated_at
		  FROM deploy_env
		 WHERE project_id = ?
		 ORDER BY key`, projectID)
	if err != nil {
		return err
	}
	var variables []legacyVariable
	for rows.Next() {
		var v legacyVariable
		if err := rows.Scan(&v.key, &v.value, &v.updatedAt); err != nil {
			rows.Close()
			return err
		}
		variables = append(variables, v)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, v := range variables {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO deploy_variable_revisions(
			  environment_id, key, revision, sensitivity, scopes, value_enc, active,
			  value_digest, created_by, created_at)
			VALUES(?, ?, 1, 'secret', 'runtime', ?, 1, ?, 'migration', ?)
			ON CONFLICT(environment_id, key, revision) DO NOTHING`,
			environmentID, v.key, v.value, deploymentContentDigest([]byte(v.value)), v.updatedAt); err != nil {
			return err
		}
	}
	return nil
}

func migrateLegacyRuns(
	ctx context.Context,
	tx *sql.Tx,
	projectID, environmentID, sourceID, buildPlanID, runtimePlanID int64,
) (int64, error) {
	type legacyRun struct {
		id, startedAt, endedAt    int64
		status, trigger, actor    string
		fromCommit, toCommit, log string
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, started_at, ended_at, status, trigger, actor, from_commit, to_commit, log
		  FROM deploy_runs
		 WHERE project_id = ? AND (environment_id = 0 OR state = '')
		 ORDER BY started_at, id`, projectID)
	if err != nil {
		return 0, err
	}
	var runs []legacyRun
	for rows.Next() {
		var r legacyRun
		if err := rows.Scan(&r.id, &r.startedAt, &r.endedAt, &r.status, &r.trigger,
			&r.actor, &r.fromCommit, &r.toCommit, &r.log); err != nil {
			rows.Close()
			return 0, err
		}
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	var releaseNumber, predecessorID, liveReleaseID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(release_number), 0) FROM deploy_releases WHERE environment_id = ?`,
		environmentID).Scan(&releaseNumber); err != nil {
		return 0, err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE((
		  SELECT id FROM deploy_releases
		   WHERE environment_id = ?
		   ORDER BY release_number DESC LIMIT 1
		), 0)`, environmentID).Scan(&predecessorID); err != nil {
		return 0, err
	}

	for _, r := range runs {
		state, stepState := legacyRunStates(r.status)
		operation := "deploy"
		if r.trigger == "rollback" {
			operation = "rollback"
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE deploy_runs
			   SET environment_id = ?, state = ?, operation = ?, requested_at = ?,
			       plan_revision = 1
			 WHERE id = ?`,
			environmentID, state, operation, r.startedAt, r.id); err != nil {
			return 0, err
		}
		if err := migrateLegacyRunVariableSnapshot(ctx, tx, r.id, environmentID, r.startedAt); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO deploy_run_plan_snapshots(
			  run_id, environment_id, dependencies_json, checks_json, digest,
			  copied_from_run_id, created_at)
			VALUES(?, ?, '[]', '[]', ?, 0, ?)
			ON CONFLICT(run_id) DO NOTHING`, r.id, environmentID,
			deploymentContentDigest([]byte("[]"), []byte("[]")), r.startedAt); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO deploy_steps(
			  run_id, step_key, ordinal, status, attempt, started_at, ended_at,
			  evidence_json, last_seq)
			VALUES(?, 'legacy_pipeline', 1, ?, 1, ?, ?, '{"migration":"0.6.6"}', 1)
			ON CONFLICT(run_id, step_key, attempt) DO NOTHING`,
			r.id, stepState, r.startedAt, r.endedAt); err != nil {
			return 0, err
		}
		var stepID int64
		if err := tx.QueryRowContext(ctx, `
			SELECT id FROM deploy_steps
			 WHERE run_id = ? AND step_key = 'legacy_pipeline' AND attempt = 1`,
			r.id).Scan(&stepID); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO deploy_log_chunks(
			  run_id, step_id, seq, event_type, stream, ts, text, data_json)
			VALUES(?, ?, 1, 'step.log', 'status', ?, ?, '{"migration":"0.6.6"}')
			ON CONFLICT(run_id, seq) DO NOTHING`,
			r.id, stepID, r.startedAt, r.log); err != nil {
			return 0, err
		}

		if r.status != "success" {
			continue
		}
		releaseNumber++
		revision := r.toCommit
		if revision == "" {
			revision = r.fromCommit
		}
		activatedAt := r.endedAt
		if activatedAt == 0 {
			activatedAt = r.startedAt
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO deploy_releases(
			  project_id, environment_id, release_number, run_id, predecessor_release_id,
			  state, plan_revision, source_id, build_plan_id, runtime_plan_id,
			  source_revision, source_identity_json, strategy, expected_downtime, provenance_json,
			  created_at, activated_at)
			VALUES(?, ?, ?, ?, ?, 'retained', 1, ?, ?, ?, ?, '{}', 'stop_first', 1,
			       '{"migration":"0.6.6"}', ?, ?)
			ON CONFLICT(run_id) WHERE run_id <> 0 DO NOTHING`,
			projectID, environmentID, releaseNumber, r.id, predecessorID,
			sourceID, buildPlanID, runtimePlanID, revision, r.startedAt, activatedAt); err != nil {
			return 0, err
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM deploy_releases WHERE run_id = ?`, r.id).
			Scan(&liveReleaseID); err != nil {
			return 0, err
		}
		legacyArtifactMetadata, err := deploymentJSON(map[string]any{
			"migration": "0.6.6", "digestUnavailable": true,
			"reason": "legacy runs did not record rendered Compose content or image digests",
		})
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO deploy_release_artifacts(
			  release_id, kind, reference, digest, metadata_json, state, created_at)
			VALUES(?, 'compose', ?, '', ?, 'unavailable', ?)
			ON CONFLICT(release_id, kind, digest, reference) DO NOTHING`,
			liveReleaseID, "legacy:"+fmt.Sprint(buildPlanID), legacyArtifactMetadata, r.startedAt); err != nil {
			return 0, err
		}
		predecessorID = liveReleaseID
		if _, err := tx.ExecContext(ctx,
			`UPDATE deploy_runs SET release_id = ? WHERE id = ?`, liveReleaseID, r.id); err != nil {
			return 0, err
		}
	}
	return liveReleaseID, nil
}

type migratedVariableSnapshot struct {
	Name        string `json:"name"`
	Sensitivity string `json:"sensitivity"`
	Scopes      string `json:"scopes"`
	ValueDigest string `json:"valueDigest"`
}

func migrateLegacyRunVariableSnapshot(
	ctx context.Context,
	tx *sql.Tx,
	runID, environmentID, createdAt int64,
) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, key, sensitivity, scopes, value_digest
		  FROM deploy_variable_revisions
		 WHERE environment_id = ? AND active = 1
		 ORDER BY key, id`, environmentID)
	if err != nil {
		return err
	}
	type ref struct {
		id       int64
		snapshot migratedVariableSnapshot
	}
	refs := []ref{}
	for rows.Next() {
		var current ref
		if err := rows.Scan(
			&current.id, &current.snapshot.Name, &current.snapshot.Sensitivity,
			&current.snapshot.Scopes, &current.snapshot.ValueDigest,
		); err != nil {
			rows.Close()
			return err
		}
		refs = append(refs, current)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	snapshot := make([]migratedVariableSnapshot, 0, len(refs))
	for _, current := range refs {
		snapshot = append(snapshot, current.snapshot)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO deploy_run_variable_snapshots(
		  run_id, environment_id, digest, copied_from_run_id, created_at)
		VALUES(?, ?, ?, 0, ?)
		ON CONFLICT(run_id) DO NOTHING`,
		runID, environmentID, deploymentContentDigest(raw), createdAt); err != nil {
		return err
	}
	for ordinal, current := range refs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO deploy_run_variable_revisions(run_id, variable_revision_id, ordinal)
			VALUES(?, ?, ?)
			ON CONFLICT(run_id, variable_revision_id) DO NOTHING`,
			runID, current.id, ordinal+1); err != nil {
			return err
		}
	}
	return nil
}

func deploymentContentDigest(parts ...[]byte) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write(part)
		_, _ = hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func deploymentJSON(value any) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func legacyRunStates(status string) (run, step string) {
	switch status {
	case "success":
		return "succeeded", "passed"
	case "failed":
		return "failed", "failed"
	default:
		return "running", "running"
	}
}
