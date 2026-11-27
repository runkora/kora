// The short kora story, for a GIF: a developer mentions @kora-agent on a pull
// request, kora routes it to the Code Reviewer, and its answer lands on GitHub.
// The agent and the trigger are in place before it starts.
import { feature } from "../lib/feature.mjs";
import { PULL, followRun, mention, preparePull, removeMention, reply, resetForMention, routing } from "./story/scenes.mjs";

export default feature(
  {
    name: "mention-to-answer",
    title: "Mention @kora-agent on GitHub and kora routes the work",
    start: `https://github.com/${PULL.repo}/pull/${PULL.number}`,
    github: true,
    // Under GitHub's 10 MB limit for an image in a README.
    gif: { width: 880, fps: 10 },
    prepare: preparePull,
    reset: resetForMention,
    after: removeMention,
  },
  async (demo) => {
    await mention(demo, { cutTo: false });
    await routing(demo);
    await followRun(demo);
    await reply(demo);
  },
);
