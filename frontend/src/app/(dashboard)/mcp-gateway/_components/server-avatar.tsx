/** @tier 3 — mcp-gateway-scoped. The square initial tile for an MCP server. */
'use client';

import { Text } from '@orbit/hades';

const SIZES = { xs: 22, sm: 28, md: 38, lg: 56 } as const;
const FONT = { xs: 10, sm: 13, md: 15, lg: 22 } as const;

interface ServerAvatarProps {
  name: string;
  /** The first-party kora server gets the accent tile; everything else neutral. */
  accent?: boolean;
  size?: keyof typeof SIZES;
}

export function ServerAvatar({ name, accent = false, size = 'md' }: ServerAvatarProps) {
  const px = SIZES[size];
  return (
    <Text
      as="span"
      style={{
        width: px,
        height: px,
        flexShrink: 0,
        borderRadius: 'var(--radius-md)',
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        fontFamily: 'var(--font-mono)',
        fontWeight: 700,
        fontSize: FONT[size],
        lineHeight: 1,
        background: accent ? 'var(--color-accent-muted)' : 'var(--color-surface-overlay)',
        border: `1px solid ${accent ? 'var(--color-accent-border)' : 'var(--color-border-subtle)'}`,
        color: accent ? 'var(--color-accent)' : 'var(--color-muted)',
      }}
    >
      {(name[0] ?? '?').toUpperCase()}
    </Text>
  );
}
