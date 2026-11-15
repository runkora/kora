'use client';

/**
 * @tier 3 — events-scoped. One event in a modal, built as the delivery detail modal is: the
 * summary and type in the header, a status strip, then the detail. The header's buttons step to
 * the next newer or older loaded event without closing.
 */

import {
  Button,
  Icon,
  Modal,
  ModalBody,
  ModalHeader,
  Separator,
  Stack,
  StatusDot,
  Text,
} from '@runkora/hades';
import { relativeTime } from '@runkora/lib/relative-time';
import { eventSummary, eventTone, tabLabelOf } from '../_types';
import type { KoraEvent, SeenPrincipal } from '../_types';
import { effectsOf, findEvent } from './event-parts';
import { EventDetail } from './event-detail';

export interface EventDetailModalProps {
  event: KoraEvent;
  /** The loaded feed, newest first: the stepping order and where causes and effects are found. */
  events: KoraEvent[];
  principals: SeenPrincipal[];
  traceUrlTemplate: string;
  onOpen: (id: string) => void;
  onClose: () => void;
}

export function EventDetailModal({
  event,
  events,
  principals,
  traceUrlTemplate,
  onOpen,
  onClose,
}: EventDetailModalProps) {
  const index = events.indexOf(event);
  const newer = index > 0 ? events[index - 1] : undefined;
  const older = events[index + 1];
  const category = tabLabelOf(event.type);
  return (
    <Modal open onClose={onClose} size="xl">
      <ModalHeader
        title={eventSummary(event)}
        description={event.type}
        actions={
          <Stack direction="horizontal" gap="tight">
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label="Newer event"
              disabled={newer === undefined}
              onClick={() => newer !== undefined && onOpen(newer.id)}
            >
              <Icon name="chevron-up" size="xs" />
            </Button>
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label="Older event"
              disabled={older === undefined}
              onClick={() => older !== undefined && onOpen(older.id)}
            >
              <Icon name="chevron-down" size="xs" />
            </Button>
          </Stack>
        }
      />

      <Stack
        direction="horizontal"
        align="center"
        justify="between"
        gap="snug"
        paddingBlock="snug"
        paddingInline="comfortable"
      >
        <Stack direction="horizontal" align="center" gap="snug">
          <StatusDot status={eventTone(event.type)} glow role="img" aria-label={category} />
          <Text size="supporting">{category}</Text>
        </Stack>
        <Text size="caption" color="subtle">
          {relativeTime(event.time)}
        </Text>
      </Stack>
      <Separator />

      <ModalBody flush gapless constrained>
        <EventDetail
          event={event}
          principals={principals}
          traceUrlTemplate={traceUrlTemplate}
          cause={findEvent(events, event.koracausedby)}
          effects={effectsOf(event, events)}
          onOpenEvent={onOpen}
        />
      </ModalBody>
    </Modal>
  );
}
