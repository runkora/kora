'use client';

/**
 * @tier 3 — chat-scoped. Starts a run of any agent from one composer, then hands over to that run's
 * page without a reload. The viewer's own runs sit in the left pane.
 */

import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useEffect, useRef, useState, type RefObject } from 'react';
import {
  Button,
  EmptyState,
  Icon,
  SidePanel,
  SidePanelAccessibleTitle,
  Stack,
  Text,
  Textarea,
  TooltipProvider,
} from '@runkora/hades';
import { APP_ROUTES } from '@runkora/lib/routing';
import type { AgentTypeRow as Agent } from '@runkora/app/(dashboard)/agents/_types';
import { fetchStartRun } from '@runkora/app/(dashboard)/agents/_services/agents-api';
import { useSession } from '@runkora/app/(dashboard)/_components/permissions-provider';
import {
  RunLayoutContext,
  useOwnLayout,
} from '@runkora/app/(chat)/agents/[slug]/runs/[runId]/_lib/run-layout';
import { READING_WIDTH } from '@runkora/app/(chat)/agents/[slug]/runs/[runId]/_lib/run-styles';
import {
  BOX,
  FIELD,
  SendButton,
} from '@runkora/app/(chat)/agents/[slug]/runs/[runId]/_components/run-composer';
import { useActiveAgents } from '../_hooks/use-active-agents';
import { useOwnRuns } from '../_hooks/use-own-runs';
import { handOver } from '../_lib/hand-over';
import { selectAgent } from '../_lib/select-agent';
import { AgentPicker } from './agent-picker';
import { HeadingReveal } from './heading-reveal';
import { HistoryPane } from './history-pane';

/** The run page's composer frame with the agent to start in its footer. */
function NewRunComposer({
  agents,
  agent,
  onAgent,
  onStart,
  frameRef,
}: {
  agents: Agent[];
  agent: Agent | null;
  onAgent: (a: Agent) => void;
  /** Starts the run. Resolves to an error to show, or null once it started. */
  onStart: (task: string) => Promise<string | null>;
  frameRef: RefObject<HTMLDivElement | null>;
}) {
  const [text, setText] = useState('');
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const allowed = !!agent?.canRun;
  const ready = allowed && !!text.trim() && !sending;
  // A started run leaves the page, so the composer stays sending until then.
  const send = async () => {
    if (!ready) return;
    setSending(true);
    const failed = await onStart(text.trim()).catch(() => 'Something went wrong.');
    setError(failed);
    if (failed) setSending(false);
  };
  return (
    <Stack gap="snug">
      <Stack ref={frameRef} overflow="hidden" style={BOX}>
        <Stack gap="snug" style={{ padding: '14px 10px 10px 14px' }}>
          <Textarea
            variant="bare"
            autoResize
            rows={3}
            maxRows={14}
            autoFocus
            value={text}
            disabled={!allowed}
            aria-label="Describe the work"
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault();
                void send();
              }
            }}
            placeholder={
              !agent
                ? 'Choose an agent to start'
                : allowed
                  ? `What should ${agent.name} work on?`
                  : `You cannot start runs of ${agent.name}`
            }
            style={FIELD}
          />
          <Stack direction="horizontal" align="center" gap="tight">
            <AgentPicker agents={agents} value={agent} onChange={onAgent} />
            <Stack grow />
            <SendButton ready={ready} onClick={() => void send()} />
          </Stack>
        </Stack>
      </Stack>
      {error && (
        <Text size="caption" color="status-error" role="alert">
          {error}
        </Text>
      )}
    </Stack>
  );
}

/** `agentSlug` selects an agent from the page's address. */
export function ChatPage({
  agentSlug,
  height = '100dvh',
}: {
  agentSlug?: string;
  height?: string;
}) {
  const { user, loaded: sessionLoaded } = useSession();
  const firstName = user?.name.trim().split(/\s+/)[0] || undefined;
  const own = useOwnRuns();
  const agents = useActiveAgents();
  const [picked, setPicked] = useState<Agent | null>(null);
  const [drawer, setDrawer] = useState(false);
  const router = useRouter();
  const frameRef = useRef<HTMLDivElement>(null);

  // The page lays out by its own width, as the run page does, so the pane folds at the same sizes.
  const [layout, rootRef] = useOwnLayout();

  // A start that returns after the viewer left must not pull them to the run.
  const left = useRef(false);
  useEffect(() => {
    left.current = false;
    return () => {
      left.current = true;
    };
  }, []);

  // The heading plays once, so it waits for the viewer's name as well as the lists.
  const loaded = agents !== null && own.loaded && sessionLoaded;
  const agent = picked ?? (loaded ? selectAgent(agents.list, own.runs, agentSlug) : null);

  // The run's page replaces this one in history. The composer's box goes with it, so it moves.
  const start = async (task: string) => {
    if (!agent) return 'Choose an agent to start.';
    const result = await fetchStartRun(agent.id, { task });
    if (result.error || !result.run) return result.error ?? 'The run did not start.';
    if (left.current) return null;
    const from = frameRef.current?.getBoundingClientRect();
    handOver(result.run.id, { own, ...(from ? { enter: { from, layout } } : {}) });
    router.replace(APP_ROUTES.agents.chatRun(agent.slug, result.run.id));
    return null;
  };

  const wide = layout === 'wide';
  const phone = layout === 'phone';
  return (
    <RunLayoutContext.Provider value={layout}>
      <TooltipProvider>
        <Stack
          ref={rootRef}
          direction="horizontal"
          overflow="hidden"
          position="relative"
          style={{ height, background: 'var(--color-background)' }}
        >
          {wide && <HistoryPane own={own} />}
          <Stack grow minWidth="none" gap="none" overflow="hidden" style={{ height }}>
            {!wide && (
              <Stack
                direction="horizontal"
                style={{ padding: phone ? '14px 16px 0' : '22px 32px 0' }}
              >
                <Button
                  size="icon-sm"
                  variant="ghost"
                  aria-label="Your runs"
                  onClick={() => setDrawer(true)}
                >
                  <Icon name="panel-left" size="2xs" label="" />
                </Button>
              </Stack>
            )}
            {/* The heading and composer sit a little above the middle, where the eye lands on an empty page. */}
            <Stack grow justify="center" style={{ padding: phone ? '0 12px 10vh' : '0 32px 12vh' }}>
              <Stack
                gap="relaxed"
                style={{ maxWidth: READING_WIDTH, width: '100%', margin: '0 auto' }}
              >
                {loaded && agents.list.length === 0 ? (
                  <EmptyState
                    icon="bot"
                    title="No agents to start"
                    description={
                      agents.error ??
                      'No agent is set up for you yet. Create one, or ask an admin for access to one.'
                    }
                    action={
                      <Button variant="outline" size="sm" asChild>
                        <Link href={APP_ROUTES.agents.list}>Go to agents</Link>
                      </Button>
                    }
                  />
                ) : (
                  loaded && (
                    <>
                      <HeadingReveal name={firstName} />
                      <NewRunComposer
                        agents={agents.list}
                        agent={agent}
                        onAgent={setPicked}
                        onStart={start}
                        frameRef={frameRef}
                      />
                    </>
                  )
                )}
              </Stack>
            </Stack>
          </Stack>
          <SidePanel
            side="left"
            width={320}
            open={drawer && !wide}
            onClose={() => setDrawer(false)}
          >
            <SidePanelAccessibleTitle>Your runs</SidePanelAccessibleTitle>
            <HistoryPane own={own} drawer />
          </SidePanel>
        </Stack>
      </TooltipProvider>
    </RunLayoutContext.Provider>
  );
}
