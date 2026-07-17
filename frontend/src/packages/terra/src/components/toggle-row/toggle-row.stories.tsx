import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import { Stack } from '../layout/stack';
import { Toggle } from '../toggle/toggle';
import { Checkbox } from '../checkbox/checkbox';
import { Text } from '../typography/typography';
import { Separator } from '../separator/separator';
import { ToggleRow } from './toggle-row';

const meta: Meta<typeof ToggleRow> = {
  title: 'Terra/Atoms/ToggleRow',
  component: ToggleRow,
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
type Story = StoryObj<typeof ToggleRow>;

// ── Stories ──────────────────────────────────────────────────────────────────

/**
 * The simplest case: a Toggle paired with a label and description.
 * Click the label text to toggle — the `htmlFor` association handles it.
 */
export const WithToggle: Story = {
  render: function Render() {
    const [enabled, setEnabled] = useState(false);
    return (
      <div className="w-80">
        <ToggleRow
          htmlFor="streaming"
          label="Enable streaming"
          description="Stream responses token by token as they are generated."
          control={<Toggle id="streaming" checked={enabled} onChange={setEnabled} />}
        />
      </div>
    );
  },
};

/**
 * Same layout with a Checkbox as the control.
 */
export const WithCheckbox: Story = {
  render: function Render() {
    const [checked, setChecked] = useState(false);
    return (
      <div className="w-80">
        <ToggleRow
          htmlFor="persist-ctx"
          label="Persist conversation context"
          description="Retain tool call history between sessions."
          control={<Checkbox id="persist-ctx" checked={checked} onChange={(v) => setChecked(v)} />}
        />
      </div>
    );
  },
};

/**
 * Without a description — the control aligns naturally to the single label line.
 */
export const LabelOnly: Story = {
  render: function Render() {
    const [enabled, setEnabled] = useState(true);
    return (
      <div className="w-80">
        <ToggleRow
          htmlFor="auto-retry"
          label="Auto-retry on failure"
          control={<Toggle id="auto-retry" checked={enabled} onChange={setEnabled} />}
        />
      </div>
    );
  },
};

/**
 * `disabled` dims the row and blocks pointer events.
 */
export const Disabled: Story = {
  render: () => (
    <div className="w-80">
      <ToggleRow
        htmlFor="mcp-disabled"
        label="MCP tool access"
        description="Requires at least one MCP server configured."
        disabled
        control={<Toggle id="mcp-disabled" checked={false} onChange={() => {}} />}
      />
    </div>
  ),
};

/**
 * Multiple ToggleRows in a settings section — shows how they compose with
 * a Separator between groups.
 */
export const SettingsGroup: Story = {
  render: function Render() {
    const [stream, setStream] = useState(true);
    const [retry, setRetry] = useState(false);
    const [ctx, setCtx] = useState(true);
    const [strict, setStrict] = useState(false);

    return (
      <div className="w-96">
        <Stack direction="vertical" gap="comfortable">
          <Text size="supporting" weight="strong" color="muted">
            Behaviour
          </Text>
          <Stack direction="vertical" gap="comfortable">
            <ToggleRow
              htmlFor="s-stream"
              label="Streaming responses"
              description="Emit tokens incrementally rather than buffering the full response."
              control={<Toggle id="s-stream" checked={stream} onChange={setStream} />}
            />
            <Separator span="full" />
            <ToggleRow
              htmlFor="s-retry"
              label="Auto-retry on tool failure"
              description="Retry failed tool calls up to 3 times before surfacing the error."
              control={<Toggle id="s-retry" checked={retry} onChange={setRetry} />}
            />
            <Separator span="full" />
            <ToggleRow
              htmlFor="s-ctx"
              label="Persist context across sessions"
              description="Retain conversation history and tool results between runs."
              control={<Toggle id="s-ctx" checked={ctx} onChange={setCtx} />}
            />
            <Separator span="full" />
            <ToggleRow
              htmlFor="s-strict"
              label="Strict tool schema validation"
              description="Reject tool calls that do not exactly match the declared schema."
              disabled
              control={<Toggle id="s-strict" checked={strict} onChange={setStrict} />}
            />
          </Stack>
        </Stack>
      </div>
    );
  },
};
