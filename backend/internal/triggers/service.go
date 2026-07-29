package triggers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	agentsclient "github.com/clear-route/orbit/internal/platform/clients/agents"
	policyclient "github.com/clear-route/orbit/internal/platform/clients/policy"
	"github.com/clear-route/orbit/internal/platform/logging"
)

const (
	// sourceTokenTTL bounds the JIT SERVICE token minted to start a run. It only
	// needs to outlive a single StartRun call to orbit-agents.
	sourceTokenTTL = 2 * time.Minute
	// dispatchTimeout bounds the detached background dispatch of a delivery.
	dispatchTimeout = 60 * time.Second
	// payloadExcerptBytes caps the stored payload excerpt for debugging.
	payloadExcerptBytes = 512
)

// sourceRunScopes are granted to every source/trigger principal at provisioning
// time, before any agent is bound. It is deliberately only the agent-type read:
// CreateTrigger looks the agent type up under the principal's own
// (tenant-scoped) identity to derive its canonical slug, and nothing else here
// happens before a binding exists.
//
// The broad agents:runs:read/write pair is NOT granted. Scope hierarchy is by
// prefix, so agents:runs:write covers every agents:runs:<slug>:write — granting
// it would have made the per-agent narrowing in triggerRunScopes decorative, and
// a leaked source token could start any agent in the tenant. The route scope on
// POST /api/v1/runs is authenticated-only for exactly this reason (see
// cmd/orbit-agents/main.go agentRoutes); the authoritative check is
// orbit-agents' own per-slug Authz.checkRun.
var sourceRunScopes = []string{"agents:types:read"}

// triggerRunScopes are the per-agent run scopes a source/trigger principal needs
// to start runs for a specific agent slug. These are the ONLY run scopes a
// trigger principal ever holds, so its blast radius is the one agent it is bound
// to.
func triggerRunScopes(slug string) []string {
	return []string{"agents:runs:" + slug + ":read", "agents:runs:" + slug + ":write"}
}

// TokenMinter mints a short-lived platform token for a non-interactive principal.
// It is satisfied by the identity service and injected from main, so the triggers
// package never imports the identity feature package.
type TokenMinter interface {
	MintServiceToken(ctx context.Context, tenantID, principalID, displayName string, ttl time.Duration) (string, error)
}

// IngestOutcome is the result of accepting an inbound delivery.
type IngestOutcome int

const (
	// IngestAccepted means the delivery was verified, recorded, and queued for dispatch.
	IngestAccepted IngestOutcome = iota
	// IngestDuplicate means the delivery id was already processed (redelivery).
	IngestDuplicate
)

// Service implements the Triggers business logic.
type Service struct {
	repo        Repository
	registry    *Registry
	cipher      *secretCipher
	policy      *policyclient.Client
	agents      *agentsclient.Client
	minter      TokenMinter
	admission   *Admission
	outcomes    outcomeRecorder
	logger      logging.Logger
	metrics     *Metrics // nil records nothing
	webhookBase string   // external path prefix for inbound webhooks, incl trailing slash
}

// NewService constructs the Triggers service. webhookBase is the absolute path
// prefix for inbound webhook URLs (e.g. "/api/v1/webhooks/").
//
// metrics may be nil, in which case nothing is recorded: instrumentation is
// injected rather than reached for globally, so a caller that has not wired a
// meter gets exactly the behavior it had before.
func NewService(
	repo Repository,
	registry *Registry,
	cipher *secretCipher,
	policy *policyclient.Client,
	agents *agentsclient.Client,
	minter TokenMinter,
	webhookBase string,
	logger logging.Logger,
	metrics *Metrics,
) *Service {
	return &Service{
		metrics:     metrics,
		repo:        repo,
		registry:    registry,
		cipher:      cipher,
		policy:      policy,
		agents:      agents,
		minter:      minter,
		admission:   NewAdmission(repo, logger),
		outcomes:    outcomeRecorder{repo: repo, logger: logger},
		logger:      logger,
		webhookBase: webhookBase,
	}
}

// NewSecretCipherFromKey exposes secretCipher construction to the binary wiring.
func NewSecretCipherFromKey(key []byte) (*secretCipher, error) { return newSecretCipher(key) }

// WebhookPath returns the inbound webhook path for a source id.
func (s *Service) WebhookPath(sourceID string) string { return s.webhookBase + sourceID }

// ─── Payload retention ────────────────────────────────────────────────────────

// GetRetention returns a tenant's payload retention window.
//
// A tenant who has never chosen one inherits the platform default, and says so:
// is_default is the difference between "we picked 30" and "we picked nothing",
// which decides whether a later change to the default moves this tenant.
func (s *Service) GetRetention(ctx context.Context, tenantID string) (*RetentionSettings, error) {
	days, configured, err := s.repo.GetRetention(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if !configured {
		return &RetentionSettings{PayloadDays: DefaultPayloadRetentionDays, IsDefault: true}, nil
	}
	return &RetentionSettings{PayloadDays: days}, nil
}

// SetRetention chooses how long this tenant's delivery payloads are kept.
//
// The window can only be shortened relative to the platform default. That
// asymmetry is the point: retention is a promise about third-party payload data
// the platform is holding, and a tenant extending it would raise the platform's
// exposure rather than their own.
func (s *Service) SetRetention(ctx context.Context, tenantID string, req UpdateRetentionRequest) (*RetentionSettings, error) {
	if req.PayloadDays < MinPayloadRetentionDays || req.PayloadDays > DefaultPayloadRetentionDays {
		return nil, fmt.Errorf("%w: payload_days must be between %d and %d",
			ErrValidation, MinPayloadRetentionDays, DefaultPayloadRetentionDays)
	}
	if err := s.repo.SetRetention(ctx, tenantID, req.PayloadDays); err != nil {
		return nil, err
	}
	s.logger.Info(ctx, "payload retention updated",
		logging.Field{Key: "tenant_id", Value: tenantID},
		logging.Field{Key: "payload_days", Value: req.PayloadDays},
	)
	return &RetentionSettings{PayloadDays: req.PayloadDays}, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// Admission bounds. The ceilings are deliberately low: these settings exist to
// protect the platform from a trigger that fires faster than its agent runs, and
// a caller who needs more parallelism than this wants more triggers, not a
// bigger number here. maxParallelCeiling matches the triggers_max_parallel_check
// constraint in the admission_control migration — validating against a looser
// bound here would turn a caller's mistake into a database error.
const (
	minParallel        int16 = 1
	maxParallelCeiling int16 = 16
	minQueueDepth      int16 = 1
	maxQueueDepth      int16 = 500
	minTimeoutSeconds  int16 = 5
	maxTimeoutSeconds  int16 = 600
)

// What a trigger gets when the caller tunes nothing. These are business policy,
// so they live here beside the bounds rather than in the storage layer: a create
// writes them explicitly, and the matching column defaults in the migration are
// only a backstop for rows this service did not write.
//
// The default overflow is per kind (DefaultOverflowForKind) because it is the
// one setting where the right answer depends on what fired.
const (
	defaultMaxParallel    int16 = 4
	defaultMaxQueueDepth  int16 = 32
	defaultTimeoutSeconds int16 = 30
)

// Text bounds on what a trigger stores.
//
// Both columns are unbounded TEXT, so without these an authenticated caller can
// write a row of any size and, in the template's case, have it rendered into
// every prompt the trigger ever starts. The numbers are chosen to be invisible
// to real use: a name is a label a person reads in a list, and eight kilobytes
// of prompt is far past the point where a template is doing templating.
const (
	maxNameLen           = 128
	maxPromptTemplateLen = 8 << 10 // 8 KiB
)

// validateTextBounds checks the free-text fields of a create or update. Applied
// identically to both, so a value that could not be created cannot be edited in
// either.
func validateTextBounds(name, promptTemplate string) error {
	if len(name) > maxNameLen {
		return fmt.Errorf("%w: name must be %d characters or fewer", ErrValidation, maxNameLen)
	}
	if len(promptTemplate) > maxPromptTemplateLen {
		return fmt.Errorf("%w: prompt_template must be %d bytes or fewer", ErrValidation, maxPromptTemplateLen)
	}
	return nil
}

// Validate checks the settings against the platform bounds. Nil fields are
// unset and always valid.
//
// One implementation for create and update deliberately: a value that could not
// be created must not be reachable by editing.
func (a AdmissionSettings) Validate() error {
	if a.Overflow != nil {
		switch *a.Overflow {
		case OverflowWait, OverflowReplace, OverflowRefuse:
		default:
			return fmt.Errorf("%w: overflow must be one of %q, %q or %q", ErrValidation, OverflowWait, OverflowReplace, OverflowRefuse)
		}
	}
	if err := inRange("max_parallel", a.MaxParallel, minParallel, maxParallelCeiling); err != nil {
		return err
	}
	if err := inRange("max_queue_depth", a.MaxQueueDepth, minQueueDepth, maxQueueDepth); err != nil {
		return err
	}
	return inRange("timeout_seconds", a.TimeoutSeconds, minTimeoutSeconds, maxTimeoutSeconds)
}

// applyTo copies the settings the caller actually sent onto a trigger. An unset
// field leaves whatever the trigger already carries: the platform default on a
// create, the stored value on an update.
func (a AdmissionSettings) applyTo(t *Trigger) {
	if a.MaxParallel != nil {
		t.MaxParallel = *a.MaxParallel
	}
	if a.Overflow != nil {
		t.Overflow = *a.Overflow
	}
	if a.MaxQueueDepth != nil {
		t.MaxQueueDepth = *a.MaxQueueDepth
	}
	if a.TimeoutSeconds != nil {
		t.TimeoutSeconds = *a.TimeoutSeconds
	}
	if a.DedupeEnabled != nil {
		t.DedupeEnabled = *a.DedupeEnabled
	}
	if a.AutopauseEnabled != nil {
		t.AutopauseEnabled = *a.AutopauseEnabled
	}
}

// validateAdmissionCreate checks the free text and admission settings of a
// create request. It is called from each kind's validate* function so that a bad
// value is rejected before any principal or grant is provisioned: a rejected
// create must leave nothing behind.
func validateAdmissionCreate(req CreateTriggerRequest) error {
	if err := validateTextBounds(strings.TrimSpace(req.Name), req.PromptTemplate); err != nil {
		return err
	}
	return req.Validate()
}

// applyAdmissionUpdate validates and applies a PATCH's admission tuning, with
// exactly the create-time bounds. Omitted fields leave the stored value alone.
func applyAdmissionUpdate(t *Trigger, req UpdateTriggerRequest) error {
	// applyTriggerEdits has already copied the request's text onto t, so the
	// bounds are checked against what would actually be stored — an omitted
	// field keeps the stored value and is checked against that.
	if err := validateTextBounds(t.Name, t.PromptTemplate); err != nil {
		return err
	}
	if err := req.Validate(); err != nil {
		return err
	}
	req.applyTo(t)
	return nil
}

func inRange(field string, v *int16, lo, hi int16) error {
	if v == nil {
		return nil
	}
	if *v < lo || *v > hi {
		return fmt.Errorf("%w: %s must be between %d and %d", ErrValidation, field, lo, hi)
	}
	return nil
}

// applyAdmissionCreate writes a freshly built trigger's admission settings:
// every platform default first, then whatever the caller chose over the top.
//
// The row is fully determined here rather than half here and half in the
// storage layer. Defaulting is business policy, and a create that writes zeros
// for the repository to interpret means the settings a caller reads back depend
// on which write path stored them.
func applyAdmissionCreate(t *Trigger, req CreateTriggerRequest) error {
	if err := validateAdmissionCreate(req); err != nil {
		return err
	}
	t.MaxParallel = defaultMaxParallel
	t.Overflow = DefaultOverflowForKind(t.Kind)
	t.MaxQueueDepth = defaultMaxQueueDepth
	t.TimeoutSeconds = defaultTimeoutSeconds
	t.DedupeEnabled = true
	t.AutopauseEnabled = true

	req.applyTo(t)
	return nil
}

func validateWebhookTriggerCreate(req CreateTriggerRequest) error {
	switch {
	case strings.TrimSpace(req.Name) == "":
		return fmt.Errorf("%w: name is required", ErrValidation)
	case req.SourceID == "":
		return fmt.Errorf("%w: source_id is required for webhook triggers", ErrValidation)
	case req.AgentTypeID == "":
		return fmt.Errorf("%w: agent_type_id is required", ErrValidation)
	// An empty event_type is legal and means "any event this source emits";
	// dispatch treats '' as match-all.
	case strings.TrimSpace(req.PromptTemplate) == "":
		return fmt.Errorf("%w: prompt_template is required", ErrValidation)
	}
	return validateAdmissionCreate(req)
}

func validateScheduledTriggerCreate(req CreateTriggerRequest) error {
	switch {
	case strings.TrimSpace(req.Name) == "":
		return fmt.Errorf("%w: name is required", ErrValidation)
	case req.AgentTypeID == "":
		return fmt.Errorf("%w: agent_type_id is required", ErrValidation)
	case strings.TrimSpace(req.PromptTemplate) == "":
		return fmt.Errorf("%w: prompt_template is required", ErrValidation)
	case req.ScheduleConfig == nil:
		return fmt.Errorf("%w: schedule_config is required for scheduled triggers", ErrValidation)
	}
	if err := validateAdmissionCreate(req); err != nil {
		return err
	}
	return validateScheduleConfig(req.ScheduleConfig)
}

func validateManualTriggerCreate(req CreateTriggerRequest) error {
	switch {
	case strings.TrimSpace(req.Name) == "":
		return fmt.Errorf("%w: name is required", ErrValidation)
	case req.AgentTypeID == "":
		return fmt.Errorf("%w: agent_type_id is required", ErrValidation)
	case strings.TrimSpace(req.PromptTemplate) == "":
		return fmt.Errorf("%w: prompt_template is required", ErrValidation)
	}
	if err := validateAdmissionCreate(req); err != nil {
		return err
	}
	return validateManualInputs(req.ManualConfig)
}

// validateManualInputs checks an input declaration is usable before it is
// stored. A declaration that could never be satisfied is caught here rather than
// at fire time, when the caller has no way to fix it. Shared by create and
// update so the two cannot drift.
func validateManualInputs(mc *ManualConfig) error {
	if mc == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, f := range mc.Inputs {
		if err := f.Validate(); err != nil {
			return err
		}
		if seen[f.Name] {
			return fmt.Errorf("%w: input %q is declared more than once", ErrValidation, f.Name)
		}
		seen[f.Name] = true
	}
	return nil
}

func validateScheduleConfig(sc *ScheduleConfig) error {
	if sc == nil {
		return fmt.Errorf("%w: schedule_config is required", ErrValidation)
	}
	if strings.TrimSpace(sc.CronExpression) == "" {
		return fmt.Errorf("%w: schedule_config.cron_expression is required", ErrValidation)
	}
	if strings.TrimSpace(sc.Timezone) == "" {
		return fmt.Errorf("%w: schedule_config.timezone is required", ErrValidation)
	}
	// Parse the expression in its timezone. An unparseable cron previously
	// stored fine and then silently never fired, which is the worst failure
	// mode available: the trigger looks healthy and does nothing.
	if _, _, err := ParseSchedule(sc); err != nil {
		return err
	}
	return nil
}

func redactSecret(s *WebhookSource) *WebhookSource {
	cp := *s
	cp.SecretCiphertext = ""
	return &cp
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return strings.ToValidUTF8(string(b[:n]), "")
}

// maxPayloadBytes caps what is stored per delivery. An oversized payload is
// truncated and flagged, never rejected: the trigger should still fire, and a
// sender must not be able to break its own automation by sending a large body.
const maxPayloadBytes = 1 << 20 // 1 MiB

// capturePayload returns the body to persist, bounded by maxPayloadBytes.
func capturePayload(body []byte) []byte {
	if len(body) > maxPayloadBytes {
		return body[:maxPayloadBytes]
	}
	return body
}

// recordSourceRejection counts an inbound request that failed authentication.
//
// Best-effort by design: the caller is already being refused, and failing to
// increment a counter must not change that outcome or slow the refusal down.
func (s *Service) recordSourceRejection(ctx context.Context, sourceID, reason string) {
	if err := s.repo.RecordSourceRejection(ctx, sourceID, reason); err != nil {
		s.logger.Error(ctx, "failed to record source rejection",
			logging.Field{Key: "source_id", Value: sourceID},
			logging.Field{Key: "reason", Value: reason},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

// RecordMissedTick records a scheduled fire that was due while the platform was
// down, without running it.
//
// The record is the point: previously a missed fire left nothing at all, so
// "did my nightly job run?" was unanswerable after an outage. The same unique
// (trigger_id, scheduled_at) key that stops replicas double-firing also stops
// this from racing a real fire for the same minute.
func (s *Service) RecordMissedTick(ctx context.Context, t Trigger, scheduledAt time.Time) {
	minute := scheduledAtMinute(scheduledAt)
	triggerID := t.ID
	reason := "platform was not running when this fire was due; catch-up is off for this trigger"

	inserted, err := s.repo.InsertScheduledDelivery(ctx, &Delivery{
		ID:          uuid.NewString(),
		TenantID:    t.TenantID,
		TriggerID:   &triggerID,
		TriggerName: t.Name,
		Kind:        KindScheduled,
		Status:      StatusMissed,
		ScheduledAt: &minute,
		Reason:      &reason,
	})
	if err != nil {
		s.logger.Error(ctx, "failed to record missed scheduled fire",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "scheduled_at", Value: minute},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	if inserted {
		s.logger.Info(ctx, "recorded missed scheduled fire",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "scheduled_at", Value: minute},
		)
	}
}
