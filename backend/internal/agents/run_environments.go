package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// ErrEnvironmentNotAttached rejects an environment the run's agent does not have.
var ErrEnvironmentNotAttached = errors.New("environment is not attached to this agent")

// ErrEnvironmentSwitch rejects provisioning into another environment while the
// run's sandbox is alive. replace_environment switches.
var ErrEnvironmentSwitch = errors.New("the run's sandbox runs in another environment")

// ErrEnvironmentRequired rejects a replacement that names no environment.
var ErrEnvironmentRequired = errors.New("environment is required")

// ErrSandboxImageUnavailable means the environment's SandboxImage is missing or rejected.
var ErrSandboxImageUnavailable = errors.New("sandbox image unavailable")

// errSandboxImagePending means the operator has not validated the image yet. A later attempt can succeed.
var errSandboxImagePending = errors.New("sandbox image not validated yet")

// Span statuses of the sandbox provisioning steps.
var (
	errLoadRun             = errors.New("load run and agent type")
	errResolveEnvironment  = errors.New("resolve environment")
	errResolveSandboxImage = errors.New("resolve sandbox image")
	errProvisionPod        = errors.New("provision sandbox pod")
)

// failSpan records err on span with a static status and returns err.
func failSpan(span trace.Span, status, err error) error {
	span.RecordError(err)
	span.SetStatus(codes.Error, status.Error())
	return err
}

// runEnvironmentStore is what sandbox provisioning reads environments from and
// where it records the environment of a run's current sandbox.
type runEnvironmentStore interface {
	environmentLookup
	SetRunEnvironment(ctx context.Context, runID string, environmentID *string) error
}

// eventSandboxEnvironment records which environment and image a sandbox started in.
const eventSandboxEnvironment = "sandbox_environment"

// RunEnvironment is one environment a run may use, as the agent sees it.
type RunEnvironment struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Image       string          `json:"image"`
	Size        EnvironmentSize `json:"size"`
	Default     bool            `json:"default"`
	Current     bool            `json:"current"`
}

// agentEnvironments lists the environments the agent may use, default first.
// An agent with none attached has the built-in default.
func (s *RunService) agentEnvironments(ctx context.Context, at AgentType) ([]Environment, error) {
	if len(at.Environments) == 0 {
		return []Environment{DefaultEnvironment(s.cfg.EnvironmentSizes.Default)}, nil
	}
	envs, err := s.envs.ListEnvironmentsByIDs(ctx, at.TenantID, environmentIDs(at.Environments))
	if err != nil {
		return nil, fmt.Errorf("load environments: %w", err)
	}
	if len(envs) != len(at.Environments) {
		return nil, fmt.Errorf("load environments: %w", ErrNotFound)
	}
	return envs, nil
}

// pickEnvironment chooses the environment for a sandbox: the named one, else
// the run's current one, else the agent's default.
func pickEnvironment(envs []Environment, name, current string) (Environment, error) {
	if name != "" {
		names := make([]string, 0, len(envs))
		for _, e := range envs {
			if e.Name == name {
				return e, nil
			}
			names = append(names, e.Name)
		}
		return Environment{}, fmt.Errorf("%w: %q; this agent has %s", ErrEnvironmentNotAttached, name, strings.Join(names, ", "))
	}
	for _, e := range envs {
		if e.ID == current {
			return e, nil
		}
	}
	return envs[0], nil
}

// recordedEnvironment is the environment the run record names, "" for the built-in default.
func recordedEnvironment(run Run) string {
	if run.EnvironmentID == nil {
		return ""
	}
	return *run.EnvironmentID
}

// currentEnvironment is the environment ID of the run's sandbox, "" for the
// built-in default. A live pod's own annotation wins over the run record,
// which a provision that timed out mid-start can leave behind.
func (s *RunService) currentEnvironment(ctx context.Context, run Run, namespace string) (id string, live bool, err error) {
	podEnv, live, err := s.k8s.SandboxEnvironment(ctx, run.ID, namespace)
	if err != nil || live {
		return podEnv, live, err
	}
	return recordedEnvironment(run), false, nil
}

// sandboxSpec resolves the environment's image to its pinned digest. A missing
// or invalid SandboxImage cannot start a sandbox.
func (s *RunService) sandboxSpec(ctx context.Context, namespace string, env Environment) (SandboxSpec, error) {
	img, err := usableSandboxImage(ctx, s.images, namespace, env.SandboxImage, ErrSandboxImageUnavailable, errSandboxImagePending)
	if err != nil {
		return SandboxSpec{}, err
	}
	spec := SandboxSpec{
		EnvironmentID: env.ID,
		Environment:   env.Name,
		Image:         img.PinnedRef(),
		Size:          env.Size,
	}
	if ref := img.Spec.PullSecretRef; ref != nil {
		spec.PullSecret = ref.Name
	}
	return spec, nil
}

// recordSandboxEnvironment stores the run's environment and emits the event
// naming the environment and image a sandbox started in.
func (s *RunService) recordSandboxEnvironment(ctx context.Context, runID, action string, spec SandboxSpec) {
	if err := s.envs.SetRunEnvironment(ctx, runID, environmentIDPtr(spec.EnvironmentID)); err != nil {
		s.logger.Warn(ctx, "recording the run's environment failed",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
	payload, _ := json.Marshal(map[string]string{
		"action":      action,
		"environment": spec.Environment,
		"image":       spec.Image,
	})
	at := eventTime()
	seqNo, err := s.repo.AppendRunEvent(ctx, RunEvent{RunID: runID, EventType: eventSandboxEnvironment, Payload: payload, CreatedAt: at})
	if err != nil {
		s.logger.Warn(ctx, "persisting the sandbox_environment event failed",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		seqNo = 0
	}
	s.pubsub.Publish(runID, PushedEvent{Type: eventSandboxEnvironment, Payload: payload, SequenceNo: seqNo, At: at})
}

// ListRunEnvironments lists the environments the run's agent may use.
func (s *RunService) ListRunEnvironments(ctx context.Context, runID string) ([]RunEnvironment, error) {
	run, at, err := s.runAndAgent(ctx, runID)
	if err != nil {
		return nil, err
	}
	envs, err := s.agentEnvironments(ctx, *at)
	if err != nil {
		return nil, err
	}
	current, live, err := s.currentEnvironment(ctx, *run, s.cfg.TenantNamespaceFn(run.TenantID))
	if err != nil {
		return nil, err
	}
	out := make([]RunEnvironment, 0, len(envs))
	for i, e := range envs {
		out = append(out, RunEnvironment{
			Name:        e.Name,
			Description: e.Description,
			Image:       e.SandboxImage,
			Size:        e.Size,
			Default:     i == 0,
			Current:     live && e.ID == current,
		})
	}
	return out, nil
}

// runAndAgent loads a run and its agent type.
func (s *RunService) runAndAgent(ctx context.Context, runID string) (*Run, *AgentType, error) {
	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	at, err := s.atRepo.GetAgentType(ctx, run.AgentTypeID)
	if err != nil {
		return nil, nil, fmt.Errorf("get agent type: %w", err)
	}
	return run, at, nil
}

// environmentIDPtr stores the built-in default, which has no ID, as NULL.
func environmentIDPtr(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}
