package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wayy01/Just-Dashboard/backend/internal/hostexec"
)

// LegacyStepExecutor keeps the shipped local-checkout/Compose path available
// while its API and UI are migrated. The persistent engine owns its run row,
// lease and transcript; Deployer still owns the exact 0.6.6 Git/reset/env/
// operator-command/Compose behavior.
type LegacyStepExecutor struct {
	deployer *Deployer
}

func NewLegacyStepExecutor(deployer *Deployer) *LegacyStepExecutor {
	return &LegacyStepExecutor{deployer: deployer}
}

func (e *LegacyStepExecutor) Execute(ctx context.Context, execution StepExecution) StepResult {
	if execution.Step.Key != StepLegacyPipeline {
		return unavailableExecutor{}.Execute(ctx, execution)
	}
	var metadata struct {
		TargetCommit string `json:"targetCommit"`
	}
	if err := json.Unmarshal(execution.Run.Metadata, &metadata); err != nil {
		return StepResult{State: StepFailed, ErrorCode: "invalid_plan", ErrorMessage: err.Error()}
	}
	project, err := e.deployer.store.Get(ctx, execution.Run.ProjectID)
	if err != nil {
		return StepResult{State: StepFailed, ErrorCode: "deploy_not_found", ErrorMessage: err.Error()}
	}
	before, _ := e.deployer.Inspect(ctx, project)
	fromCommit := ""
	if before != nil {
		fromCommit = before.SHA
	}
	writer := &legacyOutputWriter{output: execution.Output}
	toCommit, err := e.deployer.pipeline(ctx, project, metadata.TargetCommit, writer)
	var groupErr *hostexec.GroupError
	if errors.As(err, &groupErr) {
		return StepResult{
			State: StepCancelled, Cleanup: mustJSON(groupErr.Result),
			ErrorCode: "cancelled", ErrorMessage: groupErr.Error(),
			FromCommit: fromCommit, ToCommit: toCommit,
		}
	}
	if writer.err != nil {
		return StepResult{
			State: StepFailed, ErrorCode: "event_persist_failed", ErrorMessage: writer.err.Error(),
			FromCommit: fromCommit, ToCommit: toCommit,
		}
	}
	if err != nil {
		return StepResult{
			State: StepFailed, ErrorCode: "legacy_pipeline_failed", ErrorMessage: err.Error(),
			FromCommit: fromCommit, ToCommit: toCommit,
		}
	}
	if toCommit == "" {
		toCommit = fromCommit
	}
	return StepResult{
		State: StepPassed, Evidence: mustJSON(map[string]any{
			"fromCommit": fromCommit, "toCommit": toCommit, "compatibility": true,
		}),
		FromCommit: fromCommit, ToCommit: toCommit,
	}
}

type legacyOutputWriter struct {
	output StepOutput
	err    error
}

func (w *legacyOutputWriter) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if w.output == nil {
		w.err = fmt.Errorf("persistent deployment output is unavailable")
		return 0, w.err
	}
	if err := w.output.Log("stdout", string(data)); err != nil {
		w.err = err
		return 0, err
	}
	return len(data), nil
}
