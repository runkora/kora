/**
 * Icon
 *
 * The Terra icon primitive. Every icon in the product must come through
 * this component — direct imports from `lucide-react` are banned by ESLint.
 *
 * Two usage modes:
 *   • Bare  — inline inside Buttons, NavItems, and flex rows.
 *   • Container — a rounded box for feature icons, empty states, and card corners.
 *
 * All size and colour come from typed props. `className` is intentionally
 * not exposed — if a prop doesn't exist yet, open a Terra PR.
 */

import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Icon } from './icon';
import type { IconContainerVariant, IconName, IconSize } from './icon';

// ─── Meta ─────────────────────────────────────────────────────────────────────

const meta: Meta<typeof Icon> = {
  title: 'Terra/Atoms/Icon',
  component: Icon,
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#070a0f' }],
    },
  },
  tags: ['autodocs'],
  argTypes: {
    name: { control: 'select' },
    size: {
      control: 'select',
      options: ['2xs', 'xs', 'sm', 'md', 'lg', 'xl'] satisfies IconSize[],
    },
    color: {
      control: 'select',
      options: [
        'default',
        'accent',
        'muted',
        'subtle',
        'rose',
        'status-healthy',
        'status-error',
        'status-degraded',
      ],
    },
    container: { control: 'boolean' },
    containerVariant: {
      control: 'select',
      options: ['accent', 'surface', 'muted'] satisfies IconContainerVariant[],
    },
    filled: { control: 'boolean' },
    label: { control: 'text' },
    className: { table: { disable: true } },
  },
};

export default meta;
type Story = StoryObj<typeof Icon>;

// ─── Helpers ──────────────────────────────────────────────────────────────────

/** Labelled demo cell used inside grid rows. */
function Cell({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-2">
      {children}
      <Text size="micro" color="subtle" mono>
        {label}
      </Text>
    </div>
  );
}

// ─── Stories ──────────────────────────────────────────────────────────────────

/**
 * Default interactive story — use the Controls panel to explore every prop.
 */
export const Default: Story = {
  args: {
    name: 'bot',
    size: 'md',
  },
};

/**
 * Every icon in the curated registry rendered at `md` size. Use this story
 * to verify a newly added icon, audit coverage, and confirm nothing is broken
 * after a Lucide version bump.
 */
export const AllIcons: Story = {
  name: 'All Icons',
  render: () => {
    const icons: IconName[] = [
      'activity',
      'alert-circle',
      'arrow-down',
      'arrow-left',
      'arrow-right',
      'arrow-up',
      'arrow-up-down',
      'bar-chart',
      'bell',
      'bot',
      'check',
      'check-circle',
      'chevron-down',
      'chevron-right',
      'chevron-up',
      'circle-alert',
      'circle-check',
      'circle-dot',
      'circle-x',
      'clipboard',
      'clock',
      'close',
      'code-2',
      'copy',
      'cpu',
      'database',
      'dollar-sign',
      'edit',
      'ellipsis',
      'external-link',
      'eye',
      'eye-off',
      'file-check',
      'file-text',
      'filter',
      'flask',
      'grip-vertical',
      'hard-drive',
      'heart',
      'info',
      'key',
      'layers',
      'layout-dashboard',
      'layout-grid',
      'library',
      'line-chart',
      'list',
      'loader',
      'lock',
      'log-out',
      'menu',
      'minus',
      'more-horizontal',
      'more-vertical',
      'network',
      'panel-left',
      'plus',
      'refresh',
      'refresh-cw',
      'route',
      'scroll-text',
      'search',
      'send',
      'server',
      'settings',
      'shield',
      'shield-check',
      'shield-off',
      'sparkles',
      'square-terminal',
      'terminal',
      'thumbs-down',
      'thumbs-up',
      'trash',
      'trending-up',
      'triangle-alert',
      'user',
      'user-check',
      'users',
      'wrench',
      'x-circle',
      'zap',
    ];

    return (
      <div className="grid grid-cols-10 gap-x-6 gap-y-4 p-4">
        {icons.map((name) => (
          <Cell key={name} label={name}>
            <Icon name={name} size="md" />
          </Cell>
        ))}
      </div>
    );
  },
};

/**
 * The six size tokens from `2xs` (14 px) to `xl` (32 px).
 * Sizes are named by purpose, not by pixel count.
 */
export const Sizes: Story = {
  render: () => (
    <Stack direction="horizontal" gap="comfortable" align="center">
      {(['2xs', 'xs', 'sm', 'md', 'lg', 'xl'] satisfies IconSize[]).map((size) => (
        <Cell key={size} label={size}>
          <Icon name="shield" size={size} />
        </Cell>
      ))}
    </Stack>
  ),
};

/**
 * All eight semantic colour tokens for bare (non-container) icons.
 * When `container` is true, colour is controlled by `containerVariant` instead.
 */
export const Colors: Story = {
  render: () => (
    <Stack direction="horizontal" gap="comfortable" align="center">
      <Cell label="default">
        <Icon name="shield" size="md" color="default" />
      </Cell>
      <Cell label="accent">
        <Icon name="shield" size="md" color="accent" />
      </Cell>
      <Cell label="muted">
        <Icon name="shield" size="md" color="muted" />
      </Cell>
      <Cell label="subtle">
        <Icon name="shield" size="md" color="subtle" />
      </Cell>
      <Cell label="rose">
        <Icon name="shield" size="md" color="rose" />
      </Cell>
      <Cell label="status-healthy">
        <Icon name="circle-check" size="md" color="status-healthy" />
      </Cell>
      <Cell label="status-error">
        <Icon name="circle-x" size="md" color="status-error" />
      </Cell>
      <Cell label="status-degraded">
        <Icon name="triangle-alert" size="md" color="status-degraded" />
      </Cell>
    </Stack>
  ),
};

/**
 * The `filled` prop applies `fill-current` — useful for icons like `heart`
 * that have a filled visual variant meaningful in context (liked, active).
 */
export const Filled: Story = {
  render: () => (
    <Stack direction="horizontal" gap="comfortable" align="center">
      <Cell label="outline (default)">
        <Icon name="heart" size="lg" color="rose" />
      </Cell>
      <Cell label="filled">
        <Icon name="heart" size="lg" color="rose" filled />
      </Cell>
    </Stack>
  ),
};

/**
 * Container mode wraps the icon in a proportional rounded box. The container
 * size is automatically one step larger than the icon size to provide visual
 * breathing room. Three container variants map to surface/accent/muted tokens.
 */
export const Container: Story = {
  render: () => (
    <Stack direction="vertical" gap="comfortable">
      <Stack direction="horizontal" gap="comfortable" align="center">
        {(['accent', 'surface', 'muted'] satisfies IconContainerVariant[]).map((v) => (
          <Cell key={v} label={v}>
            <Icon name="bot" size="md" container containerVariant={v} />
          </Cell>
        ))}
      </Stack>
      <Stack direction="horizontal" gap="comfortable" align="center">
        {(['2xs', 'xs', 'sm', 'md', 'lg', 'xl'] satisfies IconSize[]).map((size) => (
          <Cell key={size} label={size}>
            <Icon name="sparkles" size={size} container containerVariant="accent" />
          </Cell>
        ))}
      </Stack>
    </Stack>
  ),
};

/**
 * Accessible icon — passing `label` attaches `role="img"` + `aria-label` so
 * the icon conveys meaning to screen readers. Omitting `label` keeps the icon
 * `aria-hidden` (decorative).
 */
export const AccessibleLabel: Story = {
  name: 'Accessible Label',
  render: () => (
    <Stack direction="horizontal" gap="comfortable" align="center">
      <Cell label="aria-hidden (decorative)">
        <Icon name="trash" size="lg" color="rose" />
      </Cell>
      <Cell label='label="Delete agent"'>
        <Icon name="trash" size="lg" color="rose" label="Delete agent" />
      </Cell>
    </Stack>
  ),
};

/**
 * Common Orbit product compositions — how the Icon component pairs with other
 * Terra primitives in real UI patterns.
 */
export const InContext: Story = {
  name: 'In Context',
  render: () => (
    <Stack direction="vertical" gap="comfortable" className="max-w-sm">
      {/* Feature card header */}
      <div className="flex items-center gap-3 rounded-xl border border-border-subtle bg-surface p-4">
        <Icon name="shield-check" size="lg" container containerVariant="accent" />
        <div>
          <Text size="supporting" weight="strong">
            Policy engine
          </Text>
          <Text size="caption" color="muted">
            OPA v0.63 · 847 rules
          </Text>
        </div>
      </div>

      {/* Empty-state hero icon */}
      <div className="flex flex-col items-center gap-3 rounded-xl border border-border-subtle bg-surface py-8">
        <Icon name="bot" size="xl" container containerVariant="accent" />
        <Text size="supporting" color="muted" align="center">
          No agents deployed yet
        </Text>
      </div>

      {/* Inline status row */}
      <div className="flex items-center gap-2">
        <Icon name="circle-check" size="sm" color="status-healthy" />
        <Text size="supporting" color="muted">
          All systems healthy
        </Text>
      </div>

      {/* Inline destructive action hint */}
      <div className="flex items-center gap-2">
        <Icon name="triangle-alert" size="sm" color="status-degraded" />
        <Text size="supporting" color="status-degraded">
          3 agents degraded
        </Text>
      </div>
    </Stack>
  ),
};
