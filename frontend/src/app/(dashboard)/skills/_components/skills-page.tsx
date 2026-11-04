'use client';

/** @tier 3 — skills-scoped. The overview: header, Marketplace picker, search and results. */

import { useState, useSyncExternalStore } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import {
  Alert,
  Button,
  EmptyState,
  Icon,
  Input,
  Label,
  PageSectionHeader,
  Popover,
  PopoverContent,
  PopoverTrigger,
  RadioGroup,
  RadioGroupItem,
  Stack,
  Text,
} from '@runkora/hades';
import { ViewToggle, type ViewToggleProps } from '@runkora/components';
import { APP_ROUTES } from '@runkora/lib/routing';
import { useSkillsContext } from '../_context/skills-context';
import { MIN_SEARCH_LENGTH, type SkillsLoadError } from '../_types';
import { SkillGrid } from './skill-grid';
import { SkillList } from './skill-list';
import { SkillsPagination } from './skills-pagination';
import { SkillsResultsSkeleton } from './skills-results-skeleton';

type ViewMode = ViewToggleProps['viewMode'];

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

export function SkillsPage() {
  const { marketplaces, browse, skills, total, status, error } = useSkillsContext();
  const [preferredView, setView] = useState<ViewMode>('cards');
  const isDesktop = useSyncExternalStore(subscribeDesktop, getDesktop, getServerDesktop);
  const view: ViewMode = isDesktop ? preferredView : 'cards';
  const toggle = <ViewToggle viewMode={view} onToggle={setView} />;
  const showResults = !error && total > 0 && !(status === 'loading' && skills.length === 0);
  const selected = marketplaces.some((marketplace) => marketplace.slug === browse.marketplace);

  return (
    <Stack gap="comfortable">
      <PageSectionHeader title="Skills" description="Browse the skills in your Marketplaces." />

      {marketplaces.length === 0 ? (
        <NoMarketplaces />
      ) : !selected ? (
        <EmptyState
          icon="box"
          title="Marketplace not found"
          description="No Marketplace matches this address."
          action={
            <Button asChild variant="outline" size="sm">
              <Link href={APP_ROUTES.skills.root}>Open Skills</Link>
            </Button>
          }
        />
      ) : (
        <>
          <Toolbar viewToggle={showResults && isDesktop && view === 'cards' && toggle} />
          {showResults ? (
            <Stack gap="comfortable">
              {view === 'list' ? <SkillList toolbarActions={toggle} /> : <SkillGrid />}
              <SkillsPagination />
            </Stack>
          ) : (
            <NoResults />
          )}
        </>
      )}
    </Stack>
  );
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

function LoadErrorAlert({ error, onRetry }: { error: SkillsLoadError; onRetry: () => void }) {
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
      <Button variant="outline" size="xs" onClick={onRetry}>
        Try again
      </Button>
    </Alert>
  );
}

function Toolbar({ viewToggle }: { viewToggle: React.ReactNode }) {
  const { marketplaces, browse, searchText, setSearchText, selectMarketplace } = useSkillsContext();
  const [pickerOpen, setPickerOpen] = useState(false);
  const current = marketplaces.find((marketplace) => marketplace.slug === browse.marketplace);
  const searchLength = searchText.trim().length;

  return (
    <Stack gap="tight">
      <Stack direction={{ base: 'vertical', lg: 'horizontal' }} align="center" gap="snug">
        <Stack grow minWidth="none">
          <Input
            placeholder="Search skills"
            value={searchText}
            onChange={(event) => setSearchText(event.target.value)}
            aria-label="Search skills"
          />
        </Stack>

        <Popover open={pickerOpen} onOpenChange={setPickerOpen}>
          <PopoverTrigger asChild>
            <Button variant="outline" aria-label={`Marketplace: ${current?.name}`}>
              <Icon name="filter" size="2xs" />
              {current?.name}
            </Button>
          </PopoverTrigger>
          <PopoverContent align="end">
            <RadioGroup
              value={browse.marketplace}
              onValueChange={(slug) => {
                selectMarketplace(slug);
                setPickerOpen(false);
              }}
            >
              <Stack gap="snug">
                {marketplaces.map((marketplace) => (
                  <Label key={marketplace.slug}>
                    <Stack direction="horizontal" align="center" gap="snug">
                      <RadioGroupItem value={marketplace.slug} />
                      <Stack gap="none">
                        <Text as="span" size="supporting">
                          {marketplace.name}
                        </Text>
                        <Text as="span" size="caption" color="subtle" mono>
                          {marketplace.repository}
                        </Text>
                      </Stack>
                    </Stack>
                  </Label>
                ))}
              </Stack>
            </RadioGroup>
          </PopoverContent>
        </Popover>

        {viewToggle}
      </Stack>

      {searchLength > 0 && searchLength < MIN_SEARCH_LENGTH && (
        <Text size="caption" color="subtle">
          Type at least {MIN_SEARCH_LENGTH} characters to search.
        </Text>
      )}
    </Stack>
  );
}

/** The error, loading or empty state shown in place of the results. */
function NoResults() {
  const { browse, skills, status, error, retry, clearSearch } = useSkillsContext();

  if (error) return <LoadErrorAlert error={error} onRetry={retry} />;
  if (status === 'loading' && skills.length === 0) return <SkillsResultsSkeleton />;

  if (browse.search) {
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

  return (
    <EmptyState
      icon="box"
      title="No skills in this Marketplace"
      description="This Marketplace has no skills that you can view."
    />
  );
}
