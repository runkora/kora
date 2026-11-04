/**
 * skills-service sample data for skills-upstream.mjs, which stands in for the live Go
 * service behind both the Server Components and the /api/skills BFF route.
 */
import type { Marketplace, Skill } from '@runkora/shared/api';

export const TEAM: Marketplace = {
  slug: 'team-skills',
  name: 'skills',
  repository: 'example-org/skills',
};
export const PARTNER: Marketplace = {
  slug: 'partner-skills',
  name: 'reference-skills',
  repository: 'partner-org/reference-skills',
};
/** skills-service answers 502 for this Marketplace, as for an unavailable credential. */
export const BROKEN: Marketplace = {
  slug: 'broken-skills',
  name: 'broken-skills',
  repository: 'example-org/broken-skills',
};
export const UNAVAILABLE_MESSAGE = 'The GitHub App credential for this Marketplace is unavailable.';

export const MARKETPLACES: Marketplace[] = [TEAM, PARTNER, BROKEN];

function skill(repository: string, path: string, description: string): Skill {
  const name = path.split('/').pop() ?? path;
  return {
    path,
    name,
    description,
    repository,
    files: [
      { path: 'SKILL.md', content: `# ${name}\n\n${description}` },
      { path: 'reference/notes.md', content: `Notes for ${name}.` },
    ],
  };
}

/** Thirty skills, so the default page size of 25 gives two pages. */
export const SKILLS: Record<string, Skill[]> = {
  [TEAM.slug]: [
    skill(TEAM.repository, 'skills/pdf', 'Read and write PDF files.'),
    skill(TEAM.repository, 'skills/docx', 'Edit Word documents.'),
    ...Array.from({ length: 28 }, (_, i) =>
      skill(
        TEAM.repository,
        `skills/review-${String(i + 1).padStart(2, '0')}`,
        'Reviews a change.',
      ),
    ),
  ],
  [PARTNER.slug]: [skill(PARTNER.repository, 'tools/linter', 'Lints configuration files.')],
};
