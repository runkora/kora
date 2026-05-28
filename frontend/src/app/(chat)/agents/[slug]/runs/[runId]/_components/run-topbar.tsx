'use client';

/**
 * @tier 3 — run-chat-scoped.
 * 44px topbar: back nav, logo + agent name, model badge, run ID badge, ctx meter, canvas toggle.
 */

import Link from 'next/link';
import { APP_ROUTES } from '@orbit/lib/routing';
import { Stack, Text, Button, Badge, StatusDot } from '@orbit/terra';
import { useRunContext } from '../_context/run-context';

function OrbitalMark({ size = 9 }: { size?: number }) {
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

export function RunTopbar() {
  const { slug, agentName, model, runId, runState, canvasState, openCanvas } = useRunContext();

  const canvasIsOpen = canvasState.isOpen;
  const hasCanvasTabs = canvasState.tabs.length > 0;

  function handleCanvasToggle() {
    if (hasCanvasTabs && canvasState.activeTabId) {
      openCanvas(canvasState.activeTabId);
    } else if (hasCanvasTabs) {
      openCanvas(canvasState.tabs[0].id);
    }
  }

  const displayRunId = runId.length > 8 ? runId.slice(-8) : runId;

  const isLive = runState === 'active' || runState === 'starting' || runState === 'completing';
  const dotStatus =
    runState === 'failed'
      ? 'error'
      : isLive
        ? 'healthy'
        : runState === 'idle'
          ? 'degraded'
          : 'idle';

  return (
    <Stack
      direction="horizontal"
      align="center"
      style={{
        gap: 10,
        padding: '0 16px',
        height: 44,
        background: 'var(--color-surface)',
        borderBottom: '1px solid var(--color-border-subtle)',
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
          color: 'var(--color-subtle)',
          textDecoration: 'none',
          fontSize: 14,
        }}
        aria-label="Back to agent"
      >
        ←
      </Link>

      {/* Logo + agent name */}
      <Stack direction="horizontal" align="center" style={{ gap: 8 }}>
        <Stack
          align="center"
          justify="center"
          style={{
            width: 20,
            height: 20,
            background: 'var(--color-surface-elevated)',
            borderRadius: 4,
            color: 'var(--color-accent)',
            flexShrink: 0,
          }}
          aria-hidden
        >
          <OrbitalMark size={9} />
        </Stack>
        <Text
          as="span"
          style={{
            fontWeight: 600,
            fontSize: 14,
            color: 'var(--color-foreground)',
            letterSpacing: '-0.01em',
          }}
        >
          {agentName}
        </Text>
      </Stack>

      {/* Live status + model + run ID */}
      <Stack direction="horizontal" align="center" style={{ gap: 6 }}>
        <StatusDot status={dotStatus} size="sm" glow={isLive} aria-label={runState} />
        <Badge variant="dim" size="sm" mono>
          {model}
        </Badge>
        <Badge variant="dim" size="sm" mono>
          {displayRunId}
        </Badge>
      </Stack>

      <Stack grow />

      {/* Context meter */}
      <Stack direction="horizontal" align="center" style={{ gap: 7 }}>
        <Stack
          style={{
            width: 52,
            height: 2,
            background: 'var(--color-border-subtle)',
            borderRadius: 2,
            overflow: 'hidden',
          }}
        >
          <Stack
            style={{
              height: '100%',
              width: '21%',
              background: 'var(--color-accent)',
              borderRadius: 2,
            }}
          />
        </Stack>
        <Text
          as="span"
          style={{ fontSize: 12, color: 'var(--color-subtle)', fontFamily: 'var(--font-mono)' }}
        >
          42k
        </Text>
      </Stack>

      {/* Canvas toggle */}
      <Button
        type="button"
        onClick={handleCanvasToggle}
        variant={canvasIsOpen ? 'outline' : 'outline'}
        size="sm"
        style={{
          background: canvasIsOpen
            ? 'color-mix(in oklch, var(--color-accent) 12%, transparent)'
            : 'transparent',
          borderColor: canvasIsOpen
            ? 'color-mix(in oklch, var(--color-accent) 35%, transparent)'
            : undefined,
          color: canvasIsOpen ? 'var(--color-accent)' : 'var(--color-subtle)',
        }}
        aria-label={canvasIsOpen ? 'Close canvas' : 'Open canvas'}
        aria-pressed={canvasIsOpen}
      >
        <CanvasIcon />
        Canvas
      </Button>
    </Stack>
  );
}
