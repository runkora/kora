import type { Meta, StoryObj } from '@storybook/react';
import { Block, Example, Option, OptionTable } from '../../docs/story-ui';
import { Grid } from './grid';

/**
 * Grid
 *
 * Lays children out in equal columns. Use it when items should line up in
 * rows and columns. For a single line of items, use Stack.
 */
const meta: Meta<typeof Grid> = {
  title: 'Hades/Layout/Grid',
  component: Grid,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Grid>;

const cells = (count: number) =>
  Array.from({ length: count }, (_, i) => <Block key={i}>{i + 1}</Block>);

export const Overview: Story = {
  render: () => (
    <div className="max-w-2xl">
      <Example title="Three columns">
        <Grid cols={3} gap="comfortable">
          {cells(6)}
        </Grid>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Options"
        note="Size options also take a value per breakpoint, for example cols={{ base: 1, md: 3 }}."
      >
        <Option name="cols={2}" use="Number of equal columns: 1, 2, 3, 4, 5, 6 or 12.">
          <Grid cols={2} gap="tight">
            {cells(4)}
          </Grid>
        </Option>
        <Option name="cols={4}" use="More columns make each one narrower.">
          <Grid cols={4} gap="tight">
            {cells(8)}
          </Grid>
        </Option>
        <Option
          name="cols={{ base: 1, md: 3 }}"
          use="One column on small screens, three from the md breakpoint."
        >
          <Grid cols={{ base: 1, md: 3 }} gap="tight">
            {cells(3)}
          </Grid>
        </Option>
        <Option
          name='gap="relaxed"'
          use="Space between all cells. The names are on the Spacing page."
        >
          <Grid cols={3} gap="relaxed">
            {cells(3)}
          </Grid>
        </Option>
        <Option
          name='gapX="tight" gapY="loose"'
          use="Different space between columns and between rows."
        >
          <Grid cols={3} gapX="tight" gapY="loose">
            {cells(6)}
          </Grid>
        </Option>
        <Option name="asChild" use="Render your own element, such as a list, in place of a div.">
          <Grid asChild cols={2} gap="tight">
            <ul className="list-none">
              {cells(4).map((cell, i) => (
                <li key={i}>{cell}</li>
              ))}
            </ul>
          </Grid>
        </Option>
      </OptionTable>
    </div>
  ),
};
