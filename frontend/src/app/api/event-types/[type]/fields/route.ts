import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { fetchEventTypeSchema, schemaFields } from '@runkora/server/event-types-client';

type Params = { type: string };

// GET /api/event-types/[type]/fields: the paths an event of the type carries, from its schema.
export const GET = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { type } = await (context?.params ?? Promise.resolve({ type: '' }));
    const schema = await fetchEventTypeSchema(session.accessToken, type);
    return NextResponse.json({ data: { fields: schemaFields(schema) } });
  },
);
