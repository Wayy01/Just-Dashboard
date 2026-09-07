package api

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/backups"
	"github.com/Wayy01/Just-Dashboard/backend/internal/deploy"
)

// deploymentBackupGate is an adapter, not a second backup implementation.
// Backups remains responsible for jobs, artifacts, retention, credentials and
// execution; Deployments receives only the small evidence record it may store
// safely in a run transcript.
type deploymentBackupGate struct {
	store  *backups.Store
	runner *backups.Runner
	now    func() time.Time
}

func newDeploymentBackupGate(store *backups.Store, runner *backups.Runner) *deploymentBackupGate {
	return &deploymentBackupGate{store: store, runner: runner, now: time.Now}
}

func (g *deploymentBackupGate) Evaluate(ctx context.Context, request deploy.BackupGateRequest) (deploy.BackupGateEvidence, error) {
	evidence := deploy.BackupGateEvidence{JobID: request.JobID, Status: "unavailable"}
	if g == nil || g.store == nil || g.runner == nil {
		return evidence, errors.New("Backups feature is unavailable")
	}
	job, err := g.store.Get(ctx, request.JobID)
	if err != nil {
		evidence.Detail = "backup job was not found"
		return evidence, err
	}
	for _, source := range request.PersistentSources {
		if filepath.IsAbs(source) && !backupCoversPath(job.Sources, source) {
			evidence.Detail = "backup job does not cover every persistent path"
			return evidence, fmt.Errorf("backup job %d does not cover persistent path %s", request.JobID, source)
		}
	}
	if !request.RequiredBeforeDeploy && request.MaxAgeSeconds == 0 {
		evidence.Status, evidence.Fresh = "not_required", true
		return evidence, nil
	}
	var run *backups.Run
	if request.RequiredBeforeDeploy {
		run, err = g.runner.Execute(ctx, request.JobID, "deployment")
	} else {
		run, err = g.store.LastRun(ctx, request.JobID)
	}
	if err != nil {
		evidence.Detail = "backup run could not be obtained"
		return evidence, err
	}
	evidence.RunID, evidence.Status = run.ID, string(run.Status)
	evidence.StartedAt, evidence.EndedAt = run.StartedAt, run.EndedAt
	if run.Status != backups.StatusSuccess || run.EndedAt == nil {
		evidence.Detail = "backup run did not succeed"
		return evidence, errors.New("backup run did not succeed")
	}
	evidence.Fresh = request.MaxAgeSeconds == 0 ||
		g.now().UTC().Sub(run.EndedAt.UTC()) <= time.Duration(request.MaxAgeSeconds)*time.Second
	if !evidence.Fresh {
		evidence.Detail = "latest successful backup is older than the policy maximum"
	}
	// The existing Backups owner has no persisted restore-test record yet. The
	// false value is explicit, so a deployment policy that requires one blocks
	// rather than treating artifact existence as recovery proof.
	evidence.RestoreTested = false
	return evidence, nil
}

func backupCoversPath(sources []string, wanted string) bool {
	wanted = filepath.Clean(wanted)
	for _, source := range sources {
		source = filepath.Clean(strings.TrimSpace(source))
		if source == wanted {
			return true
		}
		relative, err := filepath.Rel(source, wanted)
		if err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
