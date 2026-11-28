# Create a Grader

In runkora, a Grader scores every run of one agent against fixed questions, so a platform team can see whether the agent keeps doing what it should. You declare a Grader as a `Grader` resource in Git, next to the agent it scores. The console has no form for creating one. It shows the Grader's results once it is applied. See [Graders](../concepts/graders.md) for how questions become verdicts.

Before you start, the [agent](create-an-agent.md) the Grader scores and the model it uses must exist. The model is a `VirtualModel` that is ready. The operator waits for both, so you can apply the resources in any order. See [Declarative resources](declarative-resources.md) for the repository layout and how the operator reports status.

## Choose what to ask

Write each question about one fact in the run's recorded events, such as a tool call the agent made or a result it reported. The decision model reads the events and nothing else. Prefer a question the events can answer, such as "Did the agent post a review through its own tool calls?", over one that needs outside knowledge.

| Type | Use it when | You set |
| --- | --- | --- |
| `boolean` | The answer is yes or no | `expect`: `true` when yes passes, `false` when no passes |
| `choice` | The answer is one of several named options | `options`, and `accept` for the names that pass |
| `scale` | The answer is a point on an ordered range | `levels`, lowest first, and `accept` for the names that pass |

Give each option and level a `name` and a `description`. The model reads the description, so write it to be unambiguous. A `choice` gets a `none` option from kora, for a run where no other option fits.

When a question has no subject on some runs, list the answers that say so in `skipOn`. A `scale` must set `skipOn`, and `skipOn` cannot overlap `accept`. kora does not count a skipped question for or against the run.

To grade only some of the agent's runs, set `appliesWhen` to a yes or no question, such as "Was the agent asked to review a pull request?". A run that does not meet it is not graded.

## Write the resource

Declare the Grader in the agent's tenant namespace.

```yaml
apiVersion: runkora.io/v1alpha1
kind: Grader
metadata:
  name: code-reviewer-review
spec:
  agent: code-reviewer
  description: Checks that a review reports correctness problems.
  model: { family: typesafe, name: jev-1-14 }
  minPassRate: 90%
  appliesWhen: Was the agent asked to review a pull request?
  questions:
    - name: posted-review
      type: boolean
      question: Did the agent post a review through its own tool calls?
      expect: true
    - name: findings
      type: scale
      question: How many correctness problems did the review report?
      levels:
        - { name: no-review, description: No review was posted }
        - { name: none-found, description: No correctness problems }
        - { name: some, description: One or more correctness problems }
      accept: [none-found, some]
      skipOn: [no-review]
```

| Field | Default | What it sets |
| --- | --- | --- |
| `agent` | | The agent in the same namespace whose runs the Grader scores |
| `description` | | One line on what the Grader checks. The console shows it as the Grader's name |
| `model` | | The decision model, as `family` and `name` |
| `questions` | | 1 to 32 questions |
| `appliesWhen` | Every run | A yes or no question that decides whether a run is graded |
| `threshold` | `pass: 0.7`, `fail: 0.3` | The band that turns a probability into a verdict. `fail` must be below `pass` |
| `minPassRate` | None | The share of graded runs that must pass, from `0%` to `100%` |

Apply the resource with `kubectl apply` or commit it to your GitOps repository. Check it with `kubectl get graders -n <namespace>`. A Grader is ready when its `Accepted` and `Ready` conditions are `True`.

## Set the thresholds

The decision model answers with probabilities. A run passes a question at or above the `pass` threshold and fails it at or below the `fail` threshold. Between them the question is `uncertain`. Raise `pass` to demand more confidence. A question can set its own `threshold` when one needs a different band.

## Know what changes a version

A change to `model`, `threshold`, `appliesWhen` or `questions` makes a new version of the Grader. kora regrades the last 30 graded runs under it. A change to `threshold` alone rescores the stored answers without a model call. A change to `description` or `minPassRate` makes no new version.

## Read the grades

A Grader scores runs that end after you apply it. Open the agent and read the results in three places:

- On a run, the **Grades** section of the run page rail lists each Grader's verdict and each question's verdict and probability. A run that has not ended reads **Not graded yet**.
- On the agent, the **Grades** tab lists each Grader with the share of its last 30 graded runs that passed, and its standing against `minPassRate`. Failing Graders come first. Choose one to see each question's answers and the runs that failed or were uncertain.
- On the agent's **Runs** tab, a grade filter lists the runs with a given verdict. On a Grader page, **All failed runs for this question** opens the Runs tab filtered to that question's failed runs.

A Grader reads **Not enough runs** until it has 15 graded runs, and **No minimum** when it sets no `minPassRate`. A grade reads **Too large to grade** when the run's events do not fit the model, and **Grading failed** when grading failed.

The same results are available through the API:

| Endpoint | Returns |
| --- | --- |
| `GET /api/v1/runs/{id}/grades` | The run's grade for each Grader |
| `GET /api/v1/agent-types/{id}/graders` | Each Grader of the agent with its standing |
| `GET /api/v1/agent-types/{id}/graders/{name}` | One Grader with its spec and per-question results |
| `GET /api/v1/runs?grader=&question=&verdict=` | Runs filtered by grade |

The `graders.list` and `graders.get` tools of the kora MCP server return the second and third. The `runs.list` tool takes `grader`, `question` and `verdict`. The MCP server cannot create or change a Grader.

## Control access and cost

Anyone who can read the agent's runs can read its grades. A [delegated](../concepts/identity.md#delegated-agents) agent's Graders also need `agents:delegated:read`, because their counts include runs other people started. See [Policy and roles](policy-and-roles.md#per-agent-access).

Each grade is a call to the decision model, and its cost counts against the tenant's budget ceiling. A grade shows its model, input tokens and cost. A longer run costs more to grade, up to the model's input window.
