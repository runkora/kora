'use client';

/**
 * @tier 3 — events-scoped. One event in full, laid out as the delivery detail modal is: inset
 * facts with the links out to its agent, run, triggers, delivery and trace, a rule, then the
 * event chain, the fields an update changed and the payloads.
 */

import type { ReactNode } from 'react';
import {
  Anchor,
  Badge,
  Button,
  CodeBlock,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  CopyButton,
  Icon,
  SectionLabel,
  Separator,
  Stack,
  StatusDot,
  Text,
} from '@runkora/hades';
import Link from 'next/link';
import { APP_ROUTES } from '@runkora/lib/routing';
import { relativeTime } from '@runkora/lib/relative-time';
import {
  bareType,
  deliveryIdOf,
  eventSummary,
  eventTone,
  formatUtc,
  principalName,
  resourceOf,
  shortId,
  traceHref,
  traceIdOf,
  triggerIdsOf,
} from '../_types';
import type { EventPrincipalType, KoraEvent, SeenPrincipal } from '../_types';
import { EventPrincipalBadge } from './event-parts';

/** A trigger's name from the principals the feed has seen, or its short id. */
export function triggerName(id: string, principals: SeenPrincipal[], event?: KoraEvent): string {
  const own = event?.data.entity.name;
  if (event?.subject === `triggers/${id}` && typeof own === 'string' && own !== '') return own;
  const subject = `trigger:${id}`;
  const name = principals.find((p) => p.subject === subject)?.name;
  // A trigger token with no display name records its subject as the name.
  return name !== undefined && name !== subject ? name : shortId(id);
}

/** A label on the left, its value on the right, as in the delivery detail. */
function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Stack
      direction="horizontal"
      align="center"
      justify="between"
      gap="comfortable"
      paddingBlock="tight"
    >
      <Stack shrink={false}>
        <Text size="caption" color="muted">
          {label}
        </Text>
      </Stack>
      <Stack direction="horizontal" align="center" gap="snug" minWidth="none" justify="end">
        {children}
      </Stack>
    </Stack>
  );
}

function InternalLink({
  href,
  mono = false,
  children,
}: {
  href: string;
  mono?: boolean;
  children: string;
}) {
  return (
    <Anchor asChild size="md">
      <Link href={href}>
        <Text as="span" size="supporting" mono={mono}>
          {children}
        </Text>
      </Link>
    </Anchor>
  );
}

function PrincipalValue({
  name,
  subject,
  type,
}: {
  name: string;
  subject: string;
  type: EventPrincipalType;
}) {
  return (
    <>
      <Text size="supporting" weight="emphasis" truncate title={subject}>
        {name}
      </Text>
      <EventPrincipalBadge type={type} />
    </>
  );
}

/** One entity value in the changed-fields list: a list of strings reads as badges. */
function FieldValue({ value }: { value: unknown }) {
  if (Array.isArray(value) && value.every((v) => typeof v === 'string'))
    return (
      <Stack direction="horizontal" gap="tight" wrap>
        {value.map((v) => (
          <Badge key={v} variant="elevated" size="sm" mono>
            {v}
          </Badge>
        ))}
      </Stack>
    );
  return (
    <Text size="supporting" mono color="muted">
      {value === undefined
        ? 'not in the event'
        : typeof value === 'string'
          ? value
          : JSON.stringify(value)}
    </Text>
  );
}

export interface EventDetailProps {
  event: KoraEvent;
  /** The principals the feed has seen, for trigger and delegator names. */
  principals: SeenPrincipal[];
  /** The chart's trace URL template; empty when tracing has no UI. */
  traceUrlTemplate: string;
  /** The loaded event that caused this one. */
  cause?: KoraEvent;
  /** The loaded events this one caused. */
  effects: KoraEvent[];
  onOpenEvent: (id: string) => void;
}

export function EventDetail({
  event,
  principals,
  traceUrlTemplate,
  cause,
  effects,
  onOpenEvent,
}: EventDetailProps) {
  const delegator =
    event.koradelegator === undefined
      ? undefined
      : principals.find((p) => p.subject === event.koradelegator);
  const triggerIds = triggerIdsOf(event);
  const deliveryId = deliveryIdOf(event);
  const traceId = traceIdOf(event.traceparent);
  const trace = traceId === null ? null : traceHref(traceUrlTemplate, traceId);
  const resource = resourceOf(event);
  const changed = event.data.changed ?? [];
  const deleted = bareType(event.type).endsWith('.deleted');
  // The subject's own row is left out when the Run or Delivery row already names it.
  const subjectShown =
    (resource.kind === 'Run' && event.korarun !== undefined) ||
    (resource.kind === 'Delivery' && deliveryId !== undefined) ||
    (resource.kind === 'Trigger' && triggerIds.length > 0);

  return (
    <>
      <Stack padding="snug" gap="none">
        <Stack paddingInline="cozy" gap="none">
          <Fact label="Time">
            <Text size="supporting">{formatUtc(event.time)}</Text>
            <Stack shrink={false}>
              <Text size="caption" color="subtle">
                {relativeTime(event.time)}
              </Text>
            </Stack>
          </Fact>
          <Fact label="Principal">
            <PrincipalValue
              name={principalName(event)}
              subject={event.koraprincipal}
              type={event.koraprincipaltype}
            />
          </Fact>
          {event.koradelegator !== undefined && (
            <Fact label="On behalf of">
              <PrincipalValue
                name={delegator?.name ?? event.koradelegator}
                subject={event.koradelegator}
                type="USER"
              />
            </Fact>
          )}
          {!subjectShown && (
            <Fact label={resource.kind}>
              <Text size="supporting" mono truncate title={event.subject}>
                {resource.label}
              </Text>
            </Fact>
          )}
          {event.koraagent !== undefined && (
            <Fact label="Agent">
              <InternalLink href={APP_ROUTES.agents.detail(event.koraagent)}>
                {event.koraagent}
              </InternalLink>
            </Fact>
          )}
          {event.korarun !== undefined && event.koraagent !== undefined && (
            <Fact label="Run">
              <InternalLink href={APP_ROUTES.agents.run(event.koraagent, event.korarun)} mono>
                {shortId(event.korarun)}
              </InternalLink>
            </Fact>
          )}
          {triggerIds.length > 0 && (
            <Fact label={triggerIds.length === 1 ? 'Trigger' : 'Triggers'}>
              {triggerIds.map((id) => (
                <InternalLink key={id} href={APP_ROUTES.trigger(id)}>
                  {triggerName(id, principals, event)}
                </InternalLink>
              ))}
            </Fact>
          )}
          {deliveryId !== undefined && (
            <Fact label="Delivery">
              <InternalLink href={APP_ROUTES.triggerDelivery(deliveryId)} mono>
                {shortId(deliveryId)}
              </InternalLink>
            </Fact>
          )}
          {traceId !== null && (
            <Fact label="Trace">
              {trace === null ? (
                <Text size="supporting" mono>
                  {traceId.slice(0, 12)}
                </Text>
              ) : (
                <>
                  <Anchor size="md" href={trace} target="_blank" rel="noopener noreferrer">
                    <Text as="span" size="supporting" mono>
                      {traceId.slice(0, 12)}
                    </Text>
                  </Anchor>
                  <Icon name="external-link" size="2xs" color="muted" label="" />
                </>
              )}
            </Fact>
          )}
          <Fact label="Source">
            <Text size="supporting" mono>
              {event.source}
            </Text>
          </Fact>
          <Fact label="Event ID">
            <Text size="caption" mono color="muted" truncate>
              {event.id}
            </Text>
            <CopyButton value={event.id} label="Copy event ID" />
          </Fact>
        </Stack>
      </Stack>
      <Separator />
      <Stack padding="comfortable" gap="relaxed">
        {(event.koracausedby !== undefined || effects.length > 0) && (
          <Stack gap="snug">
            <SectionLabel>Event chain</SectionLabel>
            {event.koracausedby !== undefined && (
              <ChainLink
                label="Caused by"
                event={cause}
                id={event.koracausedby}
                depth={event.korachaindepth}
                onOpenEvent={onOpenEvent}
              />
            )}
            {effects.map((e) => (
              <ChainLink
                key={e.id}
                label={`Depth ${e.korachaindepth}`}
                event={e}
                id={e.id}
                onOpenEvent={onOpenEvent}
              />
            ))}
          </Stack>
        )}

        {changed.length > 0 && (
          <Stack gap="snug">
            <Stack direction="horizontal" align="center" justify="between">
              <SectionLabel>Changed fields</SectionLabel>
              {event.data.entity_version !== undefined && (
                <Text size="caption" color="subtle">
                  Version {event.data.entity_version}, values after the change
                </Text>
              )}
            </Stack>
            <Stack gap="none">
              {changed.map((field, i) => (
                <Stack key={field} gap="none">
                  {i > 0 && <Separator />}
                  <Stack direction="horizontal" align="start" gap="comfortable" paddingBlock="snug">
                    <Stack minWidth="form-field-sm">
                      <Text size="supporting" mono weight="emphasis">
                        {field}
                      </Text>
                    </Stack>
                    <Stack grow minWidth="none">
                      <FieldValue value={event.data.entity[field]} />
                    </Stack>
                  </Stack>
                </Stack>
              ))}
            </Stack>
          </Stack>
        )}

        <Stack gap="snug">
          <SectionLabel>
            {deleted ? 'Resource before the delete' : 'Resource after the change'}
          </SectionLabel>
          <CodeBlock
            density="compact"
            language="json"
            wrap
            text={JSON.stringify(event.data.entity, null, 2)}
          />
        </Stack>

        <Collapsible>
          <Stack gap="snug">
            <Stack direction="horizontal">
              <CollapsibleTrigger asChild>
                <Button size="xs" variant="ghost">
                  <Icon name="braces" size="2xs" />
                  Full event as the API returns it
                  <Icon name="chevron-down" size="2xs" />
                </Button>
              </CollapsibleTrigger>
            </Stack>
            <CollapsibleContent>
              <CodeBlock
                density="compact"
                language="json"
                wrap
                text={JSON.stringify(event, null, 2)}
              />
            </CollapsibleContent>
          </Stack>
        </Collapsible>
      </Stack>
    </>
  );
}

/** One link in the chain: a loaded event opens in place, an unloaded one shows its id. */
function ChainLink({
  label,
  event,
  id,
  depth,
  onOpenEvent,
}: {
  label: string;
  event: KoraEvent | undefined;
  id: string;
  depth?: number;
  onOpenEvent: (id: string) => void;
}) {
  return (
    <Stack direction="horizontal" align="center" gap="snug" minWidth="none">
      <Icon
        name={depth === undefined ? 'git-branch' : 'arrow-up'}
        size="xs"
        color="muted"
        label=""
        shrink
      />
      <Stack shrink={false}>
        <Text size="caption" color="muted">
          {label}
        </Text>
      </Stack>
      {event !== undefined ? (
        <Button size="xs" variant="ghost" onClick={() => onOpenEvent(id)}>
          <StatusDot status={eventTone(event.type)} size="sm" />
          <Text as="span" size="caption" truncate>
            {eventSummary(event)}
          </Text>
        </Button>
      ) : (
        <Text size="caption" mono color="muted" truncate>
          {id}
        </Text>
      )}
      {depth !== undefined && (
        <Badge variant="dim" size="sm">
          Chain depth {depth}
        </Badge>
      )}
    </Stack>
  );
}
