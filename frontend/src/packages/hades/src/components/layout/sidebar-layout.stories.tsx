import type { Meta, StoryObj } from '@storybook/react';
import { Block, Example, Option, OptionTable } from '../../docs/story-ui';
import { SidebarLayout } from './sidebar-layout';

/**
 * SidebarLayout
 *
 * Main content next to a fixed-width side panel. The main area takes the rest
 * of the width.
 */
const meta: Meta<typeof SidebarLayout> = {
  title: 'Hades/Layout/SidebarLayout',
  component: SidebarLayout,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof SidebarLayout>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-4xl">
      <Example title="Sidebar on the right">
        <SidebarLayout sidebarWidth="detail-panel" gap="relaxed">
          <SidebarLayout.Main>
            <Block>Main content</Block>
          </SidebarLayout.Main>
          <SidebarLayout.Sidebar>
            <Block>Sidebar</Block>
          </SidebarLayout.Sidebar>
        </SidebarLayout>
      </Example>
    </div>
  ),
};

function Sample(props: Partial<React.ComponentProps<typeof SidebarLayout>>) {
  const main = (
    <SidebarLayout.Main>
      <Block>Main</Block>
    </SidebarLayout.Main>
  );
  const sidebar = (
    <SidebarLayout.Sidebar>
      <Block>Sidebar</Block>
    </SidebarLayout.Sidebar>
  );
  // The sidebar column is first when the position is start, so it goes first in the markup.
  return (
    <SidebarLayout sidebarWidth="detail-panel" gap="snug" {...props}>
      {props.sidebarPosition === 'start' ? (
        <>
          {sidebar}
          {main}
        </>
      ) : (
        <>
          {main}
          {sidebar}
        </>
      )}
    </SidebarLayout>
  );
}

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option
          name='sidebarPosition="start"'
          use="Put the sidebar on the left. List the Sidebar first inside the layout. The default is the right."
        >
          <Sample sidebarWidth={140} sidebarPosition="start" />
        </Option>
        <Option name='sidebarWidth="detail-panel"' use="A named width from the sizing scale.">
          <Sample sidebarWidth="detail-panel" />
        </Option>
        <Option name="sidebarWidth={120}" use="A number is a width in pixels.">
          <Sample sidebarWidth={120} />
        </Option>
        <Option name='sidebarWidth="10rem"' use="A string is used as a CSS width.">
          <Sample sidebarWidth="10rem" />
        </Option>
        <Option name='gap="loose"' use="Space between main and sidebar.">
          <Sample sidebarWidth={140} gap="loose" />
        </Option>
      </OptionTable>
    </div>
  ),
};
