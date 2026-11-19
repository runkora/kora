package agents

import (
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/runkora/kora/backend/internal/agents/grading"
)

// GET /api/v1/runs page sizes, the same bounds as the run events endpoint.
const (
	defaultRunsLimit = 50
	maxRunsLimit     = 200
)

// startedByTrigger is the started_by filter value that keeps runs a trigger started.
const startedByTrigger = "trigger"

var (
	// ErrInvalidRunCursor is returned for a cursor this service did not issue.
	ErrInvalidRunCursor = errors.New("invalid run cursor")
	// ErrInvalidRunState is returned for a state filter naming no run state.
	ErrInvalidRunState = errors.New("invalid run state")
	// ErrInvalidGradeFilter is returned for a malformed grader, question and verdict combination.
	// An unknown grader or question is accepted and matches no run.
	ErrInvalidGradeFilter = errors.New("invalid grade filter")
)

// liveRunStates are the states of a run that is in flight: queued, starting,
// working a turn or finishing. An idle run waits for a message and is not live.
var liveRunStates = []RunState{RunStatePending, RunStateStarting, RunStateActive, RunStateCompleting}

// runCursor is the keyset position of the last run on a page. The next page
// holds the runs strictly after it in (created_at DESC, id DESC) order.
type runCursor struct {
	CreatedAt time.Time
	ID        string
}

// encode renders the cursor as an opaque URL-safe token.
func (c runCursor) encode() string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID))
}

// decodeRunCursor reads a token made by encode. An empty token is the first page.
func decodeRunCursor(raw string) (*runCursor, error) {
	if raw == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRunCursor, err)
	}
	ts, id, ok := strings.Cut(string(b), "|")
	if !ok || id == "" {
		return nil, ErrInvalidRunCursor
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRunCursor, err)
	}
	return &runCursor{CreatedAt: t, ID: id}, nil
}

// RunScope is the set of runs a read may see: runs of the listed agent types,
// under the delegated-run visibility rule. An empty AgentTypeIDs matches no run.
type RunScope struct {
	AgentTypeIDs []string
	Vis          RunVisibility
	// Own adds the runs Vis.Subject started, of any agent type (D3: callers see their own runs).
	Own bool
	// OnlyAgentTypeIDs narrows the scope, own runs included, to these agent types when set.
	OnlyAgentTypeIDs []string
}

// appendScope adds the scope to a run query (runs as r, agent_types as at).
func appendScope(query string, args []any, scope RunScope) (string, []any) {
	ids := scope.AgentTypeIDs
	if ids == nil {
		ids = []string{}
	}
	args = append(args, ids)
	if scope.Own {
		args = append(args, scope.Vis.Subject)
		query += fmt.Sprintf(" AND (r.agent_type_id = ANY($%d) OR ($%[2]d <> '' AND r.created_by = $%[2]d))", len(args)-1, len(args))
	} else {
		query += fmt.Sprintf(" AND r.agent_type_id = ANY($%d)", len(args))
	}
	if len(scope.OnlyAgentTypeIDs) > 0 {
		args = append(args, scope.OnlyAgentTypeIDs)
		query += fmt.Sprintf(" AND r.agent_type_id = ANY($%d)", len(args))
	}
	return appendVisibility(query, args, scope.Vis)
}

// RunListQuery selects one page of runs, newest first.
type RunListQuery struct {
	// Scope is set by the service from the caller's grants; the repository applies it.
	Scope RunScope
	// States narrows to runs in these states; empty means every state.
	States []RunState
	// CreatedAfter and CreatedBefore bound created_at: at or after, and before.
	// Zero means unbounded.
	CreatedAfter  time.Time
	CreatedBefore time.Time
	// StartedByTrigger narrows to runs a trigger started.
	StartedByTrigger bool
	// Grade narrows to runs graded by a Grader's current version; nil means no grade filter.
	Grade *GradeFilter
	// After is the previous page's cursor; nil is the first page.
	After *runCursor
	Limit int
}

// RunPage is a page of runs and the cursor of the next one, empty on the last page.
type RunPage struct {
	Runs       []Run
	NextCursor string
}

// parseRunStates reads a comma-separated list of run states.
func parseRunStates(raw string) ([]RunState, error) {
	if raw == "" {
		return nil, nil
	}
	known := AllRunStates()
	var out []RunState
	for part := range strings.SplitSeq(raw, ",") {
		s := RunState(strings.TrimSpace(part))
		if !slices.Contains(known, s) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidRunState, s)
		}
		out = append(out, s)
	}
	return out, nil
}

// GradeFilter keeps runs whose grade on the named Grader's current version matches. With a
// question it matches that question's answer verdict, and otherwise the run's verdict. An empty
// Verdict matches any.
type GradeFilter struct {
	Grader   string
	Question string
	Verdict  string
}

// parseGradeFilter reads the grader, question and verdict filters. Question and verdict need a
// grader. A run verdict is pass, fail or uncertain. An answer verdict can also be skipped.
func parseGradeFilter(grader, question, verdict string) (*GradeFilter, error) {
	if grader == "" {
		if question != "" || verdict != "" {
			return nil, fmt.Errorf("%w: question and verdict need grader", ErrInvalidGradeFilter)
		}
		return nil, nil
	}
	switch grading.Verdict(verdict) {
	case "", grading.Pass, grading.Fail, grading.Uncertain:
	case grading.Skipped:
		if question == "" {
			return nil, fmt.Errorf("%w: verdict skipped needs question", ErrInvalidGradeFilter)
		}
	default:
		return nil, fmt.Errorf("%w: verdict must be pass, fail, uncertain or skipped", ErrInvalidGradeFilter)
	}
	return &GradeFilter{Grader: grader, Question: question, Verdict: verdict}, nil
}

// gradeFilterField names the field a rejected grade filter is missing or has wrong.
func gradeFilterField(grader string) string {
	if grader == "" {
		return "grader"
	}
	return "verdict"
}

// appendGradeFilter adds the filter to a run query (runs as r) as an EXISTS over the current
// version's grades and answers.
func appendGradeFilter(query string, args []any, f *GradeFilter) (string, []any) {
	if f == nil {
		return query, args
	}
	args = append(args, f.Grader)
	sub := fmt.Sprintf(`SELECT 1 FROM graders gf JOIN grades gg ON gg.grader_id = gf.id AND gg.version = gf.current_version
		WHERE gf.tenant_id = r.tenant_id AND gf.agent_type_id = r.agent_type_id AND gf.name = $%d AND gg.run_id = r.id`, len(args))
	switch {
	case f.Question != "":
		args = append(args, f.Question)
		ans := fmt.Sprintf(`SELECT 1 FROM grade_answers ga WHERE ga.grade_id = gg.id AND ga.question = $%d`, len(args))
		if f.Verdict != "" {
			args = append(args, f.Verdict)
			ans += fmt.Sprintf(` AND ga.verdict = $%d`, len(args))
		}
		sub += ` AND EXISTS (` + ans + `)`
	case f.Verdict != "":
		args = append(args, f.Verdict)
		sub += fmt.Sprintf(` AND gg.verdict = $%d`, len(args))
	}
	return query + ` AND EXISTS (` + sub + `)`, args
}
