'use client';

import { useState } from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Button } from '../button/button';
import { Text } from '../typography/typography';
import { Stack } from '../layout/stack';
import { Separator } from '../separator';
import { Icon } from '../icon/icon';
import { SidePanel, SidePanelHeader, SidePanelBody, SidePanelFooter } from './side-panel';

const meta: Meta<typeof SidePanel> = {
  title: 'Terra/Molecules/SidePanel',
  component: SidePanel,
  tags: ['autodocs'],
};
export default meta;
type Story = StoryObj<typeof SidePanel>;

// Wrapper renders a trigger button so the open/close cycle is exercisable
// in the Storybook canvas — Radix handles the animation on close.
function SidePanelDemo({
  size = 'md',
  children,
}: {
  size?: 'sm' | 'md' | 'lg';
  children: (onClose: () => void) => React.ReactNode;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button size="sm" onClick={() => setOpen(true)}>
        Open panel
      </Button>
      <SidePanel open={open} onClose={() => setOpen(false)} size={size}>
        {children(() => setOpen(false))}
      </SidePanel>
    </>
  );
}

export const Default: Story = {
  render: () => (
    <SidePanelDemo>
      {(onClose) => (
        <>
          <SidePanelHeader
            title="Edit Model"
            description="Update the virtual model configuration."
          />
          <SidePanelBody>
            <Stack gap="comfortable">
              <Text size="supporting" color="muted">
                Panel body content goes here.
              </Text>
            </Stack>
          </SidePanelBody>
          <SidePanelFooter>
            <Button variant="outline" size="sm" onClick={onClose}>
              Cancel
            </Button>
            <Button size="sm">Save changes</Button>
          </SidePanelFooter>
        </>
      )}
    </SidePanelDemo>
  ),
};

export const Small: Story = {
  render: () => (
    <SidePanelDemo size="sm">
      {(onClose) => (
        <>
          <SidePanelHeader title="Quick edit" />
          <SidePanelBody>
            <Text size="supporting" color="muted">
              Compact panel — 440 px wide.
            </Text>
          </SidePanelBody>
          <SidePanelFooter>
            <Button variant="outline" size="sm" onClick={onClose}>
              Cancel
            </Button>
            <Button size="sm">Save</Button>
          </SidePanelFooter>
        </>
      )}
    </SidePanelDemo>
  ),
};

export const Large: Story = {
  render: () => (
    <SidePanelDemo size="lg">
      {(onClose) => (
        <>
          <SidePanelHeader title="Full details" description="Wide panel — 720 px." />
          <SidePanelBody>
            <Text size="supporting" color="muted">
              Large panel for complex forms or rich content.
            </Text>
          </SidePanelBody>
          <SidePanelFooter>
            <Button variant="outline" size="sm" onClick={onClose}>
              Cancel
            </Button>
            <Button size="sm">Save changes</Button>
          </SidePanelFooter>
        </>
      )}
    </SidePanelDemo>
  ),
};

export const WithoutFooter: Story = {
  render: () => (
    <SidePanelDemo>
      {() => (
        <>
          <SidePanelHeader title="Model details" description="Read-only view." />
          <SidePanelBody>
            <Text size="supporting" color="muted">
              Some panels are informational and need no footer action buttons.
            </Text>
          </SidePanelBody>
        </>
      )}
    </SidePanelDemo>
  ),
};

export const CompactHeader: Story = {
  name: 'Compact header + body',
  render: () => (
    <SidePanelDemo>
      {() => (
        <>
          <SidePanelHeader
            density="compact"
            leading={<Icon name="bot" size="sm" container containerVariant="accent" />}
            title="hermes-1"
            description="Running · us-east-1"
          />
          <SidePanelBody padding="compact">
            <Stack gap="comfortable">
              <Text size="supporting" color="muted">
                A 60 px header rail with an 18 px gutter that the body padding matches — the density
                spec for inspector panels.
              </Text>
            </Stack>
          </SidePanelBody>
        </>
      )}
    </SidePanelDemo>
  ),
};

export const WithLeading: Story = {
  name: 'With leading slot',
  render: () => (
    <SidePanelDemo>
      {(onClose) => (
        <>
          <SidePanelHeader
            leading={<Icon name="shield" size="sm" container />}
            title="Guardrails"
            description="Limits applied before every tool call."
          />
          <SidePanelBody>
            <Text size="supporting" color="muted">
              The leading slot never shrinks and renders nothing when omitted.
            </Text>
          </SidePanelBody>
          <SidePanelFooter>
            <Button variant="ghost" size="sm" onClick={onClose}>
              Close
            </Button>
          </SidePanelFooter>
        </>
      )}
    </SidePanelDemo>
  ),
};

export const FlushBody: Story = {
  render: () => (
    <SidePanelDemo>
      {(onClose) => (
        <>
          <SidePanelHeader title="Route table" />
          <SidePanelBody flush>
            <Stack paddingBlock="snug" paddingInline="comfortable">
              <Text size="caption" mono color="muted">
                GET /api/models
              </Text>
            </Stack>
            <Separator />
            <Stack paddingBlock="snug" paddingInline="comfortable">
              <Text size="caption" mono color="muted">
                POST /api/models
              </Text>
            </Stack>
            <Separator />
            <Stack paddingBlock="snug" paddingInline="comfortable">
              <Text size="caption" mono color="muted">
                DELETE /api/models/:id
              </Text>
            </Stack>
          </SidePanelBody>
          <SidePanelFooter>
            <Button variant="ghost" size="sm" onClick={onClose}>
              Close
            </Button>
          </SidePanelFooter>
        </>
      )}
    </SidePanelDemo>
  ),
};
