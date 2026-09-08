package deploy

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	basestore "github.com/Wayy01/Just-Dashboard/backend/internal/store"
)

var (
	ErrTriggerNotFound   = errors.New("deployment trigger not found")
	ErrHookDisabled      = errors.New("deployment hook is disabled")
	ErrBadHookSignature  = errors.New("deployment hook signature is invalid")
	ErrWrongEvent        = errors.New("deployment provider event is not supported")
	ErrWrongRepository   = errors.New("deployment provider repository does not match")
	ErrWrongRef          = errors.New("deployment provider ref does not match")
	ErrDeliveryReplayed  = errors.New("deployment provider delivery was already received")
	ErrWatchPathsIgnored = errors.New("deployment change does not match watched paths")
	ErrPreviewQuota      = errors.New("deployment preview quota has been reached")
)

const MaxAutomationBody = 4 << 20

type AutomationStore struct {
	db     *sql.DB
	sealer *auth.Sealer
	now    func() time.Time
}

func NewAutomationStore(st *basestore.Store, sealer *auth.Sealer) *AutomationStore {
	return &AutomationStore{db: st.DB, sealer: sealer, now: time.Now}
}

type Trigger struct {
	ID             int64         `json:"id"`
	EnvironmentID  int64         `json:"environmentId"`
	ProjectID      int64         `json:"projectId"`
	Name           string        `json:"name"`
	Kind           TriggerKind   `json:"kind"`
	Provider       string        `json:"provider,omitempty"`
	Config         TriggerConfig `json:"config"`
	HookID         string        `json:"hookId,omitempty"`
	Enabled        bool          `json:"enabled"`
	LastDeliveryAt *time.Time    `json:"lastDeliveryAt,omitempty"`
	LastStatus     string        `json:"lastStatus,omitempty"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`
}

type TriggerConfig struct {
	Repository    string   `json:"repository,omitempty"`
	Ref           string   `json:"ref,omitempty"`
	Events        []string `json:"events,omitempty"`
	WatchInclude  []string `json:"watchInclude,omitempty"`
	WatchExclude  []string `json:"watchExclude,omitempty"`
	Preview       bool     `json:"preview,omitempty"`
	PreviewQuota  int      `json:"previewQuota,omitempty"`
	PreviewDomain string   `json:"previewDomain,omitempty"`
}

type TriggerWrite struct {
	Name     string        `json:"name"`
	Kind     TriggerKind   `json:"kind"`
	Provider string        `json:"provider,omitempty"`
	Config   TriggerConfig `json:"config"`
	Enabled  bool          `json:"enabled"`
}

type TriggerCreated struct {
	Trigger Trigger `json:"trigger"`
	Secret  string  `json:"secret"`
}

func (s *AutomationStore) ListTriggers(ctx context.Context, projectID, environmentID int64) ([]Trigger, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id,t.environment_id,e.project_id,t.name,t.kind,t.provider,t.config_json,t.hook_id,t.enabled,t.last_delivery_at,t.last_status,t.created_at,t.updated_at FROM deploy_triggers t JOIN deploy_environments e ON e.id=t.environment_id WHERE e.project_id=? AND t.environment_id=? ORDER BY t.id`, projectID, environmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Trigger{}
	for rows.Next() {
		v, err := scanTrigger(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func scanTrigger(row interface{ Scan(...any) error }) (*Trigger, error) {
	var t Trigger
	var cfg string
	var enabled int
	var last, created, updated int64
	if err := row.Scan(&t.ID, &t.EnvironmentID, &t.ProjectID, &t.Name, &t.Kind, &t.Provider, &cfg, &t.HookID, &enabled, &last, &t.LastStatus, &created, &updated); err != nil {
		return nil, err
	}
	if json.Unmarshal([]byte(cfg), &t.Config) != nil {
		return nil, fmt.Errorf("%w: malformed trigger configuration", ErrInvalidPlan)
	}
	t.Enabled = enabled != 0
	t.CreatedAt = time.Unix(created, 0).UTC()
	t.UpdatedAt = time.Unix(updated, 0).UTC()
	if last > 0 {
		value := time.Unix(last, 0).UTC()
		t.LastDeliveryAt = &value
	}
	return &t, nil
}

func (s *AutomationStore) TriggerByHook(ctx context.Context, hookID string) (*Trigger, string, error) {
	return s.triggerWithSecret(ctx, `t.hook_id=?`, hookID)
}

func (s *AutomationStore) TriggerByID(ctx context.Context, projectID, triggerID int64) (*Trigger, string, error) {
	return s.triggerWithSecret(ctx, `t.id=? AND e.project_id=?`, triggerID, projectID)
}

func (s *AutomationStore) triggerWithSecret(ctx context.Context, predicate string, args ...any) (*Trigger, string, error) {
	var sealed, cfg string
	var enabled int
	var last, created, updated int64
	var t Trigger
	err := s.db.QueryRowContext(ctx, `SELECT t.id,t.environment_id,e.project_id,t.name,t.kind,t.provider,t.config_json,t.hook_id,t.enabled,t.last_delivery_at,t.last_status,t.created_at,t.updated_at,t.secret_enc FROM deploy_triggers t JOIN deploy_environments e ON e.id=t.environment_id WHERE `+predicate+` AND e.archived_at=0`, args...).Scan(&t.ID, &t.EnvironmentID, &t.ProjectID, &t.Name, &t.Kind, &t.Provider, &cfg, &t.HookID, &enabled, &last, &t.LastStatus, &created, &updated, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrTriggerNotFound
	}
	if err != nil {
		return nil, "", err
	}
	if json.Unmarshal([]byte(cfg), &t.Config) != nil {
		return nil, "", fmt.Errorf("%w: malformed trigger configuration", ErrInvalidPlan)
	}
	t.Enabled = enabled != 0
	t.CreatedAt, t.UpdatedAt = time.Unix(created, 0).UTC(), time.Unix(updated, 0).UTC()
	if last > 0 {
		value := time.Unix(last, 0).UTC()
		t.LastDeliveryAt = &value
	}
	secret, err := s.sealer.Open(sealed)
	if err != nil {
		return nil, "", err
	}
	return &t, secret, nil
}

func randomHex(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *AutomationStore) CreateTrigger(ctx context.Context, projectID, environmentID int64, in TriggerWrite) (*TriggerCreated, error) {
	if err := validateTriggerWrite(&in); err != nil {
		return nil, err
	}
	cfg, _ := json.Marshal(in.Config)
	secret, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	sealed, err := s.sealer.Seal(secret)
	if err != nil {
		return nil, err
	}
	hook, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC().Unix()
	res, err := s.db.ExecContext(ctx, `INSERT INTO deploy_triggers(environment_id,name,kind,provider,config_json,secret_enc,hook_id,enabled,created_at,updated_at) SELECT id,?,?,?,?,?,?,?,?,? FROM deploy_environments WHERE id=? AND project_id=? AND archived_at=0`, in.Name, in.Kind, in.Provider, string(cfg), sealed, hook, boolInt(in.Enabled), now, now, environmentID, projectID)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	t, err := s.triggerByID(ctx, id, projectID)
	if err != nil {
		return nil, err
	}
	return &TriggerCreated{Trigger: *t, Secret: secret}, nil
}
func (s *AutomationStore) UpdateTrigger(ctx context.Context, projectID, environmentID, triggerID int64, in TriggerWrite) (*Trigger, error) {
	if err := validateTriggerWrite(&in); err != nil {
		return nil, err
	}
	cfg, _ := json.Marshal(in.Config)
	result, err := s.db.ExecContext(ctx, `UPDATE deploy_triggers SET name=?,kind=?,provider=?,config_json=?,enabled=?,updated_at=? WHERE id=? AND environment_id=? AND environment_id IN (SELECT id FROM deploy_environments WHERE project_id=?)`, in.Name, in.Kind, in.Provider, string(cfg), boolInt(in.Enabled), s.now().UTC().Unix(), triggerID, environmentID, projectID)
	if err != nil {
		return nil, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return nil, ErrTriggerNotFound
	}
	return s.triggerByID(ctx, triggerID, projectID)
}
func (s *AutomationStore) DeleteTrigger(ctx context.Context, projectID, environmentID, triggerID int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM deploy_triggers WHERE id=? AND environment_id=? AND environment_id IN (SELECT id FROM deploy_environments WHERE project_id=?)`, triggerID, environmentID, projectID)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return ErrTriggerNotFound
	}
	return nil
}

func validateTriggerWrite(in *TriggerWrite) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Provider = strings.ToLower(strings.TrimSpace(in.Provider))
	in.Config.Repository = strings.TrimSpace(in.Config.Repository)
	in.Config.Ref = strings.TrimSpace(in.Config.Ref)
	if in.Name == "" || len(in.Name) > 100 {
		return fmt.Errorf("trigger name is required")
	}
	if in.Config.PreviewQuota < 0 || in.Config.PreviewQuota > 20 {
		return fmt.Errorf("preview quota must be between 0 and 20")
	}
	valid := map[TriggerKind]bool{TriggerGenericHook: true, TriggerAPI: true, TriggerGitHub: true, TriggerGitLab: true, TriggerBitbucket: true, TriggerGitea: true}
	if !valid[in.Kind] {
		return fmt.Errorf("unsupported trigger kind %q", in.Kind)
	}
	if in.Kind == TriggerGitHub || in.Kind == TriggerGitLab || in.Kind == TriggerBitbucket || in.Kind == TriggerGitea {
		if in.Provider == "" {
			in.Provider = string(in.Kind)
		}
		if in.Provider != string(in.Kind) {
			return fmt.Errorf("provider and trigger kind disagree")
		}
		if in.Config.Repository == "" || in.Config.Ref == "" {
			return fmt.Errorf("provider triggers require repository and ref")
		}
	}
	return nil
}

func (s *AutomationStore) triggerByID(ctx context.Context, id, projectID int64) (*Trigger, error) {
	t, err := scanTrigger(s.db.QueryRowContext(ctx, `SELECT t.id,t.environment_id,e.project_id,t.name,t.kind,t.provider,t.config_json,t.hook_id,t.enabled,t.last_delivery_at,t.last_status,t.created_at,t.updated_at FROM deploy_triggers t JOIN deploy_environments e ON e.id=t.environment_id WHERE t.id=? AND e.project_id=?`, id, projectID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTriggerNotFound
	}
	return t, err
}

type ProviderEvent struct {
	DeliveryID    string
	Event         string
	Repository    string
	Ref           string
	Action        string
	Revision      string
	ChangedPaths  []string
	PreviewRef    string
	PreviewNumber int
	PreviewClosed bool
}

func VerifyProvider(provider string, headers http.Header, body []byte, secret string) (ProviderEvent, error) {
	provider = strings.ToLower(provider)
	if len(body) > MaxAutomationBody {
		return ProviderEvent{}, fmt.Errorf("payload too large")
	}
	var signature, delivery, event string
	switch provider {
	case "github":
		signature = headers.Get("X-Hub-Signature-256")
		delivery = headers.Get("X-GitHub-Delivery")
		event = headers.Get("X-GitHub-Event")
	case "gitlab":
		signature = headers.Get("X-Gitlab-Token")
		delivery = headers.Get("X-Gitlab-Event-UUID")
		event = headers.Get("X-Gitlab-Event")
	case "bitbucket":
		signature = headers.Get("X-Hub-Signature")
		delivery = headers.Get("X-Request-UUID")
		event = headers.Get("X-Event-Key")
	case "gitea":
		signature = headers.Get("X-Gitea-Signature")
		delivery = headers.Get("X-Gitea-Delivery")
		event = headers.Get("X-Gitea-Event")
	default:
		return ProviderEvent{}, ErrWrongEvent
	}
	if provider == "gitlab" {
		if !hmac.Equal([]byte(signature), []byte(secret)) {
			return ProviderEvent{}, ErrBadHookSignature
		}
	} else if !verifyHMAC(body, secret, signature) {
		return ProviderEvent{}, ErrBadHookSignature
	}
	if strings.TrimSpace(delivery) == "" || len(delivery) > 256 {
		return ProviderEvent{}, ErrWrongEvent
	}
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return ProviderEvent{}, ErrWrongEvent
	}
	result := ProviderEvent{DeliveryID: delivery, Event: event}
	if !validProviderEvent(provider, event) {
		return ProviderEvent{}, ErrWrongEvent
	}
	if provider == "github" || provider == "gitea" {
		result.Repository = nestedString(raw, "repository", "full_name")
		result.Ref = stringValue(raw["ref"])
		result.Revision = stringValue(raw["after"])
		result.Action = stringValue(raw["action"])
		result.ChangedPaths = githubChanged(raw)
		if pr, ok := raw["pull_request"].(map[string]any); ok {
			result.PreviewNumber = intValue(raw["number"])
			result.PreviewRef = nestedString(pr, "head", "ref")
			result.Revision = nestedString(pr, "head", "sha")
			result.PreviewClosed = result.Action == "closed"
		}
	}
	if provider == "gitlab" {
		result.Repository = nestedString(raw, "project", "path_with_namespace")
		result.Ref = stringValue(raw["ref"])
		result.Revision = stringValue(raw["checkout_sha"])
		result.Action = stringValue(raw["object_kind"])
		if attrs, ok := raw["object_attributes"].(map[string]any); ok {
			result.PreviewNumber = intValue(attrs["iid"])
			result.PreviewRef = stringValue(attrs["source_branch"])
			result.Revision = nestedString(attrs, "last_commit", "id")
			state := stringValue(attrs["state"])
			result.PreviewClosed = state == "closed" || state == "merged"
		}
	}
	if provider == "bitbucket" {
		result.Repository = nestedString(raw, "repository", "full_name")
		if push, ok := raw["push"].(map[string]any); ok {
			if changes, ok := push["changes"].([]any); ok && len(changes) > 0 {
				if c, ok := changes[0].(map[string]any); ok {
					result.Ref = nestedString(c, "new", "name")
					result.Revision = nestedString(c, "new", "target", "hash")
				}
			}
		}
		if pr, ok := raw["pullrequest"].(map[string]any); ok {
			result.PreviewNumber = intValue(pr["id"])
			result.PreviewRef = nestedString(pr, "source", "branch", "name")
			result.Revision = nestedString(pr, "source", "commit", "hash")
			result.PreviewClosed = strings.Contains(event, "fulfilled") || strings.Contains(event, "rejected")
		}
	}
	return result, nil
}

func validProviderEvent(provider, event string) bool {
	allowed := map[string]map[string]bool{"github": {"push": true, "pull_request": true}, "gitea": {"push": true, "pull_request": true}, "gitlab": {"Push Hook": true, "Merge Request Hook": true}, "bitbucket": {"repo:push": true, "pullrequest:created": true, "pullrequest:updated": true, "pullrequest:fulfilled": true, "pullrequest:rejected": true}}
	return allowed[provider][event]
}

func verifyHMAC(body []byte, secret, signature string) bool {
	signature = strings.TrimPrefix(signature, "sha256=")
	want := hmac.New(sha256.New, []byte(secret))
	want.Write(body)
	got, err := hex.DecodeString(signature)
	return err == nil && hmac.Equal(got, want.Sum(nil))
}
func VerifyGenericHook(body []byte, secret, signature string) bool {
	return verifyHMAC(body, secret, signature)
}
func stringValue(v any) string { s, _ := v.(string); return s }
func intValue(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	}
	return 0
}
func nestedString(v any, keys ...string) string {
	cur := v
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[k]
	}
	return stringValue(cur)
}
func githubChanged(raw map[string]any) []string {
	var out []string
	for _, name := range []string{"commits"} {
		if items, ok := raw[name].([]any); ok {
			for _, item := range items {
				if m, ok := item.(map[string]any); ok {
					for _, field := range []string{"added", "modified", "removed"} {
						if paths, ok := m[field].([]any); ok {
							for _, p := range paths {
								if s := stringValue(p); s != "" {
									out = append(out, s)
								}
							}
						}
					}
				}
			}
		}
	}
	sort.Strings(out)
	return uniqueStrings(out)
}
func uniqueStrings(in []string) []string {
	out := in[:0]
	var prior string
	for i, v := range in {
		if i == 0 || v != prior {
			out = append(out, v)
			prior = v
		}
	}
	return out
}

func MatchWatchPaths(changed, include, exclude []string) bool {
	if len(changed) == 0 {
		return len(include) == 0
	}
	for _, file := range changed {
		file = strings.TrimPrefix(path.Clean("/"+file), "/")
		included := len(include) == 0
		for _, pattern := range include {
			if globMatch(pattern, file) {
				included = true
				break
			}
		}
		if !included {
			continue
		}
		blocked := false
		for _, pattern := range exclude {
			if globMatch(pattern, file) {
				blocked = true
				break
			}
		}
		if !blocked {
			return true
		}
	}
	return false
}
func globMatch(pattern, name string) bool {
	pattern = strings.TrimPrefix(path.Clean("/"+pattern), "/")
	if strings.HasSuffix(pattern, "/**") {
		base := strings.TrimSuffix(pattern, "/**")
		return name == base || strings.HasPrefix(name, base+"/")
	}
	ok, _ := path.Match(pattern, name)
	return ok
}

func (s *AutomationStore) RecordDelivery(ctx context.Context, t *Trigger, event ProviderEvent, body []byte, status, reason string, runID int64) error {
	digest := sha256.Sum256(body)
	_, err := s.db.ExecContext(ctx, `INSERT INTO deploy_webhook_deliveries(trigger_id,delivery_id,event,repository,ref,body_digest,status,reason,run_id,received_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, t.ID, event.DeliveryID, event.Event, event.Repository, event.Ref, hex.EncodeToString(digest[:]), status, reason, runID, s.now().UTC().Unix())
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return ErrDeliveryReplayed
	}
	if err == nil {
		_, err = s.db.ExecContext(ctx, `UPDATE deploy_triggers SET last_delivery_at=?,last_status=?,updated_at=? WHERE id=?`, s.now().UTC().Unix(), status, s.now().UTC().Unix(), t.ID)
	}
	return err
}

func (s *AutomationStore) FinishDelivery(ctx context.Context, t *Trigger, deliveryID, status, reason string, runID int64) error {
	now := s.now().UTC().Unix()
	result, err := s.db.ExecContext(ctx, `UPDATE deploy_webhook_deliveries SET status=?,reason=?,run_id=? WHERE trigger_id=? AND delivery_id=?`, status, reason, runID, t.ID, deliveryID)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return ErrTriggerNotFound
	}
	_, err = s.db.ExecContext(ctx, `UPDATE deploy_triggers SET last_delivery_at=?,last_status=?,updated_at=? WHERE id=?`, now, status, now, t.ID)
	return err
}

type Schedule struct {
	ID            int64          `json:"id"`
	EnvironmentID int64          `json:"environmentId"`
	ProjectID     int64          `json:"projectId"`
	Name          string         `json:"name"`
	Expression    string         `json:"expression"`
	Timezone      string         `json:"timezone"`
	Enabled       bool           `json:"enabled"`
	NextRunAt     *time.Time     `json:"nextRunAt,omitempty"`
	Steps         []ScheduleStep `json:"steps"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}
type ScheduleStep struct {
	Action   string          `json:"action"`
	Config   json.RawMessage `json:"config"`
	Required bool            `json:"required"`
}

type ScheduleWrite struct {
	Name       string         `json:"name"`
	Expression string         `json:"expression"`
	Timezone   string         `json:"timezone"`
	Enabled    bool           `json:"enabled"`
	Steps      []ScheduleStep `json:"steps"`
}

func (s *AutomationStore) ListSchedules(ctx context.Context, projectID, environmentID int64) ([]Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.id,s.environment_id,e.project_id,s.name,s.expression,s.timezone,s.enabled,s.next_run_at,s.created_at,s.updated_at FROM deploy_schedules s JOIN deploy_environments e ON e.id=s.environment_id WHERE e.project_id=? AND s.environment_id=? ORDER BY s.id`, projectID, environmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Schedule{}
	for rows.Next() {
		var v Schedule
		var enabled int
		var next, created, updated int64
		if err := rows.Scan(&v.ID, &v.EnvironmentID, &v.ProjectID, &v.Name, &v.Expression, &v.Timezone, &enabled, &next, &created, &updated); err != nil {
			return nil, err
		}
		v.Enabled = enabled != 0
		v.CreatedAt = time.Unix(created, 0).UTC()
		v.UpdatedAt = time.Unix(updated, 0).UTC()
		if next > 0 {
			x := time.Unix(next, 0).UTC()
			v.NextRunAt = &x
		}
		steps, err := s.scheduleSteps(ctx, v.ID)
		if err != nil {
			return nil, err
		}
		v.Steps = steps
		out = append(out, v)
	}
	return out, rows.Err()
}

type ScheduleDispatch struct {
	Schedule Schedule
	DueAt    time.Time
}

func (s *AutomationStore) ClaimDueSchedules(ctx context.Context, limit int) ([]ScheduleDispatch, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	now := s.now().UTC()
	rows, err := s.db.QueryContext(ctx, `SELECT s.id,s.environment_id,e.project_id,s.name,s.expression,s.timezone,s.enabled,s.next_run_at,s.created_at,s.updated_at FROM deploy_schedules s JOIN deploy_environments e ON e.id=s.environment_id WHERE s.enabled=1 AND s.next_run_at>0 AND s.next_run_at<=? AND e.archived_at=0 ORDER BY s.next_run_at LIMIT ?`, now.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScheduleDispatch{}
	for rows.Next() {
		var v Schedule
		var enabled int
		var next, created, updated int64
		if err := rows.Scan(&v.ID, &v.EnvironmentID, &v.ProjectID, &v.Name, &v.Expression, &v.Timezone, &enabled, &next, &created, &updated); err != nil {
			return nil, err
		}
		v.Enabled = true
		v.CreatedAt = time.Unix(created, 0).UTC()
		v.UpdatedAt = time.Unix(updated, 0).UTC()
		due := time.Unix(next, 0).UTC()
		v.NextRunAt = &due
		v.Steps, err = s.scheduleSteps(ctx, v.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, ScheduleDispatch{Schedule: v, DueAt: due})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	claimed := out[:0]
	for _, item := range out {
		next, calcErr := NextCron(item.Schedule.Expression, item.Schedule.Timezone, item.DueAt)
		if calcErr != nil {
			_, _ = s.db.ExecContext(ctx, `UPDATE deploy_schedules SET enabled=0,updated_at=? WHERE id=? AND next_run_at=?`, now.Unix(), item.Schedule.ID, item.DueAt.Unix())
			continue
		}
		result, updateErr := s.db.ExecContext(ctx, `UPDATE deploy_schedules SET next_run_at=?,updated_at=? WHERE id=? AND next_run_at=?`, next.Unix(), now.Unix(), item.Schedule.ID, item.DueAt.Unix())
		if updateErr != nil {
			return nil, updateErr
		}
		changed, _ := result.RowsAffected()
		if changed == 1 {
			item.Schedule.NextRunAt = &next
			claimed = append(claimed, item)
		}
	}
	return claimed, nil
}

type AutomationScheduler struct {
	store    *AutomationStore
	dispatch func(context.Context, ScheduleDispatch) error
	cancel   context.CancelFunc
	done     chan struct{}
}

func NewAutomationScheduler(store *AutomationStore, dispatch func(context.Context, ScheduleDispatch) error) *AutomationScheduler {
	return &AutomationScheduler{store: store, dispatch: dispatch}
}
func (s *AutomationScheduler) Start(ctx context.Context) {
	if s == nil || s.cancel != nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		s.run(runCtx)
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				s.run(runCtx)
			}
		}
	}()
}
func (s *AutomationScheduler) run(ctx context.Context) {
	due, err := s.store.ClaimDueSchedules(ctx, 20)
	if err != nil {
		return
	}
	for _, item := range due {
		_ = s.dispatch(ctx, item)
	}
}
func (s *AutomationScheduler) Stop() {
	if s == nil || s.cancel == nil {
		return
	}
	s.cancel()
	<-s.done
	s.cancel = nil
}
func (s *AutomationStore) scheduleSteps(ctx context.Context, id int64) ([]ScheduleStep, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT action,config_json,required FROM deploy_schedule_steps WHERE schedule_id=? ORDER BY ordinal`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScheduleStep{}
	for rows.Next() {
		var v ScheduleStep
		var cfg string
		var required int
		if err := rows.Scan(&v.Action, &cfg, &required); err != nil {
			return nil, err
		}
		v.Config = json.RawMessage(cfg)
		v.Required = required != 0
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *AutomationStore) CreateSchedule(ctx context.Context, projectID, environmentID int64, in ScheduleWrite) (*Schedule, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 100 {
		return nil, fmt.Errorf("schedule name is required")
	}
	if in.Timezone == "" {
		in.Timezone = "UTC"
	}
	next, err := NextCron(in.Expression, in.Timezone, s.now())
	if err != nil {
		return nil, err
	}
	if len(in.Steps) == 0 {
		return nil, fmt.Errorf("schedule needs at least one action")
	}
	for _, step := range in.Steps {
		if !validScheduleAction(step.Action) || !json.Valid(step.Config) {
			return nil, fmt.Errorf("invalid scheduled action %q", step.Action)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := s.now().UTC().Unix()
	res, err := tx.ExecContext(ctx, `INSERT INTO deploy_schedules(environment_id,name,expression,timezone,enabled,next_run_at,created_at,updated_at) SELECT id,?,?,?,?,?,?,? FROM deploy_environments WHERE id=? AND project_id=? AND archived_at=0`, in.Name, in.Expression, in.Timezone, boolInt(in.Enabled), next.Unix(), now, now, environmentID, projectID)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	for i, step := range in.Steps {
		if _, err = tx.ExecContext(ctx, `INSERT INTO deploy_schedule_steps(schedule_id,ordinal,action,config_json,required) VALUES(?,?,?,?,?)`, id, i, step.Action, string(step.Config), boolInt(step.Required)); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	list, err := s.ListSchedules(ctx, projectID, environmentID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == id {
			return &list[i], nil
		}
	}
	return nil, ErrTriggerNotFound
}
func (s *AutomationStore) UpdateSchedule(ctx context.Context, projectID, environmentID, scheduleID int64, in ScheduleWrite) (*Schedule, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 100 {
		return nil, fmt.Errorf("schedule name is required")
	}
	if in.Timezone == "" {
		in.Timezone = "UTC"
	}
	next, err := NextCron(in.Expression, in.Timezone, s.now())
	if err != nil {
		return nil, err
	}
	if len(in.Steps) == 0 {
		return nil, fmt.Errorf("schedule needs at least one action")
	}
	for _, step := range in.Steps {
		if !validScheduleAction(step.Action) || !json.Valid(step.Config) {
			return nil, fmt.Errorf("invalid scheduled action %q", step.Action)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := s.now().UTC().Unix()
	result, err := tx.ExecContext(ctx, `UPDATE deploy_schedules SET name=?,expression=?,timezone=?,enabled=?,next_run_at=?,updated_at=? WHERE id=? AND environment_id=? AND environment_id IN (SELECT id FROM deploy_environments WHERE project_id=? AND archived_at=0)`, in.Name, in.Expression, in.Timezone, boolInt(in.Enabled), next.Unix(), now, scheduleID, environmentID, projectID)
	if err != nil {
		return nil, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return nil, ErrTriggerNotFound
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM deploy_schedule_steps WHERE schedule_id=?`, scheduleID); err != nil {
		return nil, err
	}
	for i, step := range in.Steps {
		if _, err = tx.ExecContext(ctx, `INSERT INTO deploy_schedule_steps(schedule_id,ordinal,action,config_json,required) VALUES(?,?,?,?,?)`, scheduleID, i, step.Action, string(step.Config), boolInt(step.Required)); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	list, err := s.ListSchedules(ctx, projectID, environmentID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == scheduleID {
			return &list[i], nil
		}
	}
	return nil, ErrTriggerNotFound
}
func (s *AutomationStore) DeleteSchedule(ctx context.Context, projectID, environmentID, scheduleID int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM deploy_schedules WHERE id=? AND environment_id=? AND environment_id IN (SELECT id FROM deploy_environments WHERE project_id=?)`, scheduleID, environmentID, projectID)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return ErrTriggerNotFound
	}
	return nil
}
func validScheduleAction(action string) bool {
	switch action {
	case "deploy", "restart", "container_command", "game_command", "backup":
		return true
	}
	return false
}

type PreviewRef struct {
	ID              int64     `json:"id"`
	TriggerID       int64     `json:"triggerId"`
	ProviderRef     string    `json:"providerRef"`
	EnvironmentID   int64     `json:"environmentId"`
	EnvironmentSlug string    `json:"environmentSlug"`
	State           string    `json:"state"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

func (s *AutomationStore) ListPreviews(ctx context.Context, projectID int64) ([]PreviewRef, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.id,p.trigger_id,p.provider_ref,p.environment_id,e.slug,p.state,p.updated_at FROM deploy_preview_refs p JOIN deploy_environments e ON e.id=p.environment_id WHERE e.project_id=? ORDER BY p.updated_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PreviewRef{}
	for rows.Next() {
		var p PreviewRef
		var updated int64
		if err := rows.Scan(&p.ID, &p.TriggerID, &p.ProviderRef, &p.EnvironmentID, &p.EnvironmentSlug, &p.State, &updated); err != nil {
			return nil, err
		}
		p.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *AutomationStore) EnsurePreview(ctx context.Context, t *Trigger, event ProviderEvent) (*PreviewRef, bool, error) {
	if !t.Config.Preview || event.PreviewNumber <= 0 {
		return nil, false, ErrWrongEvent
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	ref := fmt.Sprintf("%d", event.PreviewNumber)
	var existing PreviewRef
	var updated int64
	err = tx.QueryRowContext(ctx, `SELECT p.id,p.trigger_id,p.provider_ref,p.environment_id,e.slug,p.state,p.updated_at FROM deploy_preview_refs p JOIN deploy_environments e ON e.id=p.environment_id WHERE p.trigger_id=? AND p.provider_ref=?`, t.ID, ref).Scan(&existing.ID, &existing.TriggerID, &existing.ProviderRef, &existing.EnvironmentID, &existing.EnvironmentSlug, &existing.State, &updated)
	if err == nil {
		existing.UpdatedAt = time.Unix(updated, 0).UTC()
		if existing.State == "closed" {
			return nil, false, ErrWrongEvent
		}
		if event.PreviewClosed {
			now := s.now().UTC().Unix()
			_, err = tx.ExecContext(ctx, `UPDATE deploy_preview_refs SET state='closed',updated_at=? WHERE id=?`, now, existing.ID)
			existing.State = "closed"
		} else {
			var revision int
			if err = tx.QueryRowContext(ctx, `SELECT desired_revision FROM deploy_environments WHERE id=? AND kind='preview' AND archived_at=0`, existing.EnvironmentID).Scan(&revision); err == nil {
				next := revision + 1
				for _, table := range []string{"deploy_build_plans", "deploy_runtime_plans"} {
					columns := map[string]string{"deploy_sources": "kind,config_json,credential_id,identity_json,digest,created_at", "deploy_build_plans": "method,config_json,evidence_json,preview,digest,created_at", "deploy_runtime_plans": "config_json,preview,digest,created_at"}[table]
					_, err = tx.ExecContext(ctx, `INSERT INTO `+table+`(environment_id,revision,`+columns+`) SELECT ?,?,`+columns+` FROM `+table+` WHERE environment_id=? AND revision=?`, existing.EnvironmentID, next, existing.EnvironmentID, revision)
					if err != nil {
						break
					}
				}
				if err == nil {
					err = createPreviewSourceTx(ctx, tx, existing.EnvironmentID, revision, existing.EnvironmentID, next, event)
				}
				if err == nil {
					_, err = tx.ExecContext(ctx, `UPDATE deploy_environments SET desired_revision=?,updated_at=? WHERE id=?`, next, s.now().UTC().Unix(), existing.EnvironmentID)
				}
			}
		}
		if err != nil {
			return nil, false, err
		}
		if err = tx.Commit(); err != nil {
			return nil, false, err
		}
		return &existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	if event.PreviewClosed {
		return nil, false, ErrWrongEvent
	}
	quota := t.Config.PreviewQuota
	if quota == 0 {
		quota = 5
	}
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM deploy_preview_refs WHERE trigger_id=? AND state='open'`, t.ID).Scan(&active); err != nil {
		return nil, false, err
	}
	if active >= quota {
		return nil, false, ErrPreviewQuota
	}
	var name, strategy string
	var desired int
	var downtime, protected int
	if err = tx.QueryRowContext(ctx, `SELECT name,desired_revision,strategy,expected_downtime,protected FROM deploy_environments WHERE id=?`, t.EnvironmentID).Scan(&name, &desired, &strategy, &downtime, &protected); err != nil {
		return nil, false, err
	}
	slug := fmt.Sprintf("pr-%d", event.PreviewNumber)
	now := s.now().UTC().Unix()
	res, err := tx.ExecContext(ctx, `INSERT INTO deploy_environments(project_id,name,slug,kind,desired_revision,strategy,expected_downtime,protected,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, t.ProjectID, "Preview "+ref, slug, EnvironmentPreview, desired, strategy, downtime, 0, now, now)
	if err != nil {
		return nil, false, err
	}
	envID, _ := res.LastInsertId()
	for _, table := range []string{"deploy_build_plans", "deploy_runtime_plans"} {
		columns := map[string]string{"deploy_sources": "revision,kind,config_json,credential_id,identity_json,digest,created_at", "deploy_build_plans": "revision,method,config_json,evidence_json,preview,digest,created_at", "deploy_runtime_plans": "revision,config_json,preview,digest,created_at"}[table]
		if _, err = tx.ExecContext(ctx, `INSERT INTO `+table+`(environment_id,`+columns+`) SELECT ?,`+columns+` FROM `+table+` WHERE environment_id=? AND revision=?`, envID, t.EnvironmentID, desired); err != nil {
			return nil, false, err
		}
	}
	if err = createPreviewSourceTx(ctx, tx, t.EnvironmentID, desired, envID, desired, event); err != nil {
		return nil, false, err
	}
	// Secret values are inherited as ciphertext revisions, never opened during
	// preview creation. Managed production resources are deliberately not
	// copied: a preview may link read-only dependencies, but must create any
	// mutable runtime, route, or storage under its own ownership.
	if _, err = tx.ExecContext(ctx, `INSERT INTO deploy_variable_revisions(environment_id,key,revision,sensitivity,scopes,value_enc,value_digest,active,created_by,created_at) SELECT ?,key,revision,sensitivity,scopes,value_enc,value_digest,active,'preview',? FROM deploy_variable_revisions WHERE environment_id=? AND active=1`, envID, now, t.EnvironmentID); err != nil {
		return nil, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO deploy_dependencies(environment_id,release_id,kind,ownership,resource_kind,resource_id,config_json,created_at) SELECT ?,0,kind,ownership,resource_kind,resource_id,config_json,? FROM deploy_dependencies WHERE environment_id=? AND release_id=0 AND ownership IN ('linked','observed')`, envID, now, t.EnvironmentID); err != nil {
		return nil, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO deploy_checks(environment_id,runtime_plan_id,name,kind,phase,config_json,required,ordinal,created_at) SELECT ?,0,name,kind,phase,config_json,required,ordinal,? FROM deploy_checks WHERE environment_id=? AND runtime_plan_id=0`, envID, now, t.EnvironmentID); err != nil {
		return nil, false, err
	}
	if domain := strings.TrimSpace(t.Config.PreviewDomain); domain != "" {
		domain = strings.ToLower(strings.ReplaceAll(domain, "{number}", ref))
		if !strings.Contains(domain, ".") || strings.ContainsAny(domain, " /\\") {
			return nil, false, fmt.Errorf("%w: invalid preview domain", ErrInvalidPlan)
		}
		config, _ := json.Marshal(map[string]any{"hostname": domain, "https": true, "preview": true})
		if _, err = tx.ExecContext(ctx, `INSERT INTO deploy_dependencies(environment_id,release_id,kind,ownership,resource_kind,resource_id,config_json,created_at) VALUES(?,0,'domain','managed','proxy_site',?,?,?)`, envID, domain, string(config), now); err != nil {
			return nil, false, err
		}
	}
	res, err = tx.ExecContext(ctx, `INSERT INTO deploy_preview_refs(trigger_id,provider_ref,environment_id,state,updated_at) VALUES(?,?,?,'open',?)`, t.ID, ref, envID, now)
	if err != nil {
		return nil, false, err
	}
	previewID, _ := res.LastInsertId()
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return &PreviewRef{ID: previewID, TriggerID: t.ID, ProviderRef: ref, EnvironmentID: envID, EnvironmentSlug: slug, State: "open", UpdatedAt: time.Unix(now, 0).UTC()}, true, nil
}

func createPreviewSourceTx(ctx context.Context, tx *sql.Tx, sourceEnvironmentID int64, sourceRevision int, targetEnvironmentID int64, targetRevision int, event ProviderEvent) error {
	var kind, configText, identityText string
	var credentialID, createdAt int64
	if err := tx.QueryRowContext(ctx, `SELECT kind,config_json,credential_id,identity_json,created_at FROM deploy_sources WHERE environment_id=? AND revision=?`, sourceEnvironmentID, sourceRevision).Scan(&kind, &configText, &credentialID, &identityText, &createdAt); err != nil {
		return err
	}
	var config, identity map[string]any
	if json.Unmarshal([]byte(configText), &config) != nil || json.Unmarshal([]byte(identityText), &identity) != nil {
		return fmt.Errorf("%w: malformed preview source", ErrInvalidPlan)
	}
	if event.PreviewRef != "" {
		config["ref"] = event.PreviewRef
	}
	identity["ref"] = event.PreviewRef
	identity["revision"] = event.Revision
	identity["previewNumber"] = event.PreviewNumber
	configJSON, _ := json.Marshal(config)
	identityJSON, _ := json.Marshal(identity)
	digest := sha256.Sum256(append(append([]byte(nil), configJSON...), identityJSON...))
	_, err := tx.ExecContext(ctx, `INSERT INTO deploy_sources(environment_id,revision,kind,config_json,credential_id,identity_json,digest,created_at) VALUES(?,?,?,?,?,?,?,?)`, targetEnvironmentID, targetRevision, kind, string(configJSON), credentialID, string(identityJSON), hex.EncodeToString(digest[:]), createdAt)
	return err
}

func (s *AutomationStore) ArchiveClosedPreview(ctx context.Context, previewID int64) error {
	now := s.now().UTC().Unix()
	result, err := s.db.ExecContext(ctx, `UPDATE deploy_environments SET archived_at=?,updated_at=? WHERE id=(SELECT environment_id FROM deploy_preview_refs WHERE id=? AND state='closed') AND kind='preview'`, now, now, previewID)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return ErrTriggerNotFound
	}
	return nil
}

// NextCron implements the deliberately small five-field schedule contract. It
// walks civil minutes in the requested IANA location, so spring gaps are
// skipped and both fall-back instants remain distinct.
func NextCron(expression, timezone string, after time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone")
	}
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("cron expression must have five fields")
	}
	sets := make([]map[int]bool, 5)
	ranges := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	for i, f := range fields {
		sets[i], err = parseCronField(f, ranges[i][0], ranges[i][1])
		if err != nil {
			return time.Time{}, err
		}
	}
	candidate := after.UTC().Truncate(time.Minute).Add(time.Minute)
	limit := candidate.Add(370 * 24 * time.Hour)
	for candidate.Before(limit) {
		c := candidate.In(loc)
		if sets[0][c.Minute()] && sets[1][c.Hour()] && sets[2][c.Day()] && sets[3][int(c.Month())] && sets[4][int(c.Weekday())] {
			return candidate, nil
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("schedule has no occurrence in the next year")
}
func parseCronField(raw string, min, max int) (map[int]bool, error) {
	out := map[int]bool{}
	for _, part := range strings.Split(raw, ",") {
		step := 1
		base := part
		if strings.Contains(part, "/") {
			pieces := strings.Split(part, "/")
			if len(pieces) != 2 {
				return nil, fmt.Errorf("invalid cron field")
			}
			base = pieces[0]
			var err error
			step, err = strconv.Atoi(pieces[1])
			if err != nil || step < 1 {
				return nil, fmt.Errorf("invalid cron step")
			}
		}
		lo, hi := min, max
		if base != "*" {
			if strings.Contains(base, "-") {
				p := strings.Split(base, "-")
				if len(p) != 2 {
					return nil, fmt.Errorf("invalid cron range")
				}
				lo, _ = strconv.Atoi(p[0])
				hi, _ = strconv.Atoi(p[1])
			} else {
				lo, _ = strconv.Atoi(base)
				hi = lo
			}
		}
		if lo < min || hi > max || lo > hi {
			return nil, fmt.Errorf("cron value outside range")
		}
		for n := lo; n <= hi; n += step {
			out[n] = true
		}
	}
	return out, nil
}

type NotificationEnvelope struct {
	Event         string    `json:"event"`
	RunID         int64     `json:"runId"`
	ProjectID     int64     `json:"projectId"`
	EnvironmentID int64     `json:"environmentId"`
	State         string    `json:"state"`
	SentAt        time.Time `json:"sentAt"`
}

type NotificationChannel struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type NotificationWrite struct {
	Name    string            `json:"name"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Secret  string            `json:"secret,omitempty"`
	Events  []string          `json:"events"`
	Enabled bool              `json:"enabled"`
}
type NotificationDelivery struct {
	ID            int64      `json:"id"`
	ChannelID     int64      `json:"channelId"`
	RunID         int64      `json:"runId,omitempty"`
	Event         string     `json:"event"`
	Attempt       int        `json:"attempt"`
	Status        string     `json:"status"`
	ResponseClass string     `json:"responseClass"`
	NextAttemptAt *time.Time `json:"nextAttemptAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
}

func (s *AutomationStore) NotificationDeliveries(ctx context.Context, channelID int64, limit int) ([]NotificationDelivery, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,channel_id,run_id,event,attempt,status,response_class,next_attempt_at,created_at,completed_at FROM deploy_notification_deliveries WHERE channel_id=? ORDER BY id DESC LIMIT ?`, channelID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NotificationDelivery{}
	for rows.Next() {
		var d NotificationDelivery
		var next, created, completed int64
		if err := rows.Scan(&d.ID, &d.ChannelID, &d.RunID, &d.Event, &d.Attempt, &d.Status, &d.ResponseClass, &next, &created, &completed); err != nil {
			return nil, err
		}
		d.CreatedAt = time.Unix(created, 0).UTC()
		if next > 0 {
			x := time.Unix(next, 0).UTC()
			d.NextAttemptAt = &x
		}
		if completed > 0 {
			x := time.Unix(completed, 0).UTC()
			d.CompletedAt = &x
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *AutomationStore) ListNotificationChannels(ctx context.Context) ([]NotificationChannel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,url,events,enabled,created_at,updated_at FROM deploy_notification_channels ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NotificationChannel{}
	for rows.Next() {
		var c NotificationChannel
		var events string
		var enabled int
		var created, updated int64
		if err := rows.Scan(&c.ID, &c.Name, &c.URL, &events, &enabled, &created, &updated); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(events), &c.Events)
		c.Enabled = enabled != 0
		c.CreatedAt = time.Unix(created, 0).UTC()
		c.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *AutomationStore) CreateNotificationChannel(ctx context.Context, in NotificationWrite) (*NotificationChannel, string, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.URL = strings.TrimSpace(in.URL)
	if in.Name == "" || len(in.Name) > 100 {
		return nil, "", fmt.Errorf("channel name is required")
	}
	if !strings.HasPrefix(in.URL, "https://") && !strings.HasPrefix(in.URL, "http://") {
		return nil, "", fmt.Errorf("notification URL must use http or https")
	}
	if in.Secret == "" {
		var err error
		in.Secret, err = randomHex(32)
		if err != nil {
			return nil, "", err
		}
	}
	headers, err := json.Marshal(in.Headers)
	if err != nil {
		return nil, "", err
	}
	sealedHeaders, err := s.sealer.Seal(string(headers))
	if err != nil {
		return nil, "", err
	}
	sealedSecret, err := s.sealer.Seal(in.Secret)
	if err != nil {
		return nil, "", err
	}
	events, _ := json.Marshal(in.Events)
	now := s.now().UTC().Unix()
	res, err := s.db.ExecContext(ctx, `INSERT INTO deploy_notification_channels(name,url,headers_enc,secret_enc,events,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, in.Name, in.URL, sealedHeaders, sealedSecret, string(events), boolInt(in.Enabled), now, now)
	if err != nil {
		return nil, "", err
	}
	id, _ := res.LastInsertId()
	channels, err := s.ListNotificationChannels(ctx)
	if err != nil {
		return nil, "", err
	}
	for i := range channels {
		if channels[i].ID == id {
			return &channels[i], in.Secret, nil
		}
	}
	return nil, "", ErrTriggerNotFound
}
func (s *AutomationStore) UpdateNotificationChannel(ctx context.Context, id int64, in NotificationWrite) (*NotificationChannel, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.URL = strings.TrimSpace(in.URL)
	if in.Name == "" || len(in.Name) > 100 {
		return nil, fmt.Errorf("channel name is required")
	}
	if !strings.HasPrefix(in.URL, "https://") && !strings.HasPrefix(in.URL, "http://") {
		return nil, fmt.Errorf("notification URL must use http or https")
	}
	headers, err := json.Marshal(in.Headers)
	if err != nil {
		return nil, err
	}
	sealedHeaders, err := s.sealer.Seal(string(headers))
	if err != nil {
		return nil, err
	}
	events, _ := json.Marshal(in.Events)
	now := s.now().UTC().Unix()
	result, err := s.db.ExecContext(ctx, `UPDATE deploy_notification_channels SET name=?,url=?,headers_enc=?,events=?,enabled=?,updated_at=? WHERE id=?`, in.Name, in.URL, sealedHeaders, string(events), boolInt(in.Enabled), now, id)
	if err != nil {
		return nil, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return nil, ErrTriggerNotFound
	}
	if in.Secret != "" {
		sealedSecret, sealErr := s.sealer.Seal(in.Secret)
		if sealErr != nil {
			return nil, sealErr
		}
		if _, err = s.db.ExecContext(ctx, `UPDATE deploy_notification_channels SET secret_enc=?,updated_at=? WHERE id=?`, sealedSecret, now, id); err != nil {
			return nil, err
		}
	}
	channels, err := s.ListNotificationChannels(ctx)
	if err != nil {
		return nil, err
	}
	for i := range channels {
		if channels[i].ID == id {
			return &channels[i], nil
		}
	}
	return nil, ErrTriggerNotFound
}
func (s *AutomationStore) DeleteNotificationChannel(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM deploy_notification_channels WHERE id=?`, id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return ErrTriggerNotFound
	}
	return nil
}

// DeliverNotification signs the exact JSON bytes and deliberately discards
// the response body. History retains only its status class, so a remote
// endpoint cannot reflect a secret into this database or the deployment log.
func (s *AutomationStore) DeliverNotification(ctx context.Context, client *http.Client, channelID int64, envelope NotificationEnvelope) error {
	var url, sealedHeaders, sealedSecret, events string
	var enabled int
	if err := s.db.QueryRowContext(ctx, `SELECT url,headers_enc,secret_enc,events,enabled FROM deploy_notification_channels WHERE id=?`, channelID).Scan(&url, &sealedHeaders, &sealedSecret, &events, &enabled); err != nil {
		return err
	}
	if enabled == 0 {
		return ErrHookDisabled
	}
	secret, err := s.sealer.Open(sealedSecret)
	if err != nil {
		return err
	}
	headersJSON, err := s.sealer.Open(sealedHeaders)
	if err != nil {
		return err
	}
	var headers map[string]string
	if err = json.Unmarshal([]byte(headersJSON), &headers); err != nil {
		return err
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-JD-Event", envelope.Event)
	req.Header.Set("X-JD-Signature-256", SignNotification(body, secret))
	for key, value := range headers {
		if strings.EqualFold(key, "Host") || strings.EqualFold(key, "Content-Length") {
			continue
		}
		req.Header.Set(key, value)
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	now := s.now().UTC().Unix()
	var attempt int
	if err = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(attempt),0)+1 FROM deploy_notification_deliveries WHERE channel_id=? AND run_id=? AND event=?`, channelID, envelope.RunID, envelope.Event).Scan(&attempt); err != nil {
		return err
	}
	res, err := client.Do(req)
	status := "failed"
	responseClass := "network"
	if err == nil {
		res.Body.Close()
		responseClass = fmt.Sprintf("%dxx", res.StatusCode/100)
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			status = "delivered"
		}
	}
	completed := s.now().UTC().Unix()
	_, storeErr := s.db.ExecContext(ctx, `INSERT INTO deploy_notification_deliveries(channel_id,run_id,event,attempt,status,response_class,created_at,completed_at) VALUES(?,?,?,?,?,?,?,?)`, channelID, envelope.RunID, envelope.Event, attempt, status, responseClass, now, completed)
	if storeErr != nil {
		return storeErr
	}
	if err != nil {
		return err
	}
	if status != "delivered" {
		return fmt.Errorf("notification endpoint returned %s", responseClass)
	}
	return nil
}

func SignNotification(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
