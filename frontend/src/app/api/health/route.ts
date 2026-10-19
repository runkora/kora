/**
 * BFF route: GET /api/health
 *
 * Liveness/readiness probe. Reports the kora version and theme hash (null when absent).
 * An invalid theme document makes it throw, so the pod does not become ready.
 */
import 'server-only';
import { NextResponse } from 'next/server';
import { getTheme } from '@orbit/server/theme';

export async function GET() {
  const { koraVersion, theme } = await getTheme();
  return NextResponse.json({
    status: 'ok',
    version: koraVersion,
    theme: theme ? { hash: theme.hash } : null,
  });
}
