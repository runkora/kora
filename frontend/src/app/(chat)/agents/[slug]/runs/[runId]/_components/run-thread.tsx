'use client';

/** @tier 3 — run-page-scoped. One row of the run's thread: a message, the agent's words, a call. */

import { Avatar, Icon, Stack, StreamingCursor, Text } from '@runkora/hades';
import { InstructionsMarkdown } from '@runkora/components';
import type { Entry, Row } from '../_lib/run-view';
import { CallBlock, QuietLine } from './call-block';
import { SystemMark, TriggerEventView, triggerEventOf } from './trigger-event';

/**
 * A message from a person or an agent: a soft shaded block with the sender. A message from before
 * senders were recorded shows a plain person mark and no name.
 */
function MessageBlock({ entry }: { entry: Extract<Entry, { type: 'message' }> }) {
  const { author } = entry;
  return (
    <Stack
      gap="snug"
      style={{
        padding: '14px 18px',
        borderRadius: 10,
        background: 'var(--color-surface-elevated)',
      }}
    >
      <Stack direction="horizontal" align="center" gap="snug">
        {author.unrecorded ? (
          <Icon name="user" size="2xs" color="subtle" label="Sender not recorded" />
        ) : author.kind === 'agent' ? (
          <SystemMark icon="bot" />
        ) : (
          <Avatar name={author.name} size="2xs" />
        )}
        <Text size="caption" color="muted" style={{ flex: 1 }}>
          {author.name}
        </Text>
        <Text size="caption" color="subtle">
          {entry.time}
        </Text>
      </Stack>
      <Text size="body" preserveWhitespace style={{ lineHeight: 1.6 }}>
        {entry.text}
      </Text>
      {entry.error && (
        <Text size="caption" color="status-error" role="alert">
          {entry.error}
        </Text>
      )}
    </Stack>
  );
}

/** The agent's own words, set for reading. */
function Prose({ text, streaming }: { text: string; streaming?: boolean }) {
  return (
    <Stack style={{ fontSize: 16, lineHeight: 1.7 }}>
      <InstructionsMarkdown content={text} />
      {streaming && <StreamingCursor />}
    </Stack>
  );
}

export function RowView({ row }: { row: Row }) {
  if (row.type === 'quiet') return <QuietLine steps={row.steps} />;
  if (row.type === 'step') return <CallBlock step={row.step} nested={row.children} />;
  const e = row.entry;
  switch (e.type) {
    case 'message':
      return e.author.kind === 'trigger' ? (
        <TriggerEventView e={triggerEventOf(e)} />
      ) : (
        <MessageBlock entry={e} />
      );
    case 'text':
      return <Prose text={e.text} streaming={e.streaming} />;
    case 'plan':
      return null; // The plan's progress shows in the header and updates in place.
    case 'notice':
      return (
        <Stack gap="none">
          <Text size="supporting" color={e.tone === 'error' ? 'status-error' : 'subtle'}>
            {e.text}
          </Text>
          {e.detail && (
            <Text size="caption" color="subtle" mono>
              {e.detail}
            </Text>
          )}
        </Stack>
      );
  }
}

/** A row's key: its entry's, step's or fold's id. */
export const rowKey = (row: Row) =>
  row.type === 'entry' ? row.entry.id : row.type === 'step' ? row.step.id : row.id;
