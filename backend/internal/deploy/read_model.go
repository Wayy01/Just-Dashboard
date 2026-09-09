package deploy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// DeploymentSummary is the operator-facing read model. Health is the persisted
// activation-check outcome; current Docker state is a separate runtime observation.
type DeploymentSummary struct {
	ID               int64           `json:"id"`
	Name             string          `json:"name"`
	Profile          WorkloadProfile `json:"profile"`
	EnvironmentID    int64           `json:"environmentId"`
	EnvironmentName  string          `json:"environmentName"`
	EnvironmentKind  EnvironmentKind `json:"environmentKind"`
	DesiredRevision  int             `json:"desiredRevision"`
	LiveReleaseID    int64           `json:"liveReleaseId,omitempty"`
	LivePlanRevision int             `json:"livePlanRevision,omitempty"`
	Strategy         ReleaseStrategy `json:"strategy"`
	ExpectedDowntime bool            `json:"expectedDowntime"`
	SourceKind       SourceKind      `json:"sourceKind"`
	BuildMethod      BuildMethod     `json:"buildMethod"`
	SourceRef        string          `json:"sourceRef,omitempty"`
	SourceRevision   string          `json:"sourceRevision,omitempty"`
	Endpoint         string          `json:"endpoint,omitempty"`
	InternalPort     int             `json:"internalPort,omitempty"`
	HostPort         int             `json:"hostPort,omitempty"`
	Health           string          `json:"health"`
	PendingChanges   bool            `json:"pendingChanges"`
	LastRun          *EngineRun      `json:"lastRun,omitempty"`
	ActiveRun        *EngineRun      `json:"activeRun,omitempty"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

type ActiveWork struct {
	Run           EngineRun `json:"run"`
	ProjectName   string    `json:"projectName"`
	Environment   string    `json:"environment"`
	CurrentStep   StepKey   `json:"currentStep,omitempty"`
	CurrentStatus StepState `json:"currentStatus,omitempty"`
	QueuePosition int       `json:"queuePosition,omitempty"`
}

type FleetSlots struct {
	HeavyUsed     int `json:"heavyUsed"`
	HeavyCapacity int `json:"heavyCapacity"`
	LightUsed     int `json:"lightUsed"`
	LightCapacity int `json:"lightCapacity"`
}

type FleetReadModel struct {
	Deployments []DeploymentSummary `json:"deployments"`
	ActiveWork  []ActiveWork        `json:"activeWork"`
	Slots       FleetSlots          `json:"slots"`
}

const activeRunWhere = `state IN (
  'requested', 'validating', 'queued', 'preparing', 'running', 'verifying',
  'activating', 'failed_activation', 'restoring_previous', 'cancelling'
)`

// Fleet returns one production summary per unarchived deployment and the
// recoverable work currently occupying or waiting for host slots.
func (s *OrchestrationStore) Fleet(ctx context.Context, budget QueueBudget) (*FleetReadModel, error) {
	budget = budget.normalized()
	result := &FleetReadModel{
		Deployments: []DeploymentSummary{}, ActiveWork: []ActiveWork{},
		Slots: FleetSlots{HeavyCapacity: budget.Heavy, LightCapacity: budget.Light},
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.profile, p.updated_at,
		       e.id, e.name, e.kind, e.desired_revision, e.live_release_id,
		       e.strategy, e.expected_downtime,
		       COALESCE(l.plan_revision, 0),
		       COALESCE(src.kind, ''), COALESCE(src.identity_json, '{}'),
		       COALESCE(build.method, 'none'),
		       COALESCE(runtime.config_json, '{}'),
		       COALESCE((
		         SELECT d.resource_id FROM deploy_dependencies d
		          WHERE d.environment_id = e.id AND d.kind = 'domain'
		          ORDER BY d.id LIMIT 1
		       ), '')
		  FROM deploy_projects p
		  JOIN deploy_environments e ON e.project_id = p.id
		   AND e.slug = 'production' AND e.archived_at = 0
		  LEFT JOIN deploy_releases l ON l.id = e.live_release_id
		  LEFT JOIN deploy_sources src ON src.environment_id = e.id
		   AND src.revision = e.desired_revision
		  LEFT JOIN deploy_build_plans build ON build.environment_id = e.id
		   AND build.revision = e.desired_revision
		  LEFT JOIN deploy_runtime_plans runtime ON runtime.environment_id = e.id
		   AND runtime.revision = e.desired_revision
		 WHERE p.archived_at = 0
		 ORDER BY p.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var summary DeploymentSummary
		var updated int64
		var expectedDowntime int
		var identityJSON, runtimeJSON string
		if err := rows.Scan(
			&summary.ID, &summary.Name, &summary.Profile, &updated,
			&summary.EnvironmentID, &summary.EnvironmentName, &summary.EnvironmentKind,
			&summary.DesiredRevision, &summary.LiveReleaseID, &summary.Strategy,
			&expectedDowntime, &summary.LivePlanRevision, &summary.SourceKind,
			&identityJSON, &summary.BuildMethod, &runtimeJSON, &summary.Endpoint,
		); err != nil {
			return nil, err
		}
		summary.ExpectedDowntime = expectedDowntime != 0
		summary.UpdatedAt = unixTime(updated)
		if updated == 0 {
			summary.UpdatedAt = time.Time{}
		}
		var identity SourceIdentity
		if json.Unmarshal([]byte(identityJSON), &identity) == nil {
			summary.SourceRef = identity.Ref
			summary.SourceRevision = identity.Revision
			if summary.SourceRevision == "" {
				summary.SourceRevision = identity.Digest
			}
		}
		var runtime RuntimePlanConfig
		if json.Unmarshal([]byte(runtimeJSON), &runtime) == nil {
			summary.InternalPort = runtime.InternalPort
			summary.HostPort = runtime.HostPort
		}
		summary.Health = string(s.liveReleaseHealth(ctx, summary.LiveReleaseID))
		summary.PendingChanges = summary.LiveReleaseID == 0 || summary.LivePlanRevision != summary.DesiredRevision
		summary.LastRun, _ = s.latestProjectRun(ctx, summary.ID, false)
		summary.ActiveRun, _ = s.latestProjectRun(ctx, summary.ID, true)
		result.Deployments = append(result.Deployments, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	activeRows, err := s.db.QueryContext(ctx, `
		SELECT id FROM deploy_runs WHERE `+activeRunWhere+`
		 ORDER BY CASE WHEN state = 'queued' THEN 1 ELSE 0 END,
		          priority DESC, requested_at, id`)
	if err != nil {
		return nil, err
	}
	defer activeRows.Close()
	queuePosition := 0
	for activeRows.Next() {
		var runID int64
		if err := activeRows.Scan(&runID); err != nil {
			return nil, err
		}
		run, err := s.Run(ctx, runID)
		if err != nil {
			return nil, err
		}
		work := ActiveWork{Run: *run}
		if err := s.db.QueryRowContext(ctx, `
			SELECT p.name, e.name FROM deploy_projects p
			JOIN deploy_environments e ON e.project_id = p.id
			WHERE p.id = ? AND e.id = ?`, run.ProjectID, run.EnvironmentID).
			Scan(&work.ProjectName, &work.Environment); err != nil {
			return nil, err
		}
		_ = s.db.QueryRowContext(ctx, `
			SELECT step_key, status FROM deploy_steps
			 WHERE run_id = ? AND status IN ('running','blocked','failed','pending')
			 ORDER BY CASE status WHEN 'running' THEN 0 WHEN 'blocked' THEN 1
			          WHEN 'failed' THEN 2 ELSE 3 END, ordinal, attempt DESC LIMIT 1`, run.ID).
			Scan(&work.CurrentStep, &work.CurrentStatus)
		if run.State == RunQueued {
			queuePosition++
			work.QueuePosition = queuePosition
		}
		result.ActiveWork = append(result.ActiveWork, work)
	}
	if err := activeRows.Err(); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN slot_class = 'heavy' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN slot_class = 'light' THEN 1 ELSE 0 END), 0)
		  FROM deploy_queue_leases`).Scan(&result.Slots.HeavyUsed, &result.Slots.LightUsed); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *OrchestrationStore) liveReleaseHealth(ctx context.Context, releaseID int64) HealthOutcome {
	if releaseID == 0 {
		return HealthUnavailable
	}
	runtime, err := s.RuntimeForRelease(ctx, releaseID)
	if err != nil || runtime.State != "live" {
		return HealthUnavailable
	}
	release, err := s.Release(ctx, releaseID)
	if err != nil {
		return HealthUnavailable
	}
	var snapshot runtimeReleaseSnapshot
	foundSnapshot := false
	for _, artifact := range release.Artifacts {
		if artifact.Kind != ArtifactRuntimeConfig || artifact.State != "available" {
			continue
		}
		var envelope struct {
			Snapshot json.RawMessage `json:"snapshot"`
		}
		if json.Unmarshal(artifact.Metadata, &envelope) == nil &&
			digestBytes(envelope.Snapshot) == artifact.Digest &&
			json.Unmarshal(envelope.Snapshot, &snapshot) == nil {
			foundSnapshot = true
		}
		break
	}
	if !foundSnapshot {
		return HealthUnavailable
	}
	if len(snapshot.Checks) == 0 {
		return HealthDisabled
	}
	var healthRunID int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT id FROM deploy_runs WHERE release_id = ?
		 ORDER BY id DESC LIMIT 1`, releaseID).Scan(&healthRunID); err != nil {
		return HealthUnavailable
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT step_key, evidence_json FROM deploy_steps
		 WHERE run_id = ? AND step_key IN ('verify_readiness','verify_smoke','activate')
		   AND status IN ('passed','warning','skipped')
		 ORDER BY attempt DESC`, healthRunID)
	if err != nil {
		return HealthUnavailable
	}
	defer rows.Close()
	latest := map[StepKey]json.RawMessage{}
	for rows.Next() {
		var key StepKey
		var raw string
		if rows.Scan(&key, &raw) != nil {
			return HealthUnavailable
		}
		if _, exists := latest[key]; !exists {
			latest[key] = json.RawMessage(raw)
		}
	}
	observed := []CheckEvidence{}
	for _, key := range []StepKey{StepVerifyReadiness, StepVerifySmoke} {
		if raw := latest[key]; len(raw) != 0 {
			var evidence checkStepEvidence
			if json.Unmarshal(raw, &evidence) != nil {
				return HealthUnavailable
			}
			observed = append(observed, evidence.Checks...)
		}
	}
	if raw := latest[StepActivate]; len(raw) != 0 {
		var evidence activationStepEvidence
		if json.Unmarshal(raw, &evidence) != nil {
			return HealthUnavailable
		}
		observed = append(observed, evidence.PublicChecks...)
	}
	seen := map[string]bool{}
	for _, check := range observed {
		seen[check.Phase+"\x00"+check.Kind+"\x00"+check.Name] = true
	}
	for _, check := range snapshot.Checks {
		if !seen[check.Phase+"\x00"+check.Kind+"\x00"+check.Name] {
			return HealthUnavailable
		}
	}
	return summarizeChecks(observed)
}

func (s *OrchestrationStore) DeploymentBuildMethod(ctx context.Context, projectID int64) (BuildMethod, error) {
	var method BuildMethod
	err := s.db.QueryRowContext(ctx, `
		SELECT b.method FROM deploy_environments e
		JOIN deploy_build_plans b ON b.environment_id = e.id AND b.revision = e.desired_revision
		WHERE e.project_id = ? AND e.slug = 'production' AND e.archived_at = 0`, projectID).Scan(&method)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrEnvironmentNotFound
	}
	return method, err
}

func (s *OrchestrationStore) DeploymentSummary(ctx context.Context, projectID int64, budget QueueBudget) (*DeploymentSummary, error) {
	fleet, err := s.Fleet(ctx, budget)
	if err != nil {
		return nil, err
	}
	for i := range fleet.Deployments {
		if fleet.Deployments[i].ID == projectID {
			return &fleet.Deployments[i], nil
		}
	}
	return nil, ErrNotFound
}

func (s *OrchestrationStore) ProjectRuns(ctx context.Context, projectID int64, limit int) ([]EngineRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 30
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+engineRunColumns+`
		FROM deploy_runs WHERE project_id = ? ORDER BY requested_at DESC, id DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []EngineRun{}
	for rows.Next() {
		run, err := scanEngineRun(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *run)
	}
	return result, rows.Err()
}

func (s *OrchestrationStore) latestProjectRun(ctx context.Context, projectID int64, active bool) (*EngineRun, error) {
	query := `SELECT ` + engineRunColumns + ` FROM deploy_runs WHERE project_id = ?`
	if active {
		query += ` AND ` + activeRunWhere
	}
	query += ` ORDER BY requested_at DESC, id DESC LIMIT 1`
	run, err := scanEngineRun(s.db.QueryRowContext(ctx, query, projectID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read deployment run: %w", err)
	}
	return run, nil
}
