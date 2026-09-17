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
} from '../_data/skill-fixtures';

export async function fetchMarketplaces(): Promise<GetMarketplaces> {
  return { marketplaces: MARKETPLACE_FIXTURES };
}

export async function fetchSkills(
  marketplaceName: string,
  params: GetPackageOverviewParams,
): Promise<GetPackageOverview> {
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

  const { offset, limit } = params;

  return {
    packages: matched.slice(offset, offset + limit),
    offset,
    limit,
    total: matched.length,
  };
}

export async function fetchSkillDetail(slug: string): Promise<PackageDetail | null> {
  let packageDetail: PackageDetail | undefined;

  PACKAGE_DETAIL_FIXTURES.forEach((skill) => {
    if (skill.slug === slug) {
      packageDetail = skill;
    }
  });

  return packageDetail ?? null;
}

// taken from`_components/marketplace-search.ts` (formerly `matchesQuery`),
function matchesSearch(skill: PackageOverview, query: string): boolean {
  const trimmed = query.trim().toLowerCase();

  if (!trimmed) {
    return true;
  }

  const name = skill.title_of_package.toLowerCase();

  if (trimmed.includes('*')) {
    const pattern = trimmed.split('*').map(escapeRegExp).join('.*');
    return new RegExp(`^${pattern}$`).test(name);
  }

  return name.includes(trimmed) || skill.description.toLowerCase().includes(trimmed);
}

function escapeRegExp(literal: string): string {
  return literal.replace(/[.+?^${}()|[\]\\]/g, '\\$&');
}
