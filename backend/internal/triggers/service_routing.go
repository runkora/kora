package triggers

import (
	"context"
	"errors"
	"fmt"
	"time"

	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"
	inferenceclient "github.com/runkora/kora/backend/internal/platform/clients/inference"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// joinableRunStates are the run states a delivery may join (ADR Decision 12).
var joinableRunStates = []string{"active", "idle"}

var (
	// errNoRouting marks a routed trigger stored without its routing object.
	errNoRouting = errors.New("routed trigger has no routing configuration")
	// errNoDecisionClient marks a service built without the inference client.
	errNoDecisionClient = errors.New("no decision model client configured")
	// errNoCandidateAgents is returned when none of the candidates can be read.
	errNoCandidateAgents = errors.New("none of the candidate agents could be read")
)

// Holding a fire whose join key names a starting run (ADR Decision 7).
const (
	startingRunHolds     = 6
	startingRunHoldDelay = 10 * time.Second
)

// holdForStartingRun is returned by route when the fire should wait for runID to start.
type holdForStartingRun struct{ runID string }

func (h *holdForStartingRun) Error() string { return "run " + h.runID + " is starting" }

// dispatchRouted routes one fire of a routed trigger and records the decision
// and the outcome (ADR Decision 2). ctx records; dispatchCtx bounds the calls.
// held reports a fire returned to the queue to wait for a starting run.
func (s *Service) dispatchRouted(ctx, dispatchCtx context.Context, t Trigger, p PendingDelivery) (held bool) {
	d, runID, err := s.route(dispatchCtx, t, p)
	var hold *holdForStartingRun
	if errors.As(err, &hold) {
		reason := fmt.Sprintf("waiting for run %s to start before joining it (hold %d of %d)", hold.runID, p.HoldCount+1, startingRunHolds)
		holdErr := s.repo.HoldPending(ctx, p.ID, p.DeliveryID, startingRunHoldDelay, reason)
		if holdErr == nil {
			return true
		}
		s.logger.Error(ctx, "failed to hold a fire for a starting run",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "delivery_id", Value: p.DeliveryID},
			logging.Field{Key: "error", Value: holdErr.Error()},
		)
		err = fmt.Errorf("hold for starting run: %w", holdErr)
	}
	if p.HoldCount > 0 {
		s.clearHoldReason(ctx, t, p.DeliveryID)
	}
	if d != nil {
		if insertErr := s.repo.InsertRoutingDecision(ctx, d); insertErr != nil {
			s.logger.Error(ctx, "failed to record routing decision",
				logging.Field{Key: "trigger_id", Value: t.ID},
				logging.Field{Key: "delivery_id", Value: p.DeliveryID},
				logging.Field{Key: "error", Value: insertErr.Error()},
			)
		}
	}
	switch {
	case err != nil:
		s.recordDispatchFailure(ctx, t, p.DeliveryID, p.ID, err)
	case d.Outcome == RouteOutcomeNoRun:
		s.recordUnrouted(ctx, t, p.DeliveryID, p.ID, d)
	case d.Outcome == RouteOutcomeJoinedRun:
		s.recordStarted(ctx, t, p.DeliveryID, RunStatusJoined, runID, p.ID)
	default:
		s.recordStarted(ctx, t, p.DeliveryID, RunStatusDispatched, runID, p.ID)
	}
	return false
}

// clearHoldReason removes the hold reason from a delivery that has left the
// hold, so it no longer reads as waiting once it routes.
func (s *Service) clearHoldReason(ctx context.Context, t Trigger, deliveryID string) {
	if err := s.repo.ClearDeliveryReason(ctx, deliveryID); err != nil {
		s.logger.Error(ctx, "failed to clear a held delivery's reason",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "delivery_id", Value: deliveryID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

// recordUnrouted records a delivery that started nothing under the no-run
// fallback. Only a failed decision counts toward auto-pause, because with no
// fallback agent nothing else handles the deliveries it drops, and only when
// the failure is a configuration error (ADR Decision 11).
func (s *Service) recordUnrouted(ctx context.Context, t Trigger, deliveryID, ownPendingID string, d *RoutingDecision) {
	reason := s.brand.ProductName() + " was not sure which agent fits, and the trigger has no fallback agent"
	if d.Error != nil {
		reason = "the routing decision failed, and the trigger has no fallback agent"
	}
	s.outcomes.insert(ctx, &DeliveryRun{
		TenantID: t.TenantID, DeliveryID: deliveryID,
		TriggerID: t.ID, TriggerName: t.Name,
		Status: RunStatusUnrouted, Error: &reason,
	})
	if decisionErrorCounts(d.decideErr) {
		s.countFailure(ctx, t)
	}
	s.outcomes.deriveSettling(ctx, deliveryID, t.TenantID, ownPendingID)
}

// decisionErrorCounts reports whether a failed decision is a configuration error
// that will not fix itself: a missing scope or exhausted budget, a rejected
// request, or an unknown or disabled model. Gateway faults, rate limits and
// timeouts never count (ADR Decision 11).
func decisionErrorCounts(err error) bool {
	return errors.Is(err, inferenceclient.ErrForbidden) ||
		errors.Is(err, inferenceclient.ErrInvalidRequest) ||
		errors.Is(err, inferenceclient.ErrModelNotFound)
}

// route decides where a delivery goes and starts or joins the run. It returns
// the decision to record, which is nil when routing failed before deciding,
// and the run the delivery reached. An error is a failed start or join.
func (s *Service) route(ctx context.Context, t Trigger, p PendingDelivery) (*RoutingDecision, string, error) {
	r := t.Routing
	if r == nil {
		return nil, "", errNoRouting
	}
	tok, err := s.triggerToken(ctx, t)
	if err != nil {
		return nil, "", err
	}
	prompt := renderWebhookPrompt(t.PromptTemplate, p.Fields)
	d := &RoutingDecision{DeliveryID: p.DeliveryID, TriggerID: t.ID, TenantID: t.TenantID, QuestionVersion: routingQuestionsVersion}

	if key, ok := renderJoinKey(r, p.Fields); ok {
		d.JoinKey = &key
		if r.joins() {
			joined, joinErr := s.joinByKey(ctx, t, tok, prompt, key, p.HoldCount, d)
			var hold *holdForStartingRun
			if errors.As(joinErr, &hold) {
				return nil, "", joinErr
			}
			if joined || joinErr != nil {
				return d, deref(d.RunID), joinErr
			}
		}
	}

	choice, decideErr := s.decide(ctx, t, tok, prompt)
	if decideErr != nil {
		choice = fallbackChoice(r.FallbackAgentTypeID, FallbackDecisionFailed)
		d.Error, d.decideErr = new(decideErr.Error()), decideErr
	}
	applyChoice(d, r, choice)

	switch choice.outcome {
	case RouteOutcomeNoRun:
		return d, "", nil
	case RouteOutcomeJoinedRun:
		joinErr := s.joinRun(ctx, t, tok, choice.run.ID, prompt, p.DeliveryID)
		if joinErr == nil {
			d.RunID = &choice.run.ID
			return d, choice.run.ID, nil
		}
		if !runClosed(joinErr) {
			return d, "", fmt.Errorf("join run: %w", joinErr)
		}
		// Lost race: the run closed between the decision and the message, so a
		// new run of its agent takes the delivery (ADR Decision 15).
		d.Outcome, d.FallbackReason = RouteOutcomeNewRun, new(FallbackRunClosed)
	}

	runID, err := s.startRunAs(ctx, t, tok, choice.agentTypeID, prompt, p.DeliveryID)
	if err != nil {
		return d, "", err
	}
	d.RunID = &runID
	return d, runID, nil
}

// joinByKey joins the run the delivery's join key names, without a model call
// (ADR Decision 7). A run that is still starting returns holdForStartingRun
// while holds remain. joined is false when no open run matched, and the model
// decides; any other error is a join that failed.
func (s *Service) joinByKey(ctx context.Context, t Trigger, tok, prompt, key string, holds int16, d *RoutingDecision) (joined bool, err error) {
	runID, agentID, ok := s.keyMatch(ctx, t, key)
	if !ok {
		return false, nil
	}
	err = s.joinRun(ctx, t, tok, runID, prompt, d.DeliveryID)
	switch {
	case err == nil:
		d.Outcome, d.KeyMatched, d.AgentTypeID, d.RunID = RouteOutcomeJoinedRun, true, &agentID, &runID
		return true, nil
	case errors.Is(err, agentsclient.ErrRunStarting) && holds < startingRunHolds:
		return false, &holdForStartingRun{runID: runID}
	case runClosed(err), errors.Is(err, agentsclient.ErrRunStarting):
		return false, nil
	default:
		d.Outcome, d.KeyMatched, d.AgentTypeID = RouteOutcomeJoinedRun, true, &agentID
		return false, fmt.Errorf("join run: %w", err)
	}
}

// applyChoice records a model decision, or its fallback, on the decision.
func applyChoice(d *RoutingDecision, r *RoutingConfig, choice routeChoice) {
	d.Model, d.Threshold = &r.Model, new(r.threshold())
	d.Outcome, d.Scores = choice.outcome, choice.scores
	d.Probability, d.Confidence = choice.probability, choice.confidence
	if choice.agentTypeID != "" {
		d.AgentTypeID = &choice.agentTypeID
	}
	if choice.fallbackReason != "" {
		d.FallbackReason = &choice.fallbackReason
	}
}

// keyMatch finds the run a delivery's join key names. A match on an agent that
// is no longer a candidate is ignored.
func (s *Service) keyMatch(ctx context.Context, t Trigger, key string) (runID, agentTypeID string, ok bool) {
	runID, agentTypeID, found, err := s.repo.LatestJoinKeyRun(ctx, t.TenantID, t.ID, key)
	if err != nil {
		s.logger.Error(ctx, "join key lookup failed; the decision model decides",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return "", "", false
	}
	if !found || !t.Routing.isCandidate(agentTypeID) {
		return "", "", false
	}
	return runID, agentTypeID, true
}

// decide asks the decision model where the delivery goes. Any error leaves the
// delivery to the fallback.
func (s *Service) decide(ctx context.Context, t Trigger, tok, prompt string) (routeChoice, error) {
	r := t.Routing
	if s.decisions == nil {
		return routeChoice{}, errNoDecisionClient
	}
	types, err := s.agents.ListAgentTypes(ctx, agentsclient.Caller{Bearer: tok})
	if err != nil {
		return routeChoice{}, fmt.Errorf("list candidate agents: %w", err)
	}
	byID := make(map[string]agentsclient.AgentType, len(types))
	for _, at := range types {
		byID[at.ID] = at
	}
	var agents []agentsclient.AgentType
	for _, id := range r.Candidates {
		if at, ok := byID[id]; ok {
			agents = append(agents, at)
		}
	}
	if len(agents) == 0 {
		return routeChoice{}, errNoCandidateAgents
	}

	// No joinable run, for any reason, leaves the agent question to decide.
	var runs []agentsclient.Run
	if r.joins() {
		if runs, err = s.joinableRuns(ctx, r, tok); err != nil {
			s.logger.Warn(ctx, "listing joinable runs failed; routing among the candidate agents only",
				logging.Field{Key: "trigger_id", Value: t.ID},
				logging.Field{Key: "error", Value: err.Error()},
			)
			runs = nil
		}
	}

	q := buildRoutingQuestions(r.Model, prompt, agents, runs)
	var resp inferenceclient.SystemOneResponse
	err = s.retryYoungGrant(ctx, t, inferenceclient.ErrForbidden, func() error {
		var callErr error
		resp, callErr = s.decisions.SystemOne(ctx, tok, q.request)
		return callErr
	})
	if err != nil {
		return routeChoice{}, fmt.Errorf("decision model: %w", err)
	}
	return decideRoute(q, resp, r.threshold(), r.FallbackAgentTypeID)
}

// joinableRuns lists the open, trigger-started runs of the candidate agents,
// newest first and at most maxJoinableRuns (ADR Decisions 6 and 12). The
// filters are applied again here so a looser server cannot widen the set.
func (s *Service) joinableRuns(ctx context.Context, r *RoutingConfig, tok string) ([]agentsclient.Run, error) {
	listed, err := s.agents.ListRuns(ctx, tok, agentsclient.ListRunsQuery{
		AgentTypeIDs:     r.Candidates,
		States:           joinableRunStates,
		StartedByTrigger: true,
		Limit:            maxJoinableRuns,
	})
	if err != nil {
		return nil, fmt.Errorf("list joinable runs: %w", err)
	}
	var runs []agentsclient.Run
	for _, run := range listed {
		if run.StartedBy != nil && r.isCandidate(run.AgentTypeID) && (run.State == "active" || run.State == "idle") {
			runs = append(runs, run)
		}
		if len(runs) == maxJoinableRuns {
			break
		}
	}
	return runs, nil
}

// runClosed reports whether a join failed because the run no longer takes messages.
func runClosed(err error) bool {
	return errors.Is(err, agentsclient.ErrRunNotAcceptingMessages) || errors.Is(err, agentsclient.ErrRunNotFound)
}

// joinRun sends the delivery's prompt to a run as a trigger message.
func (s *Service) joinRun(ctx context.Context, t Trigger, tok, runID, prompt, deliveryID string) error {
	return s.agents.SendRunMessage(ctx, tok, runID, agentsclient.SendRunMessageRequest{
		Content:     prompt,
		TriggerID:   t.ID,
		TriggerName: t.Name,
		DeliveryID:  deliveryID,
	})
}
