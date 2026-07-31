import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import { Stack } from '../layout/stack';
import { Checkbox } from '../checkbox/checkbox';
import { ToggleRow } from './toggle-row';

const meta: Meta<typeof ToggleRow> = {
  title: 'Terra/Molecules/ToggleRow',
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

// ── Switch form ──────────────────────────────────────────────────────────────

/**
 * The default form. The whole row is the switch: click anywhere on it, or Tab
 * to it and press Space or Enter.
 */
export const Default: Story = {
  render: function Render() {
    const [enabled, setEnabled] = useState(false);
    return (
      <div className="w-[440px]">
        <ToggleRow
          checked={enabled}
          onChange={setEnabled}
          label="Streaming responses"
          description="Emit tokens incrementally rather than buffering the full response."
        />
      </div>
    );
  },
};

/** On: the track takes the accent colour and the copy comes to full opacity. */
export const Checked: Story = {
  render: () => (
    <div className="w-[440px]">
      <ToggleRow
        checked
        onChange={() => {}}
        label="Auto-retry on tool failure"
        description="Retry failed tool calls up to 3 times before surfacing the error."
      />
    </div>
  ),
};

/** Without a description the row collapses to a single line. */
export const LabelOnly: Story = {
  render: function Render() {
    const [enabled, setEnabled] = useState(true);
    return (
      <div className="w-[440px]">
        <ToggleRow checked={enabled} onChange={setEnabled} label="Auto-retry on failure" />
      </div>
    );
  },
};

/** `disabled` dims the row and blocks both pointer and keyboard activation. */
export const Disabled: Story = {
  render: () => (
    <div className="w-[440px]">
      <ToggleRow
        checked={false}
        onChange={() => {}}
        disabled
        label="MCP tool access"
        description="Requires at least one MCP server configured."
      />
    </div>
  ),
};

/** A settings pane: what is on is legible without reading a single label. */
export const SettingsGroup: Story = {
  render: function Render() {
    const [stream, setStream] = useState(true);
    const [retry, setRetry] = useState(false);
    const [ctx, setCtx] = useState(true);

    return (
      <div className="w-[440px]">
        <Stack gap="snug">
          <ToggleRow
            checked={stream}
            onChange={setStream}
            label="Streaming responses"
            description="Emit tokens incrementally rather than buffering the full response."
          />
          <ToggleRow
            checked={retry}
            onChange={setRetry}
            label="Auto-retry on tool failure"
            description="Retry failed tool calls up to 3 times before surfacing the error."
          />
          <ToggleRow
            checked={ctx}
            onChange={setCtx}
            label="Persist context across sessions"
            description="Retain conversation history and tool results between runs."
          />
          <ToggleRow
            checked={false}
            onChange={() => {}}
            disabled
            label="Strict tool schema validation"
            description="Reject tool calls that do not exactly match the declared schema."
          />
        </Stack>
      </div>
    );
  },
};

// ── Composed form ────────────────────────────────────────────────────────────

/**
 * Pass `control` instead of `checked`/`onChange` when the caller owns the
 * control. The row is then a bare label pairing with no container of its own.
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
