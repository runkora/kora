'use client';

/** @tier 3 — marketplace-scoped. Client boundary: Marketplace picker, search, and view. */

import { useRef, useState } from 'react';
import { usePathname, useRouter, useSearchParams } from 'next/navigation';
import {
  Button,
  ButtonGroup,
  Heading,
  Icon,
  Input,
  Label,
  Popover,
  PopoverContent,
  PopoverTrigger,
  RadioGroup,
  RadioGroupItem,
  Stack,
  Text,
} from '@orbit/terra';
import { EmptyState } from '@orbit/components';
import type { Marketplace, PackageOverview } from '@orbit/shared/api';
import { MarketplacePage } from './marketplace-page';
import { MarketplacePagination } from './marketplace-pagination';
import { SkillList } from './marketplace-skill-list';


type ViewMode = 'list' | 'card';

interface MarketplaceShellProps {
  marketplaces: Marketplace[];
  selectedMarketplace: string;
  query: string;
  currentPage: number;
  pageSize: number;
  skills: PackageOverview[];
  total: number;
}

export function MarketplaceShell({
  marketplaces,
  selectedMarketplace,
  query,
  currentPage,
  pageSize,
  skills,
  total,
}: MarketplaceShellProps) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [view, setView] = useState<ViewMode>('list');
  const [pickerOpen, setPickerOpen] = useState(false);
  const searchTimeout = useRef<ReturnType<typeof setTimeout> | null>(null);

  function navigate(overrides: Record<string, string | null>) {
    const params = new URLSearchParams(searchParams);
    Object.entries(overrides).forEach(([key, value]) => {
      if (value === null) {
        params.delete(key);
      } else {
        params.set(key, value);
      }
    });
    router.replace(`${pathname}?${params.toString()}`);
  }

  function handleSearchChange(value: string) {
    if (searchTimeout.current) {
      clearTimeout(searchTimeout.current);
    }
    searchTimeout.current = setTimeout(() => {
      navigate({ query: value || null, page: '1' });
    }, 300);
  }

  function handleMarketplaceChange(name: string) {
    navigate({ marketplace: name, page: '1' });
    setPickerOpen(false);
  }

  function clearSearch() {
    navigate({ query: null, page: '1' });
  }

  return (
    <Stack gap="comfortable">
      <Stack gap="tight">
        <Heading variant="title">Skills</Heading>
        <Text color="muted">Browse and review approved Skills.</Text>
      </Stack>

      {/* Toolbar */}
      <Stack direction={{ base: 'vertical', lg: 'horizontal' }} align="center" gap="snug">
        <Stack grow style={{ minWidth: 0 }}>
          <Input
            placeholder="Search Skills by name or description"
            defaultValue={query}
            onChange={(event) => handleSearchChange(event.target.value)}
            aria-label="Search Skills"
          />
        </Stack>

        <Stack style={{ flexShrink: 0 }}>
          <Popover open={pickerOpen} onOpenChange={setPickerOpen}>
            <PopoverTrigger asChild>
              <Button variant="outline" aria-label="Choose Marketplace">
                <Stack direction="horizontal" align="center" gap="snug">
                  <Icon name="filter" size="2xs" />
                  <Text as="span" size="supporting">
                    {selectedMarketplace || 'Select a Marketplace'}
                  </Text>
                </Stack>
              </Button>
            </PopoverTrigger>
            <PopoverContent align="end" style={{ width: 240 }}>
              <RadioGroup value={selectedMarketplace} onValueChange={handleMarketplaceChange}>
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

        <ButtonGroup aria-label="View">
          <Button
            variant={view === 'list' ? 'secondary' : 'ghost'}
            size="sm"
            onClick={() => setView('list')}
            aria-label="List view"
            aria-pressed={view === 'list'}
          >
            <Icon name="table" size="sm" />
          </Button>
          <Button
            variant={view === 'card' ? 'secondary' : 'ghost'}
            size="sm"
            onClick={() => setView('card')}
            aria-label="Card view"
            aria-pressed={view === 'card'}
          >
            <Icon name="layout-grid" size="sm" />
          </Button>
        </ButtonGroup>
      </Stack>

      {selectedMarketplace === '' ? (
        <EmptyState
          icon="filter"
          title="Select a Marketplace"
          description="Select a Marketplace to view Skills."
        />
      ) : total === 0 ? (
        <EmptyState
          icon="search"
          title="No skills match these filters"
          description="Try a broader search term, or clear the search."
          action={
            <Button variant="outline" size="sm" onClick={clearSearch}>
              Clear search
            </Button>
          }
        />
      ) : view === 'list' ? (
        <Stack gap="comfortable">
          <SkillList skills={skills} />
          <MarketplacePagination total={total} page={currentPage} pageSize={pageSize} />
        </Stack>
      ) : (
        <Stack gap="comfortable">
          <MarketplacePage skills={skills} />
          <MarketplacePagination total={total} page={currentPage} pageSize={pageSize} />
        </Stack>
      )}
    </Stack>
  );
}
