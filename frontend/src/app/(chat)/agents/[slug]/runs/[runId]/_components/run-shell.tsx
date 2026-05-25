'use client';

/**
 * @tier 3 — run-chat-scoped. Client boundary shell: mounts RunProvider and
 * renders the four layout regions (topbar, context strip, body, input).
 */

import { Stack } from '@orbit/terra';
import { RunProvider } from '../_context/run-context';
import { RunTopbar } from './run-topbar';
import { ContextStrip } from './context-strip';
import { ChatThread } from './chat-thread';
import { CanvasPanel } from './canvas-panel';
import { InputArea } from './input-area';
import { TOOL_SPIN_CSS } from './tool-row';

function RunPageLayout() {
  return (
    <>
      <style>{TOOL_SPIN_CSS}</style>
      <RunTopbar />
      <ContextStrip />
      {/* Layout body: chat + canvas side by side */}
      <Stack direction="horizontal" grow style={{ overflow: 'hidden', minHeight: 0 }}>
        <Stack grow style={{ minWidth: 0, overflow: 'hidden' }}>
          <ChatThread />
          <InputArea />
        </Stack>
        {/* Canvas panel — slide-in panel, flex-shrink handled internally */}
        <Stack style={{ flexShrink: 0 }}>
          <CanvasPanel />
        </Stack>
      </Stack>
    </>
  );
}

export function RunShell({ slug, runId }: { slug: string; runId: string }) {
  return (
    <RunProvider slug={slug} runId={runId}>
      <RunPageLayout />
    </RunProvider>
  );
}
