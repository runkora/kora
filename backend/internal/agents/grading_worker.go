package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/runkora/kora/backend/internal/agents/grading"
	identityclient "github.com/runkora/kora/backend/internal/platform/clients/identity"
	inference "github.com/runkora/kora/backend/internal/platform/clients/inference"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// The grading worker (docs/adr/2026-11-19-graders.md, Decisions 3 to 6 and 9).

const (
	// gradeDecisionTimeout is the gateway's per-attempt timeout on a grading call.
	gradeDecisionTimeout = 60 * time.Second
	// GradeClientTimeout bounds a grading call. It leaves room for one gateway retry.
	GradeClientTimeout = 130 * time.Second
	// gradeLease holds a claimed job past one call and its token retries.
	gradeLease = 5 * time.Minute
	// gradeMaxAttempts is how many claims a job gets before it records error.
	gradeMaxAttempts = 6
	// gradeFirstBackoff doubles after each failed attempt.
	gradeFirstBackoff = 5 * time.Second
	// runEndWait is how long a first grade waits for the run's terminal run_state event.
	// A start that failed records none.
	runEndWait = 30 * time.Second
	// runEndPoll is how soon a job waiting for that event is claimed again.
	runEndPoll = 2 * time.Second
	// The grading token's mint retries while the grading grant is young.
	gradeTokenFirstBackoff = 250 * time.Millisecond
	gradeTokenRetryBudget  = 6 * time.Second
)

// gradeStore is the storage the worker reads and writes.
type gradeStore interface {
	claimGradeJobs(ctx context.Context, limit int, now time.Time, lease time.Duration) ([]gradeJob, error)
	retryGradeJob(ctx context.Context, id string, at time.Time, lastError string) error
	deferGradeJob(ctx context.Context, id string, at time.Time) error
	dropGradeJob(ctx context.Context, id string) error
	graderVersionSpec(ctx context.Context, version string) ([]byte, error)
	graderModelScopes(ctx context.Context, tenantID string) ([]string, error)
	runEndRecorded(ctx context.Context, runID string) (bool, error)
	gradeState(ctx context.Context, runID string) (*grading.Snapshot, error)
	putGradeState(ctx context.Context, tenantID, runID string, s grading.Snapshot) (grading.Snapshot, error)
	getGrade(ctx context.Context, graderID, version, runID string) (*gradeRecord, error)
	finishGradeJob(ctx context.Context, jobID string, g gradeRecord) error
	GetRun(ctx context.Context, id string) (*Run, error)
	ListRunEvents(ctx context.Context, runID string, afterSeqNo int64) ([]RunEvent, error)
}

// gradingPrincipal is the tenant's grading principal. *GraderPrincipal implements it.
type gradingPrincipal interface {
	Ensure(ctx context.Context, tenantID string, modelScopes []string) error
	Token(ctx context.Context, tenantID string) (string, error)
}

// decisionClient calls System One. *inference.Client implements it.
type decisionClient interface {
	SystemOne(ctx context.Context, bearer string, req inference.SystemOneRequest) (inference.SystemOneResponse, error)
}

// GradeWorkerConfig configures the grading worker.
type GradeWorkerConfig struct {
	// WindowTokens is the decision model's input window.
	WindowTokens int
	// Concurrency is how many jobs one replica grades at once.
	Concurrency int
	// PollInterval is how often the worker claims due jobs.
	PollInterval time.Duration
}

// GradeWorker grades ended runs. Every kora-agents replica runs one.
type GradeWorker struct {
	store     gradeStore
	principal gradingPrincipal
	decisions decisionClient
	cfg       GradeWorkerConfig
	logger    logging.Logger
	now       func() time.Time
	sleep     func(context.Context, time.Duration) error
}

// NewGradeWorker builds the grading worker.
func NewGradeWorker(store *PostgresRepository, principal *GraderPrincipal, decisions *inference.Client, cfg GradeWorkerConfig, logger logging.Logger) *GradeWorker {
	return newGradeWorker(store, principal, decisions, cfg, logger)
}

func newGradeWorker(store gradeStore, principal gradingPrincipal, decisions decisionClient, cfg GradeWorkerConfig, logger logging.Logger) *GradeWorker {
	if cfg.WindowTokens <= 0 {
		cfg.WindowTokens = grading.DefaultWindow
	}
	cfg.Concurrency = max(cfg.Concurrency, 1)
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	return &GradeWorker{store: store, principal: principal, decisions: decisions, cfg: cfg, logger: logger, now: time.Now, sleep: sleepCtx}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// Start claims and grades due jobs until ctx ends, then waits for the jobs in flight.
func (w *GradeWorker) Start(ctx context.Context) {
	var inFlight sync.WaitGroup
	slots := make(chan struct{}, w.cfg.Concurrency)
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			inFlight.Wait()
			return
		case <-ticker.C:
		}
		free := cap(slots) - len(slots)
		if free == 0 {
			continue
		}
		// A claim that commits must be read, or its jobs wait out the lease.
		jobs, err := w.store.claimGradeJobs(context.WithoutCancel(ctx), free, w.now(), gradeLease)
		if err != nil {
			w.logger.Error(ctx, "claim grade jobs failed", logging.Field{Key: "error", Value: err.Error()})
			continue
		}
		for _, job := range jobs {
			slots <- struct{}{}
			inFlight.Go(func() {
				defer func() { <-slots }()
				w.process(context.WithoutCancel(ctx), job)
			})
		}
	}
}

// process grades one claimed job and settles it: written, retried, deferred or dropped.
func (w *GradeWorker) process(ctx context.Context, job gradeJob) {
	fields := []logging.Field{{Key: "job_id", Value: job.ID}, {Key: "run_id", Value: job.RunID}, {Key: "grader_id", Value: job.GraderID}}
	o := w.grade(ctx, job)
	var err error
	switch {
	case o.drop:
		w.logger.Warn(ctx, "grade job dropped", append(fields, logging.Field{Key: "error", Value: o.err.Error()})...)
		err = w.store.dropGradeJob(ctx, job.ID)
	case o.deferred:
		err = w.store.deferGradeJob(ctx, job.ID, w.now().Add(runEndPoll))
	case o.record != nil:
		if o.err != nil {
			w.logger.Warn(ctx, "grade recorded as error", append(fields, logging.Field{Key: "error", Value: o.err.Error()})...)
		}
		err = w.store.finishGradeJob(ctx, job.ID, *o.record)
		if errors.Is(err, errGradeJobGone) {
			w.logger.Info(ctx, "grade job moved or gone, grade not written", fields...)
			err = nil
		}
		if err != nil {
			o.base, o.err = o.record, err
			err = w.retry(ctx, job, o, fields)
		}
	default:
		err = w.retry(ctx, job, o, fields)
	}
	if err != nil {
		w.logger.Error(ctx, "settle grade job failed", append(fields, logging.Field{Key: "error", Value: err.Error()})...)
	}
}

// retry returns a failed job to the queue with backoff. Its last attempt records error.
func (w *GradeWorker) retry(ctx context.Context, job gradeJob, o outcome, fields []logging.Field) error {
	if job.Attempts < gradeMaxAttempts {
		backoff := gradeFirstBackoff << (job.Attempts - 1)
		w.logger.Info(ctx, "grade attempt failed, retrying", append(fields,
			logging.Field{Key: "attempt", Value: job.Attempts}, logging.Field{Key: "error", Value: o.err.Error()})...)
		return w.store.retryGradeJob(ctx, job.ID, w.now().Add(backoff), o.err.Error())
	}
	// With no run fields there is no grade to record. The job waits out the longest backoff and tries again.
	if o.base == nil {
		return w.store.retryGradeJob(ctx, job.ID, w.now().Add(gradeFirstBackoff<<(gradeMaxAttempts-1)), o.err.Error())
	}
	failed := o.base.withStatus(gradeStatusError)
	w.logger.Warn(ctx, "grade recorded as error", append(fields, logging.Field{Key: "error", Value: o.err.Error()})...)
	err := w.store.finishGradeJob(ctx, job.ID, failed)
	if errors.Is(err, errGradeJobGone) {
		return nil
	}
	return err
}

// outcome is what one attempt at a job produced. record is the grade to write. With err set
// and no record the attempt failed and may retry. base is the grade so far, which a last
// failed attempt records as error.
type outcome struct {
	record   *gradeRecord
	base     *gradeRecord
	err      error
	deferred bool
	drop     bool
}

func failed(base *gradeRecord, err error) outcome { return outcome{base: base, err: err} }

func (g gradeRecord) withStatus(status string) gradeRecord {
	g.Status, g.Verdict, g.AppliesP, g.AppliesRaw, g.Answers = status, nil, nil, nil, nil
	return g
}

func (w *GradeWorker) grade(ctx context.Context, job gradeJob) outcome {
	rawSpec, err := w.store.graderVersionSpec(ctx, job.Version)
	if errors.Is(err, ErrNotFound) {
		return outcome{drop: true, err: err}
	}
	if err != nil {
		return failed(nil, err)
	}
	version, err := parseGraderVersion(rawSpec)
	if err != nil {
		return outcome{drop: true, err: err}
	}
	base, source, err := w.base(ctx, job)
	if errors.Is(err, ErrNotFound) {
		return outcome{drop: true, err: err}
	}
	if err != nil {
		return failed(nil, err)
	}
	if job.Kind == gradeJobRecompute {
		return recompute(version, base, source)
	}

	snap, wait, err := w.snapshot(ctx, job)
	switch {
	case err != nil:
		return failed(&base, err)
	case wait:
		return outcome{deferred: true}
	}
	base.StateHash = &snap.Hash
	return w.ask(ctx, job, version, base, snap)
}

// ask fits the snapshot to the window, asks System One and scores its answers.
func (w *GradeWorker) ask(ctx context.Context, job gradeJob, version graderVersion, base gradeRecord, snap grading.Snapshot) outcome {
	req := inference.SystemOneRequest{Model: version.modelName(), Questions: version.decisionQuestions(), DecisionTimeout: gradeDecisionTimeout}
	overhead, err := json.Marshal(req)
	if err != nil {
		return failed(&base, fmt.Errorf("encode grading request: %w", err))
	}
	// The window is net of the questions, which the call sends beside the state.
	state, err := grading.Fit(snap.JSON, w.cfg.WindowTokens-grading.EstimateTokens(overhead))
	if err != nil {
		return failed(&base, err)
	}
	if state.Cap != grading.Uncapped {
		base.StateCap = new(int(state.Cap))
	}
	if state.TooLarge {
		r := base.withStatus(gradeStatusTooLarge)
		return outcome{record: &r}
	}
	req.State = json.RawMessage(state.JSON)

	resp, err := w.call(ctx, job.TenantID, req)
	if errors.Is(err, inference.ErrBudgetExceeded) {
		// The tenant ceiling holds until its window resets, so the grade records error now.
		r := base.withStatus(gradeStatusError)
		return outcome{record: &r, err: err}
	}
	if err != nil {
		return failed(&base, err)
	}
	base.InputTokens = new(resp.Usage.InputTokens)
	base.ModelServed = nonEmpty(resp.Model)
	base.UsageID = nonEmpty(resp.UsageID)
	base.CostUSD = resp.CostUSD
	r, err := scored(version, base, resp.Answers)
	if err != nil {
		return failed(&base, err)
	}
	return outcome{record: &r}
}

// base is the grade's run fields. A first grade reads them from the run. A regrade or
// recompute reads them from the source grade, which it also returns.
func (w *GradeWorker) base(ctx context.Context, job gradeJob) (gradeRecord, *gradeRecord, error) {
	g := gradeRecord{TenantID: job.TenantID, GraderID: job.GraderID, Version: job.Version, RunID: job.RunID}
	if job.Kind == gradeJobGrade {
		run, err := w.store.GetRun(ctx, job.RunID)
		if err != nil {
			return g, nil, err
		}
		g.AgentTypeID, g.AgentConfig, g.RunOutcome, g.RunStartedAt = run.AgentTypeID, run.AgentConfig, string(run.State), run.CreatedAt
		if run.StartedAt != nil {
			g.RunStartedAt = *run.StartedAt
		}
		return g, nil, nil
	}
	src, err := w.store.getGrade(ctx, job.GraderID, job.SourceVersion, job.RunID)
	if err != nil {
		return g, nil, err
	}
	g.AgentTypeID, g.AgentConfig, g.RunOutcome, g.RunStartedAt = src.AgentTypeID, src.AgentConfig, src.RunOutcome, src.RunStartedAt
	g.SourceVersion = job.SourceVersion
	return g, src, nil
}

// snapshot returns the run's stored snapshot, building and storing it on the run's first
// grade. wait is set while the run's end is not yet recorded in its events.
func (w *GradeWorker) snapshot(ctx context.Context, job gradeJob) (grading.Snapshot, bool, error) {
	stored, err := w.store.gradeState(ctx, job.RunID)
	if err != nil || stored != nil {
		return deref(stored), false, err
	}
	if job.Kind == gradeJobGrade && w.now().Sub(job.CreatedAt) < runEndWait && job.Attempts < gradeMaxAttempts {
		ended, endErr := w.store.runEndRecorded(ctx, job.RunID)
		if endErr != nil {
			return grading.Snapshot{}, false, endErr
		}
		if !ended {
			return grading.Snapshot{}, true, nil
		}
	}
	events, err := w.store.ListRunEvents(ctx, job.RunID, 0)
	if err != nil {
		return grading.Snapshot{}, false, err
	}
	in := make([]grading.Event, len(events))
	for i, e := range events {
		in[i] = grading.Event{Seq: e.SequenceNo, Type: e.EventType, Payload: e.Payload}
	}
	snap, err := grading.BuildSnapshot(in)
	if err != nil {
		return grading.Snapshot{}, false, err
	}
	snap, err = w.store.putGradeState(ctx, job.TenantID, job.RunID, snap)
	return snap, false, err
}

func deref(s *grading.Snapshot) grading.Snapshot {
	if s == nil {
		return grading.Snapshot{}
	}
	return *s
}

// call makes the System One call as the tenant's grading principal. The grant is set to the
// models of the tenant's live Graders, so a model no Grader asks any more is not kept.
func (w *GradeWorker) call(ctx context.Context, tenantID string, req inference.SystemOneRequest) (inference.SystemOneResponse, error) {
	scopes, err := w.store.graderModelScopes(ctx, tenantID)
	if err != nil {
		return inference.SystemOneResponse{}, err
	}
	if err = w.principal.Ensure(ctx, tenantID, scopes); err != nil {
		return inference.SystemOneResponse{}, err
	}
	tok, err := w.token(ctx, tenantID)
	if err != nil {
		return inference.SystemOneResponse{}, err
	}
	return w.decisions.SystemOne(ctx, tok, req)
}

// token mints the grading token. A mint can fail in transit for a moment, such as in a mesh
// disruption, so it retries within a short budget. Any answer from identity is final for this
// attempt, and the job's own backoff retries it.
func (w *GradeWorker) token(ctx context.Context, tenantID string) (string, error) {
	backoff := gradeTokenFirstBackoff
	deadline := w.now().Add(gradeTokenRetryBudget)
	for {
		tok, err := w.principal.Token(ctx, tenantID)
		if err == nil {
			return tok, nil
		}
		final := errors.Is(err, identityclient.ErrMintForbidden) || errors.Is(err, identityclient.ErrUnexpectedStatus) ||
			errors.Is(err, identityclient.ErrMissingToken)
		if final || w.now().Add(backoff).After(deadline) {
			return "", err
		}
		if sleepErr := w.sleep(ctx, backoff); sleepErr != nil {
			return "", err
		}
		backoff *= 2
	}
}

// scored is the grade the model's answers give under the version.
func scored(version graderVersion, base gradeRecord, answers map[string]inference.Answer) (gradeRecord, error) {
	applies, in, err := version.scoreInput(answers)
	if err != nil {
		return base, err
	}
	g, err := grading.Score(version.gradingSpec(), applies, in)
	if err != nil {
		return base, err
	}
	r := base
	r.Status = string(g.Status)
	if g.Verdict != "" {
		r.Verdict = new(string(g.Verdict))
	}
	if version.AppliesWhen != "" {
		r.AppliesP = applies
		if r.AppliesRaw, err = storedAnswer(grading.Boolean, answers[appliesKey]); err != nil {
			return base, err
		}
	}
	results := make(map[string]grading.QuestionResult, len(g.Questions))
	for _, q := range g.Questions {
		results[q.Question] = q
	}
	r.Answers = nil
	for _, q := range version.Questions {
		a, ok := answers[q.Name]
		if !ok {
			continue
		}
		raw, err := storedAnswer(grading.QuestionType(q.Type), a)
		if err != nil {
			return base, err
		}
		// A run not graded on appliesWhen has no question results, so it stores no answers.
		res, ok := results[q.Name]
		if !ok {
			continue
		}
		r.Answers = append(r.Answers, gradeAnswer{Question: q.Name, Raw: raw, P: res.P, Verdict: string(res.Verdict)})
	}
	return r, nil
}

// recompute rescores the source grade's stored answers under the version's thresholds.
func recompute(version graderVersion, base gradeRecord, src *gradeRecord) outcome {
	// A recompute makes no call, so it has no usage and costs a known zero. source_version names the grade it read.
	base.StateHash, base.StateCap, base.CostUSD = src.StateHash, src.StateCap, new(0.0)
	answers := make(map[string]inference.Answer, len(src.Answers)+1)
	if len(src.AppliesRaw) > 0 {
		var a inference.Answer
		if err := json.Unmarshal(src.AppliesRaw, &a); err != nil {
			return outcome{drop: true, err: fmt.Errorf("decode applies answer: %w", err)}
		}
		answers[appliesKey] = a
	}
	for _, sa := range src.Answers {
		var a inference.Answer
		if err := json.Unmarshal(sa.Raw, &a); err != nil {
			return outcome{drop: true, err: fmt.Errorf("decode answer %s: %w", sa.Question, err)}
		}
		answers[sa.Question] = a
	}
	r, err := scored(version, base, answers)
	if err != nil {
		return outcome{drop: true, err: err}
	}
	return outcome{record: &r}
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
