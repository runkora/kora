import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { schedulePreview } from '@orbit/server/triggers-client';

interface PreviewBody {
  cron_expression?: string;
  timezone?: string;
}

// POST /api/triggers/schedule-preview — the next few fires for a cron
// expression.
//
// The answer comes from the scheduler that will actually run the trigger, so
// what the console shows and what fires cannot drift. A malformed expression is
// the upstream's verdict to give, not this route's to guess.
export const POST = withAuth(async (req: NextRequest, session) => {
  const body = (await req.json().catch(() => ({}))) as PreviewBody;
  const cron = body.cron_expression?.trim();
  if (!cron) {
    return NextResponse.json(
      { code: 'validation_error', message: 'cron_expression is required' },
      { status: 400 },
    );
  }
  const preview = await schedulePreview(session.accessToken, {
    cron_expression: cron,
    timezone: body.timezone?.trim() || 'UTC',
  });
  return NextResponse.json({ data: preview });
});
