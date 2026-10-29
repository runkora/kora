import type { MockRequest, MockRoutes } from '@runkora/storybook/mock-api';
import {
  CHAT_FIELDS,
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

/** The client-side calls the modal and the wizard make while open. */
export const catalogRoutes: MockRoutes = {
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
