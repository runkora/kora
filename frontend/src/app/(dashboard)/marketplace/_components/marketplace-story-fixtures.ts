/** @tier 3 — marketplace-scoped. Browser-safe sample Skills shared by result stories. */

import type { PackageOverview } from '@orbit/shared/api';

export const marketplaceStorySkills: PackageOverview[] = [
  {
    slug: 'platform-engineering/skills/api-contract-diff',
    title_of_package: 'API Contract Diff',
    description: 'Diffs OpenAPI specifications and flags breaking changes.',
  },
  {
    slug: 'platform-engineering/skills/cluster-upgrade-planner',
    title_of_package:
      'Cluster Upgrade Planning and Compatibility Review for Production Kubernetes Workloads',
    description:
      'Plans a Kubernetes upgrade by checking version compatibility, identifying workloads that need changes, and sequencing the rollout with validation and rollback steps. Highlights deprecated APIs and includes validation checkpoints and recovery steps for every stage of the rollout.',
  },
  {
    slug: 'platform-engineering/skills/react-expert',
    title_of_package: 'React Expert',
    description: 'Reviews React components for accessibility, state handling, and clear interfaces.',
  },
  {
    slug: 'platform-engineering/skills/react-reviewer',
    title_of_package: 'React Reviewer',
    description: 'Reviews React components for accessibility and state handling.',
  },
  {
    slug: 'platform-engineering/skills/reference-notes',
    title_of_package: 'Reference Notes',
    description: '',
  },
  {
    slug: 'platform-engineering/skills/terraform-plan-reviewer',
    title_of_package: 'Terraform Plan Reviewer',
    description: 'Reviews infrastructure changes and highlights unexpected resource replacements.',
  },
];
