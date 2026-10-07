import 'server-only';

import type {
  GetMarketplaces,
  GetPackageOverview,
  GetPackageOverviewParams,
  PackageDetail,
  PackageOverview,
} from '@orbit/shared/api';
import {
  MARKETPLACE_FIXTURES,
  PACKAGE_DETAIL_FIXTURES,
  PACKAGE_FIXTURES_BY_MARKETPLACE,
} from './marketplace-fixtures';

// Mock Marketplace client. It serves fixtures until the Marketplace Service
// exists. The signatures already have the shape that a real client needs:
// each call takes the session access token and, for browse calls, an
// optional abort signal. The mock does not use the token.
//
// The real client must use a 10-second timeout, together with the caller's
// abort signal (golden-path checklist). Like triggers-client.ts, it should
// throw an ApiError with the upstream status for each failed response.
//
// The real client must also pass `sort` to the backend. The backend must sort
// all matching skills before it applies offset and limit, as this mock does,
// so that the order is correct across pages.

export async function fetchMarketplaces(
  accessToken: string,
  signal?: AbortSignal,
): Promise<GetMarketplaces> {
  throwIfAborted(signal);
  return { marketplaces: MARKETPLACE_FIXTURES };
}

export async function fetchSkills(
  accessToken: string,
  marketplaceName: string,
  params: GetPackageOverviewParams,
  signal?: AbortSignal,
): Promise<GetPackageOverview> {
  throwIfAborted(signal);
  const all = PACKAGE_FIXTURES_BY_MARKETPLACE[marketplaceName] ?? [];

  let matched: PackageOverview[] = all;
  if (params.search) {
    matched = [];
    all.forEach((skill) => {
      if (matchesSearch(skill, params.search!)) {
        matched.push(skill);
      }
    });
  }

  // Sort all matching skills before the page is cut, so that the order is
  // correct across pages. Sort a copy, because the fixtures are shared.
  let ordered: PackageOverview[];
  if (params.sort === 'name-asc') {
    // A to Z by skill name.
    ordered = [...matched].sort((a, b) => a.title_of_package.localeCompare(b.title_of_package));
  } else if (params.sort === 'name-desc') {
    // Z to A by skill name.
    ordered = [...matched].sort((a, b) => b.title_of_package.localeCompare(a.title_of_package));
  } else {
    // No sort requested. Keep the source order.
    ordered = matched;
  }

  const { offset, limit } = params;

  return {
    packages: ordered.slice(offset, offset + limit),
    offset,
    limit,
    total: ordered.length,
  };
}

export async function fetchSkillDetail(
  accessToken: string,
  slug: string,
): Promise<PackageDetail | null> {
  let packageDetail: PackageDetail | undefined;

  PACKAGE_DETAIL_FIXTURES.forEach((skill) => {
    if (skill.slug === slug) {
      packageDetail = skill;
    }
  });

  return packageDetail ?? null;
}

/** Stops a mock call when the caller cancels it, as a real fetch does. */
function throwIfAborted(signal: AbortSignal | undefined) {
  if (signal) {
    signal.throwIfAborted();
  }
}

function matchesSearch(skill: PackageOverview, query: string): boolean {
  const trimmed = query.trim().toLowerCase();

  if (!trimmed) {
    return true;
  }

  const name = skill.title_of_package.toLowerCase();

  return name.includes(trimmed);
}
