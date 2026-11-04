import { Stack } from '@runkora/hades';

export default function RunLayout({ children }: { children: React.ReactNode }) {
  return (
    <Stack
      style={
        {
          '--chat-deep': 'var(--color-background)',
          '--chat-bg': 'var(--color-surface)',
          '--chat-surf': 'var(--color-surface-elevated)',
          '--chat-surf2': 'var(--color-surface-overlay)',
          '--chat-surf3':
            'color-mix(in oklch, var(--color-surface-overlay) 92%, var(--color-foreground))',
          '--chat-fg': 'var(--color-foreground)',
          '--chat-muted': 'var(--color-muted)',
          '--chat-subtle': 'var(--color-subtle)',
          '--chat-bd': 'var(--color-border)',
          '--chat-bds': 'var(--color-border-subtle)',
          '--chat-acc': 'var(--color-accent)',
          '--chat-acc-h': 'var(--color-accent-hover)',
          '--chat-acc-dim': 'var(--color-accent-subtle)',
          '--chat-acc-bdr': 'var(--color-accent-border)',
          '--chat-healthy': 'var(--color-status-healthy)',
          '--chat-degraded': 'var(--color-status-degraded)',
          '--chat-error': 'var(--color-status-error)',
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
