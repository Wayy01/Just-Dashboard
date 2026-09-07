package api

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/deploy"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleDeploymentTriggers(w http.ResponseWriter, r *http.Request) error {
	projectID, environmentID, err := deploymentEnvironmentIDs(r)
	if err != nil {
		return err
	}
	items, err := s.modules.deployAutomation.ListTriggers(r.Context(), projectID, environmentID)
	if err != nil {
		return httpx.Internal(err)
	}
	httpx.JSON(w, http.StatusOK, items)
	return nil
}

func (s *Server) handleDeploymentTriggerCreate(w http.ResponseWriter, r *http.Request) error {
	projectID, environmentID, err := deploymentEnvironmentIDs(r)
	if err != nil {
		return err
	}
	var req deploy.TriggerWrite
	if err = httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	result, err := s.modules.deployAutomation.CreateTrigger(r.Context(), projectID, environmentID, req)
	if err != nil {
		return mapAutomationError(err)
	}
	httpx.SetAudit(r, "deploy.trigger.create", result.Trigger.Name, map[string]any{"kind": result.Trigger.Kind, "repository": result.Trigger.Config.Repository})
	httpx.JSON(w, http.StatusCreated, result)
	return nil
}
func automationParam(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		return 0, httpx.BadRequest("%s id must be positive", name)
	}
	return id, nil
}
func (s *Server) handleDeploymentTriggerUpdate(w http.ResponseWriter, r *http.Request) error {
	projectID, environmentID, err := deploymentEnvironmentIDs(r)
	if err != nil {
		return err
	}
	triggerID, err := automationParam(r, "trigger")
	if err != nil {
		return err
	}
	var req deploy.TriggerWrite
	if err = httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	item, err := s.modules.deployAutomation.UpdateTrigger(r.Context(), projectID, environmentID, triggerID, req)
	if err != nil {
		return mapAutomationError(err)
	}
	httpx.SetAudit(r, "deploy.trigger.update", item.Name, map[string]any{"kind": item.Kind})
	httpx.JSON(w, http.StatusOK, item)
	return nil
}
func (s *Server) handleDeploymentTriggerDelete(w http.ResponseWriter, r *http.Request) error {
	projectID, environmentID, err := deploymentEnvironmentIDs(r)
	if err != nil {
		return err
	}
	triggerID, err := automationParam(r, "trigger")
	if err != nil {
		return err
	}
	if err = s.modules.deployAutomation.DeleteTrigger(r.Context(), projectID, environmentID, triggerID); err != nil {
		return mapAutomationError(err)
	}
	httpx.SetAudit(r, "deploy.trigger.delete", fmt.Sprint(triggerID), nil)
	httpx.NoContent(w)
	return nil
}

type watchSimulationRequest struct {
	ChangedPaths []string `json:"changedPaths"`
	Include      []string `json:"include"`
	Exclude      []string `json:"exclude"`
}

func (s *Server) handleDeploymentWatchPathSimulate(w http.ResponseWriter, r *http.Request) error {
	if _, _, err := deploymentEnvironmentIDs(r); err != nil {
		return err
	}
	var req watchSimulationRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	matched := deploy.MatchWatchPaths(req.ChangedPaths, req.Include, req.Exclude)
	httpx.SetAudit(r, "deploy.trigger.test", "watch-paths", map[string]any{"changedCount": len(req.ChangedPaths), "matched": matched})
	httpx.JSON(w, http.StatusOK, map[string]any{"matched": matched})
	return nil
}

func (s *Server) handleDeploymentSchedules(w http.ResponseWriter, r *http.Request) error {
	projectID, environmentID, err := deploymentEnvironmentIDs(r)
	if err != nil {
		return err
	}
	items, err := s.modules.deployAutomation.ListSchedules(r.Context(), projectID, environmentID)
	if err != nil {
		return httpx.Internal(err)
	}
	httpx.JSON(w, http.StatusOK, items)
	return nil
}

func (s *Server) handleDeploymentScheduleCreate(w http.ResponseWriter, r *http.Request) error {
	projectID, environmentID, err := deploymentEnvironmentIDs(r)
	if err != nil {
		return err
	}
	var req deploy.ScheduleWrite
	if err = httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	item, err := s.modules.deployAutomation.CreateSchedule(r.Context(), projectID, environmentID, req)
	if err != nil {
		return mapAutomationError(err)
	}
	httpx.SetAudit(r, "deploy.schedule.create", item.Name, map[string]any{"expression": item.Expression, "timezone": item.Timezone})
	httpx.JSON(w, http.StatusCreated, item)
	return nil
}
func (s *Server) handleDeploymentScheduleDelete(w http.ResponseWriter, r *http.Request) error {
	projectID, environmentID, err := deploymentEnvironmentIDs(r)
	if err != nil {
		return err
	}
	scheduleID, err := automationParam(r, "schedule")
	if err != nil {
		return err
	}
	if err = s.modules.deployAutomation.DeleteSchedule(r.Context(), projectID, environmentID, scheduleID); err != nil {
		return mapAutomationError(err)
	}
	httpx.SetAudit(r, "deploy.schedule.delete", fmt.Sprint(scheduleID), nil)
	httpx.NoContent(w)
	return nil
}

func (s *Server) handleDeploymentPreviews(w http.ResponseWriter, r *http.Request) error {
	projectID, err := parseID(r)
	if err != nil {
		return err
	}
	items, err := s.modules.deployAutomation.ListPreviews(r.Context(), projectID)
	if err != nil {
		return httpx.Internal(err)
	}
	httpx.JSON(w, http.StatusOK, items)
	return nil
}

func (s *Server) handleDeploymentNotifications(w http.ResponseWriter, r *http.Request) error {
	items, err := s.modules.deployAutomation.ListNotificationChannels(r.Context())
	if err != nil {
		return httpx.Internal(err)
	}
	httpx.JSON(w, http.StatusOK, items)
	return nil
}
func (s *Server) handleDeploymentNotificationCreate(w http.ResponseWriter, r *http.Request) error {
	var req deploy.NotificationWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	channel, secret, err := s.modules.deployAutomation.CreateNotificationChannel(r.Context(), req)
	if err != nil {
		return mapAutomationError(err)
	}
	httpx.SetAudit(r, "deploy.notification.create", channel.Name, map[string]any{"events": channel.Events})
	httpx.JSON(w, http.StatusCreated, map[string]any{"channel": channel, "secret": secret})
	return nil
}
func (s *Server) handleDeploymentNotificationTest(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "channel"), 10, 64)
	if err != nil || id <= 0 {
		return httpx.BadRequest("notification channel id must be positive")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	err = s.modules.deployAutomation.DeliverNotification(ctx, nil, id, deploy.NotificationEnvelope{Event: "test", SentAt: time.Now().UTC()})
	if err != nil {
		return httpx.Err(http.StatusBadGateway, "notification_failed", "notification delivery failed")
	}
	httpx.SetAudit(r, "deploy.notification.test", fmt.Sprint(id), nil)
	httpx.JSON(w, http.StatusOK, map[string]any{"delivered": true})
	return nil
}
func (s *Server) handleDeploymentNotificationDelete(w http.ResponseWriter, r *http.Request) error {
	id, err := automationParam(r, "channel")
	if err != nil {
		return err
	}
	if err = s.modules.deployAutomation.DeleteNotificationChannel(r.Context(), id); err != nil {
		return mapAutomationError(err)
	}
	httpx.SetAudit(r, "deploy.notification.delete", fmt.Sprint(id), nil)
	httpx.NoContent(w)
	return nil
}

func (s *Server) handleDeploymentProviderWebhook(w http.ResponseWriter, r *http.Request) error {
	return s.handleAutomationWebhook(w, r, true)
}

func (s *Server) handleDeploymentGenericWebhook(w http.ResponseWriter, r *http.Request) error {
	return s.handleAutomationWebhook(w, r, false)
}

func (s *Server) handleAutomationWebhook(w http.ResponseWriter, r *http.Request, providerSpecific bool) error {
	hookID := chi.URLParam(r, "hookID")
	httpx.SetAuditActor(r, "webhook")
	var trigger *deploy.Trigger
	var secret string
	var err error
	if rawTriggerID := chi.URLParam(r, "triggerID"); rawTriggerID != "" {
		projectID, parseErr := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		triggerID, triggerErr := strconv.ParseInt(rawTriggerID, 10, 64)
		if parseErr != nil || triggerErr != nil || projectID <= 0 || triggerID <= 0 {
			return httpx.Err(http.StatusUnauthorized, "bad_signature", "unknown or unauthorised deploy hook")
		}
		trigger, secret, err = s.modules.deployAutomation.TriggerByID(r.Context(), projectID, triggerID)
	} else {
		trigger, secret, err = s.modules.deployAutomation.TriggerByHook(r.Context(), hookID)
	}
	if err != nil {
		return httpx.Err(http.StatusUnauthorized, "bad_signature", "unknown or unauthorised deploy hook")
	}
	if !trigger.Enabled {
		return httpx.Err(http.StatusForbidden, "hook_disabled", "deployment hook is disabled")
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, deploy.MaxAutomationBody))
	if err != nil {
		return httpx.Err(http.StatusRequestEntityTooLarge, "payload_too_large", "webhook payload exceeds 4 MiB")
	}

	var event deploy.ProviderEvent
	if providerSpecific {
		provider := strings.ToLower(chi.URLParam(r, "provider"))
		if provider != trigger.Provider {
			err = deploy.ErrWrongEvent
		} else {
			event, err = deploy.VerifyProvider(provider, r.Header, body, secret)
		}
	} else {
		event = deploy.ProviderEvent{DeliveryID: strings.TrimSpace(r.Header.Get("Idempotency-Key")), Event: "deploy", Repository: trigger.Config.Repository, Ref: trigger.Config.Ref}
		if event.DeliveryID == "" {
			event.DeliveryID = fmt.Sprintf("body:%x", sha256.Sum256(body))
		}
		if !deploy.VerifyGenericHook(body, secret, r.Header.Get("X-JD-Signature-256")) {
			err = deploy.ErrBadHookSignature
		}
	}
	if err == nil {
		err = validateAutomationEvent(trigger, event)
	}
	if err == nil && !event.PreviewClosed && !deploy.MatchWatchPaths(event.ChangedPaths, trigger.Config.WatchInclude, trigger.Config.WatchExclude) {
		err = deploy.ErrWatchPathsIgnored
	}
	if err != nil {
		_ = s.modules.deployAutomation.RecordDelivery(r.Context(), trigger, event, body, "rejected", automationReason(err), 0)
		return mapAutomationError(err)
	}
	// Reserve the provider delivery before preview or queue side effects. This
	// unique row is the replay fence even when two identical requests arrive
	// concurrently.
	if err = s.modules.deployAutomation.RecordDelivery(r.Context(), trigger, event, body, "processing", "", 0); err != nil {
		return mapAutomationError(err)
	}
	project, err := s.modules.deployStore.Get(r.Context(), trigger.ProjectID)
	if err != nil {
		return mapDeployError(err)
	}
	environmentID, operation, runTrigger := trigger.EnvironmentID, deploy.OperationDeploy, trigger.Kind
	var previewID int64
	if trigger.Config.Preview && event.PreviewNumber > 0 {
		preview, created, previewErr := s.modules.deployAutomation.EnsurePreview(r.Context(), trigger, event)
		if previewErr != nil {
			_ = s.modules.deployAutomation.FinishDelivery(r.Context(), trigger, event.DeliveryID, "rejected", automationReason(previewErr), 0)
			return mapAutomationError(previewErr)
		}
		environmentID, runTrigger = preview.EnvironmentID, deploy.TriggerPreview
		previewID = preview.ID
		if event.PreviewClosed {
			operation = deploy.OperationPreviewRemove
		} else if created {
			operation = deploy.OperationPreviewCreate
		} else {
			operation = deploy.OperationPreviewUpdate
		}
	}
	run, err := s.enqueueNormalizedDeployment(r.Context(), project, environmentID, operation, 0, runTrigger, "webhook", trigger.HookID+":"+event.DeliveryID)
	if err != nil {
		_ = s.modules.deployAutomation.FinishDelivery(r.Context(), trigger, event.DeliveryID, "rejected", "enqueue_failed", 0)
		return mapDeployError(err)
	}
	if event.PreviewClosed {
		if err = s.modules.deployAutomation.ArchiveClosedPreview(r.Context(), previewID); err != nil {
			return httpx.Internal(err)
		}
	}
	if err = s.modules.deployAutomation.FinishDelivery(r.Context(), trigger, event.DeliveryID, "accepted", "", run.ID); err != nil {
		return mapAutomationError(err)
	}
	httpx.SetAudit(r, "deploy.provider.delivery", trigger.Name, map[string]any{"provider": trigger.Provider, "deliveryId": event.DeliveryID, "runId": run.ID})
	httpx.JSON(w, http.StatusAccepted, map[string]any{"accepted": true, "runId": run.ID, "state": run.State})
	return nil
}

func validateAutomationEvent(t *deploy.Trigger, e deploy.ProviderEvent) error {
	allowed := len(t.Config.Events) == 0
	for _, value := range t.Config.Events {
		if strings.EqualFold(value, e.Event) {
			allowed = true
		}
	}
	if !allowed {
		return deploy.ErrWrongEvent
	}
	if t.Config.Repository != "" && !strings.EqualFold(strings.TrimSuffix(t.Config.Repository, ".git"), strings.TrimSuffix(e.Repository, ".git")) {
		return deploy.ErrWrongRepository
	}
	if t.Config.Ref != "" && e.PreviewNumber == 0 {
		want, got := strings.TrimPrefix(t.Config.Ref, "refs/heads/"), strings.TrimPrefix(e.Ref, "refs/heads/")
		if want != got {
			return deploy.ErrWrongRef
		}
	}
	return nil
}

func automationReason(err error) string {
	switch {
	case errors.Is(err, deploy.ErrBadHookSignature):
		return "bad_signature"
	case errors.Is(err, deploy.ErrWrongEvent):
		return "wrong_event"
	case errors.Is(err, deploy.ErrWrongRepository):
		return "wrong_repository"
	case errors.Is(err, deploy.ErrWrongRef):
		return "wrong_ref"
	case errors.Is(err, deploy.ErrWatchPathsIgnored):
		return "watch_paths_ignored"
	case errors.Is(err, deploy.ErrDeliveryReplayed):
		return "delivery_replayed"
	case errors.Is(err, deploy.ErrPreviewQuota):
		return "preview_quota"
	}
	return "rejected"
}

func mapAutomationError(err error) error {
	code, status := automationReason(err), http.StatusUnprocessableEntity
	if errors.Is(err, deploy.ErrBadHookSignature) {
		status = http.StatusUnauthorized
	}
	if errors.Is(err, deploy.ErrDeliveryReplayed) {
		status = http.StatusConflict
	}
	if code != "rejected" {
		return httpx.Err(status, code, err.Error())
	}
	return httpx.BadRequest("%v", err)
}

func (s *Server) dispatchDeploymentSchedule(ctx context.Context, item deploy.ScheduleDispatch) error {
	project, err := s.modules.deployStore.Get(ctx, item.Schedule.ProjectID)
	if err != nil {
		return err
	}
	// The immutable scheduled_action run is the chain history. Its configuration
	// remains on the schedule step rows; the deployment executor performs the
	// release path and the run's notify step reports the terminal outcome.
	_, err = s.enqueueNormalizedDeployment(ctx, project, item.Schedule.EnvironmentID,
		deploy.OperationScheduled, 0, deploy.TriggerSchedule, "scheduler",
		fmt.Sprintf("schedule:%d:%d", item.Schedule.ID, item.DueAt.Unix()))
	return err
}
