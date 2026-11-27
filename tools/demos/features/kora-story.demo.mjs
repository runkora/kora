// The kora story, about a minute: create the Code Reviewer, create a trigger
// that lets kora choose the agent for every @kora-agent mention, then mention
// @kora-agent on a pull request and follow the run to its answer on GitHub.
import { feature } from "../lib/feature.mjs";
import { createAgent, createTrigger, followRun, mention, preparePull, reply, resetForCreate, routing } from "./story/scenes.mjs";

export default feature(
  {
    name: "kora-story",
    title: "Build an agent, route work to it, and see it answer on GitHub",
    start: "/agents",
    github: true,
    prepare: preparePull,
    reset: (api) => resetForCreate(api),
    after: (api) => resetForCreate(api, { after: true }),
  },
  async (demo) => {
    await createAgent(demo);
    await createTrigger(demo);
    await mention(demo);
    await routing(demo);
    await followRun(demo);
    await reply(demo);
  },
);
