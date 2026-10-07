import type { Meta, StoryObj } from '@storybook/react';
import { Block } from '../../docs/story-ui';
import { NavItem } from '../nav-item/nav-item';
import { Navigation } from '../navigation/navigation';
import { Section } from '../section/section';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { PageLayout } from './page-layout';

/**
 * PageLayout
 *
 * The page shell: a sidebar, a header bar and a scrolling main area. The
 * sidebar and header are optional. Fill the main area with Section blocks.
 */
const meta: Meta = {
  title: 'Terra/Layout/PageLayout',
  tags: ['autodocs'],
  parameters: { layout: 'fullscreen' },
};

export default meta;
type Story = StoryObj;

const NAV_ITEMS = ['Overview', 'Agents', 'Policies', 'Credentials', 'Events', 'Settings'];

export const Overview: Story = {
  render: () => (
    <div className="h-screen">
      <PageLayout
        sidebar={
          <Navigation>
            <Stack gap="none" paddingInline="snug" paddingBlock="snug">
              {NAV_ITEMS.map((item, i) => (
                <NavItem key={item} state={i === 0 ? 'active' : undefined} asChild>
                  <a href="#">{item}</a>
                </NavItem>
              ))}
            </Stack>
          </Navigation>
        }
        header={
          <Text size="supporting" weight="strong">
            Header
          </Text>
        }
      >
        <Section heading="Active agents" description="Agents running in this workspace.">
          <Block>Content</Block>
        </Section>
        <Section heading="Recent events">
          <Block>Content</Block>
        </Section>
      </PageLayout>
    </div>
  ),
};
