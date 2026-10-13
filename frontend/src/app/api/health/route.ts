/**
 * BFF route: GET /api/health
 *
 * Liveness/readiness probe for Kubernetes. Calls no upstream service, so the
 * pod is ready as soon as Next.js serves requests. It reports the running Hades
 * version, the running contract version and the loaded theme's hash (null without a theme), per the
 * white-labelling theme ADR, decision 3; a bundle that fails to load makes
 * this throw, which is the point: the pod must not become ready.
 */
import 'server-only';
import { NextResponse } from 'next/server';
import { getTheme } from '@orbit/server/theme';

export async function GET() {
  const { hadesVersion, contractVersion, theme } = await getTheme();
  return NextResponse.json({
    status: 'ok',
    hadesVersion,
    contractVersion,
    theme: theme ? { hash: theme.hash } : null,
  });
}
