'use client';

/**
 * @tier 3 — run-page-scoped. A trigger's message shown as the event that caused it: who
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
import { isSafeUrl } from '@runkora/lib/safe-url';
import { parseTriggerMessage } from '../_lib/trigger-message';
import { useRunLayout } from '../_lib/run-layout';
import type { Entry, TriggerEventSummary, TriggerOrigin } from '../_lib/run-view';
import { LINE, NOWRAP } from '../_lib/run-styles';

/** An event summary placed in the thread: when it arrived and the prompt it rendered. */
export interface TriggerEvent extends TriggerEventSummary {
  trigger: string;
  time: string;
  joined?: boolean;
  prompt: string;
}

/** A round mark for something that is not a person: a trigger, a bot or the platform. */
export function SystemMark({ icon = 'zap', size = 18 }: { icon?: IconName; size?: number }) {
  return (
    <Stack
      align="center"
      justify="center"
      shrink
      style={{
        width: size,
        height: size,
        borderRadius: 999,
        background: 'var(--color-surface-overlay)',
      }}
    >
      <Icon name={icon} size="2xs" color="muted" label="" />
    </Stack>
  );
}

function Mark({ actor }: { actor: TriggerEvent['actor'] }) {
  if (actor.kind === 'person') return <Avatar name={actor.name} size="2xs" />;
  return <SystemMark icon={actor.icon ?? 'zap'} />;
}

function ObjectLink({ object }: { object: TriggerEvent['object'] }) {
  if (!object.label) return null;
  // A summary's URL is recorded data; only an http(s) one becomes a link.
  return isSafeUrl(object.url) ? (
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
    <Stack direction="horizontal" align="center" gap="snug" position="relative">
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
      preserveWhitespace
      style={{ borderLeft: '2px solid var(--color-border)', paddingLeft: 10 }}
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
          preserveWhitespace
          style={{ paddingTop: 4, display: 'block' }}
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
function promptEvent(input: {
  trigger: string;
  time: string;
  joined?: boolean;
  prompt: string;
  via?: TriggerOrigin['via'];
}): TriggerEvent {
  const parts = parseTriggerMessage(input.prompt);
  const title = parts.lead.split(/(?<=[.!?])\s/)[0];
  const meta = parts.fields.map((f) => `${f.key} ${f.value}`);
  // A message whose trigger kind is unknown, such as one joining a run, claims no kind.
  const look = (input.via && FALLBACK[input.via]) || {
    icon: 'zap' as const,
    verb: 'sent a message',
  };
  return {
    actor: { name: input.trigger, kind: 'system', icon: look.icon },
    verb: look.verb,
    object: { label: '' },
    trigger: input.trigger,
    time: input.time,
    joined: input.joined,
    // A prompt with neither a lead sentence nor fields has nothing to preview.
    ...(title || meta.length
      ? { unfurl: { icon: look.icon, title: title || input.trigger, meta } }
      : {}),
    prompt: input.prompt,
  };
}

/** A trigger's message as its event: the adapter's summary when there is one, else from the prompt. */
export function triggerEventOf(e: Extract<Entry, { type: 'message' }>): TriggerEvent {
  const base = { trigger: e.author.name, time: e.time, joined: e.joined, prompt: e.text };
  return e.event ? { ...e.event, ...base } : promptEvent({ ...base, via: e.author.via });
}
