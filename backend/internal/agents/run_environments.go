package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// ErrEnvironmentNotAttached rejects an environment the run's agent does not have.
var ErrEnvironmentNotAttached = errors.New("environment is not attached to this agent")

// ErrEnvironmentSwitch rejects provisioning into another environment while the
// run's sandbox is alive. replace_environment switches.
var ErrEnvironmentSwitch = errors.New("the run's sandbox runs in another environment")

// ErrSameEnvironment rejects a replacement into the environment the sandbox already runs in.
var ErrSameEnvironment = errors.New("the run's sandbox already runs in this environment")

// ErrSandboxImageUnavailable means the environment's SandboxImage is missing or not valid.
var ErrSandboxImageUnavailable = errors.New("sandbox image unavailable")

// Span statuses of the sandbox provisioning steps.
var (
	errLoadRun             = errors.New("load run and agent type")
	errResolveEnvironment  = errors.New("resolve environment")
	errResolveSandboxImage = errors.New("resolve sandbox image")
)

// errReplaceEnvironment is the span status of a failed replacement.
var errReplaceEnvironment = errors.New("replace environment")

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

// SetEnvironments installs what sandbox provisioning needs to resolve a run's
// environment: the environment store, the SandboxImage reader and the sizes.
func (s *RunService) SetEnvironments(envs environmentLookup, images sandboxImageLookup, sizes EnvironmentSizes) {
	s.envs, s.images, s.sizes = envs, images, sizes
}

// agentEnvironments lists the environments the agent may use, default first.
// An agent with none attached has the built-in default.
func (s *RunService) agentEnvironments(ctx context.Context, at AgentType) ([]Environment, error) {
	if len(at.Environments) == 0 || s.envs == nil {
		return []Environment{DefaultEnvironment(s.sizes.Default)}, nil
	}
	out := make([]Environment, 0, len(at.Environments))
	for _, ae := range at.Environments {
		env, err := s.envs.GetEnvironment(ctx, at.TenantID, ae.EnvironmentID)
		if err != nil {
			return nil, fmt.Errorf("load environment %q: %w", ae.EnvironmentID, err)
		}
		if ae.IsDefault {
			out = append([]Environment{*env}, out...)
		} else {
			out = append(out, *env)
		}
	}
	return out, nil
}

// pickEnvironment chooses the environment for a sandbox: the named one, else
// the run's current one, else the agent's default.
func pickEnvironment(envs []Environment, name string, current *string) (Environment, error) {
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
	if current != nil {
		for _, e := range envs {
			if e.ID == *current {
				return e, nil
			}
		}
	}
	return envs[0], nil
}

// currentEnvironment is the environment ID of the run's sandbox, "" for the
// built-in default. A live pod's own annotation wins over the run record,
// which a provision that timed out mid-start can leave behind.
func (s *RunService) currentEnvironment(ctx context.Context, run Run, namespace string) (id string, live bool, err error) {
	podEnv, live, err := s.k8s.SandboxEnvironment(ctx, run.ID, namespace)
	if err != nil || live {
		return podEnv, live, err
	}
	if run.EnvironmentID == nil {
		return "", false, nil
	}
	return *run.EnvironmentID, false, nil
}

// sandboxSpec resolves the environment's image to its pinned digest. A missing
// or invalid SandboxImage cannot start a sandbox.
func (s *RunService) sandboxSpec(ctx context.Context, namespace string, env Environment) (SandboxSpec, error) {
	name := env.ImageName()
	img, err := s.images.SandboxImage(ctx, namespace, name)
	if errors.Is(err, ErrNotFound) {
		return SandboxSpec{}, fmt.Errorf("%w: sandbox image %q is not registered", ErrSandboxImageUnavailable, name)
	}
	if err != nil {
		return SandboxSpec{}, err
	}
	if !img.Valid() {
		return SandboxSpec{}, fmt.Errorf("%w: sandbox image %q is not valid: %s", ErrSandboxImageUnavailable, name, sandboxImageProblem(img))
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
	var envID *string
	if spec.EnvironmentID != "" {
		envID = &spec.EnvironmentID
	}
	if err := s.repo.SetRunEnvironment(ctx, runID, envID); err != nil {
		s.logger.Warn(ctx, "recording the run's environment failed",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
	payload, err := json.Marshal(map[string]string{
		"action":      action,
		"environment": spec.Environment,
		"image":       spec.Image,
	})
	if err != nil {
		return
	}
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
	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	at, err := s.atRepo.GetAgentType(ctx, run.AgentTypeID)
	if err != nil {
		return nil, fmt.Errorf("get agent type: %w", err)
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
			Image:       e.ImageName(),
			Size:        e.Size,
			Default:     i == 0,
			Current:     live && e.ID == current,
		})
	}
	return out, nil
}

// ReplaceEnvironment replaces the run's sandbox with one in another of its
// agent's environments. Running commands end; the workspace stays.
func (s *RunService) ReplaceEnvironment(ctx context.Context, runID, name string) (Sandbox, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.ReplaceEnvironment",
		trace.WithAttributes(attribute.String("run_id", runID), attribute.String("environment", name)))
	defer span.End()

	fail := func(status, err error) (Sandbox, error) {
		span.RecordError(err)
		span.SetStatus(codes.Error, status.Error())
		return Sandbox{}, err
	}
	run, at, err := s.runAndAgent(ctx, runID)
	if err != nil {
		return fail(errLoadRun, err)
	}
	envs, err := s.agentEnvironments(ctx, *at)
	if err != nil {
		return fail(errResolveEnvironment, err)
	}
	env, err := pickEnvironment(envs, strings.TrimSpace(name), nil)
	if err != nil {
		return fail(errResolveEnvironment, err)
	}
	namespace := s.cfg.TenantNamespaceFn(run.TenantID)
	current, live, err := s.currentEnvironment(ctx, *run, namespace)
	if err != nil {
		return fail(errResolveEnvironment, err)
	}
	if live && env.ID == current {
		return fail(errResolveEnvironment, fmt.Errorf("%w: %q", ErrSameEnvironment, env.Name))
	}
	spec, err := s.sandboxSpec(ctx, namespace, env)
	if err != nil {
		return fail(errResolveSandboxImage, err)
	}
	spec.KubeClusters, _ = s.sandboxKubeClusters(ctx, *at)

	if err = s.k8s.DeleteSandbox(ctx, runID, namespace); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, errReplaceEnvironment.Error())
		return Sandbox{}, fmt.Errorf("delete sandbox: %w", err)
	}
	sandbox, err := s.k8s.ProvisionSandbox(ctx, *run, at.Slug, namespace, spec)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, errReplaceEnvironment.Error())
		return Sandbox{}, fmt.Errorf("provision sandbox pod: %w", err)
	}
	s.recordSandboxEnvironment(ctx, runID, "replace", spec)
	sandbox.CanaryHost = s.egressCanaryHost(ctx, *at)
	return sandbox, nil
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
