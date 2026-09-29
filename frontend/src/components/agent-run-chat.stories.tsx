/**
 * A mock of the agent run page (top bar, reasoning, tool calls, reply box) assembled from Terra
 * parts. No shipped component renders it yet; it shows how a run is meant to read.
 */

import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import type { IconName } from '@orbit/terra';
import {
  Badge,
  Button,
  ChatBubble,
  Code,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  Icon,
  IconTile,
  InputSurface,
  Progress,
  Separator,
  Card,
  StatusDot,
  Stack,
  StreamingCursor,
  Text,
  Textarea,
  TypingIndicator,
} from '@orbit/terra';

// ── Types ─────────────────────────────────────────────────────────────────────

interface MockTool {
  id: string;
  name: string;
  args?: Record<string, string>;
  icon: IconName;
  status: 'running' | 'done' | 'error';
  result?: string;
  timing?: string;
}

const TOOL_STATUS = {
  running: { dot: 'pending', color: 'subtle' },
  done: { dot: 'healthy', color: 'status-healthy' },
  error: { dot: 'error', color: 'status-error' },
} as const;

const TOOL_ICON_COLOR = {
  terminal: 'status-healthy',
  'file-text': 'accent',
  database: 'status-degraded',
} as const;

// ── Topbar ────────────────────────────────────────────────────────────────────

function RunTopbar({
  agentName,
  model,
  runId,
  isActive,
}: {
  agentName: string;
  model: string;
  runId: string;
  isActive?: boolean;
}) {
  return (
    <Stack shrink gap="none">
      <Stack
        direction="horizontal"
        align="center"
        gap="cozy"
        paddingInline="comfortable"
        paddingBlock="snug"
      >
        <Button type="button" variant="muted" size="icon-sm" aria-label="Back">
          <Icon name="arrow-left" size="2xs" />
        </Button>

        <Stack direction="horizontal" align="center" gap="snug">
          <Icon name="bot" size="xs" container containerVariant="surface" color="accent" />
          <Text as="span" size="supporting" weight="strong">
            {agentName}
          </Text>
        </Stack>

        <Stack direction="horizontal" align="center" gap="snug">
          <StatusDot
            status={isActive ? 'healthy' : 'idle'}
            size="sm"
            glow={isActive}
            role="img"
            aria-label={isActive ? 'Active' : 'Ended'}
          />
          <Badge variant="dim" size="sm" mono>
            {model}
          </Badge>
          <Badge variant="dim" size="sm" mono>
            {runId}
          </Badge>
        </Stack>

        <Stack grow />

        <Stack direction="horizontal" align="center" gap="snug">
          <Stack minWidth="form-field-sm" maxWidth="form-field-sm">
            <Progress value={21} size="hairline" label="Context used" />
          </Stack>
          <Text as="span" size="caption" color="subtle" mono>
            42k
          </Text>
        </Stack>

        <Button type="button" variant="outline" size="sm">
          <Icon name="panel-left" size="xs" />
          Canvas
        </Button>
      </Stack>
      <Separator span="full" />
    </Stack>
  );
}

// ── Run row ───────────────────────────────────────────────────────────────────

/** One collapsible row of a run: reasoning and tool calls share it so they read as one list. */
function RunRow({
  icon,
  title,
  timing,
  status,
  detail,
  expandLabel,
}: {
  icon: React.ReactNode;
  title: React.ReactNode;
  timing?: string;
  status: MockTool['status'];
  detail?: React.ReactNode;
  expandLabel: string;
}) {
  const [open, setOpen] = useState(false);
  const state = TOOL_STATUS[status];

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <Card subtle>
        <Stack
          direction="horizontal"
          align="center"
          gap="cozy"
          paddingBlock="snug"
          paddingInline="cozy"
        >
          <IconTile size="md">{icon}</IconTile>
          <Stack grow minWidth="none">
            {title}
          </Stack>
          <Stack direction="horizontal" align="center" gap="cozy" shrink>
            {timing && (
              <Text as="span" size="caption" color="subtle" mono>
                {timing}
              </Text>
            )}
            <Stack direction="horizontal" align="center" gap="tight">
              <StatusDot status={state.dot} size="sm" glow={status === 'running'} />
              <Text as="span" size="caption" color={state.color} mono>
                {status}
              </Text>
            </Stack>
            {detail && (
              <CollapsibleTrigger asChild>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={open ? `Collapse ${expandLabel}` : `Expand ${expandLabel}`}
                >
                  <Icon name="chevron-right" size="xs" rotate={open ? 90 : 0} />
                </Button>
              </CollapsibleTrigger>
            )}
          </Stack>
        </Stack>

        {detail && (
          <CollapsibleContent>
            <Separator span="full" />
            <Stack paddingBlock="snug" paddingInline="comfortable">
              {detail}
            </Stack>
          </CollapsibleContent>
        )}
      </Card>
    </Collapsible>
  );
}

// ── Reasoning row ─────────────────────────────────────────────────────────────

function ReasoningRow({
  isActive,
  duration,
  content,
}: {
  isActive?: boolean;
  duration?: string;
  content?: string;
}) {
  if (!isActive && !content) return null;

  return (
    <RunRow
      icon={<Icon name="sparkles" size="xs" color="accent" />}
      title={
        <Text as="span" size="caption" weight="emphasis" truncate>
          Reasoning
        </Text>
      }
      timing={duration}
      status={isActive ? 'running' : 'done'}
      expandLabel="reasoning"
      detail={
        isActive || !content ? undefined : (
          <Text size="supporting" color="muted">
            {content}
          </Text>
        )
      }
    />
  );
}

// ── Tool card ─────────────────────────────────────────────────────────────────

function ToolCard({ tool }: { tool: MockTool }) {
  return (
    <RunRow
      icon={
        <Icon
          name={tool.icon}
          size="xs"
          color={TOOL_ICON_COLOR[tool.icon as keyof typeof TOOL_ICON_COLOR] ?? 'accent'}
        />
      }
      title={
        <Text as="span" size="caption" mono truncate>
          <Text as="span" size="caption" weight="emphasis" mono>
            {tool.name}
          </Text>
          {Object.entries(tool.args ?? {}).map(([k, v], i) => (
            <Text key={k} as="span" size="caption" color="muted" mono>
              {i === 0 ? '  ' : ', '}
              <Text as="span" size="caption" color="subtle" mono>
                {k}=
              </Text>
              &quot;{v}&quot;
            </Text>
          ))}
        </Text>
      }
      timing={tool.timing}
      status={tool.status}
      expandLabel="output"
      detail={
        tool.status !== 'running' && tool.result ? (
          <Text size="caption" color="muted" mono>
            {tool.result}
          </Text>
        ) : undefined
      }
    />
  );
}

// ── Agent turn ────────────────────────────────────────────────────────────────

function AgentTurn({
  agentName,
  model,
  timestamp,
  reasoning,
  reasoningDuration,
  tools,
  children,
  isGenerating,
}: {
  agentName: string;
  model: string;
  timestamp: string;
  reasoning?: string;
  reasoningDuration?: string;
  tools?: MockTool[];
  children?: React.ReactNode;
  isGenerating?: boolean;
}) {
  const hasTools = !!tools?.length;
  const hasContent = !!children;
  const reasoningActive = isGenerating && !hasTools && !hasContent;

  return (
    <Stack gap="cozy">
      <Stack direction="horizontal" align="center" gap="snug">
        <Icon name="bot" size="sm" container containerVariant="surface" color="accent" />
        <Text as="span" size="supporting" weight="emphasis">
          {agentName}
        </Text>
        <Badge variant="dim" size="sm" mono>
          {model}
        </Badge>
        <Text as="span" size="caption" color="subtle" mono>
          · {timestamp}
        </Text>
      </Stack>

      <Stack gap="cozy">
        <Stack gap="tight" align="start">
          <ReasoningRow
            isActive={reasoningActive}
            duration={reasoningDuration}
            content={reasoning}
          />
          {tools?.map((t) => (
            <ToolCard key={t.id} tool={t} />
          ))}
        </Stack>

        {isGenerating && !hasContent && <TypingIndicator />}

        {hasContent && (
          <Stack maxWidth="prose" gap="snug">
            {children}
            {isGenerating && <StreamingCursor />}
          </Stack>
        )}
      </Stack>
    </Stack>
  );
}

// ── Human turn ────────────────────────────────────────────────────────────────

function HumanTurn({ children }: { children: React.ReactNode }) {
  return (
    <ChatBubble direction="end" variant="accent">
      <Text size="supporting">{children}</Text>
    </ChatBubble>
  );
}

// ── Session divider ───────────────────────────────────────────────────────────

function SessionDivider({ time }: { time: string }) {
  return (
    <Stack direction="horizontal" align="center" gap="cozy">
      <Stack grow minWidth="none">
        <Separator span="full" />
      </Stack>
      <Stack shrink>
        <Text as="span" size="caption" color="subtle" mono>
          {time}
        </Text>
      </Stack>
      <Stack grow minWidth="none">
        <Separator span="full" />
      </Stack>
    </Stack>
  );
}

// ── Input area ────────────────────────────────────────────────────────────────

const CONTEXT_TOOLS = [
  { id: 'sandbox', label: 'Sandbox', icon: 'terminal' },
  { id: 'files', label: 'Files', icon: 'file-text' },
  { id: 'memory', label: 'Memory', icon: 'database' },
] as const satisfies readonly { id: string; label: string; icon: IconName }[];

type ContextToolId = (typeof CONTEXT_TOOLS)[number]['id'];

const KEY_HINTS = [
  { keys: '↵', label: 'send' },
  { keys: '⇧↵', label: 'newline' },
  { keys: '/', label: 'commands' },
];

function InputArea({ agentName, disabled }: { agentName: string; disabled?: boolean }) {
  const [value, setValue] = useState('');
  const [active, setActive] = useState<Set<ContextToolId>>(new Set());
  const canSend = !!value.trim() && !disabled;

  function toggle(id: ContextToolId) {
    setActive((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  }

  function handleKey(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && !e.shiftKey && canSend) {
      e.preventDefault();
      setValue('');
    }
  }

  return (
    <Stack shrink paddingBottom="cozy" gap="snug">
      <InputSurface>
        <Stack padding="cozy" gap="cozy">
          <Textarea
            variant="bare"
            rows={2}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={handleKey}
            aria-label="Reply to the agent"
            placeholder={
              disabled ? `${agentName} run has ended` : 'Reply, or press / for commands…'
            }
            disabled={disabled}
          />

          <Stack direction="horizontal" align="center" justify="between" gap="snug">
            <Stack direction="horizontal" align="center" gap="tight">
              {CONTEXT_TOOLS.map((tool) => (
                <Button
                  key={tool.id}
                  type="button"
                  size="sm"
                  shape="pill"
                  variant="outline"
                  disabled={disabled}
                  selected={active.has(tool.id)}
                  aria-pressed={active.has(tool.id)}
                  onClick={() => toggle(tool.id)}
                >
                  <Icon name={tool.icon} size="xs" />
                  {tool.label}
                </Button>
              ))}
            </Stack>

            <Button
              type="button"
              size="icon"
              variant={canSend ? 'default' : 'secondary'}
              disabled={!canSend}
              aria-label="Send"
              onClick={() => setValue('')}
            >
              <Icon name="arrow-up" size="sm" />
            </Button>
          </Stack>
        </Stack>
      </InputSurface>

      <Stack direction="horizontal" align="center" gap="comfortable">
        {KEY_HINTS.map((hint) => (
          <Stack key={hint.label} direction="horizontal" align="center" gap="tight">
            <Badge variant="dim" size="sm" mono>
              {hint.keys}
            </Badge>
            <Text as="span" size="micro" color="subtle" mono>
              {hint.label}
            </Text>
          </Stack>
        ))}
      </Stack>
    </Stack>
  );
}

// ── Mock data ─────────────────────────────────────────────────────────────────

const AGENT = 'Security Reviewer';
const MODEL = 'claude-haiku-4-5';

const REASONING =
  "I should read package.json first to understand the project structure, then grep for common secret patterns. Environment files are typically gitignored so I'll focus on tracked source files — API_KEY, SECRET, and PASSWORD are the highest-signal patterns to check.";

const TOOLS = {
  provision: {
    id: 't1',
    name: 'provision_sandbox',
    icon: 'database' as const,
    status: 'done' as const,
    timing: '1.2s',
    result: 'ready at /workspaces/cdca7326',
  },
  read: {
    id: 't2',
    name: 'read',
    icon: 'file-text' as const,
    args: { file: 'package.json' },
    status: 'done' as const,
    timing: '0.1s',
    result: '42 lines: next.js 15, typescript, @orbit/terra',
  },
  grep: {
    id: 't3',
    name: 'bash',
    icon: 'terminal' as const,
    args: { cmd: 'grep -rn "API_KEY|SECRET|PASSWORD" src/' },
    status: 'done' as const,
    timing: '2.1s',
    result: '0 matches',
  },
  grepRunning: {
    id: 't3',
    name: 'bash',
    icon: 'terminal' as const,
    args: { cmd: 'grep -rn "API_KEY|SECRET|PASSWORD" src/' },
    status: 'running' as const,
  },
};

// ── Layout ────────────────────────────────────────────────────────────────────

function RunLayout({ isLive }: { isLive?: boolean }) {
  return (
    <Stack minHeight="screen" gap="none">
      <RunTopbar agentName={AGENT} model={MODEL} runId="9bb80e2e" isActive={isLive} />

      <Stack grow direction="horizontal" justify="center" paddingInline="comfortable">
        <Stack grow minWidth="none" maxWidth="wide" gap="comfortable" paddingBlock="relaxed">
          <SessionDivider time="22:38" />

          <HumanTurn>Hello</HumanTurn>
          <HumanTurn>https://clearroute.io</HumanTurn>
          <HumanTurn>
            {
              "Can you do a thorough security audit of this entire repository? I want you to check for hardcoded secrets, exposed API keys, insecure dependencies, any SQL injection vectors, XSS vulnerabilities in the frontend, and make sure all auth middleware is correctly applied to every route. Also check that we're not accidentally logging sensitive data anywhere in the request/response cycle."
            }
          </HumanTurn>

          <HumanTurn>
            Hi, can you open a sandbox and tell me what tools you have available
          </HumanTurn>

          <AgentTurn agentName={AGENT} model={MODEL} timestamp="22:38:04" tools={[TOOLS.provision]}>
            <Text size="supporting">
              Sandbox is ready. You have access to <Code>bash</Code>, <Code>edit</Code>,{' '}
              <Code>read</Code>, and <Code>web_search</Code> in <Code>/workspaces/cdca7326</Code>.
            </Text>
          </AgentTurn>

          <HumanTurn>Can you check if there are any exposed secrets in this repo?</HumanTurn>

          {isLive ? (
            <AgentTurn
              agentName={AGENT}
              model={MODEL}
              timestamp="22:38:41"
              reasoning={REASONING}
              reasoningDuration="3s"
              tools={[TOOLS.read, TOOLS.grepRunning]}
              isGenerating
            />
          ) : (
            <AgentTurn
              agentName={AGENT}
              model={MODEL}
              timestamp="22:38:41"
              reasoning={REASONING}
              reasoningDuration="3s"
              tools={[TOOLS.read, TOOLS.grep]}
            >
              <Text size="supporting">
                No secrets found. All credentials are loaded from environment variables. Nothing is
                hardcoded in <Code>src/</Code>.
              </Text>
              <Text size="supporting" color="muted">
                Scanned for <Code>API_KEY</Code>, <Code>SECRET</Code>, <Code>PASSWORD</Code> with 0
                matches.
              </Text>
            </AgentTurn>
          )}
        </Stack>
      </Stack>

      <Stack direction="horizontal" justify="center" paddingInline="comfortable">
        <Stack grow minWidth="none" maxWidth="wide">
          <InputArea agentName={AGENT} disabled={!isLive} />
        </Stack>
      </Stack>
    </Stack>
  );
}

// ── Meta ──────────────────────────────────────────────────────────────────────

/**
 * A mock of the agent run page assembled from Terra parts. The local helpers (RunTopbar,
 * ReasoningRow, ToolCard, turns, InputArea) are compositions of Terra components, not shipped UI.
 */
const meta: Meta = {
  tags: ['autodocs'],
  title: 'App/Organisms/AgentRunChat',
  parameters: {
    layout: 'fullscreen',
  },
};

export default meta;
type Story = StoryObj;

export const Complete: Story = {
  name: 'Complete run',
  render: () => <RunLayout />,
};

export const Live: Story = {
  name: 'Live: streaming',
  render: () => <RunLayout isLive />,
};
