/** @tier 3 — marketplace-scoped. Browser-safe sample Skills shared by result stories. */

import type { Marketplace, SkillSummary } from '@orbit/shared/api';

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

/** Display names and identifiers stay distinct, matching the skills-service contract. */
export const marketplaceStoryPlatform: Marketplace = {
  name: 'Platform Engineering',
  marketplaceSlug: 'platform-engineering__skills',
  link: 'https://github.com/platform-engineering/skills',
};

export const marketplaceStoryCore: Marketplace = {
  name: 'ClearRoute Core',
  marketplaceSlug: 'clearroute__core-skills',
  link: 'https://github.com/clearroute/core-skills',
};
