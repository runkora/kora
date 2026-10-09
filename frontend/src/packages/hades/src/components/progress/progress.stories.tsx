import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Progress } from './progress';

/**
 * Progress
 *
 * A horizontal bar that shows how full something is, such as CPU use or a deploy.
 * It needs a label so screen readers know what it measures.
 */
const meta: Meta<typeof Progress> = {
  title: 'Hades/Atoms/Progress',
  component: Progress,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Progress>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-sm space-y-8">
      <Example title="Resource usage">
        <div className="space-y-4">
          <Progress label="CPU" value={42} variant="healthy" showValue />
          <Progress label="Memory" value={71} variant="degraded" showValue />
          <Progress label="Disk" value={94} variant="error" showValue />
        </div>
      </Example>
      <Example title="Deploy in progress">
        <Progress label="Deploy progress" value={45} variant="pending" showValue />
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl space-y-10">
      <OptionTable
        title="Variant"
        note="The fill colour tells the reader how to feel about the value."
      >
        <Option name='variant="default"' use="Plain usage in the brand colour. The default.">
          <Progress label="Storage used" value={55} showValue />
        </Option>
        <Option name='variant="healthy"' use="A safe level.">
          <Progress label="Disk activity" value={38} variant="healthy" showValue />
        </Option>
        <Option name='variant="degraded"' use="Getting close to a limit.">
          <Progress label="Network use" value={78} variant="degraded" showValue />
        </Option>
        <Option name='variant="error"' use="Over a limit or failing.">
          <Progress label="CPU" value={96} variant="error" showValue />
        </Option>
        <Option name='variant="pending"' use="Work that is still running.">
          <Progress label="Deploy progress" value={45} variant="pending" showValue />
        </Option>
      </OptionTable>
      <OptionTable title="Size, value and extras">
        <Option
          name='size="hairline"'
          use="2 px. A thin bar in page chrome, such as a wizard header."
        >
          <Progress label="Setup progress, hairline" value={60} size="hairline" />
        </Option>
        <Option name='size="sm"' use="6 px. Inside a table cell or a card footer.">
          <Progress label="Setup progress, small" value={60} size="sm" />
        </Option>
        <Option name='size="md"' use="8 px. The default.">
          <Progress label="Setup progress, medium" value={60} size="md" />
        </Option>
        <Option name='size="lg"' use="12 px. A full-width panel.">
          <Progress label="Setup progress, large" value={60} size="lg" />
        </Option>
        <Option name="showValue" use="Print the percentage to the right of the bar.">
          <Progress label="Memory" value={72} showValue />
        </Option>
        <Option name="value={0}" use="An empty bar.">
          <Progress label="No data yet" value={0} showValue />
        </Option>
        <Option name="value={100}" use="A full bar.">
          <Progress label="Complete" value={100} variant="healthy" showValue />
        </Option>
        <Option
          name="max"
          use="Set the top of the scale when the value is not a percentage. 30 of 120 shows 25%."
        >
          <Progress label="Runs used" value={30} max={120} showValue />
        </Option>
      </OptionTable>
    </div>
  ),
};
