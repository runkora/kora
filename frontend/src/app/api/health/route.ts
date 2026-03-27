/**
 * BFF route: GET /api/health
 *
 * Lightweight liveness/readiness probe for Kubernetes.
 * Returns 200 immediately without calling any upstream service so that
 * the pod is considered ready as soon as Next.js is serving requests.
 */
import 'server-only';
import { NextResponse } from 'next/server';

export async function GET() {
  return NextResponse.json({ status: 'ok' });
}
