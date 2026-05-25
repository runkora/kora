# Agents UI — Design & Implementation Notes
> Scratch file. Will be formalized into PRD + ADR.

---

## 1. Full Page Inventory

### 1.1 Route Structure

```
/agents                                  → Agent Types list (overview + management)
/agents/new                              → Create Agent Type form
/agents/{slug}                           → Agent Type detail (tabs: Overview | Runs | Settings)
/agents/{slug}/runs/new                  → Start a new run (could be modal on /agents/{slug})
/agents/{slug}/runs/{runId}              → Run chat UI (full viewport, no dashboard chrome)
```

Route registration (`lib/routing/routes.ts`):
```typescript
agents: {
  list:   '/agents',
  new:    '/agents/new',
  detail: (slug: string) => `/agents/${slug}`,
  runs:   (slug: string) => `/agents/${slug}/runs`,
  run:    (slug: string, runId: string) => `/agents/${slug}/runs/${runId}`,
}
```

---

## 2. Screen-by-Screen Design

### 2.1 Agents List — `/agents`

**Purpose:** Central hub. Shows all agent types for the tenant. Entry point for creating new agents or starting runs.

**Layout:** Standard dashboard chrome (sidebar + header). `main className="px-6 py-8"`.

**Above the fold:**
- Page heading: "Agents" (Heading variant="title")
- Subheading: "Manage agent definitions and monitor active runs." (Text color="muted")
- Right side of heading row: "New Agent" Button (primary, icon="plus") — gated on `agents:types:write` scope via ScopeGuard

**Metric cards row (Grid cols=3, gap="comfortable"):**
- Active agent types (count with state="active")
- Total active runs (sum of runs in pending/starting/active/completing states)
- Total runs today (count)
These use the existing `MetricCard` Tier 2 component.

**Agent types table (DataTable):**

Columns:
| Column | Content | Notes |
|--------|---------|-------|
| Name | `name` (Text weight="heavy") + `slug` below (Text mono size="supporting" color="muted") | |
| Framework | Badge variant based on framework value ("claude-code" \| "codex" \| "opencode") | |
| State | StatusBadge mapped: active→healthy, provisioning→degraded, deleting→error | |
| Active runs | Count of runs in non-terminal states | Numeric, right-aligned |
| Created | Relative timestamp (e.g. "3 days ago") | |
| Actions | Icon buttons: Start Run (play icon), Settings (gear icon), Delete (trash icon, danger) | Delete gated on scope |

Row click → navigates to `/agents/{slug}`

**Empty state:** EmptyState component with bot icon, "No agents yet", "Create your first agent to get started." + "New Agent" CTA button.

**Scope gating:**
- Page visible with `agents:types:read`
- "New Agent" button + Delete action visible with `agents:types:write`
- ScopeGuard wraps the whole page with `agents:types:read`

---

### 2.2 Create Agent Type — `/agents/new`

**Purpose:** Form for creating a new agent type. Only accessible with `agents:types:write`.

**Layout:** Dashboard chrome. Narrower content column (max-w ~700px centred or left-aligned with standard padding).

**Breadcrumb:** Agents → New Agent

**Form sections:**

**Section 1 — Identity**
- Name (Input, required, placeholder "e.g. Code Review Agent") → auto-derives slug
- Slug (Input, monospace font, readonly-ish with manual override toggle, shows derived value, validated unique-per-tenant on blur)
- Description (Textarea, optional, placeholder "What does this agent do?", ~3 rows)

**Section 2 — Behaviour**
- Framework (NativeSelect or radio group): 
  - claude-code (default, Anthropic's CLI coding agent)
  - codex (OpenAI Codex CLI)
  - opencode (open-source)
  - Show brief description per option
- System Prompt (Textarea, monospace, 8 rows, placeholder "You are a coding assistant…")

**Section 3 — Permissions**
- Tool Scopes (ScopePicker component — already exists in Tier 2): 
  - Shows available scopes: shell, filesystem, network, etc.
  - Multi-select with grouped accordion view
  - Selected scopes shown as ScopeChip pills
- Grant Scopes (same ScopePicker pattern):
  - OPA-enforced scopes the agent is granted (e.g. inference:invoke)

**Section 4 — Capacity**
- Max concurrent runs per pod (Input type=number, default 10, min 1, max 50)
- Helper text: "Maximum simultaneous runs assigned to a single pod replica."

**Actions:**
- "Create Agent" button (primary, triggers POST /api/v1/agent-types)
- "Cancel" link → back to /agents

**Post-submit behaviour:**
- On success → redirect to `/agents/{slug}` (the new agent type detail page)
- Agent type starts in `provisioning` state — the detail page shows a provisioning banner

**Validation:**
- Name: required, non-empty
- Slug: required, lowercase alphanumeric + hyphens, unique (validated async)
- Description: optional
- System prompt: required
- At least one tool scope recommended (Callout if empty, not a hard block)

---

### 2.3 Agent Type Detail — `/agents/{slug}`

**Purpose:** Management view for a specific agent type. See state, manage settings, view and start runs.

**Layout:** Dashboard chrome. Full width.

**Header row:**
- Agent name (Heading variant="title") + framework badge
- State indicator: StatusBadge (active/provisioning/deleting) with pulsing dot for provisioning
- Slug in monospace (Text mono color="muted")
- Actions: "Start Run" button (primary), "Delete Agent" button (danger, ghost, scoped to write)

**Provisioning banner (conditional):**
- Callout variant="info" shown when state="provisioning"
- "Agent is being provisioned. This usually takes 30–60 seconds."
- Auto-dismisses / updates when agent transitions to active (poll or SSE)

**Start Run modal (triggered by "Start Run" button):**
- Task input (Textarea, required, placeholder "Describe the task for this agent…", ~4 rows)
- Provision sandbox upfront (Toggle/Checkbox): "Pre-provision sandbox" with helper text "Reserve a sandbox pod before the agent starts. Reduces latency on first tool use."
- Confirm button: "Start Run" (primary) → POST /api/v1/runs with task + sandbox flag, then navigate to `/agents/{slug}/runs/{runId}`
- Cancel button (secondary)

**Tabs (TabsList / TabsTrigger / TabsContent):**

#### Tab 1: Overview
- Description (Text)
- System prompt preview (Card with monospace content, truncated with "Show full" toggle)
- Tool scopes: row of ScopeChip components
- Grant scopes: row of ScopeChip components
- Capacity: "Max {N} concurrent runs per pod"
- Created / Updated timestamps

#### Tab 2: Runs
- "Start New Run" button (right-aligned)
- Runs table (DataTable, scoped to `agents:runs:read`)

Runs table columns:
| Column | Content |
|--------|---------|
| Run ID | Short ID, monospace (Text mono) |
| State | StatusBadge: pending→idle, starting→degraded, active→healthy, completing→degraded, completed→muted, failed→error |
| Started | Relative timestamp |
| Duration | Computed from started_at → completed_at (or "ongoing") |
| Created by | Principal ID (could be truncated / display name) |
| Actions | "Open" (eye icon) → navigates to run chat; "Terminate" (stop icon, danger) for active runs |

Row click → navigate to `/agents/{slug}/runs/{runId}`

Empty state: "No runs yet. Start your first run to begin."

#### Tab 3: Settings
- Edit form (same fields as create, pre-populated)
- Save Changes button
- Danger Zone section (Card with red border):
  - "Delete Agent Type"
  - Warning text: "Deleting an agent type will terminate all active runs and permanently remove the definition."
  - ConfirmDialog triggered by Delete button (existing Tier 2 component)

---

### 2.4 Run Chat UI — `/agents/{slug}/runs/{runId}`

**Purpose:** Live interaction interface for a specific run. Full viewport — exits the standard dashboard chrome entirely (separate layout file with no sidebar/header).

**This is the interface designed in `terra-agent-chat-v2.html` and specced in `agent-chat-ui-spec.md`.**

See section 4 below for full component breakdown and section 5 for backend event wiring.

---

## 3. Navigation Updates

Update `nav-entries.ts`:
- Remove `wip: true` from Agents entry
- Remove `badge: 12` (was mock data)
- Keep `icon: 'bot'`
- No `requiredScope` on the nav entry itself (scoped internally per page)

Add sub-navigation within the agents section? Model-gateway pattern uses flat routes with internal tabs. For agents, the natural breakdown is:
- Sidebar item "Agents" → `/agents` (list)
- No sidebar sub-items needed — navigation within agents happens via table row clicks and breadcrumbs

---

## 4. Run Chat UI — Component Breakdown

The chat page at `/agents/{slug}/runs/{runId}` is a full-viewport layout with **no dashboard chrome**. This requires a separate layout file: `src/app/(dashboard)/agents/[slug]/runs/[runId]/layout.tsx` that overrides the parent layout (or we move it outside the dashboard route group entirely — TBD with the routing decision).

### 4.1 Layout Structure

```
┌─────────────────────────────────────────────────────────────┐
│  RunTopbar (46px)                                            │
├─────────────────────────────────────────────────────────────┤
│  ContextStrip (32px)                                         │
├──────────────────────────────────┬──────────────────────────┤
│                                  │                           │
│  ChatThread (flex: 1, scroll)    │  CanvasPanel              │
│                                  │  (0 → 520px slide)        │
├──────────────────────────────────┤                           │
│  InputArea (fixed bottom)        │                           │
└──────────────────────────────────┴──────────────────────────┘
```

### 4.2 RunTopbar

Elements left-to-right:
- Orbit logomark (20px square, acc-dim fill + acc-bdr border, orbital SVG icon)
- "Orbit AI" label (13px, font-weight 600)
- Separator `/`
- **Model pill**: live-dot (5px green circle, pulsing animation) + model name in monospace (e.g. `claude-sonnet-4-6`)
- Separator `/` (subtle)
- **Session pill**: session/run ID in monospace, subtle colour
- Spacer (flex: 1)
- **Context meter**: "ctx" label + 72px progress bar (3px height, surf3 track, acc fill) + "42k / 200k" token count
- **Session log icon button** (27×27px)
- **Canvas toggle button**: "Canvas" label + split-pane icon, three states (default/hover/active)

Back navigation: breadcrumb or back button ("← {agentName}" or "← Agents") — needs design decision. Probably a small back button at the very left before the logo.

Data sources:
- Model name: from `AgentType.framework` (maps to model string)
- Run ID: from URL param
- Token count: from SSE events or session state (starts unknown, updates as events flow)

### 4.3 ContextStrip

File chips representing files currently in the agent's context/workspace. Initially empty. Populated by watching `tool_use` events where tool name is `read_file` or `write_file` — accumulate unique file paths and display as chips.

Clicking a chip → opens that file in the canvas panel (if a canvas view exists for it).

`+ add file` chip → future implementation, out of scope v1.

### 4.4 ChatThread

Scrollable flex container. Auto-scrolls to bottom during streaming (with smart "don't scroll if user has scrolled up" logic — threshold ~100px from bottom).

**Session divider:** Full-width date/time label with extending line. "Session started · {date} · {time}"

**Human turn:**
- Right-aligned bubble row
- Bubble: max-width 68%, acc-dim bg, acc-bdr border, border-radius 10 10 3 10, padding 9px 14px
- No avatar, no label

**Agent turn:**
- Byline row: 18px orbital icon square + "Orbit AI" label (12px, muted) + timestamp (11px, subtle, mono)
- Content area (26px left indent from icon):
  - Thinking block (collapsible)
  - Agent text paragraphs
  - Tool rows
  - Approval prompt
  - Interleaved in document order as produced

### 4.5 Agent Content Elements

**Thinking block:**
- 2px solid bds left border, 12px left padding
- Toggle row: OrbitalLoader (sm, 70% opacity) + "Thought for N seconds" (11.5px, subtle) + token count (mono, 10.5px, 35% opacity)
- Body: 12px italic, subtle, collapsed by default
- aria-expanded, keyboard accessible

**Agent text:**
- `<Text>` paragraphs (Terra), color="fg", line-height 1.65
- Adjacent paragraphs: 6px margin-top
- Inline code: `<Code>` (Terra), acc colour, acc-dim bg, bds border, 3px radius

**Tool row:**
- 2px left border, border-color driven by status: running=amber, done=green, error=red, canvas-active=acc
- Contents: emoji icon (12px) + tool name (mono 12px, muted) + truncated arg (mono 11.5px, subtle, flex:1 ellipsis) + status badge + pop-out button (⤢)
- Status badge:
  - running: InlineOrbitalSpinner (CSS-only, 14px) + "running" text (degraded colour)
  - done: "✓ {result}" (healthy colour)
  - error: "✗ {detail}" (error colour)
- Pop-out button: 22×22px, opacity 0 normally, 1 on row hover, always 1 when canvas-active. Transitions border-color on status change (0.15s)
- Tool enter animation: slides up 3px with fade on appearance (0.2s)

**Approval prompt:**
- acc-dim bg, acc-bdr border, 8px radius
- Header: acc diamond icon (◈) + plain-English description
- Body: longer explanation + exact command in monospace (Code component)
- Actions row: Approve (primary Button), Edit (secondary Button), Reject (danger Button)
- After action: collapses to resolved state (greyed, chosen action shown). Does NOT send chat message — sends structured tool result.

### 4.6 Live Loading State

Two mutually exclusive regions in each agent turn:
- **live-area**: shown while agent is working
- **full-content**: shown once agent is done

**Orbital status bar (top of live-area):**
- OrbitalLoader (md, 44px) on left
- Text block right: "Working…" (11.5px, muted, weight 500) + step label (11px, subtle, mono)
- Step label animates on change: step-in keyframe (translateY 3px → 0, opacity 0→1, 0.25s)
- Done: opacity fades to 0 (0.3s), then height collapses (delayed 0.25s). No layout jump.

**Progressive tool rows:** appear below status bar as tools fire. Run → done/error transitions in-place.

**Streaming text:** streams char-by-char into live text block. Streaming cursor (2px × 13px, acc, smooth fade not hard blink) at end of text. Text and tool rows coexist simultaneously.

**Transition to full content:**
1. Orbital status bar fades + collapses
2. 650ms pause
3. live-area hidden, full-content visible (final formatted version with all pop-out buttons, complete thinking block, etc.)

### 4.7 Canvas Panel

Slide-out from right, width 0 → 520px, transition 0.25s ease. Background: deep. 1px left border (transparent when closed).

**Canvas topbar (36px):**
- Tab bar (flex:1, scrollable, hidden scrollbar)
  - Each tab: ext prefix (acc, 70% opacity) + filename + ● modifier dot (degraded) if diff view
  - Tab states: default (subtle), hover (surf2 bg, muted text), active (bg bg, fg text, 2px acc bottom border)
- Close × button (24×24px, subtle)

**View types:**

*File read:*
- Header: path (subtle) / filename (muted) + line count
- Syntax-highlighted code, mono 12.5px, deep bg, line-height 1.7
- Line numbers: 36px wide, right-aligned, bds colour, user-select none

*Diff view (write):*
- Header: path/filename + +N/-N stat (green/red) + copy button
- Addition lines: oklch(30% 0.1 140 / 0.13) bg, green +
- Deletion lines: oklch(25% 0.1 25 / 0.13) bg, red -
- Context lines: subtle colour
- Hunk headers: surf bg with borders

*Terminal view (bash):*
- Header: "terminal" label + command + exit code status
- Content: mono 12.5px on oklch(10% 0.004 248) bg, padding 12/14px
- Colour classes: err=red, ok=green, dim=subtle

**Activity log (bottom, max 110px):**
- Header: "Agent activity" (10px uppercase) + live indicator (InlineOrbitalSpinner + "live") — hidden when run complete
- Rows: timestamp (10px, bds) + tool name (muted) + arg (flex:1, ellipsis) + result icon (✓/✗/spinner)
- Mono 11px font

**Canvas state management:**
- `{ isOpen: boolean, tabs: CanvasTab[], activeTabId: string | null }`
- Lives in parent context (RunContext)
- Opening/closing drives CSS class → width transition
- All view content pre-rendered, toggled via display

**Tool row ↔ canvas sync:**
- Each tool_use event creates a canvas view when it resolves
- Tool row pop-out button points to that view ID
- When canvas active tab changes, tool row canvas-active class updates

### 4.8 InputArea

Fixed bottom of chat panel. surf bg, 1px top bds border.

**Slash command bar:**
- Horizontal scrollable row of pill chips
- Commands: /continue, /approve, /reject, /checkpoint, /explain
- Active state: acc-bdr border, acc text, acc-dim bg
- Typing / in textarea filters/shows command list (v1 can be static chips)

**Textarea container:**
- surf2 bg, bds border, 8px radius, 9/10px padding
- Focus: acc-bdr border, acc-dim box-shadow ring
- Auto-grows (min 20px, max 110px) from scrollHeight

**Buttons:**
- `@ file` chip button (mono 11px, bds border, subtle text)
- Send button (29×29px, acc bg, dark icon, arrow SVG)

**Input footer:**
- Left: kbd hints (↵ send, ⇧↵ newline, / commands)
- Right: token count (mono, 10.5px)

---

## 5. Backend SSE Event Wiring

The run chat page opens an SSE connection to:
```
GET /api/v1/runs/{runId}/events
Authorization: Bearer {token}
Last-Event-ID: {sequenceNo}  ← for reconnect/replay
```

Event → UI mapping:

| Backend Event | Payload | UI Action |
|---|---|---|
| `run_state` | `{state}` | Update run state indicator in topbar. If `failed`/`completed`, disable input, show terminal state |
| `text_delta` | `{delta: string}` | Append delta to live text block, ensure streaming cursor visible |
| `thinking_delta` | `{delta: string}` | Append delta to thinking body (live-area thinking section) |
| `message_stop` | — | Hide streaming cursor, show wave bars briefly, begin transition to full-content |
| `tool_use` | `{id, name, input}` | Add tool row in `run` state (amber), add to activity log, update context strip if file op |
| `tool_result` | `{tool_use_id, content, is_error}` | Transition corresponding tool row to done/err, create canvas view, update activity log |
| `message_complete` | `{text, thinking}` | Final full text for full-content swap (used to hydrate the post-stream final view) |

**SSE client pattern:**
- Use `EventSource` API or `fetch` with `ReadableStream` (latter allows auth headers)
- Since we need an auth header, use `fetch` + stream reader (EventSource doesn't support headers)
- BFF route handler proxies the SSE connection, injecting auth from session
- Client uses a custom `useRunEvents` hook that manages the stream lifecycle

**Sending messages:**
```
POST /api/v1/runs/{runId}/messages
{ content: string }
```
Via BFF route. After send, the SSE stream delivers the response events.

---

## 6. New Terra Components Required (need approval)

### 6.1 OrbitalLoader

**Classification:** Tier 1 (zero Orbit domain knowledge — purely a branded animation)  
**Sizes:** `sm` (32×32px) and `md` (44×44px)  
**Props:**
```typescript
interface OrbitalLoaderProps {
  size: 'sm' | 'md'
  className?: string
}
```

**Implementation:** CSS-only animations. Counter-rotation pattern: `rotate(Xdeg) translateX(r) rotate(-Xdeg)` for dots. No JS animation loops. See spec section 9.1 for full animation parameters.

**Sub-variant:** `InlineOrbitalSpinner` — 14×14px CSS-only version via pseudo-elements. Used in tool row status badges and activity log. Could be a separate component or a `size="inline"` variant.

### 6.2 ToolRow (tentative)

**Classification:** Possibly Tier 2 (Orbit-domain-aware: knows about tool names and run states) or Tier 3 (run-scoped)  
**Decision:** Start as Tier 3 in `_components/`, promote if reused outside this feature.

### 6.3 Existing Terra components to use

- `ChatBubble` → human message bubble (may need style variant for the specific border-radius 10 10 3 10)
- `StreamingCursor` → the 2px blinking cursor (exists, verify exact animation matches spec)
- `TypingIndicator` → wave bars (exists, verify it matches 5-bar wave pattern)
- `InputSurface` → textarea wrapper
- `SidePanel` → canvas panel (verify it supports the 520px width and slide animation — may need to confirm this matches the exact spec)
- `Tabs` → canvas tab bar
- `Modal` → for run start confirmation, delete confirmation

---

## 7. Design Token Gaps

The chat UI spec introduces surface depth and accent variants beyond what Terra currently defines:

| Token | Value | Purpose |
|---|---|---|
| `--deep` | oklch(13% 0.005 248) | Canvas bg, terminal bg, code blocks |
| `--surf` | oklch(22% 0.005 248) | Topbar, input area, panel headers |
| `--surf2` | oklch(27% 0.005 248) | Hover states, chips, secondary inputs |
| `--surf3` | oklch(32% 0.005 248) | Scrollbar thumbs, progress track |
| `--acc-dim` | oklch(72% 0.17 8 / 0.10) | Accent bg fill |
| `--acc-bdr` | oklch(72% 0.17 8 / 0.30) | Accent border |
| `--healthy` | #9ce37d | Done/success |
| `--degraded` | #ffd046 | Running/in-progress |

**Decision needed:** Map to existing Terra tokens where semantically close, or introduce as chat-scoped CSS custom properties on the run page layout. Given the chat page has its own full-viewport layout, scoping to that layout is acceptable for v1.

---

## 8. Permission / Scope Gating Summary

| Page / Action | Required Scope |
|---|---|
| View agent types list | `agents:types:read` |
| Create agent type | `agents:types:write` |
| Delete agent type | `agents:types:write` |
| View runs | `agents:runs:read` |
| Start run | `agents:runs:write` |
| Send message to run | `agents:runs:write` |
| Terminate run | `agents:runs:write` |
| View specific agent type | `agents:runs:{slug}:read` (or `agents:types:read`) |

ScopeGuard wraps each page/action. Nav entry for Agents shown to all authenticated users (read scope gates the content).

---

## 9. State Management Approach

### Pages 1–3 (list, create, detail): Standard pattern
- RSC page fetches initial data via server service
- Shell component → Provider → hook → context
- Hook exposes: data, status, error, refresh, create, delete actions

### Run chat page: Stream-first pattern (different from standard pages)
- RSC page fetches initial run + agent type metadata only
- Client shell opens SSE connection via `useRunEvents` hook
- SSE events drive a reducer-style state machine:
  - `turns`: array of agent turns, each with `elements` (text, tool, thinking, approval)
  - `liveState`: current streaming state (null when idle, populated during response)
  - `canvasState`: { isOpen, tabs, activeTabId }
  - `runState`: current backend run state
- Context distributes all of the above to:
  - `ChatThread` (renders turns)
  - `RunTopbar` (model name, run state, context meter)
  - `ContextStrip` (accumulated file chips)
  - `CanvasPanel` (canvas state)
  - `InputArea` (send message, slash commands, enabled/disabled based on run state)

### Run reducer events:
```
STREAM_START        → init new agent turn with liveState
TEXT_DELTA          → append to liveState.text
THINKING_DELTA      → append to liveState.thinking
TOOL_USE            → add tool to liveState.tools (status: running)
TOOL_RESULT         → update tool status in liveState.tools, create canvas view
MESSAGE_STOP        → begin full-content swap sequence
MESSAGE_COMPLETE    → hydrate final turn content, clear liveState
RUN_STATE_CHANGE    → update runState
```

---

## 10. BFF Routes Needed

```typescript
// Add to ApiRoute in lib/routing/api-routes.ts:
agentTypes: {
  GET:    ()         => '/api/agents/types',
  POST:   ()         => '/api/agents/types',
  detail: (id)       => `/api/agents/types/${id}`,
  DELETE: (id)       => `/api/agents/types/${id}`,
},
runs: {
  GET:     (typeId)  => `/api/agents/types/${typeId}/runs`,
  POST:    (typeId)  => `/api/agents/types/${typeId}/runs`,
  detail:  (runId)   => `/api/agents/runs/${runId}`,
  DELETE:  (runId)   => `/api/agents/runs/${runId}`,
  message: (runId)   => `/api/agents/runs/${runId}/messages`,
  events:  (runId)   => `/api/agents/runs/${runId}/events`,  // SSE proxy
},
```

The SSE proxy BFF route needs special handling: it streams the response from the backend SSE endpoint directly to the client, injecting auth. Next.js route handlers support `ReadableStream` responses.

---

## 11. Open Questions / Decisions

1. **Chat page layout**: ⏳ Undecided — will revisit when we get to the run chat implementation. Options remain: full opt-out of dashboard chrome vs. a dashboard-chrome-aware full-width layout.

2. **Start run flow**: ✅ Modal on the agent type detail page. Modal includes:
   - **Task input** (Textarea): the initial message/task to send to the agent at run start (becomes the first user message)
   - **Provision sandbox upfront** (Checkbox/Toggle): whether to pre-provision the sandbox pod before the agent starts (vs. on-demand when first needed). Helps avoid first-tool latency for runs that will definitely need a sandbox.
   - Confirm / Cancel actions. On confirm → POST /api/v1/runs then navigate to the run chat page.

3. **OrbitalLoader**: ✅ Approved — add to Terra as part of this implementation.

4. **Polling for provisioning state**: ✅ Client-side polling (every 2s) on the agent type detail page until state transitions from `provisioning` to `active`.

5. **Run list views**: ✅ 
   - `/agents` (aggregate): all agent types + a recent runs section showing runs across all types for the tenant
   - `/agents/{slug}` Runs tab: runs scoped to that agent type only

6. **Canvas panel responsiveness**: ✅ The chat UI must be fully responsive. The canvas panel 520px example was illustrative only. Responsive approach:
   - Canvas panel: collapses completely below a breakpoint (e.g. md), accessible via a dedicated button/sheet instead
   - Chat thread: full width on mobile, shared layout on desktop
   - Input area: stacks vertically on mobile (slash row wraps, footer hint hidden)
   - Context strip: horizontally scrollable on small screens
   - Consider `Show` component from Terra for responsive visibility toggling

---

## 12. Implementation Order (proposed)

1. **Types + API types** (`_types/index.ts`, `src/shared/api.ts` additions)
2. **BFF routes** (server services + route handlers for agent types and runs)
3. **Agents list page** (`/agents`) — agent types table + metrics
4. **Create agent type form** (`/agents/new`)
5. **Agent type detail page** (`/agents/{slug}`) — with tabs
6. **OrbitalLoader Terra component** (pending approval)
7. **Run chat page shell** (`/agents/{slug}/runs/{runId}`) — layout, topbar, context strip
8. **Chat thread + human turns**
9. **Agent turn + tool rows** (static/completed state first)
10. **SSE hook + live loading state** (streaming, progressive tool rows)
11. **Canvas panel** (slide-out, tabs, view types, activity log)
12. **Input area + message sending**
13. **Approval prompt**
