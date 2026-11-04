'use client';

/** @tier 3 — skills-scoped. The overview: header, Marketplace and Favourites chips, search and results. */

import { useState, useSyncExternalStore } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import {
  Alert,
  Button,
  EmptyState,
  Icon,
  Input,
  PageSectionHeader,
  Stack,
  Text,
} from '@runkora/hades';
import { FilterChip, ViewToggle, type ViewMode } from '@runkora/components';
import { APP_ROUTES } from '@runkora/lib/routing';
import { useSkillsContext } from '../_context/skills-context';
import { MIN_SEARCH_LENGTH, type SkillsLoadError } from '../_types';
import { SkillGrid } from './skill-grid';
import { SkillList } from './skill-list';
import { SkillsPagination } from './skills-pagination';
import { SkillsResultsSkeleton } from './skills-results-skeleton';

// As on the MCP servers page: phones always get cards and no toggle. The server snapshot is
// false, so the first paint is the card view.
const DESKTOP_QUERY = '(min-width: 768px)';
const subscribeDesktop = (onChange: () => void) => {
  const mq = window.matchMedia(DESKTOP_QUERY);
  mq.addEventListener('change', onChange);
  return () => mq.removeEventListener('change', onChange);
};
const getDesktop = () => window.matchMedia(DESKTOP_QUERY).matches;
const getServerDesktop = () => false;

const plural = (count: number, noun: string) => `${count} ${noun}${count === 1 ? '' : 's'}`;

export function SkillsPage() {
  const { marketplaces, browse } = useSkillsContext();
  const known = !browse.marketplace || marketplaces.some((m) => m.slug === browse.marketplace);

  return (
    <Stack gap="comfortable">
      <Stack gap="none">
        <PageSectionHeader
          title="Skills"
          description="Find a skill for a job, read what it does and see where it comes from."
        />
        {marketplaces.length > 0 && <Summary />}
      </Stack>

      {marketplaces.length === 0 ? (
        <NoMarketplaces />
      ) : !known ? (
        <EmptyState
          icon="box"
          title="Marketplace not found"
          description="No Marketplace matches this address."
          action={
            <Button asChild variant="outline" size="sm">
              <Link href={APP_ROUTES.skills.root}>Show all skills</Link>
            </Button>
          }
        />
      ) : (
        <Browse />
      )}
    </Stack>
  );
}

/** "74 skills from 3 Marketplaces", or the Marketplace count while any list is still unread. */
function Summary() {
  const { marketplaces } = useSkillsContext();
  const counts = marketplaces.map((m) => m.skillCount);
  const sources = plural(marketplaces.length, 'Marketplace');
  const text = counts.every((count) => count !== null)
    ? `${plural(
        counts.reduce<number>((sum, count) => sum + (count ?? 0), 0),
        'skill',
      )} from ${sources}`
    : sources;
  return (
    <Text size="supporting" color="muted">
      {text}
    </Text>
  );
}

function Browse() {
  const { marketplaces, browse, skills, total, status, error, favourites, selectChip } =
    useSkillsContext();
  const [preferredView, setView] = useState<ViewMode>('cards');
  const isDesktop = useSyncExternalStore(subscribeDesktop, getDesktop, getServerDesktop);
  const view: ViewMode = isDesktop ? preferredView : 'cards';
  const toggle = <ViewToggle viewMode={view} onToggle={setView} />;
  const allCounts = marketplaces.map((m) => m.skillCount);
  const chips = [
    {
      key: 'all',
      label: 'All',
      count: allCounts.every((c) => c !== null)
        ? allCounts.reduce<number>((sum, c) => sum + (c ?? 0), 0)
        : null,
      active: !browse.favourites && !browse.marketplace,
      select: () => selectChip(''),
    },
    ...marketplaces.map((m) => ({
      key: m.slug,
      label: m.repository,
      count: m.skillCount,
      active: !browse.favourites && browse.marketplace === m.slug,
      select: () => selectChip(m.slug),
    })),
    {
      key: 'favourites',
      label: 'Favourites',
      count: favourites.size,
      active: browse.favourites,
      select: () => selectChip(null),
    },
  ];
  // The search row and the table stay while a search matches nothing, to let it change.
  const searchable = total > 0 || !!browse.search;

  return (
    <>
      <Stack direction="horizontal" align="center" justify="between" gap="snug">
        <Stack direction="horizontal" align="center" gap="snug" wrap>
          {chips.map((chip) => (
            <FilterChip
              key={chip.key}
              label={chip.label}
              count={chip.count}
              active={chip.active}
              onClick={chip.select}
            />
          ))}
        </Stack>
        <Text size="caption" color="subtle">
          {plural(total, 'skill')}
        </Text>
      </Stack>

      <Failures />

      {error ? (
        <LoadErrorAlert error={error} />
      ) : status === 'loading' && skills.length === 0 ? (
        <SkillsResultsSkeleton />
      ) : !searchable ? (
        <NoSkills />
      ) : view === 'list' ? (
        <Stack gap="comfortable">
          <SkillList toolbarActions={toggle} />
          {total > 0 && <SkillsPagination />}
        </Stack>
      ) : (
        <>
          <SearchRow toggle={isDesktop && toggle} />
          {total === 0 ? (
            <NoMatches />
          ) : (
            <Stack gap="comfortable">
              <SkillGrid />
              <SkillsPagination />
            </Stack>
          )}
        </>
      )}
    </>
  );
}

function SearchRow({ toggle }: { toggle: React.ReactNode }) {
  const { searchText, setSearchText } = useSkillsContext();
  const searchLength = searchText.trim().length;

  return (
    <Stack gap="tight">
      <Stack direction="horizontal" align="center" justify="between" gap="snug">
        <Stack asChild grow maxWidth="compact">
          <Input
            placeholder="Search skills…"
            aria-label="Search skills"
            value={searchText}
            onChange={(event) => setSearchText(event.target.value)}
          />
        </Stack>
        {toggle}
      </Stack>
      {searchLength > 0 && searchLength < MIN_SEARCH_LENGTH && (
        <Text size="caption" color="subtle">
          Type at least {MIN_SEARCH_LENGTH} characters to search.
        </Text>
      )}
    </Stack>
  );
}

/** An all-Marketplaces page leaves out a Marketplace it could not read, and says so here. */
function Failures() {
  const { failures, marketplaces, retry } = useSkillsContext();
  const repository = (slug: string) => marketplaces.find((m) => m.slug === slug)?.repository;

  return failures.map((failure) => (
    <LoadErrorAlert
      key={failure.marketplace}
      error={{
        status: 502,
        message: `${repository(failure.marketplace) ?? failure.marketplace}: ${failure.message}`,
      }}
      onRetry={retry}
    />
  ));
}

/** The Marketplace list is empty, or could not load; a retry reloads the page. */
function NoMarketplaces() {
  const { error } = useSkillsContext();
  const router = useRouter();

  if (error) return <LoadErrorAlert error={error} onRetry={() => router.refresh()} />;
  return (
    <EmptyState
      icon="box"
      title="No Marketplaces available"
      description="No Marketplace sources are configured."
    />
  );
}

function LoadErrorAlert({ error, onRetry }: { error: SkillsLoadError; onRetry?: () => void }) {
  const { retry } = useSkillsContext();
  return (
    <Alert variant="error">
      <Icon name="triangle-alert" color="status-error" />
      <Stack grow>
        {error.status === 502 && (
          <Text size="caption" weight="heavy" color="status-error">
            This Marketplace is unavailable.
          </Text>
        )}
        <Text size="caption" color="status-error">
          {error.message}
        </Text>
      </Stack>
      <Button variant="outline" size="xs" onClick={onRetry ?? retry}>
        Try again
      </Button>
    </Alert>
  );
}

function NoSkills() {
  const { browse } = useSkillsContext();
  return browse.favourites ? (
    <EmptyState
      icon="heart"
      title="No favourite skills"
      description="Select the heart on a skill to keep it here."
    />
  ) : (
    <EmptyState
      icon="box"
      title={browse.marketplace ? 'No skills in this Marketplace' : 'No skills yet'}
      description={
        browse.marketplace
          ? 'This Marketplace has no skills that you can view.'
          : 'Your Marketplaces have no skills that you can view.'
      }
    />
  );
}

function NoMatches() {
  const { clearSearch } = useSkillsContext();
  return (
    <EmptyState
      icon="search"
      title="No skills match your search"
      description="Try a broader search, or clear the search."
      action={
        <Button variant="outline" size="sm" onClick={clearSearch}>
          Clear search
        </Button>
      }
    />
  );
}
