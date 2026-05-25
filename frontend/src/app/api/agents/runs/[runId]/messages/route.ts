import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { MOCK_RUNS } from '../../../_mock/data';

export const POST = withAuth(
  async (req: NextRequest, _session, context?: { params: Promise<{ runId: string }> }) => {
    const { runId } = await (context?.params ?? Promise.resolve({ runId: '' }));
    const run = MOCK_RUNS.find((r) => r.id === runId);
    if (!run) {
      return NextResponse.json({ code: 'not_found', message: 'Run not found' }, { status: 404 });
    }
    const body = (await req.json()) as { content: string };
    if (!body.content?.trim()) {
      return NextResponse.json(
        { code: 'validation_error', message: 'content is required' },
        { status: 400 },
      );
    }
    // Mock: acknowledge receipt. Real SSE response arrives via events stream.
    return NextResponse.json({ ok: true, run_id: runId });
  },
);
