/**
 * BFF route: GET /api/identity/health
 *
 * Proxies the identity service health check server-side so the browser never
 * communicates with the Go API directly.
 */
import 'server-only';
import { NextResponse } from 'next/server';
import { orbitClient, ApiError } from '@orbit/server/client';
import type { HealthResponse } from '@orbit/shared/api';

export async function GET() {
  try {
    const data = await orbitClient.get<HealthResponse>('/identity/health');
    return NextResponse.json({ data });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json(
        { error: { code: err.code, message: err.message } },
        { status: err.status },
      );
    }
    const isTimeout = err instanceof DOMException && err.name === 'TimeoutError';
    console.error(`[api/identity/health] ${isTimeout ? 'timeout' : 'unexpected error'}:`, err);
    return NextResponse.json(
      {
        error: {
          code: isTimeout ? 'BACKEND_TIMEOUT' : 'BACKEND_UNREACHABLE',
          message: isTimeout ? 'Backend timed out' : 'Backend unreachable',
        },
      },
      { status: 502 },
    );
  }
}
