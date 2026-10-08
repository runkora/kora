/** @tier 3 — marketplace-scoped. Interactive preview of the client boundary and its overview. */

import type { Meta, StoryObj } from '@storybook/react';
import type { PackageOverview } from '@orbit/shared/api';
import { mockApi, type MockRequest } from '@orbit/storybook/mock-api';
import { PreferencesProvider } from '../../_preferences/preferences-context';
import { PREFERENCE_DEFAULTS } from '../../_preferences/preferences.schema';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from '../_types';
import { parseBrowseParams } from '../_utils/browse-params';
import { MarketplaceShell } from './marketplace-shell';

const platformSkills: PackageOverview[] = [
  {
    slug: 'platform-engineering/skills/api-contract-diff',
    title_of_package: 'API Contract Diff',
    description: 'Diffs OpenAPI specifications and flags breaking changes.',
  },
  {
    slug: 'platform-engineering/skills/cluster-upgrade-planner',
    title_of_package: 'Cluster Upgrade Planner',
    description:
      'Plans a Kubernetes upgrade by checking version compatibility, identifying workloads that need changes, and sequencing the rollout with validation and rollback steps.',
  },
  {
    slug: 'platform-engineering/skills/react-expert',
    title_of_package: 'React Expert',
    description:
      'Reviews React components for accessibility, state handling, and clear interfaces.',
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
  ...Array.from({ length: 30 }, (_, index) => {
    const number = String(index + 1).padStart(2, '0');
    return {
      slug: `platform-engineering/skills/platform-review-${number}`,
      title_of_package: `Platform Review ${number}`,
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
        slug: 'clearroute-core/skills/architecture-review',
        title_of_package: 'Architecture Review',
        description: 'Reviews component responsibilities and dependency boundaries.',
      },
      {
        slug: 'clearroute-core/skills/code-review',
        title_of_package: 'Code Review',
        description: 'Reviews a change for correctness and maintainability.',
      },
      {
        slug: 'clearroute-core/skills/release-notes',
        title_of_package: 'Release Notes',
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
    .filter((skill) => skill.title_of_package.toLowerCase().includes(query))
    .sort((a, b) => direction * a.title_of_package.localeCompare(b.title_of_package));
  const pageCount = Math.max(1, Math.ceil(skills.length / requested.pageSize));
  const browse = {
    ...requested,
    marketplace: source.name,
    page: Math.min(requested.page, pageCount),
  };
  const offset = (browse.page - 1) * browse.pageSize;

  return {
    marketplaces: sources.map(({ name }) => ({ name })),
    requested,
    browse,
    skills: skills.slice(offset, offset + browse.pageSize),
    total: skills.length,
  };
}

const meta: Meta<typeof MarketplaceShell> = {
  title: 'App/Marketplace/Shell',
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
