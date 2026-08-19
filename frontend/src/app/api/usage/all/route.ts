import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { fetchAllUsageFromGateway } from '../../../(dashboard)/model-gateway/_services/usage-server';
import {
  fetchAllTenantUsers,
  listPATsFromIdentity,
} from '../../../(dashboard)/model-gateway/_services/tokens-server';
import type { PrincipalIdentityMap } from '../../../(dashboard)/model-gateway/_types';

// GET /api/usage/all — admin tenant-wide inference log.
//
// Proxies GET /v1/usage/all on the inference gateway (gated by
// platform:analytics:read at the edge) under the caller's own token, so the
// tenant is always forced from the verified JWT — this route adds no
// authorization of its own. In parallel it resolves the tenant user list into
// a principal→identity map so the client can render the Principal column
// without a per-row lookup. principal_id / principal_type are pass-through
// narrowing filters; they can only narrow within the caller's tenant.
export const GET = withAuth(async (req: NextRequest, session) => {
  const sp = req.nextUrl.searchParams;
  const key = sp.get('key') ?? undefined;
  const cursor = sp.get('cursor') ?? undefined;
  const principalId = sp.get('principal_id') ?? undefined;
  const principalType = sp.get('principal_type') ?? undefined;
  const status = sp.get('status') ?? undefined;
  const modelId = sp.get('model_id') ?? undefined;
  const from = sp.get('from') ?? undefined;
  const to = sp.get('to') ?? undefined;

  const [usageResult, patsResult, usersResult] = await Promise.all([
    fetchAllUsageFromGateway(session.accessToken, {
      tokenId: key,
      cursor,
      principalId,
      principalType,
      status,
      modelId,
      from,
      to,
    }),
    listPATsFromIdentity(session.accessToken),
    fetchAllTenantUsers(session.accessToken),
  ]);

  if (!usageResult.ok) {
    console.error(
      `[GET /api/usage/all] upstream error ${usageResult.status}: ${usageResult.message}`,
    );
    const clientStatus = usageResult.status >= 500 ? 502 : usageResult.status;
    return NextResponse.json(
      {
        code: 'upstream_error',
        message: 'Failed to load tenant usage logs from inference gateway',
      },
      { status: clientStatus },
    );
  }

  // Build the principal → identity attribution map from the tenant user list.
  // Only HUMAN principals resolve here; agents/PATs fall back to their raw ID
  // (or SPIFFE slug) client-side via resolvePrincipalDisplay.
  const identities: PrincipalIdentityMap = {};
  if (usersResult.ok) {
    for (const u of usersResult.users) {
      identities[u.id] = { email: u.email ?? null, display_name: u.display_name ?? null };
    }
  }

  return NextResponse.json({
    records: usageResult.records,
    hasNextPage: usageResult.hasNextPage,
    nextCursor: usageResult.nextCursor,
    limit: usageResult.limit,
    pats: patsResult.ok ? patsResult.tokens : [],
    identities,
  });
});
