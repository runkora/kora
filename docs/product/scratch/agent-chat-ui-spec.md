# Agent Chat UI — Feature Specification

**Product:** Orbit AI  
**Component:** Agent Chat Interface  
**Reference mockup:** `research/terra-agent-chat-v2.html`  
**Status:** Design complete, ready for implementation

---

## 1. Overview

The agent chat UI is the primary interface for interacting with an AI coding agent. It is intentionally different from typical human chat UIs. The agent side of the conversation is a seamless, flowing display of work — a mix of prose, tool calls, and inline code — rather than a series of message bubbles. The human side uses conventional right-aligned bubbles.

The interface has four distinct regions: a topbar, a context strip, a main chat/thread panel, and an optional slide-out canvas panel for viewing file contents and terminal output.

The overall aesthetic follows the Terra design system: dark charcoal surfaces, near-monochromatic neutral greys with a warm pink/rose accent. Everything should feel dense, precise, and technical — like a high-end terminal or IDE, not a consumer chat app.

---

## 2. Design Tokens

All colours are defined in OKLCH. The implementation should use CSS custom properties. Map these to whatever token system the codebase uses, but the exact perceptual values matter for the dark-mode feel.

```
Background surfaces (darkest → lightest):
  --deep:  oklch(13% 0.005 248)   ← code blocks, canvas bg, terminal
  --bg:    oklch(18% 0.005 248)   ← main page background
  --surf:  oklch(22% 0.005 248)   ← topbar, input area, panel headers
  --surf2: oklch(27% 0.005 248)   ← hover states, chips, secondary inputs
  --surf3: oklch(32% 0.005 248)   ← scrollbar thumbs, progress track

Foreground:
  --fg:     oklch(96% 0.005 248)  ← primary text
  --muted:  oklch(72% 0.005 248)  ← secondary text, labels
  --subtle: oklch(58% 0.005 248)  ← de-emphasised text, placeholders

Borders:
  --bd:   oklch(52% 0.005 248)    ← prominent borders (input focus)
  --bds:  oklch(28% 0.005 248)    ← standard borders, dividers

Accent (warm rose/pink):
  --acc:     oklch(72% 0.17 8)    ← accent foreground, icons, highlights
  --acc-h:   oklch(82% 0.17 8)    ← hover state of accent
  --acc-dim: oklch(72% 0.17 8 / 0.10)  ← accent background fill
  --acc-bdr: oklch(72% 0.17 8 / 0.30)  ← accent border

Semantic status colours:
  --healthy:  #9ce37d   ← success / done / green
  --degraded: #ffd046   ← running / in-progress / amber
  --error:    #ef4444   ← failure / red
```

**Typography:**
- Body font: `ui-sans-serif, system-ui, -apple-system, sans-serif`
- Monospace font: `'Cascadia Code', 'Fira Code', ui-monospace, monospace`
- Base size: 13.5px, line-height 1.6

---

## 3. Page Layout

The page is a full-viewport height flex column with no overflow. Three rows stack vertically: topbar → context strip → layout body. The layout body is a flex row: chat panel (flex: 1) + canvas panel (fixed width, initially collapsed).

```
┌─────────────────────────────────────────────────────────┐
│  Topbar (46px)                                          │
├─────────────────────────────────────────────────────────┤
│  Context strip (32px)                                   │
├──────────────────────────────────┬──────────────────────┤
│                                  │                      │
│  Chat panel (flex: 1)            │  Canvas panel        │
│  ┌─ Thread (scrollable) ───────┐ │  (0 → 520px slide)  │
│  │  messages                  │ │                      │
│  └────────────────────────────┘ │                      │
│  ┌─ Input area (fixed) ────────┐ │                      │
│  └────────────────────────────┘ │                      │
└──────────────────────────────────┴──────────────────────┘
```

---

## 4. Topbar

**Height:** 46px  
**Background:** `--surf`  
**Border:** 1px bottom `--bds`

Left-to-right content:

### 4.1 Logo / brand mark
Small 20×20px square with rounded corners (4px), `--acc-dim` fill and `--acc-bdr` border. Contains a small SVG orbital icon (centre dot + outer ring). Label text "Orbit AI" at 13px, font-weight 600. In a real implementation, swap for the product's actual logo.

### 4.2 Model pill
Pill-shaped badge (border-radius 20px) containing:
- A 5×5px pulsing green dot (live indicator — see Animations section)
- Model name in monospace, e.g. `claude-sonnet-4-6`
- Colour: `--muted`

### 4.3 Session pill (optional)
Adjacent pill with session ID in monospace, `--subtle` colour. Acts as a visual indicator of the current conversation context.

### 4.4 Context meter
A small horizontal group showing context window usage: label "ctx", a 72px × 3px progress bar (`--surf3` track, `--acc` fill), and a `42k / 200k` token count. This is informational only — it helps the user know how much of the context window has been consumed.

### 4.5 Icon buttons
Square 27×27px icon buttons with transparent background, `--subtle` icon colour. On hover: `--surf2` background, `--muted` icon, `--bds` border. Currently includes a document/log icon for session history.

### 4.6 Canvas toggle button
A slightly more prominent button (not just an icon) that reads "Canvas" with a split-pane SVG icon. This button toggles the canvas panel open/closed.

**States:**
- Default: transparent background, `--bds` border, `--subtle` text
- Hover: `--surf2` bg, `--bd` border, `--muted` text
- Active (canvas open): `--acc-dim` bg, `--acc-bdr` border, `--acc` text

---

## 5. Context Strip

**Height:** ~32px  
**Background:** `--surf`  
**Border:** 1px bottom `--bds`

A horizontal strip of file chips representing files currently in the agent's context window.

### 5.1 File chips
Pill-shaped (border-radius 20px). Shows `{ext}` prefix in `--acc` colour at 75% opacity, then `path/filename` with the filename bolded. On hover: `--acc-bdr` border, `--acc` text colour.

Clicking a file chip opens that file in the canvas panel.

### 5.2 Add chip
A dashed-border pill with "+ add file" text. On hover: `--acc-bdr` border, `--acc` colour. Opens a file picker or search to add more context files.

---

## 6. Chat Thread

The thread is a scrollable `flex: 1` container with `padding: 20px 0 12px`. Thin scrollbar (5px width, `--surf3` thumb).

### 6.1 Session divider
A full-width decorative row with a horizontal rule and centred label: "Session started · Date · Time". Font size 10.5px, uppercase, letter-spacing 0.07em, colour `--bds`. The line extends to the right with `::after { flex: 1; height: 1px; background: --bds; opacity: 0.4 }`.

### 6.2 Human message (user turn)

Right-aligned row with 4px vertical / 20px horizontal padding. Contains a single bubble element:

- Max width: 68% of panel
- Background: `--acc-dim`
- Border: 1px solid `--acc-bdr`
- Border radius: 10px 10px 3px 10px (flat bottom-right corner)
- Padding: 9px 14px
- Font colour: `--fg`, line-height 1.55

No avatar, no label. Pure bubble on the right side.

### 6.3 Agent turn (agent response)

Each agent turn has two sub-elements: a **byline** and **content**.

**Byline:**
- 18×18px square icon (same orbital SVG as logo), `--acc-dim` fill, `--acc-bdr` border, 3px border-radius
- "Orbit AI" label at 12px, `--muted`, font-weight 500
- Timestamp at 11px, `--subtle`, monospace
- Margin-bottom: 10px

**Content:**
All agent content is indented 26px from the left (aligning with the text next to the byline icon). The content is a seamless vertical flow — no wrapper boxes or cards. It consists of any combination of:

1. Thinking block
2. Agent text paragraphs
3. Tool rows
4. Approval prompt

These elements flow naturally in document order, interleaved however the agent produces them.

---

## 7. Agent Content Elements

### 7.1 Thinking block

A collapsible section shown when the model has extended thinking enabled.

**Structure:**
- Left border: 2px solid `--bds`, padding-left 12px
- Toggle row: clickable, contains a small orbital loader (sm size, 70% opacity) + "Thought for N seconds" text (11.5px, `--subtle`)
- Clicking the toggle row expands/collapses the body
- Body (collapsed by default): 12px italic text, `--subtle` colour, line-height 1.65
- Optional token count displayed inline in monospace at 10.5px, 35% opacity

The small orbital in the toggle header replaces what would otherwise be a plain chevron icon. This is a deliberate brand touch — even at rest, completed thinking blocks show a mini orbital.

### 7.2 Agent text

Plain `<p>` tags. `--fg` colour, line-height 1.65. Adjacent paragraphs have 6px top margin between them.

Inline `code` spans: `--mono` font, 12px, `--acc` colour, `--acc-dim` background, `--bds` border, 3px border-radius, 1px 5px padding.

### 7.3 Tool row

Tool rows are the core UI element for displaying agent tool use. Each row represents one tool invocation.

**Structure:**
```
┌ 2px coloured left border
│  [icon] [tool_name] [argument...........] [status badge] [⤢]
└
```

**Left border colour by status:**
- Running: `oklch(50% 0.1 80 / 0.6)` (amber)
- Done: `oklch(45% 0.1 140 / 0.5)` (green)
- Error: `oklch(45% 0.15 25 / 0.5)` (red)
- Canvas-active (currently shown in canvas): `--acc` (pink)

**Row contents:**
- **Icon:** 12px emoji or icon (📄 read, ✏️ write, ⚡ bash, 🔍 search, etc.)
- **Tool name:** monospace 12px, `--muted`
- **Argument:** monospace 11.5px, `--subtle`, flex: 1, overflow ellipsis (truncated if too long)
- **Status badge:**
  - Running: small inline orbital spinner (see Animations) + "running" in `--degraded` colour
  - Done: "✓ {result}" in `--healthy` colour
  - Error: "✗ {error}" in `--error` colour
- **Pop-out button (⤢):** 22×22px, appears on row hover only (opacity 0 → 1 transition). On hover: `--surf2` bg, `--acc` icon, `--acc-bdr` border. When the row is canvas-active: always visible, `--acc-dim` bg, `--acc` icon.

The pop-out button opens the tool's output in the canvas panel (see Canvas section). Row height is compact — just the single header line, no expanded body by default.

**Status border transition:** `transition: border-color 0.15s` so the border smoothly changes colour as a tool transitions from running → done/error.

### 7.4 Approval prompt

Shown when the agent wants explicit user permission before executing a potentially impactful action (e.g., running an integration test, deploying, deleting files).

**Structure:**
- Container: `--acc-dim` background, `--acc-bdr` border, 8px border-radius, overflow hidden
- Header: 8px/12px padding, `--acc-bdr` bottom border. Contains an accent diamond icon (◈) and a plain-English description of what will happen
- Body: 9px/12px padding. Longer explanation + the exact command in monospace
- Actions row: `--acc-bdr` top border, three buttons:
  - **Approve** (primary): `--acc` background, dark text, rounded 4px
  - **Edit** (secondary): transparent, `--bds` border, `--muted` text
  - **Reject** (danger): transparent, red border, `--error` text

Button hover states: primary brightens to `--acc-h`, secondary gets `--surf2` bg, danger gets a faint red background fill.

---

## 8. Live Loading State

This is the most important interaction in the UI. When the agent is actively working, the interface shows a real-time view of its progress — not a spinner that blocks everything until it's done.

### 8.1 Live area vs. full content

The agent turn contains two mutually exclusive regions:
- `#live-area` — shown while the agent is working
- `#full-content` — shown once the agent is done

When a new response begins, `#full-content` is hidden and `#live-area` is displayed.

### 8.2 Orbital status bar

At the top of `#live-area`, a status bar contains:
- The **orbital loader** (md size) on the left
- A text block on the right:
  - Top line: "Working…" (11.5px, `--muted`, font-weight 500)
  - Bottom line: current step description in monospace (11px, `--subtle`) — this updates as the agent progresses through steps

The step text animates on change: slides in from 3px below with a fade, using a CSS keyframe (`step-in`).

**Completion:** When the agent finishes, the status bar receives a `done` class which triggers a CSS transition: opacity fades to 0 (0.3s), then height collapses to 0 with padding/margin collapsing simultaneously (delayed 0.25s so it fades out before collapsing). This prevents jarring layout shifts.

### 8.3 Progressive tool rows

Below the orbital status bar, tool rows appear one at a time as the agent invokes tools. Each tool starts in the `run` state (amber border, orbital spinner badge). When the tool call resolves, the row transitions to `done` or `err` state (border colour changes, badge updates to result text).

Each new tool row animates in with `tool-enter`: slides up 3px from below with a fade over 0.2s.

### 8.4 Concurrent text streaming

Text from the agent streams in character-by-character into `#live-text` while tools are still running. A blinking cursor (`<span class="cur">`) appears at the end of the streaming text. The cursor is 2px wide × 13px tall, `--acc` colour, with a smooth fade animation (not a hard blink — it eases in and out).

The text and tool rows coexist — new tool rows can appear below streaming text, and text can stream in while a previous tool row is still showing as running. This is intentional: it represents the agent's ability to reason and act simultaneously.

After streaming completes, the cursor disappears and a wave bar animation can optionally persist to indicate the agent is continuing to process (see Animations).

### 8.5 Transition to full content

Once the agent is done:
1. Orbital status bar fades out and collapses
2. Short pause (650ms)
3. `#live-area` is hidden
4. `#full-content` becomes visible — this shows the final, formatted, complete version of the response with proper thinking block, tool rows with pop-out buttons, full text, and approval prompt if any

---

## 9. Animations

### 9.1 Orbital loader (branded loading animation)

The orbital loader is the primary loading indicator. It exists in two sizes:

**Small (sm): 32×32px** — used inside the thinking block toggle and the demo button  
**Medium (md): 44×44px** — used in the orbital status bar during live loading

**Structure (all positioned absolute within a relative container):**
- **Core dot:** centre of the loader, 5px (sm) or 6px (md) circle, `--acc` fill with a glow shadow. Animates with `core-pulse`: scales between 1→0.75 and opacity 1→0.6 over 2.4s ease-in-out infinite.
- **Ring 1 (inner):** 16px/20px circle, `--acc` at 35% opacity
- **Ring 2 (outer):** 26px/32px circle, `--acc` at 18-20% opacity
- **Ring 3 (outermost, md only):** 44px circle, `--acc` at 10% opacity
- **Orbiting dots:** small filled circles that travel along the rings using `rotate(Xdeg) translateX(radius) rotate(-Xdeg)` — the counter-rotation keeps the dot from spinning on its own axis

**Dot animation parameters (md):**
- Dot 1: radius 10px, 1.8s forward, linear infinite
- Dot 2: radius 16px, 3.2s reverse, linear infinite
- Dot 3: radius 22px, 5.0s forward, linear infinite

**Dot animation parameters (sm):**
- Dot 1: radius 8px, 1.8s forward
- Dot 2: radius 13px, 3.2s reverse

The three dots at different radii and speeds create an organic, aperiodic orbital motion. The rings provide static context for the orbiting dots.

### 9.2 Inline tool spinner

Used as the status badge for a running tool row. A tiny 14×14px orbital element built with CSS pseudo-elements only (no extra DOM elements). `::before` draws the ring, `::after` draws a single orbiting dot using the same rotate/translateX/rotate technique. Dot colour is `--degraded` (amber). Animation speed: 1.4s linear infinite.

### 9.3 Streaming cursor

A 2px × 13px rounded rectangle in `--acc`. Uses a smooth ease-in-out opacity animation (`cur-fade`): 0.9→0.1→0.9 over 1.2s infinite. This is softer than a hard blink.

### 9.4 Wave bars

Five vertical bars (3px wide each, `--acc` fill, 60% opacity) that animate as audio-visualiser-style waves. Each bar animates height from 3px to 13px and back, with staggered delays (0s, 0.15s, 0.30s, 0.45s, 0.60s) to create a flowing wave motion. Used at the end of a streaming text block to indicate the agent is still processing.

### 9.5 Live dot (topbar)

A 5px green circle pulsing between opacity 1 and 0.4 over 2s ease-in-out. Indicates the model connection is live.

### 9.6 Status bar step transition (`step-in`)

When the step label text updates, the element gets a brief `step-in` animation: translates from `translateY(3px)` to `translateY(0)` with opacity 0→1 over 0.25s ease. Achieved by removing and re-adding the animation class (force reflow with `offsetHeight` read).

---

## 10. Canvas Panel

The canvas is an optional secondary pane that slides in from the right. It provides a rich view of file contents and terminal output associated with tool calls.

### 10.1 Container

**Width:** 0 (collapsed) → 520px (open)  
**Transition:** `width 0.25s ease, border-color 0.25s ease`  
**Background:** `--deep`  
**Border:** 1px left `--bds` (transparent when closed, `--bds` when open)  
**Overflow:** hidden (clips content during slide animation)

### 10.2 Canvas topbar (36px)

A slim header row containing:
- **Tab bar** (flex: 1, scrollable with hidden scrollbar): each open view gets a tab
- **Close button:** 24×24px × icon, `--subtle`, hover gets `--surf2` bg

**Canvas tabs:**
- Height 36px, monospace 11.5px font, horizontal padding 12px
- Show file extension prefix in `--acc` colour at 70% opacity, then filename
- A `●` indicator in `--degraded` if the file has been modified (diff view)
- Default state: `--subtle` colour
- Hover: `--surf2` bg, `--muted` text
- Active: `--bg` background, `--fg` text, with a 2px `--acc` bottom border line

Multiple tabs can accumulate as the user clicks pop-out buttons on different tool rows. Tabs are ordered by the sequence they were opened.

### 10.3 Canvas view types

Each tab shows one of several view types:

**File read view (source code):**
- Header: file path (subtle) + filename (muted), line count on the right
- Content: syntax-highlighted code in monospace 12.5px, `--deep` background, line-height 1.7
- Line numbers: 36px wide, right-aligned, `--bds` colour, non-selectable

**Diff view (file write):**
- Header: path + filename, `+N -N` diff stat (green/red), copy button
- Content: unified diff with three line types:
  - Context lines: `--subtle` code colour
  - Addition lines: `oklch(30% 0.1 140 / 0.13)` background, green `+` sigil, green code colour
  - Deletion lines: `oklch(25% 0.1 25 / 0.13)` background, red `-` sigil, red code colour
- Hunk headers (`@@ -N,N +N,N @@`): `--surf` background, `--subtle` text, top/bottom borders

**Terminal view (bash output):**
- Header: "terminal" label (subtle), command string + exit code, status indicator
- Content: monospace 12.5px on `oklch(10% 0.004 248)` (very dark) background, padding 12px 14px
- Colour classes: `.t-err` (red), `.t-ok` (green), `.t-dim` (subtle)

### 10.4 Activity log

A fixed-height panel at the bottom of the canvas (max 110px, scrollable). Shows a sequential log of all tool calls in the current session.

- Header: "Agent activity" label (10px uppercase) + live indicator (amber orbital spinner + "live" text) — the live indicator should be hidden once the agent is done
- Each row: timestamp (10px, `--bds`), tool name (`--muted`), argument (flex: 1, ellipsis), result icon (✓ green / ✗ red / spinner amber)
- Row font: monospace 11px

### 10.5 Canvas open/close logic

**Opening:** The canvas opens when the user clicks a pop-out button (⤢) on any tool row, or clicks a file chip in the context strip, or presses the Canvas button in the topbar (if tabs are already open).

**Tab management:** Clicking a pop-out button for a view that isn't already tabbed adds a new tab and switches to it. Clicking one that's already tabbed just switches to it.

**Tool row highlighting:** When a view is active in the canvas, the corresponding tool row in the thread gets `canvas-active` class: accent left border and always-visible pop-out button.

**Closing:** The × button in the canvas topbar closes the panel. All tabs are remembered in memory so reopening the canvas via the topbar restores them.

---

## 11. Input Area

Fixed at the bottom of the chat panel. Background `--surf`, 1px top border `--bds`, padding 10px 16px 12px.

### 11.1 Slash command bar

A scrollable horizontal row of quick-action chips above the input. Each chip is a pill in monospace 11px with `--surf2` background, `--bds` border, `--subtle` text.

Current commands: `/continue`, `/approve`, `/reject`, `/checkpoint`, `/explain`

On hover or active: `--acc-bdr` border, `--acc` text, `--acc-dim` background.

Typing `/` in the input should filter/show the command list (future implementation).

### 11.2 Textarea

A `flex: 1` auto-resizing textarea inside a container div:

- Container: `--surf2` bg, `--bds` border, 8px border-radius, 9px/10px padding
- On focus: `--acc-bdr` border, `0 0 0 2px --acc-dim` box shadow
- Textarea: transparent bg, no border/outline, `--font`, 13.5px, `--fg` colour, `--subtle` placeholder
- Min height: 20px (single line), max height: 110px (then scrolls)
- Auto-grow: recalculate height from `scrollHeight` on each `input` event

**Keyboard behaviour:**
- `Enter`: send message (prevent default)
- `Shift+Enter`: insert newline

### 11.3 Auxiliary buttons

**`@ file` button:** Monospace 11px, `--surf2` transparent bg with `--bds` border, `--subtle` text. Opens a file reference picker (at-mention for files).

**Send button:** 29×29px square, `--acc` background, dark icon. Hover: `--acc-h`. Contains an arrow/send SVG icon.

### 11.4 Input footer

Two lines of helper text at 11px:
- Left: keyboard shortcuts (`↵ send`, `⇧↵ newline`, `/ commands`) with `kbd` elements for key indicators
- Right: current session token count in monospace

---

## 12. Interaction Flows

### Sending a message
1. User types and presses Enter
2. Message bubble appears immediately in the thread (right-aligned)
3. Input clears and resets height
4. Thread scrolls to bottom
5. A new agent turn begins: byline appears, then `#live-area` becomes visible showing the orbital status bar
6. Tool rows appear progressively as the agent works
7. Text streams in character by character
8. On completion: orbital fades out, `#full-content` shows with the final formatted response

### Opening the canvas
1. User hovers a tool row → pop-out button (⤢) fades in
2. User clicks it → canvas slides in (width transition)
3. Canvas tab is created for this view, content is shown
4. Corresponding tool row gets accent left border + always-visible pop-out button
5. Canvas button in topbar gets active state

### Switching canvas tabs
1. User clicks a tab in the canvas topbar
2. Active tab style updates (accent bottom border, lighter bg)
3. Content area switches to the corresponding view
4. The tool row highlight in the thread updates to match the new active tab

### Collapsing the thinking block
1. User clicks the think-toggle row
2. `thinking` element toggles `open` class
3. `think-body` transitions from `display: none` to `display: block`
4. The orbital icon in the toggle is static (not animated) once thinking is complete

---

## 13. Implementation Notes for the Coding Agent

**Component architecture:** The thread content should be driven by a data model (array of turns, each containing typed elements: text, tool_call, approval, thinking). The live loading state is a special rendering mode that takes over the latest agent turn while streaming.

**Streaming:** Wire `#live-area` to the actual AI SDK streaming events. The `runTool` and `streamText` helpers in the mockup (`terra-agent-chat-v2.html`) are async simulation shims — replace them with real SDK callbacks. The pattern to preserve: tool rows appear when `tool_use` start events arrive and transition state when `tool_result` events arrive; text streams character-by-character from `text_delta` events; all happen concurrently.

**Orbital animation:** Implement as a standalone reusable component (`<OrbitalLoader size="sm|md" />`). Use only CSS animations — no JS-driven animation loops. The counter-rotation pattern (`rotate(Xdeg) translateX(r) rotate(-Xdeg)`) is critical: without it, the dot spins on its own axis which looks wrong.

**Canvas panel:** Implement as a controlled component. State lives in the parent: `{ isOpen: boolean, tabs: string[], activeTab: string | null }`. Opening/closing drives the CSS class that triggers the width transition. Content views are all pre-rendered (or lazily mounted) and toggled with `display: flex / none`.

**Tool row pop-out:** Each tool row needs to know its canvas view ID. When a tool call resolves (e.g., `write_file` on `src/auth/login.tsx`), the system should automatically create a canvas view containing the diff and associate it with that tool row. The row's pop-out button then points to that view ID.

**Approval prompt:** This should be dismissible. After the user clicks Approve/Reject, the prompt collapses or gets a resolved state (greyed out with the chosen action shown). It should not send a new chat message — it sends a structured response back to the agent's tool loop.

**Colour system:** Use your existing design token system but map to the OKLCH values defined in section 2. The near-zero chroma on neutrals (0.005) is intentional — it makes the "grey" very slightly cool-toned without being a flat RGB grey. The accent (hue 8, warm pink/rose) provides a distinctive branded feel.

**Scrolling:** The thread should auto-scroll to the bottom when new content is appended during streaming. Preserve scroll position if the user has scrolled up to read earlier messages (don't force them back down). A simple heuristic: only auto-scroll if the user is within ~100px of the bottom before the new content arrives.

**Accessibility:** The thinking block toggle should be a `<button>` or have `role="button"` with `aria-expanded`. Tool rows that are clickable should be keyboard accessible. The canvas panel should trap focus when open and the close button should return focus to the tool row that opened it.
