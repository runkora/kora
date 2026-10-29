import type { MockRequest, MockRoutes } from '@runkora/storybook/mock-api';
import type { TriggerDelivery } from '@runkora/shared/api';
import type { DeliveryRow } from '../_types';
import {
  CHAT_FIELDS,
  DELIVERIES,
  EVENT_TYPES_BY_PROVIDER,
  GITHUB_EVENT_TYPES,
  modelCatalog,
  PULL_REQUEST_FIELDS,
  routingCandidates,
  SCHEDULE_PREVIEW,
  SOURCES,
} from './triggers-mock-data';

/** The path segment after `after`, e.g. the provider in `/api/trigger-providers/slack/fields`. */
function segmentAfter(req: MockRequest, after: string): string {
  const parts = req.url.pathname.split('/');
  return parts[parts.indexOf(after) + 1] ?? '';
}

/** Each provider answers with its own catalog, so chat and webhook kinds each see their own. */
function providerEventTypes(provider: string) {
  return { data: EVENT_TYPES_BY_PROVIDER[provider] ?? GITHUB_EVENT_TYPES };
}

/**
 * A delivery as the API returns it, from the row the stories already hold. A routed decision is
 * left out: the modal's list only needs the outcome, and the routing stories seed the page.
 */
function toWireDelivery(d: DeliveryRow): TriggerDelivery {
  return {
    id: d.id,
    tenant_id: 'tnt_acme',
    kind: d.kind,
    ...(d.sourceId !== '' && { source_id: d.sourceId }),
    ...(d.triggerId !== null && { trigger_id: d.triggerId }),
    ...(d.triggerName !== null && { trigger_name: d.triggerName }),
    ...(d.eventType !== '' && { event_type: d.eventType }),
    status: d.status,
    ...(d.runId !== null && { run_id: d.runId }),
    ...(d.error !== null && { error: d.error }),
    ...(d.payloadExcerpt !== null && { payload_excerpt: d.payloadExcerpt }),
    ...(d.payloadBytes !== null && { payload_bytes: d.payloadBytes }),
    payload_purged: d.payloadPurged,
    ...(d.reason !== null && { reason: d.reason }),
    ...(d.firedBy !== null && { fired_by: d.firedBy }),
    ...(d.scheduledAt !== null && { scheduled_at: d.scheduledAt }),
    received_at: d.receivedAt,
    runs: d.runs.map((r, i) => ({
      id: `${d.id}_run${i}`,
      delivery_id: d.id,
      trigger_id: r.triggerId,
      ...(r.triggerName !== null && { trigger_name: r.triggerName }),
      ...(r.runId !== null && { run_id: r.runId }),
      status: r.status,
      ...(r.error !== null && { error: r.error }),
      ...(r.supersededBy !== undefined && { superseded_by: r.supersededBy }),
      created_at: r.createdAt ?? d.receivedAt,
    })),
    coalesce_keys: d.coalesceKeys.map((c) => ({
      trigger_id: c.triggerId,
      ...(c.key !== null && { key: c.key }),
      key_too_long: c.keyTooLong,
    })),
  };
}

/**
 * `GET /api/trigger-deliveries` over some deliveries, the way the API answers it: newest first,
 * narrowed by `source_id` and `trigger_id`, paged back with `received_before`, cut at `limit`.
 */
export function deliveryRoutes(rows: DeliveryRow[]): MockRoutes {
  return {
    'GET /api/trigger-deliveries': (req: MockRequest) => {
      const q = req.url.searchParams;
      const sourceId = q.get('source_id');
      const triggerId = q.get('trigger_id');
      const before = q.get('received_before');
      const limit = Number(q.get('limit')) || 100;
      const data = rows
        .filter((d) => sourceId === null || d.sourceId === sourceId)
        .filter((d) => triggerId === null || d.triggerId === triggerId)
        .filter((d) => before === null || Date.parse(d.receivedAt) < Date.parse(before))
        .sort((a, b) => Date.parse(b.receivedAt) - Date.parse(a.receivedAt))
        .slice(0, limit)
        .map(toWireDelivery);
      return { data };
    },
  };
}

/** The client-side calls the modal and the wizard make while open. */
export const catalogRoutes: MockRoutes = {
  ...deliveryRoutes(DELIVERIES),
  'GET /api/triggers/routing/candidates': { data: routingCandidates(4) },
  'GET /api/models': { data: modelCatalog(true) },
  'GET /api/trigger-sources/:id/event-types': (req: MockRequest) => {
    const id = segmentAfter(req, 'trigger-sources');
    return providerEventTypes(SOURCES.find((s) => s.id === id)?.provider ?? 'github');
  },
  'GET /api/trigger-providers/:provider/event-types': (req: MockRequest) =>
    providerEventTypes(segmentAfter(req, 'trigger-providers')),
  'GET /api/trigger-providers/:provider/fields': (req: MockRequest) => {
    const provider = segmentAfter(req, 'trigger-providers');
    const fields =
      EVENT_TYPES_BY_PROVIDER[provider]?.category === 'chat' ? CHAT_FIELDS : PULL_REQUEST_FIELDS;
    // A generic source has only the meta fields.
    return {
      data: {
        fields: provider === 'generic' ? fields.filter((f) => f.path.startsWith('meta.')) : fields,
      },
    };
  },
  'POST /api/triggers/schedule-preview': { data: SCHEDULE_PREVIEW },
  'POST /api/triggers': (req: MockRequest) => ({
    data: { id: 'trg_created01', ...(req.body as object) },
  }),
  // A provider that issues its own secret gets none back; the others get a generated one.
  'POST /api/trigger-sources': (req: MockRequest) => {
    const body = req.body as { name: string; provider: string };
    const chat = EVENT_TYPES_BY_PROVIDER[body.provider]?.category === 'chat';
    return {
      data: {
        id: 'src_created01',
        name: body.name,
        provider: body.provider,
        category: chat ? 'chat' : 'webhook',
        enabled: true,
        webhook_path: '/api/v1/webhooks/src_created01',
        ...(chat ? {} : { secret: 'whsec_9f2c41d8e7a0b3' }),
      },
    };
  },
  'PATCH /api/trigger-sources/:id': (req: MockRequest) => ({
    data: { id: segmentAfter(req, 'trigger-sources'), webhook_path: '/x' },
  }),
};
