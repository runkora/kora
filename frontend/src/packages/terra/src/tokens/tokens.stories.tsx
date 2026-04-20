import type { Meta, StoryObj } from '@storybook/react';
import React from 'react';
import { Heading, Text, Code } from '../components/typography/typography';

/**
 * Terra Design Tokens
 *
 * All swatches read live from CSS custom properties so they automatically
 * reflect any change made to tokens.css — no manual syncing required.
 */
const meta: Meta = {
  title: 'Terra/Foundation/Tokens',
  parameters: {
    layout: 'padded',
    backgrounds: { default: 'orbit' },
  },
};

export default meta;
type Story = StoryObj;

// ─── Primitives ───────────────────────────────────────────────────────────────

interface SwatchProps {
  /** CSS custom property name, e.g. `--color-background` */
  cssVar: string;
  /** Human-readable label */
  label: string;
  /** Describes when to reach for this token */
  detail?: string;
  /** Tailwind utility class hint */
  utility?: string;
}

function Swatch({ cssVar, label, detail, utility }: SwatchProps) {
  // Swatch card — one row per token
  return (
    <div className="flex items-center gap-4 py-1.5 px-1.5">
      <div
        className="h-12 w-12 shrink-0 rounded-md ring-1 ring-inset ring-white/10"
        style={{ backgroundColor: `var(${cssVar})` }}
      />{' '}
      {/* Colour lane — square filled with the live CSS custom property value */}
      <div className="flex flex-col gap-0.5 min-w-0">
        {' '}
        {/* Text lane — labeled token metadata */}
        <Text size="supporting" weight="emphasis">
          {label}
        </Text>
        <div className="flex min-w-0 font-mono">
          <Text size="caption" color="subtle" className="shrink-0">CSS:&nbsp;</Text>
          <Text size="caption" color="muted" className="truncate">{cssVar}</Text>
        </div>
        {utility && (
          <div className="flex min-w-0 font-mono">
            <Text size="caption" color="subtle" className="shrink-0">Tailwind:&nbsp;</Text>
            <Text size="caption" color="muted" className="truncate">{utility}</Text>
          </div>
        )}
        {detail && (
          <div className="flex min-w-0">
            <Text size="caption" color="subtle" className="shrink-0">Context:&nbsp;</Text>
            <Text size="caption" color="muted" className="truncate">{detail}</Text>
          </div>
        )}
      </div>
    </div>
  );
}

interface RadiusSwatchProps {
  cssVar: string;
  label: string;
  detail: string;
}

function RadiusSwatch({ cssVar, label, detail }: RadiusSwatchProps) {
  return (
    <div className="flex flex-col gap-2 min-w-0">
      <div className="flex items-end justify-center h-20 bg-surface-elevated p-3">
        <div
          className="h-12 w-12 bg-accent/20 border border-accent/40"
          style={{ borderRadius: `var(${cssVar})` }}
        />
      </div>
      <div className="space-y-0.5">
        <Text size="supporting" weight="emphasis">
          {label}
        </Text>
        <Code variant="bare">{cssVar}</Code>
        <Text size="caption" color="subtle">
          {detail}
        </Text>
      </div>
    </div>
  );
}

interface TokenGroupProps {
  title: string;
  description?: string;
  children: React.ReactNode;
}

function TokenGroup({ title, description, children }: TokenGroupProps) {
  return (
    <section className="space-y-1">
      <div className="mb-2">
        <Heading variant="label" className="text-base">
          {title}
        </Heading>
        {description && (
          <Text size="supporting" color="muted" className="mt-0.5">
            {description}
          </Text>
        )}
      </div>
      <div className="flex flex-col">{children}</div>
    </section>
  );
}

function Divider() {
  return <hr className="border-border-subtle" />;
}

// ─── Stories ──────────────────────────────────────────────────────────────────

/** Semantic colour tokens — the vocabulary every component speaks. */
export const Colors: Story = {
  render: () => (
    <div className="space-y-8 p-2">
      <TokenGroup
        title="Backgrounds"
        description="Depth without shadows — lightness progression from darkest to lightest surface."
      >
        <Swatch
          cssVar="--color-background"
          label="Background"
          utility="bg-background"
          detail="Page canvas"
        />
        <Swatch
          cssVar="--color-surface"
          label="Surface"
          utility="bg-surface"
          detail="Cards, panels"
        />
        <Swatch
          cssVar="--color-surface-elevated"
          label="Surface Elevated"
          utility="bg-surface-elevated"
          detail="Dropdowns, popovers"
        />
        <Swatch
          cssVar="--color-surface-overlay"
          label="Surface Overlay"
          utility="bg-surface-overlay"
          detail="Tooltips, modals"
        />
      </TokenGroup>

      <Divider />

      <TokenGroup
        title="Foregrounds"
        description="Text hierarchy from primary through muted to subtle."
      >
        <Swatch
          cssVar="--color-foreground"
          label="Foreground"
          utility="text-foreground"
          detail="Primary text"
        />
        <Swatch cssVar="--color-muted" label="Muted" utility="text-muted" detail="Secondary text" />
        <Swatch
          cssVar="--color-subtle"
          label="Subtle"
          utility="text-subtle"
          detail="Placeholder, meta"
        />
      </TokenGroup>

      <Divider />

      <TokenGroup
        title="Accent"
        description="Electric cyan — reads as a signal pulse. Used for interactive elements and focus states."
      >
        <Swatch
          cssVar="--color-accent"
          label="Accent"
          utility="bg-accent"
          detail="Default interactive"
        />
        <Swatch
          cssVar="--color-accent-hover"
          label="Accent Hover"
          utility="bg-accent-hover"
          detail="Hover / active state"
        />
        <Swatch
          cssVar="--color-accent-foreground"
          label="Accent Foreground"
          utility="text-accent-foreground"
          detail="Text on accent bg"
        />
      </TokenGroup>

      <Divider />

      <TokenGroup title="Borders" description="Structural separators at three intensities.">
        <Swatch
          cssVar="--color-border"
          label="Border"
          utility="border-border"
          detail="Default dividers"
        />
        <Swatch
          cssVar="--color-border-subtle"
          label="Border Subtle"
          utility="border-border-subtle"
          detail="Low-contrast dividers"
        />
        <Swatch
          cssVar="--color-border-accent"
          label="Border Accent"
          utility="border-border-accent"
          detail="Focus rings, highlights"
        />
      </TokenGroup>

      <Divider />

      <TokenGroup
        title="Status Signals"
        description="Operational state colours — the nervous system's signal vocabulary."
      >
        <Swatch cssVar="--color-status-healthy" label="Healthy" utility="text-status-healthy" />
        <Swatch cssVar="--color-status-degraded" label="Degraded" utility="text-status-degraded" />
        <Swatch cssVar="--color-status-error" label="Error" utility="text-status-error" />
        <Swatch cssVar="--color-status-idle" label="Idle" utility="text-status-idle" />
        <Swatch cssVar="--color-status-pending" label="Pending" utility="text-status-pending" />
      </TokenGroup>
    </div>
  ),
};

/** The raw palette — not for direct use in components. Semantic tokens reference these. */
export const RawPalette: Story = {
  name: 'Raw Palette',
  render: () => (
    <div className="space-y-8 p-2">
      <TokenGroup
        title="Neutral Ramp"
        description="12-step neutral scale from black to near-white. Semantic tokens reference these — use those in components, not the raw vars."
      >
        <Swatch cssVar="--terra-raw-black" label="Black" detail="#070a0f" />
        <Swatch cssVar="--terra-raw-950" label="950" detail="#0c1017" />
        <Swatch cssVar="--terra-raw-900" label="900" detail="#111827" />
        <Swatch cssVar="--terra-raw-800" label="800" detail="#1a2333" />
        <Swatch cssVar="--terra-raw-700" label="700" detail="#243044" />
        <Swatch cssVar="--terra-raw-600" label="600" detail="#2e3d52" />
        <Swatch cssVar="--terra-raw-500" label="500" detail="#4a5c72" />
        <Swatch cssVar="--terra-raw-400" label="400" detail="#6b7f96" />
        <Swatch cssVar="--terra-raw-300" label="300" detail="#8fa3b8" />
        <Swatch cssVar="--terra-raw-200" label="200" detail="#b8c8d8" />
        <Swatch cssVar="--terra-raw-100" label="100" detail="#e2eaf2" />
        <Swatch cssVar="--terra-raw-white" label="White" detail="#f0f5fa" />
      </TokenGroup>

      <Divider />

      <TokenGroup title="Accent Ramp" description="Electric cyan — signal pulse palette.">
        <Swatch cssVar="--terra-raw-accent-600" label="Accent 600" detail="#0a6ebd" />
        <Swatch cssVar="--terra-raw-accent-500" label="Accent 500" detail="#0e8fe6" />
        <Swatch cssVar="--terra-raw-accent-400" label="Accent 400" detail="#38b6ff" />
        <Swatch cssVar="--terra-raw-accent-300" label="Accent 300" detail="#7dd3fc" />
        <Swatch cssVar="--terra-raw-accent-fg" label="Accent FG" detail="#0c1017" />
      </TokenGroup>

      <Divider />

      <TokenGroup
        title="Status Raw"
        description="Unmapped status colours — prefer semantic tokens (--color-status-*)."
      >
        <Swatch cssVar="--terra-raw-healthy" label="Healthy" detail="#22c55e" />
        <Swatch cssVar="--terra-raw-degraded" label="Degraded" detail="#f59e0b" />
        <Swatch cssVar="--terra-raw-error" label="Error" detail="#ef4444" />
        <Swatch cssVar="--terra-raw-idle" label="Idle" detail="#6b7f96" />
        <Swatch cssVar="--terra-raw-pending" label="Pending" detail="#38b6ff" />
      </TokenGroup>
    </div>
  ),
};

/** Border radius scale. */
export const Radii: Story = {
  render: () => (
    <div className="space-y-6 p-2">
      <TokenGroup
        title="Border Radii"
        description="Three-step radius scale. All components draw from these tokens via Tailwind utilities."
      >
        <RadiusSwatch cssVar="--radius-sm" label="Radius SM" detail="4px — badges, tags" />
        <RadiusSwatch cssVar="--radius-md" label="Radius MD" detail="8px — inputs, buttons" />
        <RadiusSwatch cssVar="--radius-lg" label="Radius LG" detail="12px — cards, panels" />
      </TokenGroup>
    </div>
  ),
};

/** Typography families. */
export const Typography: Story = {
  render: () => (
    <div className="space-y-8 p-2">
      <section className="space-y-3">
        <div>
          <Heading variant="label" className="text-base">
            Font Families
          </Heading>
          <Text size="supporting" color="muted" className="mt-0.5">
            Geist is preferred; system fallbacks ensure consistent rendering everywhere.
          </Text>
        </div>
        <div className="space-y-6">
          <div className="space-y-1">
            <Text size="caption" color="subtle" className="font-mono">
              --font-sans · font-sans · Geist Sans → ui-sans-serif → system-ui
            </Text>
            <Text size="lead" style={{ fontFamily: 'var(--font-sans)' }}>
              The quick brown fox jumps over the lazy dog
            </Text>
            <Text size="supporting" color="muted" style={{ fontFamily: 'var(--font-sans)' }}>
              ABCDEFGHIJKLMNOPQRSTUVWXYZ abcdefghijklmnopqrstuvwxyz 0123456789
            </Text>
          </div>

          <Divider />

          <div className="space-y-1">
            <Text size="caption" color="subtle" className="font-mono">
              --font-mono · font-mono · Geist Mono → Cascadia Code → ui-monospace
            </Text>
            <Text size="lead" style={{ fontFamily: 'var(--font-mono)' }}>
              The quick brown fox jumps over the lazy dog
            </Text>
            <Text size="supporting" color="muted" style={{ fontFamily: 'var(--font-mono)' }}>
              agent.id = &quot;agt_x7k2m&quot;; status = 0x1A; latency = 42ms;
            </Text>
          </div>
        </div>
      </section>
    </div>
  ),
};
