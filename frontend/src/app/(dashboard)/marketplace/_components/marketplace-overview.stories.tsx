/** @tier 3 — marketplace-scoped. Interactive preview of the overview toolbar and Skill results. */

import type { Meta, StoryObj } from '@storybook/react';
import { expect, fireEvent, userEvent, within } from 'storybook/test';
import type { PackageOverview } from '@orbit/shared/api';
import { mockApi, type MockRequest } from '@orbit/storybook/mock-api';
import { PreferencesProvider } from '../../_preferences/preferences-context';
import { PREFERENCE_DEFAULTS } from '../../_preferences/preferences.schema';
import { MarketplaceProvider } from '../_context/marketplace-context';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from '../_types';
import { buildBrowseUrl, parseBrowseParams } from '../_utils/browse-params';
import { MarketplaceOverview } from './marketplace-overview';
import { marketplaceStorySkills } from './marketplace-story-fixtures';

const platformSkills: PackageOverview[] = [
  ...marketplaceStorySkills,
  ...Array.from({ length: 30 }, (_, index) => {
    const number = String(index + 1).padStart(2, '0');
    return {
      slug: `platform-engineering/skills/platform-review-${number}`,
      title_of_package: `Platform Review ${number}`,
      description: 'Checks a platform configuration and explains proposed changes.',
    };
  }),
];

interface StorySource {
  name: string;
  skills: PackageOverview[];
}

const defaultSource = { name: 'Platform Engineering', skills: platformSkills };
const sources: StorySource[] = [
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

function createOverview(
  requested: MarketplaceBrowseParams,
  catalogue: StorySource[] = sources,
): MarketplaceOverviewData {
  const source = catalogue.find((item) => item.name === requested.marketplace) ?? catalogue[0];
  if (!source) {
    return {
      marketplaces: [],
      requested,
      browse: { ...requested, marketplace: '', page: 1 },
      skills: [],
      total: 0,
    };
  }

  const query = requested.query.toLowerCase();
  const direction = requested.sort === 'name-desc' ? -1 : 1;
  const skills = source.skills
    .filter((skill) => skill.title_of_package.toLowerCase().includes(query))
    .sort((a, b) => direction * a.title_of_package.localeCompare(b.title_of_package, 'en'));
  const pageCount = Math.max(1, Math.ceil(skills.length / requested.pageSize));
  const browse = {
    ...requested,
    marketplace: source.name,
    page: Math.min(requested.page, pageCount),
  };
  const offset = (browse.page - 1) * browse.pageSize;

  return {
    marketplaces: catalogue.map(({ name }) => ({ name })),
    requested,
    browse,
    skills: skills.slice(offset, offset + browse.pageSize),
    total: skills.length,
  };
}

const defaultBrowse = parseBrowseParams(new URLSearchParams());
const unavailableOverview = createOverview(defaultBrowse, []);
const initialError = 'The Marketplace catalogue is temporarily unavailable.';
const refreshError = 'Could not refresh Skills.';

const meta: Meta<typeof MarketplaceOverview> = {
  title: 'App/Marketplace/Overview',
  component: MarketplaceOverview,
  tags: ['autodocs'],
  parameters: {
    layout: 'padded',
    nextjs: { appDirectory: true },
    // Isolate each Docs preview's browse URL, fetch mocks, and play setup.
    docs: { story: { inline: false, height: '900px' } },
  },
};
export default meta;
type Story = StoryObj<typeof MarketplaceOverview>;

interface OverviewStoryOptions {
  catalogue?: StorySource[];
  initialData?: MarketplaceOverviewData;
  initialError?: string | null;
  firstRequest?: 'pending' | 'error';
}

function createOverviewStory({
  catalogue = sources,
  initialData = createOverview(defaultBrowse, catalogue),
  initialError = null,
  firstRequest,
}: OverviewStoryOptions = {}): Story {
  let requestCount = 0;

  return {
    beforeEach: () => {
      requestCount = 0;
      const previousBrowse = parseBrowseParams(new URLSearchParams(window.location.search));
      const previousState = window.history.state;
      window.history.replaceState(
        previousState,
        '',
        buildBrowseUrl(window.location.pathname, window.location.search, initialData.browse),
      );
      return () => {
        window.history.replaceState(
          previousState,
          '',
          buildBrowseUrl(window.location.pathname, window.location.search, previousBrowse),
        );
      };
    },
    decorators: [
      mockApi({
        'GET /api/marketplace/skills': ({ url }: MockRequest) => {
          requestCount += 1;
          if (requestCount === 1 && firstRequest === 'pending') {
            return new Promise<never>(() => {});
          }
          if (requestCount === 1 && firstRequest === 'error') {
            return new Response(JSON.stringify({ message: refreshError }), {
              status: 503,
              headers: { 'Content-Type': 'application/json' },
            });
          }
          return { data: createOverview(parseBrowseParams(url.searchParams), catalogue) };
        },
        'PUT /api/preferences': {},
      }),
      (Story) => (
        <PreferencesProvider initialPreferences={PREFERENCE_DEFAULTS}>
          <MarketplaceProvider initialData={initialData} initialError={initialError}>
            <Story />
          </MarketplaceProvider>
        </PreferencesProvider>
      ),
    ],
  };
}

export const Default: Story = createOverviewStory();

export const ListView: Story = createOverviewStory();

export const CardView: Story = {
  ...createOverviewStory(),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const toggle = canvas.getByRole('button', { name: 'Card view' });
    await userEvent.click(toggle);
    await expect(toggle).toHaveAttribute('aria-pressed', 'true');
    await expect(await canvas.findByRole('link', { name: /API Contract Diff/ })).toBeVisible();
  },
};

export const NoMarketplaces: Story = createOverviewStory({ catalogue: [] });

export const EmptyMarketplace: Story = createOverviewStory({
  catalogue: sources.map((source) =>
    source === defaultSource ? { ...source, skills: [] } : source,
  ),
});

export const NoSearchMatches: Story = {
  ...createOverviewStory({
    initialData: createOverview({ ...defaultBrowse, query: 'unmatched-skill' }),
  }),
  parameters: {
    docs: { description: { story: 'Clear search restores the populated example catalogue.' } },
  },
};

export const InitialLoading: Story = {
  ...createOverviewStory({
    initialData: unavailableOverview,
    initialError,
    firstRequest: 'pending',
  }),
  parameters: {
    docs: { description: { story: 'The first-load retry is pending, with no previous results.' } },
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole('button', { name: 'Try again' }));
    await expect(
      await canvas.findByRole('status', { name: 'Loading the Marketplace' }),
    ).toBeVisible();
  },
};

export const InitialError: Story = {
  ...createOverviewStory({ initialData: unavailableOverview, initialError }),
  parameters: {
    docs: {
      description: { story: 'Try again loads the example catalogue after a failed first load.' },
    },
  },
};

export const LoadingWithPreviousResults: Story = {
  ...createOverviewStory({ firstRequest: 'pending' }),
  parameters: {
    docs: {
      description: {
        story:
          'A search is pending while previous results stay visible. Changing the search starts a successful request.',
      },
    },
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    // Set the full query without competing for keyboard focus between Docs iframes.
    await fireEvent.change(canvas.getByRole('textbox', { name: 'Search Skills' }), {
      target: { value: 'React' },
    });
    await expect(await canvas.findByText('Loading Skills…')).toBeVisible();
    await expect(canvas.getByText('1–24 of 36')).toBeVisible();
  },
};

export const ErrorWithPreviousResults: Story = {
  ...createOverviewStory({ firstRequest: 'error' }),
  parameters: {
    docs: {
      description: {
        story:
          'The search failed, so previous results stay visible. Try again succeeds and shows the matching Skills.',
      },
    },
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await fireEvent.change(canvas.getByRole('textbox', { name: 'Search Skills' }), {
      target: { value: 'React' },
    });
    await expect(
      await canvas.findByText(/The results below are from the last successful request/),
    ).toBeVisible();
    await expect(canvas.getByText('1–24 of 36')).toBeVisible();
  },
};
