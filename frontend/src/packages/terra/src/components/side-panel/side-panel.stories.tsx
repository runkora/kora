import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Button } from '../button/button';
import { FormField } from '../form-field/form-field';
import { Icon } from '../icon';
import { Input } from '../input/input';
import { Text } from '../typography/typography';
import { SidePanel, SidePanelBody, SidePanelFooter, SidePanelHeader } from './side-panel';

/**
 * A panel that slides in from the right edge and holds details or a form for
 * the item the user picked. The page stays visible behind it.
 */
const meta: Meta<typeof SidePanel> = {
  title: 'Terra/Molecules/SidePanel',
  component: SidePanel,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof SidePanel>;

type PanelSize = 'sm' | 'md' | 'lg';

function EditForm({ onClose }: { onClose: () => void }) {
  return (
    <>
      <SidePanelHeader title="Edit agent" description="Changes apply to new runs." />
      <SidePanelBody>
        <FormField label="Name" id="panel-agent-name">
          <Input id="panel-agent-name" defaultValue="Billing agent" />
        </FormField>
      </SidePanelBody>
      <SidePanelFooter>
        <Button variant="outline" size="sm" onClick={onClose}>
          Cancel
        </Button>
        <Button size="sm" onClick={onClose}>
          Save
        </Button>
      </SidePanelFooter>
    </>
  );
}

function DetailsOnly() {
  return (
    <>
      <SidePanelHeader title="Run details" description="Started 3 minutes ago." />
      <SidePanelBody>
        <Text size="supporting" color="muted">
          The billing agent read 12 invoices and sent no email.
        </Text>
      </SidePanelBody>
    </>
  );
}

function CompactInspector() {
  return (
    <>
      <SidePanelHeader
        density="compact"
        leading={<Icon name="bot" size="sm" container containerVariant="accent" />}
        title="billing-agent"
        description="Running"
      />
      <SidePanelBody padding="compact">
        <Text size="supporting" color="muted">
          A shorter header and tighter padding for dense inspectors.
        </Text>
      </SidePanelBody>
    </>
  );
}

function FlushList() {
  const rows = ['agents:read', 'runs:read', 'models:read'];
  return (
    <>
      <SidePanelHeader title="Scopes" description="What this token can do." />
      <SidePanelBody flush>
        {rows.map((row) => (
          <div key={row} className="border-b border-border-subtle px-5 py-3">
            <Text size="supporting" mono>
              {row}
            </Text>
          </div>
        ))}
      </SidePanelBody>
    </>
  );
}

function OpenButton({
  label,
  size,
  width,
  children,
}: {
  label: string;
  size?: PanelSize;
  width?: number | string;
  children: (onClose: () => void) => React.ReactNode;
}) {
  const [open, setOpen] = React.useState(false);
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        {label}
      </Button>
      <SidePanel
        open={open}
        onClose={() => setOpen(false)}
        {...(size ? { size } : {})}
        {...(width ? { width } : {})}
      >
        {children(() => setOpen(false))}
      </SidePanel>
    </>
  );
}

function OpenPanel() {
  const [open, setOpen] = React.useState(false);
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        Open side panel
      </Button>
      <SidePanel open={open} onClose={() => setOpen(false)}>
        <EditForm onClose={() => setOpen(false)} />
      </SidePanel>
    </>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl">
      <Example title="Edit form">
        <Text size="supporting" color="muted">
          The panel slides in from the right. Select the button to see it.
        </Text>
        <OpenPanel />
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Options"
        note="Width goes on SidePanel. The other rows change what goes inside it. Each button opens its own sample."
      >
        <Option name='size="sm" (440px wide)' use="For a short form or a few details.">
          <OpenButton label="Open small panel" size="sm">
            {(onClose) => <EditForm onClose={onClose} />}
          </OpenButton>
        </Option>
        <Option name='size="md" (580px wide)' use="The default.">
          <OpenButton label="Open medium panel" size="md">
            {(onClose) => <EditForm onClose={onClose} />}
          </OpenButton>
        </Option>
        <Option name='size="lg" (720px wide)' use="For wide content such as tables.">
          <OpenButton label="Open wide panel" size="lg">
            {(onClose) => <EditForm onClose={onClose} />}
          </OpenButton>
        </Option>
        <Option name="width={360}" use="An exact width in pixels. Replaces size.">
          <OpenButton label="Open 360px panel" width={360}>
            {(onClose) => <EditForm onClose={onClose} />}
          </OpenButton>
        </Option>
        <Option name="No SidePanelFooter" use="For panels that only show details.">
          <OpenButton label="Open panel without footer">{() => <DetailsOnly />}</OpenButton>
        </Option>
        <Option
          name='SidePanelHeader density="compact"'
          use="A shorter header with an icon, for dense inspectors."
        >
          <OpenButton label="Open compact header">{() => <CompactInspector />}</OpenButton>
        </Option>
        <Option name="SidePanelBody flush" use="Lets a list reach the edges of the panel.">
          <OpenButton label="Open flush list">{() => <FlushList />}</OpenButton>
        </Option>
      </OptionTable>
    </div>
  ),
};
