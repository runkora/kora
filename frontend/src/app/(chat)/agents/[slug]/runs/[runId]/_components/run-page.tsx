'use client';

/**
 * @tier 3 — run-page-scoped. The run page as a three-pane workspace: sessions on the left, the run
 * read as a document in the middle, and the run's facts in a rail on the right. Panes fold into a
 * drawer and a sheet as the page narrows.
 */

import Link from 'next/link';
import { useLayoutEffect, useMemo, useRef, useState } from 'react';
import {
  BottomSheet,
  Button,
  EmptyState,
  SidePanel,
  SidePanelAccessibleTitle,
  SidePanelHeader,
  Stack,
  TooltipProvider,
} from '@runkora/hades';
import { APP_ROUTES } from '@runkora/lib/routing';
import { useHasScope } from '@runkora/app/(dashboard)/_components/permissions-provider';
import { useRunContext } from '../_context/run-context';
import { useNow } from '../_hooks/use-now';
import { layoutFor, RunLayoutContext, type RunLayout } from '../_lib/run-layout';
import { earlierRunsOf, toRunView } from '../_lib/to-run-view';
import { elapsedMs, ENDED, rowsOf } from '../_lib/run-view';
import { LINE, READING_WIDTH } from '../_lib/run-styles';
import { ConnectionBanner } from './connection-banner';
import { RunRail } from './run-rail';
import { RunHeader } from './run-header';
import { RowView, rowKey } from './run-thread';
import { Composer } from './run-composer';
import { EndLine } from './run-end-line';
import { SessionsPane } from './sessions-pane';

/** The run page, fed by the run context. `height` is the page's; a framed preview passes its device's. */
export function RunPage({ height = '100dvh' }: { height?: string }) {
  const ctx = useRunContext();
  const live = !ENDED.has(ctx.runState ?? 'pending');
  const now = useNow(live);
  const canRaiseBudget = useHasScope('platform:budgets:write');
  // The view and its rows rebuild when the run changes, not on the clock's tick.
  const base = useMemo(() => toRunView(ctx), [ctx]);
  const rows = useMemo(() => rowsOf(base.entries), [base.entries]);
  const run = useMemo(() => ({ ...base, durationMs: elapsedMs(base, now) }), [base, now]);
  const earlier = useMemo(() => earlierRunsOf(ctx.earlierRuns), [ctx.earlierRuns]);

  // Layout follows the page's own width, so a framed preview lays out like its device.
  const [layout, setLayout] = useState<RunLayout>('wide');
  const rootRef = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = rootRef.current;
    if (!el) return;
    const observer = new ResizeObserver(() => setLayout(layoutFor(el.offsetWidth)));
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  const [sheet, setSheet] = useState<'sessions' | 'details' | null>(null);
  const sessionsPane = layout === 'wide';
  const railPane = layout === 'wide' || layout === 'laptop';

  // The spacer under the thread tracks the floating composer, so the last entry stops just above it.
  const [composerHeight, setComposerHeight] = useState(0);
  const composerRef = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = composerRef.current;
    if (!el) return;
    const observer = new ResizeObserver(() => setComposerHeight(el.offsetHeight));
    observer.observe(el);
    return () => observer.disconnect();
  }, [live]);

  // The thread follows new content while the reader is at its foot, and stays put once they scroll up.
  const threadRef = useRef<HTMLDivElement>(null);
  const followRef = useRef(true);
  useLayoutEffect(() => {
    const el = threadRef.current;
    if (!el) return;
    const onScroll = () => {
      followRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    };
    el.addEventListener('scroll', onScroll, { passive: true });
    return () => el.removeEventListener('scroll', onScroll);
  }, []);
  const last = base.entries.at(-1);
  const tail = `${base.entries.length}:${last?.type === 'text' ? last.text.length : ''}`;
  useLayoutEffect(() => {
    const el = threadRef.current;
    if (el && followRef.current) el.scrollTop = el.scrollHeight;
  }, [tail, composerHeight]);

  if (ctx.loadError) {
    return (
      <Stack grow align="center" justify="center" padding="relaxed" style={{ height }}>
        <EmptyState
          icon="lock"
          title="Run unavailable"
          description={ctx.loadError}
          action={
            <Button variant="outline" size="sm" asChild>
              <Link href={APP_ROUTES.agents.list}>Back to agents</Link>
            </Button>
          }
        />
      </Stack>
    );
  }

  const sessions = (drawer?: boolean) => (
    <SessionsPane
      run={run}
      rows={rows}
      earlier={earlier}
      earlierError={ctx.earlierRunsError}
      drawer={drawer}
    />
  );

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
          {sessionsPane && sessions()}
          <Stack grow minWidth="none" gap="none" overflow="hidden" style={{ height }}>
            <Stack style={{ borderBottom: LINE }}>
              <RunHeader
                run={run}
                canRun={!!ctx.agentType?.can_run}
                onSessions={sessionsPane ? undefined : () => setSheet('sessions')}
                onDetails={railPane ? undefined : () => setSheet('details')}
              />
            </Stack>
            <ConnectionBanner />
            {/* The thread scrolls the full height; a live composer floats over its foot. */}
            <Stack gap="none" position="relative" style={{ flex: '1 1 0', minHeight: 0 }}>
              <Stack
                ref={threadRef}
                overflowY="auto"
                style={{
                  flex: '1 1 0',
                  minHeight: 0,
                  scrollbarWidth: 'none',
                  padding: layout === 'phone' ? '20px 16px 0' : '28px 32px 0',
                }}
              >
                <Stack
                  gap="relaxed"
                  style={{ maxWidth: READING_WIDTH, width: '100%', margin: '0 auto' }}
                >
                  {rows.map((row) => (
                    <RowView key={rowKey(row)} row={row} />
                  ))}
                </Stack>
                {/* Room to scroll the last entry above the composer. Safari drops a scroller's bottom padding. */}
                <Stack shrink style={{ height: live ? composerHeight : 28 }} />
              </Stack>
              {live && (
                <Stack
                  ref={composerRef}
                  position="absolute"
                  style={{
                    left: 0,
                    right: 0,
                    bottom: 0,
                    paddingTop: 32,
                    background: 'linear-gradient(to top, var(--color-background) 60%, transparent)',
                  }}
                >
                  <Composer run={run} />
                </Stack>
              )}
            </Stack>
            {!live && <EndLine run={run} canRaiseBudget={canRaiseBudget} />}
          </Stack>
          {railPane && <RunRail run={run} />}
          <SidePanel
            side="left"
            width={320}
            open={sheet === 'sessions' && !sessionsPane}
            onClose={() => setSheet(null)}
          >
            <SidePanelAccessibleTitle>Sessions</SidePanelAccessibleTitle>
            {sessions(true)}
          </SidePanel>
          {layout === 'phone' ? (
            <BottomSheet
              open={sheet === 'details'}
              onOpenChange={(open) => !open && setSheet(null)}
              title="Run details"
            >
              <RunRail run={run} sheet />
            </BottomSheet>
          ) : (
            <SidePanel
              open={sheet === 'details' && !railPane}
              onClose={() => setSheet(null)}
              width={340}
            >
              <SidePanelHeader title="Run details" />
              <RunRail run={run} sheet />
            </SidePanel>
          )}
        </Stack>
      </TooltipProvider>
    </RunLayoutContext.Provider>
  );
}
