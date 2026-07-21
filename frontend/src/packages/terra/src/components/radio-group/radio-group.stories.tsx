/**
 * RadioGroup + RadioGroupItem
 *
 * Single-select control group backed by Radix. `RadioGroupItem` is the bare
 * 18 px circle — compose it with `Label`/`Text` for simple rows, or with a
 * `Card selected` surface for card-style pickers (framework, model, and
 * environment selection in the agent builder).
 */

import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Card, CardContent } from '../card';
import { RadioGroup, RadioGroupItem } from './index';

// ─── Meta ─────────────────────────────────────────────────────────────────────

const meta: Meta<typeof RadioGroup> = {
  title: 'Terra/Atoms/RadioGroup',
  component: RadioGroup,
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#060709' }],
    },
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof RadioGroup>;

// ─── Stories ──────────────────────────────────────────────────────────────────

/** Plain radio circles with inline labels. */
export const Default: Story = {
  render: () => (
    <RadioGroup defaultValue="claude-code" aria-label="Framework">
      <Stack direction="horizontal" gap="snug" align="center">
        <RadioGroupItem value="claude-code" id="rg-claude-code" />
        <Text as="span" size="supporting">
          Claude Code
        </Text>
      </Stack>
      <Stack direction="horizontal" gap="snug" align="center">
        <RadioGroupItem value="codex" id="rg-codex" />
        <Text as="span" size="supporting">
          Codex
        </Text>
      </Stack>
      <Stack direction="horizontal" gap="snug" align="center">
        <RadioGroupItem value="custom" id="rg-custom" disabled />
        <Text as="span" size="supporting" color="muted">
          Custom (unavailable)
        </Text>
      </Stack>
    </RadioGroup>
  ),
};

/**
 * Card selection surface — the product composition. `RadioGroup` provides the
 * single-select semantics; each option is a `Card` whose `selected` prop
 * mirrors the group value.
 */
export const CardSelection: Story = {
  render: function CardSelectionStory() {
    const [value, setValue] = React.useState('sonnet');
    const options = [
      { value: 'opus', label: 'Claude Opus', hint: 'Highest capability' },
      { value: 'sonnet', label: 'Claude Sonnet', hint: 'Balanced default' },
      { value: 'haiku', label: 'Claude Haiku', hint: 'Fastest, lowest cost' },
    ];
    return (
      <RadioGroup value={value} onValueChange={setValue} aria-label="Model" className="w-[360px]">
        {options.map((option) => (
          <Card
            key={option.value}
            interactive
            selected={value === option.value}
            onClick={() => setValue(option.value)}
          >
            <CardContent padded compact>
              <Stack direction="horizontal" gap="snug" align="center">
                <RadioGroupItem value={option.value} aria-label={option.label} />
                <Stack gap="none">
                  <Text size="supporting" weight="emphasis">
                    {option.label}
                  </Text>
                  <Text size="caption" color="muted">
                    {option.hint}
                  </Text>
                </Stack>
              </Stack>
            </CardContent>
          </Card>
        ))}
      </RadioGroup>
    );
  },
};
