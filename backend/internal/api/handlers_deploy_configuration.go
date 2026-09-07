package api

import (
	"net/http"
	"strconv"

	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/deploy"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/go-chi/chi/v5"
)

func deploymentEnvironmentIDs(r *http.Request) (int64, int64, error) {
	projectID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || projectID <= 0 {
		return 0, 0, httpx.BadRequest("deployment id must be a positive integer")
	}
	environmentID, err := strconv.ParseInt(chi.URLParam(r, "env"), 10, 64)
	if err != nil || environmentID <= 0 {
		return 0, 0, httpx.BadRequest("environment id must be a positive integer")
	}
	return projectID, environmentID, nil
}

func (s *Server) handleDeploymentArchive(w http.ResponseWriter, r *http.Request) error {
	projectID, err := parseID(r)
	if err != nil {
		return err
	}
	project, err := s.modules.deployStore.Archive(r.Context(), projectID)
	if err != nil {
		return mapDeployError(err)
	}
	httpx.SetAudit(r, "deploy.archive", project.Name, map[string]any{
		"deploymentId": projectID, "resourcesRemoved": false,
	})
	httpx.JSON(w, http.StatusOK, project)
	return nil
}

func (s *Server) handleDeploymentRemovalPlan(w http.ResponseWriter, r *http.Request) error {
	projectID, err := parseID(r)
	if err != nil {
		return err
	}
	plan, err := s.modules.deployPlanning.RemovalPlan(r.Context(), projectID)
	if err != nil {
		return mapDeploymentPlanningError(err)
	}
	httpx.SetAudit(r, "deploy.removal.preview", strconv.FormatInt(projectID, 10), map[string]any{
		"targetCount": len(plan.Targets), "archived": plan.Archived, "digest": plan.Digest,
	})
	httpx.JSON(w, http.StatusOK, plan)
	return nil
}

func (s *Server) handleDeploymentRemoveManaged(w http.ResponseWriter, r *http.Request) error {
	projectID, err := parseID(r)
	if err != nil {
		return err
	}
	var request deploy.RemoveManagedRequest
	if err := httpx.DecodeJSON(r, &request); err != nil {
		return err
	}
	plan, err := s.modules.deployPlanning.RemovalPlan(r.Context(), projectID)
	if err != nil {
		return mapDeploymentPlanningError(err)
	}
	selected := map[string]bool{}
	for _, id := range request.TargetIDs {
		selected[id] = true
	}
	typed := []deploy.RemovalTarget{}
	principal := httpx.MustPrincipal(r)
	for _, target := range plan.Targets {
		if !selected[target.ID] {
			continue
		}
		if target.RequiresAdmin && !principal.Can(auth.CapSystemAdmin) {
			return httpx.Err(http.StatusForbidden, "advanced_authorization_required", "this removal target requires system administration")
		}
		if target.ConfirmationType == "typed" {
			typed = append(typed, target)
		}
	}
	if len(typed) > 1 {
		return httpx.BadRequest("remove data targets one at a time so each exact name is confirmed")
	}
	if len(typed) == 1 {
		if err := httpx.RequireTypedConfirmation(w, r, typed[0].ConfirmationPhrase); err != nil {
			return err
		}
	}
	execution, err := s.modules.deployPlanning.RemoveManaged(
		r.Context(), projectID, principal.Username(), request, newDeploymentResourceRemover(s),
	)
	if err != nil {
		return mapDeploymentPlanningError(err)
	}
	targets := make([]map[string]string, 0, len(execution.Removed))
	for _, target := range execution.Removed {
		targets = append(targets, map[string]string{"kind": target.Kind, "resourceId": target.ResourceID})
	}
	httpx.SetAudit(r, "deploy.resources.remove", strconv.FormatInt(projectID, 10), map[string]any{
		"targets": targets, "remaining": len(execution.Remaining),
	})
	httpx.JSON(w, http.StatusOK, execution)
	return nil
}

func (s *Server) handleDeploymentVariables(w http.ResponseWriter, r *http.Request) error {
	projectID, environmentID, err := deploymentEnvironmentIDs(r)
	if err != nil {
		return err
	}
	variables, err := s.modules.deployPlanning.ListVariables(r.Context(), projectID, environmentID)
	if err != nil {
		return mapDeploymentPlanningError(err)
	}
	httpx.JSON(w, http.StatusOK, variables)
	return nil
}

func (s *Server) handleDeploymentVariableReveal(w http.ResponseWriter, r *http.Request) error {
	projectID, environmentID, err := deploymentEnvironmentIDs(r)
	if err != nil {
		return err
	}
	name := chi.URLParam(r, "name")
	value, err := s.modules.deployPlanning.RevealVariable(r.Context(), projectID, environmentID, name)
	if err != nil {
		return mapDeploymentPlanningError(err)
	}
	// Secret reads use GET, so the mutation audit middleware intentionally skips
	// them. Record the privileged disclosure immediately without its value.
	s.recordAudit(r, "deploy.variable.reveal", name, map[string]any{
		"deploymentId": projectID, "environmentId": environmentID,
	})
	httpx.JSON(w, http.StatusOK, value)
	return nil
}

func (s *Server) handleDeploymentVariablePut(w http.ResponseWriter, r *http.Request) error {
	projectID, environmentID, err := deploymentEnvironmentIDs(r)
	if err != nil {
		return err
	}
	var request deploy.VariableWriteRequest
	if err := httpx.DecodeJSON(r, &request); err != nil {
		return err
	}
	name := chi.URLParam(r, "name")
	principal := httpx.MustPrincipal(r)
	result, err := s.modules.deployPlanning.PutVariable(
		r.Context(), projectID, environmentID, name, principal.Username(), request,
	)
	if err != nil {
		return mapDeploymentPlanningError(err)
	}
	httpx.SetAudit(r, "deploy.variable.update", name, map[string]any{
		"deploymentId": projectID, "environmentId": environmentID,
		"revision": result.DesiredRevision, "sensitivity": request.Sensitivity,
		"scopes": request.Scopes, "reference": request.Reference != "",
	})
	httpx.JSON(w, http.StatusOK, result)
	return nil
}

func (s *Server) handleDeploymentVariableGenerate(w http.ResponseWriter, r *http.Request) error {
	projectID, environmentID, err := deploymentEnvironmentIDs(r)
	if err != nil {
		return err
	}
	var request struct {
		Revision int      `json:"revision"`
		Scopes   []string `json:"scopes"`
	}
	if err := httpx.DecodeJSON(r, &request); err != nil {
		return err
	}
	name := chi.URLParam(r, "name")
	principal := httpx.MustPrincipal(r)
	result, err := s.modules.deployPlanning.GenerateVariable(
		r.Context(), projectID, environmentID, name, principal.Username(), request.Revision, request.Scopes,
	)
	if err != nil {
		return mapDeploymentPlanningError(err)
	}
	httpx.SetAudit(r, "deploy.variable.generate", name, map[string]any{
		"deploymentId": projectID, "environmentId": environmentID,
		"revision": result.DesiredRevision, "scopes": request.Scopes,
	})
	httpx.JSON(w, http.StatusCreated, result)
	return nil
}

func (s *Server) handleDeploymentVariableRotate(w http.ResponseWriter, r *http.Request) error {
	projectID, environmentID, err := deploymentEnvironmentIDs(r)
	if err != nil {
		return err
	}
	var request struct {
		Revision int `json:"revision"`
	}
	if err := httpx.DecodeJSON(r, &request); err != nil {
		return err
	}
	name := chi.URLParam(r, "name")
	principal := httpx.MustPrincipal(r)
	result, err := s.modules.deployPlanning.RotateVariable(
		r.Context(), projectID, environmentID, name, principal.Username(), request.Revision,
	)
	if err != nil {
		return mapDeploymentPlanningError(err)
	}
	httpx.SetAudit(r, "deploy.variable.rotate", name, map[string]any{
		"deploymentId": projectID, "environmentId": environmentID,
		"revision": result.DesiredRevision,
	})
	httpx.JSON(w, http.StatusOK, result)
	return nil
}

func (s *Server) handleDeploymentDotenvImport(w http.ResponseWriter, r *http.Request) error {
	projectID, environmentID, err := deploymentEnvironmentIDs(r)
	if err != nil {
		return err
	}
	var request deploy.DotenvImportRequest
	if err := httpx.DecodeJSON(r, &request); err != nil {
		return err
	}
	principal := httpx.MustPrincipal(r)
	result, err := s.modules.deployPlanning.ImportDotenv(
		r.Context(), projectID, environmentID, principal.Username(), request,
	)
	if err != nil {
		return mapDeploymentPlanningError(err)
	}
	httpx.SetAudit(r, "deploy.variable.import", strconv.Itoa(len(result.Variables)), map[string]any{
		"deploymentId": projectID, "environmentId": environmentID,
		"revision": result.DesiredRevision, "sensitivity": request.Sensitivity,
		"scopes": request.Scopes,
	})
	httpx.JSON(w, http.StatusOK, result)
	return nil
}

func (s *Server) handleDeploymentVariableDelete(w http.ResponseWriter, r *http.Request) error {
	projectID, environmentID, err := deploymentEnvironmentIDs(r)
	if err != nil {
		return err
	}
	var request struct {
		Revision int `json:"revision"`
	}
	if err := httpx.DecodeJSON(r, &request); err != nil {
		return err
	}
	name := chi.URLParam(r, "name")
	revision, err := s.modules.deployPlanning.DeleteVariable(
		r.Context(), projectID, environmentID, name, request.Revision,
	)
	if err != nil {
		return mapDeploymentPlanningError(err)
	}
	httpx.SetAudit(r, "deploy.variable.delete", name, map[string]any{
		"deploymentId": projectID, "environmentId": environmentID, "revision": revision,
	})
	httpx.JSON(w, http.StatusOK, map[string]int{"desiredRevision": revision})
	return nil
}

func (s *Server) handleDeploymentPending(w http.ResponseWriter, r *http.Request) error {
	projectID, environmentID, err := deploymentEnvironmentIDs(r)
	if err != nil {
		return err
	}
	state, err := s.modules.deployPlanning.PendingState(r.Context(), projectID, environmentID)
	if err != nil {
		return mapDeploymentPlanningError(err)
	}
	httpx.JSON(w, http.StatusOK, state)
	return nil
}

func (s *Server) handleDeploymentConfiguration(w http.ResponseWriter, r *http.Request) error {
	projectID, environmentID, err := deploymentEnvironmentIDs(r)
	if err != nil {
		return err
	}
	configuration, err := s.modules.deployPlanning.EnvironmentConfiguration(r.Context(), projectID, environmentID)
	if err != nil {
		return mapDeploymentPlanningError(err)
	}
	httpx.JSON(w, http.StatusOK, configuration)
	return nil
}

func (s *Server) handleDeploymentConfigurationSave(w http.ResponseWriter, r *http.Request) error {
	projectID, environmentID, err := deploymentEnvironmentIDs(r)
	if err != nil {
		return err
	}
	var request deploy.ConfigurationWriteRequest
	if err := httpx.DecodeJSON(r, &request); err != nil {
		return err
	}
	configuration, err := s.modules.deployPlanning.SaveEnvironmentConfiguration(
		r.Context(), projectID, environmentID, request,
	)
	if err != nil {
		return mapDeploymentPlanningError(err)
	}
	httpx.SetAudit(r, "deploy.configuration.update", strconv.FormatInt(environmentID, 10), map[string]any{
		"deploymentId": projectID, "environmentId": environmentID, "revision": configuration.Revision,
		"domains": len(configuration.Domains), "dependencies": len(configuration.Dependencies),
		"checks": len(configuration.Checks), "publicBind": request.Runtime.BindAddress == "0.0.0.0" || request.Runtime.BindAddress == "::",
	})
	httpx.JSON(w, http.StatusOK, configuration)
	return nil
}
