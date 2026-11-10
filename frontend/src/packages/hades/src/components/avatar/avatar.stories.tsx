import type { Meta, StoryObj } from '@storybook/react';
import { Option, OptionTable } from '../../docs/story-ui';
import { Icon } from '../icon';
import { Avatar } from './avatar';

/**
 * Avatar
 *
 * A person's initials in a round, tinted mark, sized to line up with Icon's containers.
 */
const meta: Meta<typeof Avatar> = {
  title: 'Hades/Atoms/Avatar',
  component: Avatar,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
  args: { name: 'Dana Lee', size: '2xs' },
  argTypes: {
    size: { control: 'select', options: ['xs', '2xs', 'sm', 'md'] },
  },
};

export default meta;
type Story = StoryObj<typeof Avatar>;

export const Default: Story = {};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Options"
        note="The avatar is hidden from screen readers unless you pass a label."
      >
        <Option
          name='size="xs" / "2xs" / "sm" / "md"'
          use="20px, 24px, 28px and 36px, the sizes of Icon's containers. The default is 2xs."
        >
          <div className="flex items-center gap-3">
            {(['xs', '2xs', 'sm', 'md'] as const).map((size) => (
              <Avatar key={size} name="Dana Lee" size={size} />
            ))}
          </div>
        </Option>
        <Option
          name="beside an icon circle"
          use="A person and a trigger in the same column line up at the same size."
        >
          <div className="flex items-center gap-3">
            <Avatar name="Dana Lee" />
            <Icon name="zap" size="2xs" container circle containerVariant="accent" />
          </div>
        </Option>
        <Option name='name="dana"' use="One word gives one initial.">
          <Avatar name="dana" />
        </Option>
        <Option name='label="Dana Lee"' use="Name the avatar when nothing nearby does.">
          <Avatar name="Dana Lee" label="Dana Lee" />
        </Option>
      </OptionTable>
    </div>
  ),
};
