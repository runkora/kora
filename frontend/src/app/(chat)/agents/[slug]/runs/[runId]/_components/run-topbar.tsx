'use client';

/**
 * @tier 3 — run-chat-scoped.
 * 46px topbar: back nav, logo, model pill, session pill, ctx meter, canvas toggle.
 */

import Link from 'next/link';
import { APP_ROUTES } from '@orbit/lib/routing';
import { Stack, Text, Button } from '@orbit/terra';
import { useRunContext } from '../_context/run-context';

// Orbital logomark SVG — inline, 10×10 viewBox
function OrbitalMark({ size = 10 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 12 12" fill="none" aria-hidden>
      <circle cx="6" cy="6" r="2.5" fill="currentColor" />
      <circle
        cx="6"
        cy="6"
        r="5"
        stroke="currentColor"
        strokeWidth="1.2"
        fill="none"
        opacity=".5"
      />
    </svg>
  );
}

// Canvas split-pane icon
function CanvasIcon() {
  return (
    <svg
      width="13"
      height="13"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      aria-hidden
    >
      <rect x="3" y="3" width="18" height="18" rx="2" />
      <line x1="12" y1="3" x2="12" y2="21" />
    </svg>
  );
}

// Session log document icon
function DocIcon() {
  return (
    <svg
      width="13"
      height="13"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      aria-hidden
    >
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
      <polyline points="14 2 14 8 20 8" />
    </svg>
  );
}

export function RunTopbar() {
  const { slug, model, runId, canvasState, openCanvas } = useRunContext();

  const canvasIsOpen = canvasState.isOpen;
  const hasCanvasTabs = canvasState.tabs.length > 0;

  function handleCanvasToggle() {
    if (hasCanvasTabs && canvasState.activeTabId) {
      openCanvas(canvasState.activeTabId);
    } else if (hasCanvasTabs) {
      openCanvas(canvasState.tabs[0].id);
    }
    // If no tabs open yet, the button does nothing — user must click a tool row first
  }

  // Truncate runId for display (show last 8 chars)
  const displayRunId = runId.length > 8 ? runId.slice(-8) : runId;

  return (
    <Stack
      direction="horizontal"
      align="center"
      style={{
        gap: 10,
        padding: '0 16px',
        height: 46,
        background: 'var(--chat-surf)',
        borderBottom: '1px solid var(--chat-bds)',
        flexShrink: 0,
        zIndex: 10,
      }}
    >
      {/* Back navigation */}
      <Link
        href={APP_ROUTES.agents.detail(slug)}
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          width: 27,
          height: 27,
          borderRadius: 4,
          color: 'var(--chat-subtle)',
          textDecoration: 'none',
          fontSize: 14,
        }}
        aria-label="Back to agent"
      >
        ←
      </Link>

      {/* Logo + brand */}
      <Stack
        direction="horizontal"
        align="center"
        style={{ gap: 7, fontWeight: 600, fontSize: 13 }}
      >
        <Stack
          align="center"
          justify="center"
          style={{
            width: 20,
            height: 20,
            background: 'var(--chat-acc-dim)',
            border: '1.5px solid var(--chat-acc-bdr)',
            borderRadius: 4,
            color: 'var(--chat-acc)',
          }}
          aria-hidden
        >
          <OrbitalMark size={10} />
        </Stack>
        <Text as="span" style={{ color: 'var(--chat-fg)', fontWeight: 600, fontSize: 13 }}>
          Orbit AI
        </Text>
      </Stack>

      {/* Separator */}
      <Text as="span" style={{ color: 'var(--chat-bds)', fontSize: 14 }}>
        /
      </Text>

      {/* Model pill */}
      <Stack
        direction="horizontal"
        align="center"
        style={{
          gap: 5,
          background: 'var(--chat-surf2)',
          border: '1px solid var(--chat-bds)',
          borderRadius: 20,
          padding: '2px 9px',
          fontSize: 11.5,
          color: 'var(--chat-muted)',
          fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
        }}
      >
        {/* Live dot */}
        <Text
          as="span"
          style={{
            width: 5,
            height: 5,
            borderRadius: '50%',
            background: 'var(--chat-healthy)',
            flexShrink: 0,
            animation: 'pulse 2s ease-in-out infinite',
          }}
          aria-label="Connected"
        />
        {model}
      </Stack>

      {/* Separator */}
      <Text as="span" style={{ color: 'var(--chat-bds)', opacity: 0.3, fontSize: 14 }}>
        /
      </Text>

      {/* Session pill */}
      <Stack
        direction="horizontal"
        align="center"
        style={{
          gap: 5,
          background: 'var(--chat-surf2)',
          border: '1px solid var(--chat-bds)',
          borderRadius: 20,
          padding: '2px 9px',
          fontSize: 11.5,
          color: 'var(--chat-subtle)',
          fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
        }}
      >
        {displayRunId}
      </Stack>

      {/* Spacer */}
      <Stack grow />

      {/* Context meter */}
      <Stack
        direction="horizontal"
        align="center"
        style={{
          gap: 7,
          fontSize: 11,
          color: 'var(--chat-subtle)',
        }}
      >
        <Text as="span">ctx</Text>
        <Stack
          style={{
            width: 72,
            height: 3,
            background: 'var(--chat-surf3)',
            borderRadius: 2,
            overflow: 'hidden',
          }}
        >
          <Stack
            style={{
              height: '100%',
              width: '21%',
              background: 'var(--chat-acc)',
              borderRadius: 2,
            }}
          />
        </Stack>
        <Text as="span">42k / 200k</Text>
      </Stack>

      {/* Session log icon button */}
      <Button
        type="button"
        style={{
          width: 27,
          height: 27,
          background: 'transparent',
          border: '1px solid transparent',
          borderRadius: 4,
          color: 'var(--chat-subtle)',
          cursor: 'pointer',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
        }}
        title="Session log"
        aria-label="Session log"
      >
        <DocIcon />
      </Button>

      {/* Canvas toggle */}
      <Button
        type="button"
        onClick={handleCanvasToggle}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          padding: '4px 10px',
          background: canvasIsOpen ? 'var(--chat-acc-dim)' : 'transparent',
          border: `1px solid ${canvasIsOpen ? 'var(--chat-acc-bdr)' : 'var(--chat-bds)'}`,
          borderRadius: 4,
          color: canvasIsOpen ? 'var(--chat-acc)' : 'var(--chat-subtle)',
          fontSize: 12,
          cursor: 'pointer',
        }}
        aria-label={canvasIsOpen ? 'Close canvas' : 'Open canvas'}
        aria-pressed={canvasIsOpen}
      >
        <CanvasIcon />
        Canvas
      </Button>

      {/* Keyframe for live dot — injected once */}
      <style>{`@keyframes pulse { 0%,100%{opacity:1} 50%{opacity:0.4} }`}</style>
    </Stack>
  );
}
