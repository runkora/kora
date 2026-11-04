'use client';

/** @tier 3 — skills-scoped. The result range with previous and next, under both views. */

import { Button, Icon, Stack, Text } from '@runkora/hades';
import { useSkillsContext } from '../_context/skills-context';
import { PAGE_SIZE } from '../_types';

export function SkillsPagination() {
  const { skills, offset, total, status, goToPage } = useSkillsContext();
  // The page on screen, not the one loading, so the range always matches the skills shown.
  const page = Math.floor(offset / PAGE_SIZE) + 1;
  const pageCount = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const from = skills.length === 0 ? 0 : offset + 1;
  const to = skills.length === 0 ? 0 : Math.min(total, from + skills.length - 1);
  const loading = status === 'loading';

  return (
    <Stack direction="horizontal" align="center" justify="between" gap="cozy">
      <Text size="caption" color="subtle" tabular>
        {from}–{to} of {total}
      </Text>
      <Stack direction="horizontal" align="center" gap="tight">
        <Button
          variant="ghost"
          size="icon-sm"
          disabled={loading || page <= 1}
          aria-label="Previous page"
          onClick={() => goToPage(page - 1)}
        >
          <Icon name="chevron-left" size="2xs" />
        </Button>
        <Text size="caption" color="subtle" tabular>
          Page {Math.min(page, pageCount)} of {pageCount}
        </Text>
        <Button
          variant="ghost"
          size="icon-sm"
          disabled={loading || page >= pageCount}
          aria-label="Next page"
          onClick={() => goToPage(page + 1)}
        >
          <Icon name="chevron-right" size="2xs" />
        </Button>
      </Stack>
    </Stack>
  );
}
