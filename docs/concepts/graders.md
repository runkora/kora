# Graders

A Grader scores the runs of one agent against fixed questions. When a run ends, a decision model reads the run's recorded events and answers each question. kora turns the answers into a verdict for the run, and tracks how many of the agent's recent runs pass.

A Grader is a `Grader` resource, declared in Git like an agent or a trigger. It is not created in the console. See [create a Grader](../guides/create-a-grader.md) for how to define one and [declarative resources](../guides/declarative-resources.md#grade-an-agents-runs) for its fields.

## Questions

A Grader holds 1 to 32 questions, each with a unique name. Every question asks one fact about a run and has one of three types:

| Type | The model answers | A question lists |
| --- | --- | --- |
| `boolean` | Yes or no | `expect`, the answer that passes |
| `choice` | One option | `options`, and `accept` for the options that pass. kora adds a `none` option |
| `scale` | One level, lowest first | 2 to 10 `levels`, and `accept` for the levels that pass. `skipOn` is required |

A `choice` or `scale` question can set `skipOn`, a list of answers that mean the question has no subject on the run. A run with no review has nothing to count in a question about the review's findings, so the question is skipped rather than failed.

A Grader can also set `appliesWhen`, a yes or no question. A run that does not meet it is not graded.

## From answers to a verdict

The model gives each answer as a probability. For a `boolean` question, p is the probability of the expected answer. For a `choice` or `scale` question, p is the share of the probability, outside `skipOn`, that falls on the accepted answers.

A threshold turns p into a verdict. A Grader sets one threshold, 0.7 to pass and 0.3 to fail by default, and a question can set its own.

| Verdict | p is |
| --- | --- |
| `pass` | At or above the pass threshold |
| `fail` | At or below the fail threshold |
| `uncertain` | Between the two |
| `skipped` | Not computed, because the answers in `skipOn` carry the probability |

A question is `skipped` when its `skipOn` answers reach the pass threshold, and `uncertain` when they fall between the thresholds. `appliesWhen` uses the same band. A run at or below the fail threshold is `not_applicable`. A run between the thresholds is `applies_uncertain`, and is not graded.

The run's verdict is the worst of its questions. `fail` beats `uncertain`, and `uncertain` beats `pass`. A run where every question is skipped is `not_applicable`.

## Grades

A grade is a run's result for one Grader. Its status is one of:

| Status | Meaning |
| --- | --- |
| `graded` | The run has a verdict |
| `not_applicable` | `appliesWhen` was not met, or every question was skipped |
| `applies_uncertain` | The model was not sure that `appliesWhen` was met |
| `too_large` | The run's recorded events do not fit the decision model's input window |
| `error` | Grading failed after its retries, or the tenant's budget ceiling was reached |

Every run an agent ends is graded by every Grader of that agent. This covers `completed`, `failed`, `budget_exhausted` and `terminated` runs. A Grader grades runs that end after it is applied. It does not grade earlier runs.

The model reads the run's messages, tool calls and results, run state changes and errors. It does not read sandbox status events. When a run is too large, kora shortens the body of each tool result in steps of 4,000, 2,000, 1,000 and 500 characters, then drops the bodies, before it marks the grade `too_large`. A grade records the cap it used.

## Standing

A Grader passes or fails as a whole over its last 30 graded runs. The share of those runs with the verdict `pass` is compared with `minPassRate`. A run with the verdict `uncertain` counts as graded and not as passed.

| Status | Meaning |
| --- | --- |
| `passing` | The pass rate is at or above `minPassRate` |
| `failing` | The pass rate is below `minPassRate` |
| `not_enough_runs` | The Grader has `minPassRate` and fewer than 15 graded runs |
| `no_minimum` | The Grader sets no `minPassRate` |

## Versions

A change to a Grader's `model`, `threshold`, `appliesWhen` or `questions` makes a new version. A change to its `description` or `minPassRate` does not. The agent's views read the current version.

kora regrades the last 30 graded runs of the previous version under the new one. When only thresholds changed, it rescores the stored answers and makes no model call. Otherwise it asks the model again. Grades of earlier versions are kept.

## Cost and access

Grading runs as a service principal that kora creates for each tenant. It holds the right to invoke the models its tenant's Graders name, and no other. kora removes the grant when the tenant has no Grader left. The principal has no budget of its own, so the tenant's budget ceiling bounds its spend. A grade records the model, the input tokens and the cost.

Grades follow access to the agent's runs. Anyone who can read an agent's runs can read its grades. For a [delegated](identity.md#delegated-agents) agent, a Grader's counts include runs other people started, so reading them also needs `agents:delegated:read`.

## Where grades appear

- The run page shows a **Grades** section in its rail. It lists each Grader's verdict and one line per question with its verdict and p.
- The agent page has a **Grades** tab. It lists each Grader with its standing, failing Graders first, and opens a Grader page with its questions, its answers and the runs that failed or were uncertain on each question.
- The agent's **Runs** tab filters runs by Grader, question and verdict. A link on the Grader page opens it filtered to a question's failed runs.
- The `graders.list` and `graders.get` tools of the kora MCP server return a Grader's results. The `runs.list` tool takes `grader`, `question` and `verdict`.

## Related decisions

- [Graders](../adr/2026-11-19-graders.md)
- [Decision models and the System One API](../adr/2026-10-10-systemone-decision-models.md)
