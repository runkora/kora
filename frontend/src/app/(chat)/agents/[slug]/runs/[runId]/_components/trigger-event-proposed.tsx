'use client';

/**
 * @tier 3 — proposed, run-page-scoped. A trigger's message shown as the event that caused it: who
 * did what to which object, a preview of that object, and the rendered prompt behind a toggle.
 */

import { useState } from 'react';
import {
  Anchor,
  Avatar,
  Button,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  Icon,
  Stack,
  Text,
  type IconName,
} from '@runkora/hades';
import { parseTriggerMessage } from '../_lib/trigger-message-proposed';
import { useRunLayout } from '../_lib/run-layout-proposed';
import type { TriggerEventSummary, TriggerOrigin } from '../_lib/run-view-proposed';

/** An event summary placed in the thread: when it arrived and the prompt it rendered. */
export interface TriggerEvent extends TriggerEventSummary {
  trigger: string;
  time: string;
  joined?: boolean;
  prompt: string;
}

const LINE = '1px solid var(--color-border-subtle)';
const NOWRAP: React.CSSProperties = { whiteSpace: 'nowrap', flexShrink: 0 };

function Mark({ actor }: { actor: TriggerEvent['actor'] }) {
  if (actor.kind === 'person') return <Avatar name={actor.name} size="2xs" />;
  return (
    <Stack
      align="center"
      justify="center"
      style={{
        width: 18,
        height: 18,
        borderRadius: 999,
        background: 'var(--color-surface-overlay)',
        flexShrink: 0,
      }}
    >
      <Icon name={actor.icon ?? 'zap'} size="2xs" color="muted" label="" />
    </Stack>
  );
}

function ObjectLink({ object }: { object: TriggerEvent['object'] }) {
  if (!object.label) return null;
  return object.url ? (
    <Anchor href={object.url} target="_blank" rel="noopener noreferrer">
      {object.label}
    </Anchor>
  ) : (
    <Text as="span" size="supporting" weight="emphasis">
      {object.label}
    </Text>
  );
}

/** "dev-reviewer commented on PR #6", then the trigger and the time, quieter. */
function EventLine({ e }: { e: TriggerEvent }) {
  const phone = useRunLayout() === 'phone';
  return (
    <Stack direction="horizontal" align="center" gap="snug" style={{ position: 'relative' }}>
      {/* The mark hangs in the gutter so the event lines up with the blocks around it. A phone has no gutter, so it sits inline. */}
      {phone ? (
        <Mark actor={e.actor} />
      ) : (
        <Stack
          style={{ position: 'absolute', left: -30, top: '50%', transform: 'translateY(-50%)' }}
        >
          <Mark actor={e.actor} />
        </Stack>
      )}
      <Text size="supporting" truncate style={{ flex: 1, minWidth: 0 }}>
        <Text as="span" size="supporting" weight="emphasis">
          {e.actor.name}
        </Text>{' '}
        <Text as="span" size="supporting" color="muted">
          {e.verb}
        </Text>{' '}
        <ObjectLink object={e.object} />
      </Text>
      <Text size="caption" color="subtle" style={NOWRAP}>
        {e.joined ? 'joined · ' : ''}
        {e.time}
      </Text>
    </Stack>
  );
}

function Quote({ text }: { text: string }) {
  return (
    <Text
      size="supporting"
      style={{
        borderLeft: '2px solid var(--color-border)',
        paddingLeft: 10,
        whiteSpace: 'pre-wrap',
      }}
    >
      {text}
    </Text>
  );
}

/** The object as a link preview, the way chat tools unfurl a pasted link. */
function Unfurl({ u }: { u: NonNullable<TriggerEvent['unfurl']> }) {
  return (
    <Stack
      direction="horizontal"
      align="center"
      gap="snug"
      style={{ border: LINE, borderRadius: 8, padding: '8px 10px' }}
    >
      <Icon name={u.icon} size="xs" color="muted" label="" />
      <Stack grow minWidth="none" gap="none">
        <Text size="supporting" weight="emphasis" truncate>
          {u.title}
        </Text>
        <Text size="caption" color="subtle" truncate>
          {u.meta.join(' · ')}
          {u.diff && (
            <>
              {' · '}
              <Text as="span" size="caption" color="status-healthy" tabular>
                +{u.diff.added}
              </Text>{' '}
              <Text as="span" size="caption" color="status-error" tabular>
                −{u.diff.removed}
              </Text>
            </>
          )}
        </Text>
      </Stack>
      {u.status && (
        <Text
          size="caption"
          color={u.status.tone === 'bad' ? 'status-error' : 'status-healthy'}
          style={NOWRAP}
        >
          {u.status.label}
        </Text>
      )}
    </Stack>
  );
}

/** The rendered prompt the agent received, for whoever needs it. */
function Prompt({ e }: { e: TriggerEvent }) {
  const [open, setOpen] = useState(false);
  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger asChild>
        <Button size="sm" variant="ghost" style={{ paddingInline: 6, marginLeft: -6, height: 22 }}>
          <Icon name={open ? 'chevron-down' : 'chevron-right'} size="2xs" label="" />
          <Text as="span" size="caption" color="subtle">
            Prompt from {e.trigger}
          </Text>
        </Button>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <Text
          size="caption"
          color="muted"
          mono
          style={{ whiteSpace: 'pre-wrap', paddingTop: 4, display: 'block' }}
        >
          {e.prompt}
        </Text>
      </CollapsibleContent>
    </Collapsible>
  );
}

/** The event line, a quote when a person wrote something, the object's preview, then the prompt. */
export function TriggerEventView({ e }: { e: TriggerEvent }) {
  return (
    <Stack gap="snug">
      <EventLine e={e} />
      <Stack gap="snug">
        {e.quote && <Quote text={e.quote} />}
        {e.unfurl && <Unfurl u={e.unfurl} />}
        <Prompt e={e} />
      </Stack>
    </Stack>
  );
}

const FALLBACK: Record<string, { icon: IconName; verb: string }> = {
  manual: { icon: 'play', verb: 'was fired' },
  scheduled: { icon: 'clock', verb: 'ran on schedule' },
  event: { icon: 'activity', verb: 'received an event' },
  webhook: { icon: 'webhook', verb: 'received a webhook' },
};

/**
 * An event read from what any trigger's run holds: the trigger's name and the rendered prompt.
 * The preview's title is the template's first sentence; its facts are the template's own
 * `Key: {{ path }}` lines, filled from the data block the platform appends. Generic webhooks use
 * this, and every other trigger falls back to it until its adapter records a summary.
 */
export function promptEvent(input: {
  trigger: string;
  time: string;
  joined?: boolean;
  prompt: string;
  via?: TriggerOrigin['via'];
}): TriggerEvent {
  const parts = parseTriggerMessage(input.prompt);
  const title = parts.lead.split(/(?<=[.!?])\s/)[0] || input.trigger;
  const look = FALLBACK[input.via ?? 'webhook'] ?? FALLBACK.webhook;
  return {
    actor: { name: input.trigger, kind: 'system', icon: look.icon },
    verb: look.verb,
    object: { label: '' },
    trigger: input.trigger,
    time: input.time,
    joined: input.joined,
    unfurl: { icon: look.icon, title, meta: parts.fields.map((f) => `${f.key} ${f.value}`) },
    prompt: input.prompt,
  };
}

/** A generic webhook's event. */
export const webhookEvent = (input: Parameters<typeof promptEvent>[0]) =>
  promptEvent({ ...input, via: 'webhook' });
