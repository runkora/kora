'use client';

/** @tier 3 — marketplace-scoped. Overview: header, toolbar, request notices, and Skill results. */

import { useState } from 'react';
import {
  Alert,
  Button,
  Icon,
  Input,
  Label,
  NativeSelect,
  PageSectionHeader,
  Popover,
  PopoverContent,
  PopoverTrigger,
  RadioGroup,
  RadioGroupItem,
  Stack,
  Text,
} from '@orbit/terra';
import type { SkillSort } from '@orbit/shared/api';
import { EmptyState, ViewToggle } from '@orbit/components';
import type { ViewToggleProps } from '@orbit/components';
import { useMarketplaceContext } from '../_context/marketplace-context';
import { MIN_SEARCH_LENGTH, SORT_OPTIONS } from '../_types';
import { toResultState } from '../_utils/browse-params';
import { MarketplacePagination } from './marketplace-pagination';
import { MarketplaceResultsSkeleton } from './marketplace-results-skeleton';
import { MarketplaceSkillGrid } from './marketplace-skill-grid';
import { MarketplaceSkillList } from './marketplace-skill-list';

type ViewMode = ViewToggleProps['viewMode'];

/** The label of each sort option in the toolbar. */
const SORT_LABELS: Record<SkillSort, string> = {
  'name-asc': 'Sort: A–Z',
  'name-desc': 'Sort: Z–A',
};

export function MarketplaceOverview() {
  const { marketplaces } = useMarketplaceContext();
  const [view, setView] = useState<ViewMode>('list');

  return (
    <Stack gap="comfortable">
      <PageSectionHeader title="Skills" description="Browse and review approved Skills." />

      {marketplaces.length === 0 ? (
        <BlockingState />
      ) : (
        <>
          <Toolbar view={view} onViewChange={setView} />
          <RequestNotice />
          <Results view={view} />
        </>
      )}
    </Stack>
  );
}

/** Shown when no usable Marketplace data exists: an error, a load, or no Marketplaces. */
function BlockingState() {
  const { status, error, retry } = useMarketplaceContext();

  if (status === 'error') {
    let message: string;
    if (error) {
      // The request returned an explanation.
      message = error;
    } else {
      // No explanation.
      message = 'The request failed.';
    }
    return (
      <Alert variant="error">
        <Icon name="triangle-alert" color="status-error" />
        <Stack grow>
          <Text size="caption" color="status-error">
            Could not load the Marketplace. {message}
          </Text>
        </Stack>
        <Button variant="outline" size="xs" onClick={retry}>
          Try again
        </Button>
      </Alert>
    );
  }

  if (status === 'loading') {
    // A retry after a failed first load. The skeleton stands in for the
    // toolbar and the results until the Marketplace list arrives.
    return (
      <Stack gap="comfortable" role="status" aria-label="Loading the Marketplace">
        <MarketplaceResultsSkeleton />
      </Stack>
    );
  }

  return (
    <EmptyState
      icon="box"
      title="No Marketplaces available"
      description="No Marketplace sources are configured."
    />
  );
}

function Toolbar({
  view,
  onViewChange,
}: {
  view: ViewMode;
  onViewChange: (view: ViewMode) => void;
}) {
  const { marketplaces, browse, searchText, setSearchText, selectMarketplace, setSort } =
    useMarketplaceContext();
  const [pickerOpen, setPickerOpen] = useState(false);

  function handleMarketplaceChange(name: string) {
    selectMarketplace(name);
    setPickerOpen(false);
  }

  function handleSortChange(value: string) {
    const option = SORT_OPTIONS.find((sort) => sort === value);
    if (option) {
      // One of the sort options. Any other value keeps the current order.
      setSort(option);
    }
  }

  const searchLength = searchText.trim().length;
  const searchTooShort = searchLength > 0 && searchLength < MIN_SEARCH_LENGTH;

  return (
    <Stack gap="tight">
      <Stack direction={{ base: 'vertical', lg: 'horizontal' }} align="center" gap="snug">
        <Stack grow style={{ minWidth: 0 }}>
          <Input
            placeholder="Search Skills by name or description"
            value={searchText}
            onChange={(event) => setSearchText(event.target.value)}
            aria-label="Search Skills"
          />
        </Stack>

        <Stack shrink>
          <Popover open={pickerOpen} onOpenChange={setPickerOpen}>
            <PopoverTrigger asChild>
              <Button variant="outline" aria-label={`Marketplace: ${browse.marketplace}`}>
                <Icon name="filter" size="2xs" />
                {browse.marketplace}
              </Button>
            </PopoverTrigger>
            <PopoverContent align="end" style={{ width: 240 }}>
              <RadioGroup value={browse.marketplace} onValueChange={handleMarketplaceChange}>
                <Stack gap="snug">
                  {marketplaces.map((marketplace) => (
                    <Label key={marketplace.name}>
                      <Stack direction="horizontal" align="center" gap="snug">
                        <RadioGroupItem value={marketplace.name} />
                        <Text as="span" size="supporting">
                          {marketplace.name}
                        </Text>
                      </Stack>
                    </Label>
                  ))}
                </Stack>
              </RadioGroup>
            </PopoverContent>
          </Popover>
        </Stack>

        <Stack shrink>
          <NativeSelect
            value={browse.sort}
            onChange={(event) => handleSortChange(event.target.value)}
            aria-label="Sort skills"
          >
            {SORT_OPTIONS.map((sort) => (
              <option key={sort} value={sort}>
                {SORT_LABELS[sort]}
              </option>
            ))}
          </NativeSelect>
        </Stack>

        <ViewToggle viewMode={view} onToggle={onViewChange} />
      </Stack>

      {searchTooShort && (
        <Text size="caption" color="subtle" role="status">
          Type at least {MIN_SEARCH_LENGTH} characters to search.
        </Text>
      )}
    </Stack>
  );
}

/** Non-blocking notices above the results: a refresh in progress, or a failed refresh. */
function RequestNotice() {
  const { status, error, retry } = useMarketplaceContext();

  if (status === 'loading') {
    return (
      <Alert variant="info" role="status" aria-live="polite">
        <Icon name="loader" />
        <Text size="caption">Loading Skills…</Text>
      </Alert>
    );
  }

  if (status === 'error') {
    let message: string;
    if (error) {
      // The request returned an explanation.
      message = error;
    } else {
      // No explanation.
      message = 'The request failed.';
    }
    return (
      <Alert variant="error">
        <Icon name="triangle-alert" color="status-error" />
        <Stack grow>
          <Text size="caption" color="status-error">
            {message} The results below are from the last successful request.
          </Text>
        </Stack>
        <Button variant="outline" size="xs" onClick={retry}>
          Try again
        </Button>
      </Alert>
    );
  }

  return null;
}

function Results({ view }: { view: ViewMode }) {
  const { browse, total, clearSearch } = useMarketplaceContext();
  const resultState = toResultState(total, browse.query);

  if (resultState === 'configured_but_empty') {
    return (
      <EmptyState
        icon="box"
        title="No Skills in this Marketplace"
        description="This Marketplace has no Skills that you can view."
      />
    );
  }

  if (resultState === 'no_matches') {
    return (
      <EmptyState
        icon="search"
        title="No Skills match your search"
        description="Try a broader search term, or clear the search."
        action={
          <Button variant="outline" size="sm" onClick={clearSearch}>
            Clear search
          </Button>
        }
      />
    );
  }

  return (
    <Stack gap="comfortable">
      {view === 'list' ? <MarketplaceSkillList /> : <MarketplaceSkillGrid />}
      <MarketplacePagination />
    </Stack>
  );
}
