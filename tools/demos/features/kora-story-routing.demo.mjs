// The kora story with a second mention, about 75 seconds: as kora-story, then
// the same trigger on an issue, where kora chooses the Coder.
import { feature } from "../lib/feature.mjs";
import { createAgent, createTrigger, followRun, mention, preparePull, reply, resetForCreate, routing, secondMention } from "./story/scenes.mjs";

export default feature(
  {
    name: "kora-story-routing",
    title: "One trigger, and kora picks the right agent for each mention",
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
    await secondMention(demo);
  },
);
