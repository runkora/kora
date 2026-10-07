import type { Meta, StoryObj } from '@storybook/react';
import { Group, Page, Tile, readColour, readShadow } from './foundation-ui';

/**
 * Elevation
 *
 * Shadows for floating surfaces, and overlays that dim the page behind a panel
 * or dialog.
 */
const meta: Meta = {
  title: 'Terra/Foundation/Elevation',
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj;

const SHADOWS = [
  {
    cssVar: '--shadow-sm',
    label: 'Shadow small',
    utility: 'shadow-sm',
    detail: 'Small floating buttons',
  },
  {
    cssVar: '--shadow-elevated',
    label: 'Shadow elevated',
    utility: 'shadow-elevated',
    detail: 'Menus and floating panels',
  },
];

// Fixed tones, so the overlay looks the same in every brand and theme.
const STRIPES = ['#f4f4f2', '#9aa0a6', '#4d6b80', '#1a1d20'];

const OVERLAYS = [
  {
    cssVar: '--color-overlay',
    label: 'Overlay',
    utility: 'bg-overlay',
    detail: 'Behind side panels',
  },
  {
    cssVar: '--color-overlay-strong',
    label: 'Overlay strong',
    utility: 'bg-overlay-strong',
    detail: 'Behind dialogs',
  },
];

export const Shadows: Story = {
  render: () => (
    <Page>
      <Group title="Shadows" min={260}>
        {SHADOWS.map((s) => (
          <Tile
            key={s.cssVar}
            {...s}
            read={readShadow}
            preview={(ref) => (
              <div className="flex h-36 items-center justify-center bg-background">
                <div
                  ref={ref}
                  className="h-16 w-32 rounded-lg border border-border-subtle bg-surface-elevated"
                  style={{ boxShadow: `var(${s.cssVar})` }}
                />
              </div>
            )}
          />
        ))}
      </Group>

      <Group
        title="Overlays"
        description="Semi-transparent layers that dim the page. The right half of each strip has the overlay applied."
        min={260}
      >
        {OVERLAYS.map((o) => (
          <Tile
            key={o.cssVar}
            {...o}
            read={readColour}
            preview={(ref) => (
              <div className="relative flex h-28 overflow-hidden">
                {STRIPES.map((colour) => (
                  <div key={colour} className="flex-1" style={{ background: colour }} />
                ))}
                <div
                  ref={ref}
                  className="absolute inset-y-0 right-0 w-1/2"
                  style={{ background: `var(${o.cssVar})` }}
                />
              </div>
            )}
          />
        ))}
      </Group>
    </Page>
  ),
};
