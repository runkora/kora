'use client';

import { Suspense, useSyncExternalStore } from 'react';

import { usePathname, useSearchParams } from 'next/navigation';
import Link from 'next/link';
import { Button, Icon, Indicator, Separator, Show, Stack, Text } from '@orbit/terra';
import { ThemeToggle } from '@orbit/components';
import { useSidebarState } from './sidebar-state';

/**
 * Per-section header content: `title` renders on the left of the top bar.
 * Sections not listed fall back to a plain "Dashboard" title.
 */
interface SectionHeader {
  title: string;
  tag?: string;
  // showEndpoint renders the governed-endpoint note; the host is derived from the
  // dashboard origin at render (not hardcoded) — see mcpGatewayHost.
  showEndpoint?: boolean;
  // keepQueryOnBack makes the detail Back button keep the query of the detail
  // URL. Set it only for a section whose detail links carry the list state in
  // the query, so that Back returns to the same list results.
  keepQueryOnBack?: boolean;
}

const SECTIONS: Record<string, SectionHeader> = {
  agents: { title: 'Agents' },
  policy: { title: 'Policy' },
  'model-gateway': { title: 'Model Gateway' },
  'mcp-gateway': { title: 'MCP Servers' },
  chat: { title: 'Chat' },
};

// useSyncExternalStore plumbing for reading the browser host: the host never
// changes, so the store has no subscription; the server snapshot is empty.
const subscribeNoop = () => () => {};
const getServerHost = () => '';

export function DashboardHeader() {
  const pathname = usePathname();
  const { toggle, isOpen } = useSidebarState();

  const segments = pathname.split('/').filter(Boolean);
  const section = segments[0] ?? '';
  // The dashboard home ('/') and any unlisted section have no SECTIONS entry —
  // fall back to a plain "Dashboard" title rather than dereferencing undefined
  // (header.title below is not optional-chained, and would crash the whole page).
  const header = SECTIONS[section] ?? { title: 'Dashboard' };
  const isDetail = segments.length > 1;
  const detailLabel = segments[segments.length - 1];

  return (
    <Stack
      direction="horizontal"
      align="center"
      justify="between"
      height="full"
      grow
      paddingInline="comfortable"
    >
      <Stack direction="horizontal" align="center" gap="snug">
        {/* Hamburger — only visible on mobile (hidden at md+) */}
        <Show below="md">
          <Button
            variant="ghost"
            size="icon"
            aria-label={isOpen ? 'Close navigation' : 'Open navigation'}
            aria-expanded={isOpen}
            onClick={toggle}
          >
            <Icon name="menu" />
          </Button>
        </Show>

        {isDetail && header ? (
          // Detail: breadcrumb trail (section › item).
          <Stack direction="horizontal" align="center" style={{ gap: 10 }}>
            {header.keepQueryOnBack === true ? (
              // useSearchParams requires a Suspense boundary. The fallback is
              // the plain Back button to the section root.
              <Suspense fallback={<BackButton href={`/${section}`} />}>
                <BackButtonWithQuery section={section} />
              </Suspense>
            ) : (
              <BackButton href={`/${section}`} />
            )}
            <Text as="span" style={{ fontSize: 13, color: 'var(--color-subtle)' }}>
              {header.title}
            </Text>
            <Icon name="chevron-right" size="xs" color="subtle" />
            <Text
              as="span"
              mono
              style={{ fontSize: 12, fontWeight: 600, color: 'var(--color-foreground)' }}
            >
              {detailLabel}
            </Text>
          </Stack>
        ) : header ? (
          // Section root: title.
          <Stack direction="horizontal" align="center" style={{ gap: 8 }}>
            <Text as="span" style={{ fontSize: 13, fontWeight: 600 }}>
              {header.title}
            </Text>
          </Stack>
        ) : (
          <Text as="span" style={{ fontSize: 13, fontWeight: 600 }}>
            Dashboard
          </Text>
        )}
      </Stack>

      <Stack direction="horizontal" align="center" gap="snug">
        <Badge variant="outline" size="sm">
          {process.env.NEXT_PUBLIC_KORA_VERSION ?? 'dev'}
        </Badge>
        <ThemeToggle />
      </Stack>
    </Stack>
  );
}

/** The detail Back button. */
function BackButton({ href }: { href: string }) {
  return (
    <Button
      variant="outline"
      size="sm"
      asChild
      style={{ height: 28, padding: '0 9px 0 7px', gap: 5, fontSize: 12 }}
    >
      <Link href={href}>
        <Icon name="chevron-left" size="xs" />
        Back
      </Link>
    </Button>
  );
}

/**
 * The detail Back button for a section with `keepQueryOnBack`. It keeps the
 * query of the detail URL, so that Back returns to the same list results.
 * The link always stays in the section.
 */
function BackButtonWithQuery({ section }: { section: string }) {
  const searchParams = useSearchParams();
  const query = searchParams.toString();

  let href: string;
  if (query) {
    // The detail URL carries the list state. Return to the same results.
    href = `/${section}?${query}`;
  } else {
    // The detail URL has no list state. Open the section root.
    href = `/${section}`;
  }
  return <BackButton href={href} />;
}
