import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { composeStories } from '@storybook/react';
import { Heading, Stack, Text } from '../..';

/**
 * Component Grid
 *
 * Every Terra component on one page, for checking a brand or colour change
 * at a glance. Each block is that component's Overview story, so this page
 * never drifts from the component stories.
 */
const meta: Meta = {
  title: 'Terra/Overview/Component Grid',
  parameters: { layout: 'fullscreen' },
};

export default meta;
type Story = StoryObj;

declare global {
  interface ImportMeta {
    webpackContext(
      directory: string,
      options: { recursive: boolean; regExp: RegExp },
    ): { keys(): string[]; (id: string): unknown };
  }
}

type StoryFile = { default: { title?: string } } & Record<string, unknown>;

const context = import.meta.webpackContext('..', { recursive: true, regExp: /\.stories\.tsx$/ });

/** The groups shown, in order. Foundation and this page are left out. */
const GROUPS = ['Layout', 'Atoms', 'Molecules', 'Organisms'] as const;

interface Entry {
  group: (typeof GROUPS)[number];
  name: string;
  Overview: React.ComponentType;
}

const entries: Entry[] = context
  .keys()
  .filter((key) => !key.includes('component-grid'))
  .flatMap((key) => {
    const file = context(key) as StoryFile;
    const [root, group, ...rest] = (file.default.title ?? '').split('/');
    if (root !== 'Terra' || !GROUPS.includes(group as Entry['group'])) return [];
    const Overview = (composeStories(file as never) as Record<string, React.ComponentType>)
      .Overview;
    return Overview ? [{ group: group as Entry['group'], name: rest.join(' / '), Overview }] : [];
  });

export const ComponentGrid: Story = {
  render: () => (
    <Stack gap="relaxed" padding="relaxed">
      {GROUPS.map((group) => (
        <Stack key={group} gap="relaxed">
          <Heading variant="title">{group}</Heading>
          {entries
            .filter((entry) => entry.group === group)
            .sort((a, b) => a.name.localeCompare(b.name))
            .map(({ name, Overview }) => (
              <Stack key={name} gap="snug">
                <Text size="caption" color="muted" mono>
                  {name}
                </Text>
                <Overview />
              </Stack>
            ))}
        </Stack>
      ))}
    </Stack>
  ),
};
