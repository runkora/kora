'use client';

/**
 * @tier 2 — kora-domain-aware: run source kinds and providers. Links to what started a run, as
 * compact chips. Used by: the agent detail page's run rows, the run view's top bar.
 */

import {
  Badge,
  Icon,
  Popover,
  PopoverContent,
  PopoverTrigger,
  Stack,
  Text,
  type IconName,
} from '@runkora/hades';
import type { RunSource } from '@runkora/shared/api';
import { isSafeUrl } from '@runkora/lib/safe-url';

const PROVIDER_ICON: Record<NonNullable<RunSource['provider']>, IconName> = {
  github: 'github',
  slack: 'hash',
  alertmanager: 'prometheus',
};

function sourceIcon(source: RunSource): IconName {
  if (source.kind === 'repository') return 'folder';
  if (source.kind === 'branch') return 'git-branch';
  return (source.provider && PROVIDER_ICON[source.provider]) ?? 'zap';
}

const sourceKey = (s: RunSource) => `${s.kind}:${s.url}`;

// A chip sits inside a clickable row; its clicks must not also open the run.
const stop = (e: React.SyntheticEvent) => e.stopPropagation();

function SourceChip({ source }: { source: RunSource }) {
  return (
    <Badge asChild interactive variant="secondary" size="sm" shape="pill">
      <a
        href={source.url}
        target="_blank"
        rel="noopener noreferrer"
        aria-label={`${source.label} (opens in a new tab)`}
        title={source.label}
        style={{ maxWidth: '10rem', textDecoration: 'none' }}
      >
        <Icon name={sourceIcon(source)} size="xs" color="muted" shrink />
        <Text as="span" size="caption" color="foreground" truncate>
          {source.label}
        </Text>
      </a>
    </Badge>
  );
}

function MoreChip({ sources }: { sources: RunSource[] }) {
  return (
    <Popover>
      <Badge asChild interactive variant="dim" size="sm" shape="pill">
        <PopoverTrigger
          aria-label={`${sources.length} more ${sources.length === 1 ? 'link' : 'links'}`}
        >
          +{sources.length}
        </PopoverTrigger>
      </Badge>
      <PopoverContent align="end">
        <Stack gap="none" maxWidth="compact">
          {sources.map((source) => (
            <a
              key={sourceKey(source)}
              href={source.url}
              target="_blank"
              rel="noopener noreferrer"
              aria-label={`${source.label} (opens in a new tab)`}
              className="min-w-0 rounded-sm no-underline hover:bg-surface-overlay focus-visible:outline-2 focus-visible:outline-accent"
            >
              <Stack
                direction="horizontal"
                align="center"
                gap="snug"
                minWidth="none"
                paddingBlock="tight"
                paddingInline="snug"
              >
                <Icon name={sourceIcon(source)} size="xs" color="muted" shrink />
                <Text as="span" size="supporting" truncate title={source.label}>
                  {source.label}
                </Text>
                <Icon name="external-link" size="xs" color="subtle" shrink />
              </Stack>
            </a>
          ))}
        </Stack>
      </PopoverContent>
    </Popover>
  );
}

export interface RunSourceChipsProps {
  sources?: RunSource[] | null;
  /** How many delivery chips show before the rest fold into "+N". */
  max?: number;
}

/**
 * One chip per delivery (kind `trigger`), up to `max`; repository, branch and further links
 * fold into a "+N" chip that lists them. A run with no delivery link shows its first link.
 */
export function RunSourceChips({ sources, max = 2 }: RunSourceChipsProps) {
  // Only http(s) URLs are linkable; anything else (e.g. javascript:) is dropped.
  const safe = (sources ?? []).filter((s) => isSafeUrl(s.url));
  if (safe.length === 0) return null;
  const deliveries = safe.filter((s) => s.kind === 'trigger');
  const shown = (deliveries.length > 0 ? deliveries : safe).slice(0, max);
  const folded = safe.filter((s) => !shown.includes(s));
  return (
    <Stack
      direction="horizontal"
      align="center"
      justify="end"
      gap="tight"
      minWidth="none"
      onClick={stop}
      onKeyDown={stop}
    >
      {shown.map((source) => (
        <SourceChip key={sourceKey(source)} source={source} />
      ))}
      {folded.length > 0 && <MoreChip sources={folded} />}
    </Stack>
  );
}
