import 'server-only';

import type { TriggerField } from '@runkora/shared/api';
import { config } from '@runkora/server/config';
import { ApiError } from './api-error';

const UPSTREAM_TIMEOUT_MS = 10_000;

/** The part of a JSON Schema the field list reads. */
interface SchemaNode {
  type?: string;
  const?: string;
  description?: string;
  enum?: (string | number)[];
  properties?: Record<string, SchemaNode>;
  items?: SchemaNode;
}

/** Refuses an empty or dot segment, which fetch would resolve to another kora-events path. */
function typeSegment(eventType: string): string {
  if (eventType === '' || eventType === '.' || eventType === '..') {
    throw new ApiError(404, 'not_found', 'Not found.');
  }
  return encodeURIComponent(eventType);
}

/** GET /v1/events/types/{type} on kora-events: the type's JSON Schema. */
export async function fetchEventTypeSchema(
  accessToken: string,
  eventType: string,
): Promise<SchemaNode> {
  const response = await fetch(`${config.koraEventsUrl}/events/types/${typeSegment(eventType)}`, {
    headers: { Accept: 'application/json', Authorization: `Bearer ${accessToken}` },
    cache: 'no-store',
    signal: AbortSignal.timeout(UPSTREAM_TIMEOUT_MS),
  });
  if (!response.ok) {
    const err = (await response.json().catch(() => ({}))) as { code?: string; message?: string };
    throw new ApiError(response.status, err.code ?? 'UNKNOWN', err.message ?? response.statusText);
  }
  return (await response.json()) as SchemaNode;
}

/**
 * The dotted paths an event of the schema's type carries, in path order. The
 * envelope attributes are grouped as Event and the payload as Resource.
 */
export function schemaFields(schema: SchemaNode): TriggerField[] {
  const out: TriggerField[] = [];
  const walk = (node: SchemaNode, prefix: string) => {
    for (const [name, child] of Object.entries(node.properties ?? {})) {
      const path = prefix ? `${prefix}.${name}` : name;
      // An array's enum lists the values its items may take.
      const values =
        child.const !== undefined ? [child.const] : (child.enum ?? child.items?.enum)?.map(String);
      out.push({
        path,
        group: path === 'data' || path.startsWith('data.') ? 'Resource' : 'Event',
        ...(child.description !== undefined && { description: child.description }),
        ...(values !== undefined && values.length > 0 && { enum: values }),
      });
      if (child.type === 'object') walk(child, path);
    }
  };
  walk(schema, '');
  return out.sort((a, b) => a.path.localeCompare(b.path));
}
