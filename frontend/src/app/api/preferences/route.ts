import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { config } from '@orbit/server/config';

const PREFS_URL = () => `${config.orbitApiUrl}/identity/preferences`;

const EMPTY_PREFS = {} as const;

export const GET = withAuth(async (_req, session) => {
  let res: Response;
  try {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 500);
    res = await fetch(PREFS_URL(), {
      headers: { Authorization: `Bearer ${session.accessToken}` },
      signal: controller.signal,
    });
    clearTimeout(timeout);
  } catch {
    // Identity service unavailable or 500 ms timeout — return hardcoded defaults.
    return NextResponse.json(EMPTY_PREFS, { status: 200 });
  }

  if (!res.ok) {
    return NextResponse.json(EMPTY_PREFS, { status: 200 });
  }

  const data: unknown = await res.json();
  return NextResponse.json(data);
});

export const PUT = withAuth(async (req: NextRequest, session) => {
  let body: string;
  try {
    body = await req.text();
  } catch {
    return NextResponse.json(
      { code: 'bad_request', message: 'Could not read request body' },
      { status: 400 },
    );
  }

  let res: Response;
  try {
    res = await fetch(PREFS_URL(), {
      method: 'PUT',
      headers: {
        Authorization: `Bearer ${session.accessToken}`,
        'Content-Type': 'application/json',
      },
      body,
    });
  } catch {
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to save preferences' },
      { status: 502 },
    );
  }

  if (!res.ok) {
    const clientStatus = res.status >= 500 ? 502 : res.status;
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to save preferences' },
      { status: clientStatus },
    );
  }

  return new NextResponse(null, { status: 204 });
});

export const DELETE = withAuth(async (_req, session) => {
  let res: Response;
  try {
    res = await fetch(PREFS_URL(), {
      method: 'DELETE',
      headers: { Authorization: `Bearer ${session.accessToken}` },
    });
  } catch {
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to delete preferences' },
      { status: 502 },
    );
  }

  if (!res.ok) {
    const clientStatus = res.status >= 500 ? 502 : res.status;
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to delete preferences' },
      { status: clientStatus },
    );
  }

  return new NextResponse(null, { status: 204 });
});
