import { Stack } from '@orbit/terra';

export default function RunLayout({ children }: { children: React.ReactNode }) {
  return (
    <Stack
      style={
        {
          '--chat-deep': 'oklch(13% 0.005 248)',
          '--chat-bg': 'oklch(18% 0.005 248)',
          '--chat-surf': 'oklch(22% 0.005 248)',
          '--chat-surf2': 'oklch(27% 0.005 248)',
          '--chat-surf3': 'oklch(32% 0.005 248)',
          '--chat-fg': 'oklch(96% 0.005 248)',
          '--chat-muted': 'oklch(72% 0.005 248)',
          '--chat-subtle': 'oklch(58% 0.005 248)',
          '--chat-bd': 'oklch(52% 0.005 248)',
          '--chat-bds': 'oklch(28% 0.005 248)',
          '--chat-acc': 'oklch(72% 0.17 8)',
          '--chat-acc-h': 'oklch(82% 0.17 8)',
          '--chat-acc-dim': 'oklch(72% 0.17 8 / 0.10)',
          '--chat-acc-bdr': 'oklch(72% 0.17 8 / 0.30)',
          '--chat-healthy': '#9ce37d',
          '--chat-degraded': '#ffd046',
          '--chat-error': '#ef4444',
          height: '100dvh',
          overflow: 'hidden',
          fontSize: '13.5px',
          lineHeight: 'var(--leading-relaxed, 1.625)',
          background: 'var(--chat-bg)',
          color: 'var(--chat-fg)',
          fontFamily: 'ui-sans-serif, system-ui, -apple-system, sans-serif',
        } as React.CSSProperties
      }
    >
      {children}
    </Stack>
  );
}
