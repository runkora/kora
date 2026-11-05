/**
 * skills-service sample data for skills-upstream.mjs, which stands in for the live Go
 * service behind both the Server Components and the /api/skills BFF routes.
 */
import type { Marketplace, Skill, SkillSummary } from '@runkora/shared/api';

const COMMIT = '3f2a9c1e0b7d4f5a6c8e9d0f1a2b3c4d5e6f7a8b';

function marketplace(
  slug: string,
  repository: string,
  skillCount: number | null,
  origin: Marketplace['origin'] = 'external',
): Marketplace {
  return {
    slug,
    origin,
    name: repository.split('/')[1] ?? repository,
    repository,
    ref: 'main',
    commit: skillCount === null ? null : COMMIT,
    syncedAt: skillCount === null ? null : '2026-11-05T01:12:00Z',
    skillCount,
  };
}

export const TEAM = marketplace('team-skills', 'example-org/skills', 30, 'internal');
export const PARTNER = marketplace('partner-skills', 'partner-org/reference-skills', 2);
/** skills-service answers 502 for this Marketplace, as for an unavailable credential. */
export const BROKEN = marketplace('broken-skills', 'example-org/broken-skills', null);
export const UNAVAILABLE_MESSAGE = 'This Marketplace could not be loaded. Try again later.';

export const MARKETPLACES: Marketplace[] = [TEAM, PARTNER, BROKEN];

/** A skill with its detail and its supporting file bodies, which the stand-in serves one at a time. */
export interface FixtureSkill {
  summary: SkillSummary;
  detail: Skill;
  bodies: Record<string, string>;
}

function skill(source: Marketplace, path: string, title: string): FixtureSkill {
  const name = path.split('/').pop() ?? path;
  const description = `Does the ${name} job.`;
  const notes = `Notes for ${name}.`;
  return {
    summary: {
      marketplace: source.slug,
      path,
      name,
      title,
      description,
      version: null,
      tags: [],
      tools: [],
      manualOnly: false,
    },
    detail: {
      marketplace: source.slug,
      path,
      name,
      title,
      description,
      origin: source.origin,
      repository: source.repository,
      ref: source.ref,
      commit: COMMIT,
      version: null,
      tags: [],
      tools: [],
      manualOnly: false,
      content: `---\nname: ${name}\ndescription: ${description}\n---\n\n# ${title}\n\nUse ${name} when the job needs it.\n`,
      files: [{ path: 'reference/notes.md', size: notes.length }],
    },
    bodies: { 'reference/notes.md': notes },
  };
}

/** Thirty skills in one Marketplace, two in another: three pages of ten across both. */
export const SKILLS: FixtureSkill[] = [
  skill(TEAM, 'skills/pdf', 'PDF Processing Guide'),
  skill(TEAM, 'skills/docx', 'DOCX editing'),
  ...Array.from({ length: 28 }, (_, i) =>
    skill(
      TEAM,
      `skills/review-${String(i + 1).padStart(2, '0')}`,
      `Review ${String(i + 1).padStart(2, '0')}`,
    ),
  ),
  skill(PARTNER, 'tools/linter', 'Linter'),
  skill(PARTNER, 'tools/café notes', 'Café notes'),
];
