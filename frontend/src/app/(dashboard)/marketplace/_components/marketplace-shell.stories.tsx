/** @tier 3 — marketplace-scoped. Interactive preview of the client boundary and its overview. */

import type { Meta, StoryObj } from '@storybook/react';
import type { SkillSummary } from '@orbit/shared/api';
import { mockApi, type MockRequest } from '@orbit/storybook/mock-api';
import { PreferencesProvider } from '../../_preferences/preferences-context';
import { PREFERENCE_DEFAULTS } from '../../_preferences/preferences.schema';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from '../_types';
import { parseBrowseParams } from '../_utils/browse-params';
import { MarketplaceShell } from './marketplace-shell';
import { marketplaceStoryPlatform, marketplaceStoryCore } from './marketplace-story-fixtures';

const platformSkills: SkillSummary[] = [
  {
    skillSlug: 'platform-engineering__skills__api-contract-diff',
    skillName: 'api-contract-diff',
    name: 'API Contract Diff',
    description: 'Diffs OpenAPI specifications and flags breaking changes.',
  },
  {
    skillSlug: 'platform-engineering__skills__cluster-upgrade-planner',
    skillName: 'cluster-upgrade-planner',
    name: 'Cluster Upgrade Planner',
    description:
      'Plans a Kubernetes upgrade by checking version compatibility, identifying workloads that need changes, and sequencing the rollout with validation and rollback steps.',
  },
  {
    skillSlug: 'platform-engineering__skills__react-expert',
    skillName: 'react-expert',
    name: 'React Expert',
    description:
      'Reviews React components for accessibility, state handling, and clear interfaces.',
  },
  {
    skillSlug: 'platform-engineering__skills__react-reviewer',
    skillName: 'react-reviewer',
    name: 'React Reviewer',
    description: 'Reviews React components for accessibility and state handling.',
  },
  {
    skillSlug: 'platform-engineering__skills__reference-notes',
    skillName: 'reference-notes',
    name: 'Reference Notes',
    description: '',
  },
  {
    skillSlug: 'platform-engineering__skills__terraform-plan-reviewer',
    skillName: 'terraform-plan-reviewer',
    name: 'Terraform Plan Reviewer',
    description: 'Reviews infrastructure changes and highlights unexpected resource replacements.',
  },
  ...Array.from({ length: 30 }, (_, index) => {
    const number = String(index + 1).padStart(2, '0');
    return {
      skillSlug: `platform-engineering__skills__platform-review-${number}`,
      skillName: `platform-review-${number}`,
      name: `Platform Review ${number}`,
      description: 'Checks a platform configuration and explains proposed changes.',
    };
  }),
];

const defaultSource = { ...marketplaceStoryPlatform, skills: platformSkills };
const sources = [
  defaultSource,
  {
    ...marketplaceStoryCore,
    skills: [
      {
        skillSlug: 'clearroute__core-skills__architecture-review',
        skillName: 'architecture-review',
        name: 'Architecture Review',
        description: 'Reviews component responsibilities and dependency boundaries.',
      },
      {
        skillSlug: 'clearroute__core-skills__code-review',
        skillName: 'code-review',
        name: 'Code Review',
        description: 'Reviews a change for correctness and maintainability.',
      },
      {
        skillSlug: 'clearroute__core-skills__release-notes',
        skillName: 'release-notes',
        name: 'Release Notes',
        description: 'Summarizes changes for a release.',
      },
    ],
  },
];

function createOverview(requested: MarketplaceBrowseParams): MarketplaceOverviewData {
  const source =
    sources.find((item) => item.marketplaceSlug === requested.marketplace) ?? defaultSource;
  const query = requested.query.toLowerCase();
  const direction = requested.sort === 'name-desc' ? -1 : 1;
  const skills = source.skills
    .filter((skill) => skill.name.toLowerCase().includes(query))
    .sort((a, b) => direction * a.name.localeCompare(b.name));
  const pageCount = Math.max(1, Math.ceil(skills.length / requested.pageSize));
  const browse = {
    ...requested,
    marketplace: source.marketplaceSlug,
    page: Math.min(requested.page, pageCount),
  };
  const offset = (browse.page - 1) * browse.pageSize;

  return {
    marketplaces: sources.map(({ name, marketplaceSlug, link }) => ({
      name,
      marketplaceSlug,
      link,
    })),
    requested,
    browse,
    skills: skills.slice(offset, offset + browse.pageSize),
    total: skills.length,
  };
}

const meta: Meta<typeof MarketplaceShell> = {
  title: 'Pages/Marketplace/Shell',
  component: MarketplaceShell,
  tags: ['autodocs'],
  parameters: {
    layout: 'padded',
    nextjs: { appDirectory: true },
  },
  decorators: [
    mockApi({
      'GET /api/marketplace/skills': ({ url }: MockRequest) => ({
        data: createOverview(parseBrowseParams(url.searchParams)),
      }),
      'PUT /api/preferences': {},
    }),
    (Story) => (
      <PreferencesProvider initialPreferences={PREFERENCE_DEFAULTS}>
        <Story />
      </PreferencesProvider>
    ),
  ],
};
export default meta;
type Story = StoryObj<typeof MarketplaceShell>;

export const Default: Story = {
  args: {
    initialOverview: createOverview(parseBrowseParams(new URLSearchParams())),
    initialError: null,
  },
};
