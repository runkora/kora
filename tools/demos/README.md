# Feature walkthroughs

Each file in `features/` scripts one walkthrough of a kora feature. The runner
plays it against a running kora and records a video with a visible cursor,
click ripples, human-paced typing and captions. The scripts live beside the
code they drive, so a change that moves a button updates its walkthrough in the
same PR.

## Recording

Start the local cluster (`make up`), then:

```bash
make demos                          # every feature -> tools/demos/out/<name>.mp4
make demos FEATURE=agent-create     # one feature
make demos-check                    # every feature at full speed, no recording
```

`make demos-check` runs each walkthrough's steps without pauses or recording.
It fails when a step's target is missing or a step no longer works. Run it
before recording a release.

Each recording writes two files to `out/`:

- `<name>.mp4`: H.264 at 2880x1800, 60 fps.
- `<name>.timeline.json`: every step with its time in milliseconds and its
  target's box on screen, for editing passes such as zooms.

A failed step saves `out/<name>.failure.png`.

`KORA_URL`, `KORA_USERNAME` and `KORA_PASSWORD` point the runner at another
deployment. They default to the local cluster's dev user. The runner signs in
once and keeps the session in `.auth/` until it expires.

## Writing a feature

A feature declares where it starts, how to reset the platform, and the steps a
person would take:

```js
import { feature } from "../lib/feature.mjs";

export default feature(
  { name: "agent-create", title: "Create an agent", start: "/agents", reset },
  async (demo) => {
    await demo.caption("Every agent starts with a verified identity");
    await demo.click(demo.page.getByRole("button", { name: "New Agent" }));
    await demo.type(demo.page.getByPlaceholder("Deployment Assistant"), "Release Notes Writer");
    await demo.pause(800);
  },
);
```

| Step | Recording | Check |
| --- | --- | --- |
| `click(locator)` | Moves the cursor on an eased path and clicks | Clicks |
| `type(locator, text)` | Clicks the field and types at a human pace | Fills |
| `hover(locator)` | Moves the cursor to the target | Hovers |
| `caption(text)` | Shows the caption until the next one; no text hides it | Skipped |
| `pause(ms)` | Holds the frame | Skipped |

`demo.page` is the Playwright page for anything else, such as waiting for a
result. Target elements by role and accessible name, as a person would find
them.

`reset` receives an authenticated request context and returns the platform to
the feature's starting state. It runs before and after the walkthrough, so a
feature that creates something deletes it again.

## Setup (once)

```bash
cd tools/demos && npm install && npm exec playwright install chromium
```

## How it works

- `lib/overlay.mjs` draws the cursor, ripple and caption inside the page.
  Headless Chromium paints no cursor of its own.
- `lib/recorder.mjs` records through the DevTools screencast, which sends a
  frame on every repaint, and encodes with the ffmpeg binary from
  `ffmpeg-static`. Playwright's built-in video is low-bitrate VP8 at 1x.
- The browser runs at a real device scale factor of 2. The screencast ignores
  Playwright's emulated scale factor and would otherwise record at 1x.
- Mouse paths and typing jitter use a fixed seed. Two recordings move and type
  the same way, and differences between releases come from the product.
