/** @tier 3 — marketplace-scoped. Interactive preview of the client boundary and its overview. */

import type { Meta, StoryObj } from '@storybook/react';
import type { SkillSummary } from '@orbit/shared/api';
import { mockApi, type MockRequest } from '@orbit/storybook/mock-api';
import { PreferencesProvider } from '../../_preferences/preferences-context';
import { PREFERENCE_DEFAULTS } from '../../_preferences/preferences.schema';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from '../_types';
import { parseBrowseParams } from '../_utils/browse-params';
import { MarketplaceShell } from './marketplace-shell';
import {
  marketplaceStoryMarketplace,
  marketplaceStorySkillDetail,
} from './marketplace-story-fixtures';

const platformSkills: SkillSummary[] = [
  {
    skillSlug: 'platform-engineering/skills/api-contract-diff',
    name: 'API Contract Diff',
    description: 'Diffs OpenAPI specifications and flags breaking changes.',
  },
  {
    skillSlug: 'platform-engineering/skills/cluster-upgrade-planner',
    name: 'Cluster Upgrade Planner',
    description:
      'Plans a Kubernetes upgrade by checking version compatibility, identifying workloads that need changes, and sequencing the rollout with validation and rollback steps.',
  },
  {
    skillSlug: 'platform-engineering/skills/react-expert',
    name: 'React Expert',
    description:
      'Reviews React components for accessibility, state handling, and clear interfaces.',
  },
  {
    skillSlug: 'platform-engineering/skills/react-reviewer',
    name: 'React Reviewer',
    description: 'Reviews React components for accessibility and state handling.',
  },
  {
    skillSlug: 'platform-engineering/skills/reference-notes',
    name: 'Reference Notes',
    description: '',
  },
  {
    skillSlug: 'platform-engineering/skills/terraform-plan-reviewer',
    name: 'Terraform Plan Reviewer',
    description: 'Reviews infrastructure changes and highlights unexpected resource replacements.',
  },
  ...Array.from({ length: 30 }, (_, index) => {
    const number = String(index + 1).padStart(2, '0');
    return {
      skillSlug: `platform-engineering/skills/platform-review-${number}`,
      name: `Platform Review ${number}`,
      description: 'Checks a platform configuration and explains proposed changes.',
    };
  }),
];

const defaultSource = { name: 'Platform Engineering', skills: platformSkills };
const sources = [
  defaultSource,
  {
    name: 'ClearRoute Core',
    skills: [
      {
        skillSlug: 'clearroute-core/skills/architecture-review',
        name: 'Architecture Review',
        description: 'Reviews component responsibilities and dependency boundaries.',
      },
      {
        skillSlug: 'clearroute-core/skills/code-review',
        name: 'Code Review',
        description: 'Reviews a change for correctness and maintainability.',
      },
      {
        skillSlug: 'clearroute-core/skills/release-notes',
        name: 'Release Notes',
        description: 'Summarizes changes for a release.',
      },
    ],
  },
];

function createOverview(requested: MarketplaceBrowseParams): MarketplaceOverviewData {
  const source = sources.find((item) => item.name === requested.marketplace) ?? defaultSource;
  const query = requested.query.toLowerCase();
  const direction = requested.sort === 'name-desc' ? -1 : 1;
  const skills = source.skills
    .filter((skill) => skill.name.toLowerCase().includes(query))
    .sort((a, b) => direction * a.name.localeCompare(b.name));
  const pageCount = Math.max(1, Math.ceil(skills.length / requested.pageSize));
  const browse = {
    ...requested,
    marketplace: source.name,
    page: Math.min(requested.page, pageCount),
  };
  const offset = (browse.page - 1) * browse.pageSize;

  return {
    marketplaces: sources.map(({ name }) => marketplaceStoryMarketplace(name)),
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
      'GET /api/marketplace/skill': ({ url }: MockRequest) => ({
        data: marketplaceStorySkillDetail(url.searchParams.get('skill') ?? ''),
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
