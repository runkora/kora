# Find and read runs

In runkora, the Runs tab lists every run you can see, and each run opens as a page that records what started it, what the agent did and what it cost. Open **Runs** in the console to see the list. The console opens on this tab.

## Find a run

The Runs tab lists runs newest first, from you, your team and your triggers. The list updates live. A run created while the page is open shows a **New** badge. When new runs arrive while you read an older page, a button such as **2 new runs** loads them. If the live stream drops, a banner says it is reconnecting, and changes appear once it is back.

Each row shows the run's state, its title, the agent that ran it, what started it and when. A run waiting for a message shows **Waiting**. A run that reached its budget shows **Stopped at its budget**. The title is the run's task summary. Until kora has one, the row shows a placeholder.

Choose a row to open the run. Choose **Start work** to [start a run from chat](start-a-run-from-chat.md).

### Filter the list

The filters apply on the server, and the URL keeps them, so back, reload and shared links restore them.

| Filter | Options | Shows |
| --- | --- | --- |
| Quick filter | **All** | Every run you can see |
| | **Waiting** | Runs in the `idle` state |
| | **Mine** | Runs you started |
| | **Active** | Runs in the `pending`, `starting`, `active` or `completing` state |
| | **Failed** | Runs in the `failed` or `budget_exhausted` state |
| Agent | **All agents**, or one agent | Runs of that agent. The list holds the agents you can read. |
| Source | **All sources** | Every run |
| | **People** | Runs no trigger started, such as runs a person started in chat or through the API |
| | **Webhooks** | Runs a webhook trigger started |
| | **Schedules** | Runs a schedule trigger started |
| | **Events** | Runs an event trigger started |
| Search | Text | Runs whose title, agent or starter match the text |

Filters combine. When no run matches, the list reads "No runs match these filters" and offers **Clear filters**. Runs a person or the platform terminated stay out of the list. See [Runs and sessions](../concepts/runs-and-sessions.md#working-with-a-run).

Pages hold 50 runs. Use **Newer** and **Older** to move between them.

## Read a run

The run page records one run. It has three panes: the run's sessions and your other runs on the left, the run as a thread in the middle, and the run's details on the right. A link at the top of the left pane goes back to the Runs tab, with its filters, or to the agent. The panes fold away as the window narrows.

| Width | Layout |
| --- | --- |
| 1280 pixels or wider | All three panes |
| 1024 to 1279 pixels | The thread and the details. The sessions pane opens from the **Sessions** button. |
| 768 to 1023 pixels | The thread. **Sessions** opens a drawer, and **Details** opens a side sheet. |
| Narrower than 768 pixels | The thread. **Sessions** opens a drawer, and **Details** opens a bottom sheet. |

### The header

The header shows the run's links and branch, its title, and its actions: **Trace**, **Details** where the details are folded away, and **New run** once the run has ended. **Trace** opens the run's trace when your install sets `traceUrlTemplate`. See [Monitor kora](monitor-kora.md). On a phone, **Trace** sits in the **More actions** menu.

The header also shows the run's elapsed time and spend, and the progress through the agent's plan when it has one. A bar under it shows the run's state.

| State shown | Run state |
| --- | --- |
| Starting | `pending` or `starting` |
| Working | `active` |
| Waiting for a reply | `idle` |
| Finishing | `completing` |
| Completed | `completed` |
| Failed | `failed` |
| Budget spent | `budget_exhausted` |
| Stopped | `terminated` |

The page shows no state until it knows the run's state.

### The thread

The thread lists everything in the run, oldest first.

- **Trigger events.** A message a trigger sent shows as the event behind it. One line says who acted, what they did and links to the object, for example a pull request. A preview under it shows the object's title and facts, such as the repository, the branch, the lines added and removed, and its state. Text a person wrote, such as a comment, shows as a quote. Choose **Prompt from** *trigger* to read the prompt the agent received. A trigger with no event summary shows its name and the prompt.
- **Joined events.** When a trigger sends another event to a run that is working, the line carries **joined** next to the time.
- **People's messages.** Each message shows its sender's name. A message sent before kora recorded senders shows no sender name.
- **The agent's text and its tool calls.** Tool calls show as quiet blocks that name the command, path or result that matters. A subagent's work nests under the call that started it.

### Steer or stop the run

The composer at the foot of the thread shows what the agent is doing now. Messages you send while the agent works wait for its next step and show as **Next step** above the field.

| Action | How |
| --- | --- |
| Send a message | Write in the field and press **Enter**. **Shift+Enter** adds a line. |
| Stop the agent's current turn | Choose the stop button. It replaces **Send** while the agent works and the field is empty. |

The composer is disabled with "You cannot message this run" when you cannot message the run. For who can message, interrupt or terminate a run, see [Runs and sessions](../concepts/runs-and-sessions.md#working-with-a-run). If the connection to the agent drops, a banner says so, and **Reconnect** resumes the stream.

### The end line

An ended run closes with a line in place of the composer. It names the state, the run's duration and its spend, and the token counts when kora recorded them. For a run that reached its budget, the line says whose budget stopped it, its limit and when it resets. A person who holds `platform:budgets:write` also sees **Raise budget**, which opens the budgets page.

### The details pane

| Section | Shows |
| --- | --- |
| **People** | Everyone who started or messaged the run: people, agents and triggers. Choose it to see how each took part. A person outside the platform who acted through a trigger shows by name. |
| **Links** | The run's [sources](../concepts/runs-and-sessions.md#run-sources), such as the pull request, repository and branch |
| **Runs on** | The harness, the model and its provider, the sandbox image and environment, and each connector with what it can write. A link opens the rest of its access. |
| **Spend** | The run's cost, its model calls and its tokens in, tokens out and cached tokens |

## See also

- [Runs and sessions](../concepts/runs-and-sessions.md): what a run is, its states and how to steer it.
- [Start a run from chat](start-a-run-from-chat.md): start a run from the chat page.
- [Create a trigger](create-a-trigger.md): start runs from the events your team produces.
