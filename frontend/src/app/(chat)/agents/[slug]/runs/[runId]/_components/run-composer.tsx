'use client';

/**
 * @tier 3 — run-page-scoped. The composer a live run floats over the foot of its thread: what the
 * agent is doing now, the messages waiting for its next step, and the message field with Send or Stop.
 */

import { useState } from 'react';
import { Button, Icon, Stack, Text, Textarea } from '@runkora/hades';
import { PermissionHint } from '@runkora/components';
import { useRunContext } from '../_context/run-context';
import { useRunLayout } from '../_lib/run-layout';
import type { RunView } from '../_lib/run-view';
import { LINE, NOWRAP, READING_WIDTH } from '../_lib/run-styles';

const BOX = {
  border: LINE,
  borderRadius: 10,
  background: 'var(--color-surface)',
} as const;

/** Send and Stop share one square: 32px, the size of the composer's existing buttons. */
const ACTION = {
  width: 32,
  height: 32,
  borderRadius: 8,
  flexShrink: 0,
  padding: 0,
  border: 'none',
} as const;

function StopIcon() {
  return (
    <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor" aria-hidden>
      <rect x="4" y="4" width="16" height="16" rx="2" />
    </svg>
  );
}

/** Messages sent while the agent works, waiting for its next step. */
function Pending({ run }: { run: RunView }) {
  return (
    <Stack gap="tight" style={{ padding: '8px 0', borderBottom: LINE }}>
      {run.queued.map((q) => (
        <Stack
          key={q.id}
          direction="horizontal"
          align="center"
          gap="snug"
          style={{ padding: '0 14px' }}
        >
          <Icon name="clock" size="2xs" color="subtle" label="" />
          <Text size="supporting" color="muted" truncate style={{ flex: 1, minWidth: 0 }}>
            {q.text}
          </Text>
          <Text size="caption" color="subtle" style={NOWRAP}>
            Next step
          </Text>
        </Stack>
      ))}
    </Stack>
  );
}

/**
 * The composer offers only what the API does: one message, read at the agent's next step while it
 * works, and Stop, which ends the turn. A strip on top says what the agent is doing now.
 */
export function Composer({ run }: { run: RunView }) {
  const { addHumanMessage, interruptRun, canSteer, isStarting, isPending, stopError } =
    useRunContext();
  const phone = useRunLayout() === 'phone';
  const [text, setText] = useState('');
  const allowed = canSteer !== false;
  const ready = !!text.trim() && allowed && !isStarting;
  const working = run.state === 'active';
  const name = run.agent.name;
  const send = () => {
    if (!ready) return;
    addHumanMessage(text.trim());
    setText('');
  };
  return (
    <Stack style={{ padding: phone ? '0 12px 12px' : '0 32px 22px' }}>
      <Stack style={{ maxWidth: READING_WIDTH, width: '100%', margin: '0 auto' }}>
        <Stack overflow="hidden" style={BOX}>
          {(working || isStarting || isPending) && (
            <Stack
              direction="horizontal"
              align="center"
              gap="snug"
              style={{
                padding: '7px 10px 7px 14px',
                borderBottom: LINE,
                background: 'var(--color-surface-elevated)',
              }}
            >
              <Icon name="loader" size="2xs" color="accent" label="" />
              <Text
                size="caption"
                color="muted"
                truncate
                role="status"
                style={{ flex: 1, minWidth: 0 }}
              >
                {run.activity ?? (isStarting ? 'Starting' : isPending ? 'Sending' : 'Thinking')}
              </Text>
            </Stack>
          )}
          {run.queued.length > 0 && <Pending run={run} />}
          <Stack gap="snug" style={{ padding: '14px 10px 10px 14px' }}>
            <Textarea
              variant="bare"
              autoResize
              rows={phone ? 2 : 3}
              maxRows={phone ? 8 : 14}
              value={text}
              disabled={!allowed}
              aria-label={`Message ${name}`}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                  e.preventDefault();
                  send();
                }
              }}
              placeholder={
                !allowed
                  ? 'You cannot message this run'
                  : isStarting
                    ? `${name} is starting`
                    : working
                      ? `Message ${name}. This won't stop its work.`
                      : `Reply to ${name}`
              }
              style={{ fontSize: 14, lineHeight: 1.6, width: '100%', padding: 0, borderRadius: 0 }}
            />
            <Stack direction="horizontal" align="center" gap="snug">
              <Text
                size="caption"
                color={stopError ? 'status-error' : 'subtle'}
                style={{ flex: 1 }}
              >
                {stopError ??
                  (working && !text.trim()
                    ? 'Stop ends its current turn'
                    : phone
                      ? ''
                      : 'Shift+Enter for a new line')}
              </Text>
              {working && !text.trim() ? (
                <PermissionHint allowed={allowed} action="stop this run">
                  <Button
                    aria-label="Stop"
                    disabled={!allowed}
                    onClick={interruptRun}
                    style={{
                      ...ACTION,
                      background: 'var(--color-foreground)',
                      color: 'var(--color-background)',
                    }}
                  >
                    <StopIcon />
                  </Button>
                </PermissionHint>
              ) : (
                <Button
                  disabled={!ready}
                  aria-label="Send"
                  onClick={send}
                  style={{
                    ...ACTION,
                    background: ready ? 'var(--color-accent)' : 'var(--color-surface-overlay)',
                    color: ready ? 'var(--color-accent-foreground)' : 'var(--color-subtle)',
                  }}
                >
                  <Icon name="arrow-up" size="xs" label="" />
                </Button>
              )}
            </Stack>
          </Stack>
        </Stack>
      </Stack>
    </Stack>
  );
}
