import type { Meta, StoryObj } from '@storybook/react';
import { ColourTile, Group, Page } from './foundation-ui';

/**
 * Colour
 *
 * Every colour token with its current value for the active brand and theme.
 */
const meta: Meta = {
  title: 'Hades/Foundation/Colour',
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj;

/** The colour tokens components use. */
export const Semantic: Story = {
  render: () => (
    <Page>
      <Group title="Surfaces" description="Backgrounds, from the page up to floating panels.">
        <ColourTile
          cssVar="--color-background"
          label="Background"
          utility="bg-background"
          detail="Page background"
        />
        <ColourTile
          cssVar="--color-surface"
          label="Surface"
          utility="bg-surface"
          detail="Cards and panels"
        />
        <ColourTile
          cssVar="--color-surface-elevated"
          label="Surface elevated"
          utility="bg-surface-elevated"
          detail="Dropdowns and popovers"
        />
        <ColourTile
          cssVar="--color-surface-overlay"
          label="Surface overlay"
          utility="bg-surface-overlay"
          detail="Tooltips and dialogs"
        />
      </Group>

      <Group title="Text">
        <ColourTile
          cssVar="--color-foreground"
          label="Foreground"
          utility="text-foreground"
          detail="Main text"
        />
        <ColourTile
          cssVar="--color-muted"
          label="Muted"
          utility="text-muted"
          detail="Secondary text"
        />
        <ColourTile
          cssVar="--color-subtle"
          label="Subtle"
          utility="text-subtle"
          detail="Placeholders and captions"
        />
      </Group>

      <Group title="Accent" description="Buttons, links and focus.">
        <ColourTile
          cssVar="--color-accent"
          label="Accent"
          utility="bg-accent"
          detail="Buttons and links"
        />
        <ColourTile
          cssVar="--color-accent-hover"
          label="Accent hover"
          utility="bg-accent-hover"
          detail="Button hover"
        />
        <ColourTile
          cssVar="--color-accent-foreground"
          label="Accent foreground"
          utility="text-accent-foreground"
          detail="Text on accent fills"
        />
        <ColourTile
          cssVar="--color-accent-subtle"
          label="Accent subtle"
          utility="bg-accent-subtle"
          detail="Callout background"
        />
        <ColourTile
          cssVar="--color-accent-muted"
          label="Accent muted"
          utility="bg-accent-muted"
          detail="Selected item background"
        />
        <ColourTile
          cssVar="--color-accent-border"
          label="Accent border"
          utility="border-accent-border"
          detail="Outline on accent backgrounds"
        />
      </Group>

      <Group title="Borders">
        <ColourTile
          cssVar="--color-border"
          label="Border"
          utility="border-border"
          detail="Default divider"
        />
        <ColourTile
          cssVar="--color-border-subtle"
          label="Border subtle"
          utility="border-border-subtle"
          detail="Faint divider"
        />
        <ColourTile
          cssVar="--color-border-accent"
          label="Border accent"
          utility="border-border-accent"
          detail="Focus ring and highlights"
        />
      </Group>

      <Group title="Status" description="Health and progress states.">
        <ColourTile cssVar="--color-status-healthy" label="Healthy" utility="text-status-healthy" />
        <ColourTile
          cssVar="--color-status-degraded"
          label="Degraded"
          utility="text-status-degraded"
        />
        <ColourTile cssVar="--color-status-error" label="Error" utility="text-status-error" />
        <ColourTile cssVar="--color-status-idle" label="Idle" utility="text-status-idle" />
        <ColourTile cssVar="--color-status-pending" label="Pending" utility="text-status-pending" />
      </Group>
    </Page>
  ),
};

/** The raw colours the tokens above are built from. Components use the tokens, not these. */
export const Palette: Story = {
  render: () => (
    <Page>
      <Group title="Neutral ramp" description="12 steps, darkest to lightest." min={150}>
        {[
          'black',
          '950',
          '900',
          '800',
          '700',
          '600',
          '500',
          '400',
          '300',
          '200',
          '100',
          'white',
        ].map((step) => (
          <ColourTile key={step} cssVar={`--hades-raw-${step}`} label={step} />
        ))}
      </Group>

      <Group
        title="Accent ramp"
        description="Four accent steps and the text colour used on top of them."
        min={150}
      >
        {['600', '500', '400', '300', 'fg'].map((step) => (
          <ColourTile key={step} cssVar={`--hades-raw-accent-${step}`} label={`accent ${step}`} />
        ))}
      </Group>

      <Group title="Status" description="The raw status colours." min={150}>
        {['healthy', 'degraded', 'error', 'idle', 'pending'].map((name) => (
          <ColourTile key={name} cssVar={`--hades-raw-${name}`} label={name} />
        ))}
      </Group>
    </Page>
  ),
};

/** Colours for charts and for provider and strategy labels. */
export const DataAndIdentity: Story = {
  name: 'Data and identity',
  render: () => (
    <Page>
      <Group
        title="Chart series"
        description="Use in this order, one colour per line or bar."
        min={150}
      >
        {[1, 2, 3, 4, 5, 6, 7, 8].map((n) => (
          <ColourTile
            key={n}
            cssVar={`--color-chart-${n}`}
            label={`Series ${n}`}
            utility={`bg-chart-${n}`}
          />
        ))}
      </Group>

      <Group
        title="Provider identity"
        description="The brand colour of each cloud provider."
        min={150}
      >
        <ColourTile cssVar="--color-provider-bedrock" label="Bedrock" />
        <ColourTile cssVar="--color-provider-vertexai" label="Vertex AI" />
        <ColourTile cssVar="--color-provider-azure" label="Azure" />
      </Group>

      <Group
        title="Strategy identity"
        description="Label colour for the weighted routing strategy."
        min={150}
      >
        <ColourTile cssVar="--color-strategy-weighted" label="Weighted" />
      </Group>
    </Page>
  ),
};
