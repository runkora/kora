import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { providerFields } from '@orbit/server/triggers-client';

type Params = { provider: string };

// GET /api/trigger-providers/[provider]/fields?event_type=: one event type's fields.
export const GET = withAuth(
  async (req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { provider } = await (context?.params ?? Promise.resolve({ provider: '' }));
    const eventType = req.nextUrl.searchParams.get('event_type')?.trim() ?? '';
    if (!eventType) {
      return NextResponse.json(
        { code: 'validation_error', message: 'event_type is required' },
        { status: 400 },
      );
    }
    const fields = await providerFields(session.accessToken, provider, eventType);
    return NextResponse.json({ data: { fields } });
  },
);
