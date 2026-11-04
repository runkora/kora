/** @tier 3 — skills-scoped. Sample skills-service data shared by the Skills stories and tests. */

import type { Marketplace, MarketplaceSkillsPage, Skill, SkillSummary } from '@runkora/shared/api';
import type { SkillsOverview } from '../_types';

export const TEAM_MARKETPLACE: Marketplace = {
  slug: 'team-skills',
  name: 'skills',
  repository: 'example-org/skills',
};

export const PARTNER_MARKETPLACE: Marketplace = {
  slug: 'partner-skills',
  name: 'reference-skills',
  repository: 'partner-org/reference-skills',
};

export const STORY_MARKETPLACES = [TEAM_MARKETPLACE, PARTNER_MARKETPLACE];

export const STORY_SKILLS: SkillSummary[] = [
  {
    path: 'skills/api-contract-diff',
    name: 'api-contract-diff',
    description: 'Diffs OpenAPI specifications and flags breaking changes.',
  },
  { path: 'skills/docx', name: 'docx', description: 'Edits Word documents.' },
  { path: 'skills/pdf', name: 'pdf', description: 'Reads and writes PDF files.' },
];

/** A first page of 3 skills out of 30 in total, so the page controls show several pages. */
export function storyOverview(overrides: Partial<SkillsOverview> = {}): SkillsOverview {
  return {
    marketplaces: STORY_MARKETPLACES,
    browse: { marketplace: TEAM_MARKETPLACE.slug, search: '', page: 1 },
    skills: STORY_SKILLS,
    total: 30,
    ...overrides,
  };
}

export function storyPage(overrides: Partial<MarketplaceSkillsPage> = {}): MarketplaceSkillsPage {
  return {
    marketplace: TEAM_MARKETPLACE,
    skills: STORY_SKILLS,
    total: 30,
    offset: 0,
    limit: 25,
    ...overrides,
  };
}

export const STORY_SKILL: Skill = {
  path: 'skills/pdf',
  name: 'pdf',
  description: 'Reads and writes PDF files.',
  repository: TEAM_MARKETPLACE.repository,
  content: '# pdf\n\nRead the PDF, then answer from its text.',
  files: [
    { path: 'reference/forms.md', size: 2048 },
    { path: 'scripts/extract.py', size: 913 },
  ],
};
