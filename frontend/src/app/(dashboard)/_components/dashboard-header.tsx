'use client';

import { useSyncExternalStore } from 'react';

import { usePathname } from 'next/navigation';
import { Button, Icon, Show, Stack, Text } from '@orbit/terra';
import { ThemeToggle } from './theme-toggle';
import { useSidebarState } from './sidebar-state';
import { mcpGatewayHost } from '@orbit/lib/mcp';

/**
 * Per-section header content. `title` + `tag` render on the left of the top bar;
 * `endpoint` fills the otherwise-dead right slot with a governed-endpoint note.
 * Sections not listed fall back to a plain "Dashboard" title.
 */
interface SectionHeader {
  title: string;
  tag?: string;
  // showEndpoint renders the governed-endpoint note; the host is derived from the
  // dashboard origin at render (not hardcoded) — see mcpGatewayHost.
  showEndpoint?: boolean;
}

const SECTIONS: Record<string, SectionHeader> = {
  agents: { title: 'Agents' },
  policy: { title: 'Policy' },
  'model-gateway': { title: 'Model Gateway' },
  'mcp-gateway': { title: 'MCP Servers', tag: 'tool-gateway', showEndpoint: true },
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

  // Derived from the dashboard origin. useSyncExternalStore reads window on the
  // client and '' on the server with no hydration mismatch (window isn't
  // available during SSR); the note simply doesn't render until the client host.
  const host = useSyncExternalStore(subscribeNoop, mcpGatewayHost, getServerHost);
  const endpoint = header?.showEndpoint ? host : '';

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
          // Section root: title + service tag.
          <Stack direction="horizontal" align="center" style={{ gap: 8 }}>
            <Text as="span" style={{ fontSize: 13, fontWeight: 600 }}>
              {header.title}
            </Text>
            {header.tag ? (
              <Text
                as="span"
                mono
                style={{
                  fontSize: 11,
                  fontWeight: 500,
                  color: 'var(--color-subtle)',
                  marginLeft: 2,
                }}
              >
                {header.tag}
              </Text>
            ) : null}
          </Stack>
        ) : (
          <Text as="span" style={{ fontSize: 13, fontWeight: 600 }}>
            Dashboard
          </Text>
        )}
      </Stack>

      <Stack direction="horizontal" align="center" gap="snug">
        {!isDetail && endpoint ? (
          <Text as="span" style={{ fontSize: 12, color: 'var(--color-subtle)' }}>
            Governed endpoint ·{' '}
            <Text as="span" mono style={{ fontSize: 12, color: 'var(--color-muted)' }}>
              {endpoint}
            </Text>
          </Text>
        ) : null}
        <ThemeToggle />
      </Stack>
    </Stack>
  );
}
