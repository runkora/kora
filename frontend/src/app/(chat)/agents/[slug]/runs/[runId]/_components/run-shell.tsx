'use client';

/**
 * @tier 3 — run-chat-scoped. Client boundary shell: mounts RunProvider and
 * renders the four layout regions (topbar, context strip, body, input).
 */

import { Stack } from '@runkora/hades';
import { RunProvider } from '../_context/run-context';
import { RunTopbar } from './run-topbar';
import { ContextStrip } from './context-strip';
import { ChatThread } from './chat-thread';
import { CanvasPanel } from './canvas-panel';
import { ConnectionBanner } from './connection-banner';
import { BudgetBanner } from './budget-banner';
import { InputArea } from './input-area';

// Global keyframes and data-attribute CSS for the run chat.
// Injected once at the shell level so all child components can rely on them.
const RUN_CSS = `
[data-reasoning-pulse] {
  animation: reasoning-pulse 1.6s ease-in-out infinite;
}
@keyframes reasoning-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.15; }
}
[data-floating] {
  border-color: transparent;
  transition: border-color 0.15s, box-shadow 0.15s;
}
[data-floating]:focus-within {
  border-color: var(--color-accent);
}
button[data-context-pill] {
  transition: background 0.12s, border-color 0.12s, color 0.12s;
}
button[data-context-pill]:not([data-active]):not(:disabled):hover {
  background: color-mix(in oklch, var(--color-accent) 8%, transparent);
  border-color: color-mix(in oklch, var(--color-accent) 25%, transparent);
  color: var(--color-accent);
}
button[data-context-pill][data-active] {
  background: color-mix(in oklch, var(--color-accent) 12%, transparent);
  border-color: color-mix(in oklch, var(--color-accent) 35%, transparent);
  color: var(--color-accent);
}
button[data-context-pill][data-active]:hover {
  background: color-mix(in oklch, var(--color-accent) 20%, transparent);
  border-color: color-mix(in oklch, var(--color-accent) 50%, transparent);
}
@keyframes topbar-pulse { 0%,100%{opacity:1} 50%{opacity:0.4} }
[data-md-ol] { counter-reset: md-ol; }
[data-md-ol] > * { counter-increment: md-ol; }
[data-md-ol-num]::before { content: counter(md-ol) "."; }
`;

function RunPageLayout() {
  return (
    <>
      <style>{RUN_CSS}</style>
      <RunTopbar />
      <ConnectionBanner />
      <BudgetBanner />
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
