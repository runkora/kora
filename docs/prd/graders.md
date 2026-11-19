# Graders - Product Requirements Document

**Status:** In Review  
**Created:** 2026-11-19  
**Last Updated:** 2026-11-19  
**Owner:** Justin Wilkin  
**Stakeholders:** Platform Team  

---

## Executive Summary

A Grader scores an agent's work against fixed questions. A decision model answers the questions from the run's recorded trace. kora grades each run once, when the run ends, and stores the verdict. The run page shows how one run went. The agent's Grades tab shows how the agent performs across its recent runs.

---

## Problem Statement

### Current State

kora records every event of a run. Nothing records whether the run did what it was for. A reader learns that only by reading the thread. A change to an agent's prompt, tools, skills or model has no measure of its effect.

### User Pain Points

*   An operator reads each run to tell a good run from a bad one.
*   An agent owner cannot see whether a prompt change made the agent better or worse.
*   A failure that recurs across runs shows only to someone who reads many runs.

### Business Impact

*   Agent regressions reach requesters before an owner sees them.
*   Agent quality has no stored record a team can review.

---

## Goals and Objectives

### Primary Goals

1.  Grade every ended run of an agent against each of its Graders.
2.  Give the same stored verdict for the same trace and the same Grader version.
3.  Show each run's verdicts on the run page.
4.  Show each Grader's pass rate over its last 30 graded runs against its `minPassRate`.

### Success Metrics

None at this time.

### Non-Goals

*   Statistics views: the run strip, the config table, config comparison, question failure tables and stability.
*   Config versions. `agent_config` is a digest only.
*   Thread grading across the runs that share a Trigger `joinKey`.
*   Answer labels on a run page.
*   Offline test sets for a new Grader version.

---

## User Stories

### US-001: Declare a Grader

**As an** agent owner,  
**I want to** declare a Grader for my agent as a resource,  
**So that** its questions are reviewed and versioned with the agent.

**Acceptance Criteria:**

*   [ ] A `Grader` is a `runkora.io/v1alpha1` resource that names one agent.
*   [ ] A Grader has a `description`, a decision `model`, a `threshold`, an optional `minPassRate`, an optional `appliesWhen` and a list of `questions`.
*   [ ] A question's `type` is `boolean`, `choice` or `scale`.
*   [ ] A `boolean` question sets `expect`.
*   [ ] A `choice` or `scale` question sets `accept`. It may also set `skipOn`.
*   [ ] `threshold` defaults to pass at 0.7 and fail at 0.3. A question can set its own `threshold`.
*   [ ] A `choice` gets a `none` option added on apply.
*   [ ] Apply rejects an `accept` that lists every option, including `none`.
*   [ ] Apply rejects a `scale` without a level listed in `skipOn` for runs where the question has no subject.
*   [ ] Apply rejects duplicate names, and an `accept` or `skipOn` name that is not an option or level.
*   [ ] Apply rejects a name in both `accept` and `skipOn`, and a `skipOn` that lists every option or level.

### US-002: Grade a run when it ends

**As an** agent owner,  
**I want** each run graded once when it ends,  
**So that** every run has a verdict without anyone asking for one.

**Acceptance Criteria:**

*   [ ] A run is graded when it ends as completed, failed, budget exhausted or terminated.
*   [ ] A run that ended on its budget is graded.
*   [ ] Grading is paid through its own principal. The principal has no budget of its own. The tenant ceiling bounds it.
*   [ ] Grading cost is not part of the agent's cost.
*   [ ] One System One call answers `appliesWhen` and every question of one Grader for one run.
*   [ ] The grade records the input tokens, the served model and the cost.
*   [ ] A run that has not ended shows "Not graded yet".

### US-003: Grade the whole trace

**As an** agent owner,  
**I want** the decision model to read the run's whole recorded trace,  
**So that** the answers rest on what the agent did.

**Acceptance Criteria:**

*   [ ] The state holds the run's events in sequence order: `human_message`, `trigger_message`, `tool_use` with inputs, `tool_result`, `text_block`, `message_complete`, `run_state`, `run_error`, `turn_stopped` and `guardrail_warning`.
*   [ ] `sandbox_status`, `sandbox_environment` and `api_retry` are left out.
*   [ ] A tool result cut at 64 KiB keeps its truncation marker.
*   [ ] When the state does not fit the model's window, each tool result is capped at the first step that fits: 4000, 2000, 1000, then 500 characters, then no result bodies.
*   [ ] Messages and tool inputs are never cut.
*   [ ] A capped result keeps a marker with its original length.
*   [ ] The same run gives the same state.
*   [ ] A state that does not fit with no result bodies is recorded as `too_large` and is not graded.
*   [ ] kora stores the state as a snapshot with its hash. Regrading reads the snapshot.

### US-004: Read a run's verdicts

**As an** operator,  
**I want to** see each Grader's verdict on the run page,  
**So that** I can tell how the run went without reading the thread.

**Acceptance Criteria:**

*   [ ] The run rail lists each Grader with a verdict badge.
*   [ ] Each question shows its verdict icon, its name and p. The question text shows on hover.
*   [ ] An uncertain question shows its answer distribution.
*   [ ] A footnote gives the model, the tokens, the cost and any state cap.

### US-005: See which Graders fail

**As an** agent owner,  
**I want** a list of my agent's Graders with their pass rates,  
**So that** I see the failing checks first.

**Acceptance Criteria:**

*   [ ] The agent page has a Grades tab with a checks list in a `DataTable`.
*   [ ] The columns are status, description, the pass rate with its minimum (such as "52% passed · needs 90%") and graded runs.
*   [ ] Failing rows come first.
*   [ ] The list has search, the quick filters Failing, Passing and No minimum, and paging of 12, 25 or 50 rows.
*   [ ] A Grader with `minPassRate` and under 15 graded runs reads "Not enough runs".
*   [ ] A Grader without `minPassRate` reads "No minimum" and shows its pass rate with no passing or failing status.

### US-006: Find the runs a question failed

**As an** agent owner,  
**I want to** open a Grader and see which questions fail and on which runs,  
**So that** I can fix the agent or the question.

**Acceptance Criteria:**

*   [ ] The Grader page shows the Grader's status first.
*   [ ] "What it asks" lists each question with its pass condition and a View spec link.
*   [ ] The questions form an accordion sorted by failed plus uncertain count.
*   [ ] A question shows its type: Yes/No, Choice or Scale.
*   [ ] An expanded question lists every possible answer with a pass tick, marks skipOn levels "skips the question", and counts the window's runs that had each as the most likely answer.
*   [ ] A question expands to its failed and uncertain runs: at most 5, then Show all, up to 30.
*   [ ] "All failed runs for this question" opens the agent's Runs tab filtered by that grade.

### US-007: Change a Grader

**As an** agent owner,  
**I want** a changed Grader to regrade recent runs,  
**So that** its status reflects the new questions at once.

**Acceptance Criteria:**

*   [ ] A change to `model`, `threshold`, `appliesWhen` or `questions` is a new Grader version.
*   [ ] A change to `description` or `minPassRate` is not a new version.
*   [ ] A new version regrades the Grader's last 30 graded runs from their snapshots.
*   [ ] A change to thresholds only recomputes verdicts from the stored answers with no model call.
*   [ ] Only results from the same Grader version are compared.

---

## Proposed Solution

### High-Level Approach

kora-agents owns grading. At run end it queues one grade job per Grader of the run's agent. A worker builds the state from the run's events, calls System One through the inference gateway and writes the grade and its answers. The run page, the Grades tab and the Grader page read the stored grades.

### Verdicts

Each answer becomes p, the probability that the expected answer holds.

| Type | p |
| --- | --- |
| `boolean`, `expect: true` | the answer |
| `boolean`, `expect: false` | 1 minus the answer |
| `choice` | probability of the accepted options, divided by the probability of the options not in `skipOn` |
| `scale` | probability of the accepted levels, divided by the probability of the levels not in `skipOn` |

A question uses its own `threshold` when it sets one, and the Grader's `threshold` otherwise. p at or above `pass` is a pass. p at or below `fail` is a fail. p between the two is uncertain.

A `choice` or `scale` with `skipOn` is first checked for a skip. The skip probability is the sum over its `skipOn` names, judged on the question's band. At or above `pass` the question is `skipped`. Between the bounds it is uncertain. At or below `fail` it is scored with p.

`appliesWhen` uses the Grader's band. At or above `pass` the run is graded. At or below `fail` the run is `not_applicable`. Between, the run is `applies_uncertain` and its questions are not scored.

A run passes when every question that is not skipped passes. Any failed question makes the run fail. Otherwise any uncertain question makes it uncertain. A run where every question is skipped is `not_applicable`.

### Pass Rate

`threshold` judges one answer on one run. `minPassRate` judges the Grader across runs. The window is the Grader's last 30 graded runs. The pass rate is passed runs divided by graded runs. Uncertain runs count in the denominator. The Grader is passing when the pass rate is at or above `minPassRate`, and failing below it. The window ignores `agent_config`. A Grader with `minPassRate` and under 15 graded runs reads "Not enough runs".

### User Experience

*   The run rail gains a Grades section under Spend.
*   The agent page gains a Grades tab with the checks list.
*   Each Grader has a page with its status, "What it asks" and the question accordion.
*   The agent's Runs tab accepts a filter by Grader, question and verdict.

The designs are in Storybook: the Grades story of `Pages/Agents/Run/Rail`, and the Grades, Grader page and RunsFilteredByGrade stories of `Pages/Agents/Detail`.

### Copy

Every string is a stored fact or a label for one. The copy uses the spec's vocabulary: run, pass, fail, uncertain and Grader. It carries no advice, interpretation or invented summary. Digests, bars, grids and statistics are not shown.

---

## Scope

### In Scope

*   The `Grader` resource, its validation and its versions.
*   `agent_config` on each run: a digest of the effective agent config at run start.
*   Grading at run end, the state snapshot, the System One call and the stored grades.
*   The grading principal.
*   Regrading on a new Grader version.
*   Run grades in the run rail.
*   The agent's Grades tab.
*   The Grader page.
*   The runs list filtered by grade.

### Out of Scope

*   The items under Non-Goals.

### Future Considerations

*   Thread grading: grade the runs that share a Trigger `joinKey` as one request.
*   Config versions: an `agent_configs` table keyed by agent and digest, with a version number per digest and a diff of two configs.
*   Answer labels: a reader marks an answer as wrong. Labelled runs become a test set for a new Grader version.
*   The statistics views listed under Non-Goals.

---

## Requirements

### Functional Requirements

*   A grade is unique per Grader, Grader version and run.
*   A grade's `status` is `graded`, `not_applicable`, `applies_uncertain`, `too_large` or `error`. `verdict` is set when `status` is `graded`.
*   An answer's verdict is `pass`, `fail`, `uncertain` or `skipped`. p is empty when skipped.
*   A grade records the tool result cap it used, or none.
*   A grade refused on the tenant ceiling is recorded as `error`.
*   Every view shows the count of `too_large` runs.
*   Reading a run's grades needs permission to read the run.

### Non-Functional Requirements

None at this time.

---

## Dependencies

### Internal Dependencies

*   The System One API and decision models (`docs/adr/2026-10-10-systemone-decision-models.md`).
*   Declarative resources (`docs/adr/2026-10-24-declarative-resources.md`).
*   The run page (`docs/prd/run-page.md`).
*   Hierarchical budgets (`docs/adr/2026-06-10-hierarchical-budget-enforcement.md`).

---

## Open Questions

None at this time.

---

## Appendix

### Related Documents

*   [Graders ADR](../adr/2026-11-19-graders.md)

### Glossary

*   **Grader:** a resource with fixed questions that scores one agent's runs.
*   **Grader version:** a hash of a Grader's `model`, `threshold`, `appliesWhen` and `questions`.
*   **State:** the run's events as the decision model reads them.
*   **p:** the probability that the expected answer holds.
*   **Graded run:** a run whose grade has `status` `graded`.
