# Start a run from chat

In runkora, you start a run from the chat page: write what you want done, choose the agent and send it. The page then becomes the run's page, so you can follow the run and message the agent without leaving it. Open **Runs** and choose **Start work** to open the chat page.

You can also reach the page from the **Agents** list, with the **New Run** button, or with the play button on an agent's row. An agent's own page has a **Start Run** button. These open the chat page with that agent already selected.

## Write the task

The empty page greets you with "What should we work on next, *name*?" and shows the composer below. The composer's placeholder names the selected agent, such as "What should Code reviewer work on?".

Write the task in the field. Press **Enter** to send, or **Shift+Enter** to add a line. kora starts a run of the selected agent with your text as its first message, and the page becomes that run's page without a reload. If the run fails to start, the error shows under the composer and your text stays in the field.

You cannot choose a repository, a branch or a model on this page. The agent's configuration sets them. To change them, [edit the agent](create-an-agent.md).

## Choose the agent

The agent picker sits in the composer's footer and shows the selected agent's name. Choose it to open the list of agents you can see.

| Part of an option | Meaning |
| --- | --- |
| Name and description | The agent's name, and its description on one line |
| **Acts as you** | A [delegated](../concepts/identity.md) agent. The run acts with your access and your budget. |
| **Acts as itself** | An [autonomous](../concepts/identity.md) agent. The run acts with the agent's own access. |
| **No access** | You cannot start runs of this agent. The option is disabled. |

A search field shows when the list holds 8 agents or more. It matches an agent's name and description.

The page selects an agent for you when it opens:

1. The agent you opened the page from, when you came from the **Agents** list or an agent's page. kora selects it even when you cannot start its runs. The composer then reads "You cannot start runs of *agent*" and is disabled.
2. Otherwise, the agent of your most recent run, when you can still start it.
3. Otherwise, the first agent you can start.

A person with no agents sees **No agents to start**, with a link to **Go to agents**. An agent must be active before you can start it. [Create an agent](create-an-agent.md) or ask an administrator for access to one.

## Return to your runs

The left pane lists your own runs. It holds a **Home** link, a **New run** row and the **Your runs** list.

- **Home** goes back to the Runs tab, which the console opens on.
- **New run** opens an empty chat page.
- **Your runs** lists the runs you started, newest first. A run that is working shows a running mark. With no runs, the list reads "Runs you start appear here."
- The run that is open shows at the top of the list, with its subagents under it.

Below the widest layout, the pane opens as a drawer. Use the **Your runs** button on the empty page, or the **Sessions** button on a run's page.

Runs that a trigger started do not show in **Your runs**. Find them on the Runs tab, as described in [Find and read runs](find-and-read-runs.md).

## Work with the run

Once the run starts, the page shows the run: a header, the thread and a composer at the foot. Write a message to steer the agent, or stop its current turn. [Find and read runs](find-and-read-runs.md#read-a-run) describes the page.

## See also

- [Runs and sessions](../concepts/runs-and-sessions.md): what a run is, its states and how to steer it.
- [Find and read runs](find-and-read-runs.md): the Runs tab and the run page.
- [Create an agent](create-an-agent.md): configure the agents you start.
