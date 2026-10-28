import 'server-only';

import { config } from '@orbit/server/config';
import type { Marketplace, Skill, SkillSummary } from '@orbit/lib/marketplace';

const TIMEOUT_MS = 15_000;

async function request<T>(path: string, accessToken: string): Promise<T> {
  const res = await fetch(`${config.skillsServiceUrl}${path}`, {
    headers: {
      Authorization: `Bearer ${accessToken}`,
      'Content-Type': 'application/json',
    },
    cache: 'no-store',
    signal: AbortSignal.timeout(TIMEOUT_MS),
  });
  if (!res.ok) {
    throw new Error(`skills-service ${path} failed: ${res.status}`);
  }
  return res.json() as Promise<T>;
}

export async function listMarketplaces(accessToken: string): Promise<Marketplace[]> {
  const body = await request<{ marketplaces: Marketplace[] }>('/v1/marketplaces', accessToken);
  return body.marketplaces ?? [];
}

export async function listSkills(
  accessToken: string,
  marketplace: string,
  search?: string,
): Promise<SkillSummary[]> {
  const qs = search ? `?search=${encodeURIComponent(search)}` : '';
  const body = await request<{ skills: SkillSummary[] }>(
    `/v1/marketplaces/${encodeURIComponent(marketplace)}/skills${qs}`,
    accessToken,
  );
  return body.skills ?? [];
}

export async function getSkill(
  accessToken: string,
  marketplace: string,
  skill: string,
): Promise<Skill> {
  return request<Skill>(
    `/v1/marketplaces/${encodeURIComponent(marketplace)}/skills/${encodeURIComponent(skill)}`,
    accessToken,
  );
}
