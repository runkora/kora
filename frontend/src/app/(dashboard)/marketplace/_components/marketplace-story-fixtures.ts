/** @tier 3 — marketplace-scoped. Browser-safe sample Skills shared by result stories. */

import type { Marketplace, Skill, SkillSummary } from '@orbit/shared/api';

export const marketplaceStorySkills: SkillSummary[] = [
  {
    skillSlug: 'platform-engineering__skills__api-contract-diff',
    name: 'API Contract Diff',
    description: 'Diffs OpenAPI specifications and flags breaking changes.',
  },
  {
    skillSlug: 'platform-engineering__skills__cluster-upgrade-planner',
    name: 'Cluster Upgrade Planning and Compatibility Review for Production Kubernetes Workloads',
    description:
      'Plans a Kubernetes upgrade by checking version compatibility, identifying workloads that need changes, and sequencing the rollout with validation and rollback steps. Highlights deprecated APIs and includes validation checkpoints and recovery steps for every stage of the rollout.',
  },
  {
    skillSlug: 'platform-engineering__skills__react-expert',
    name: 'React Expert',
    description:
      'Reviews React components for accessibility, state handling, and clear interfaces.',
  },
  {
    skillSlug: 'platform-engineering__skills__react-reviewer',
    name: 'React Reviewer',
    description: 'Reviews React components for accessibility and state handling.',
  },
  {
    skillSlug: 'platform-engineering__skills__reference-notes',
    name: 'Reference Notes',
    description: '',
  },
  {
    skillSlug: 'platform-engineering__skills__terraform-plan-reviewer',
    name: 'Terraform Plan Reviewer',
    description: 'Reviews infrastructure changes and highlights unexpected resource replacements.',
  },
];

/**
 * The details of a sample Skill, for the mocked `GET /api/marketplace/skill`.
 * A skillSlug that is not in marketplaceStorySkills gets a name from its last part.
 */
export function marketplaceStorySkillDetail(skillSlug: string): Skill {
  const summary = marketplaceStorySkills.find((skill) => skill.skillSlug === skillSlug) ?? {
    skillSlug,
    name: skillSlug.split('__').pop() ?? skillSlug,
    description: '',
  };
  const folder = summary.skillSlug.split('__').pop() ?? summary.skillSlug;
  return {
    ...summary,
    skill: {
      path: `skills/${folder}/SKILL.md`,
      name: 'SKILL.md',
      content: `# ${summary.name}\n\n${summary.description}`,
    },
    documents: [],
  };
}

/**
 * A sample Marketplace. The stories use the display name as the
 * marketplaceSlug too, so a story's browse values can name the Marketplace.
 */
export function marketplaceStoryMarketplace(name: string): Marketplace {
  const repo = name.toLowerCase().replace(/[^a-z0-9]+/g, '-');
  return { name, marketplaceSlug: name, link: `https://github.com/example/${repo}` };
}
