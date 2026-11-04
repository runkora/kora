'use client';

import { usePathname } from 'next/navigation';
import { Badge, Button, Icon, Show, Stack, Text } from '@runkora/hades';
import { ThemeToggle } from './theme-toggle';
import { useSidebarState } from './sidebar-state';

/**
 * Per-section header content: `title` renders on the left of the top bar.
 * Sections not listed fall back to a plain "Dashboard" title.
 */
interface SectionHeader {
  title: string;
}

const SECTIONS: Record<string, SectionHeader> = {
  agents: { title: 'Agents' },
  policy: { title: 'Policy' },
  'model-gateway': { title: 'Model Gateway' },
  'mcp-gateway': { title: 'MCP Servers' },
  chat: { title: 'Chat' },
};

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
