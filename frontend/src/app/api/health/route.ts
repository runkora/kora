/**
 * BFF route: GET /api/health
 *
 * Liveness/readiness probe for Kubernetes. Calls no upstream service, so the
 * pod is ready as soon as Next.js serves requests. It reports the instance's
 * kora version (null for a build that is not from a release tag) and the loaded
 * theme's hash (null without a theme), per the white-labelling theme ADR,
 * decision 3; a bundle that fails to load makes
 * this throw, which is the point: the pod must not become ready.
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
