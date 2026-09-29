import type { Meta, StoryObj } from '@storybook/react';
import { Example, Frame, Option, OptionTable } from '../../docs/story-ui';
import { Button } from '../button/button';
import { Icon } from '../icon/icon';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './tooltip';

/**
 * A short label that appears when someone hovers or focuses an element. Use it
 * for a hint, not for anything the person needs to read. The samples here are
 * held open so you can see them.
 */
const meta: Meta<typeof Tooltip> = {
  title: 'Terra/Atoms/Tooltip',
  component: Tooltip,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Tooltip>;

function Stage({ children }: { children: React.ReactNode }) {
  return (
    <Frame className="flex min-h-32 items-center justify-center">
      <div className="flex justify-center">{children}</div>
    </Frame>
  );
}

function CopyHint({
  side,
  align,
  label = 'Copy to clipboard',
}: {
  side?: 'top' | 'right' | 'bottom' | 'left';
  align?: 'start' | 'center' | 'end';
  label?: string;
}) {
  return (
    <Tooltip open>
      <TooltipTrigger asChild>
        <Button variant="secondary" size="icon" aria-label="Copy">
          <Icon name="copy" size="sm" />
        </Button>
      </TooltipTrigger>
      <TooltipContent {...(side ? { side } : {})} {...(align ? { align } : {})}>
        {label}
      </TooltipContent>
    </Tooltip>
  );
}

export const Overview: Story = {
  render: () => (
    <TooltipProvider>
      <div className="max-w-xl space-y-8">
        <Example title="Tooltip on an icon button">
          <Stage>
            <CopyHint />
          </Stage>
        </Example>
      </div>
    </TooltipProvider>
  ),
};

export const Options: Story = {
  render: () => (
    <TooltipProvider>
      <div className="max-w-5xl">
        <OptionTable
          title="Options"
          note="Side and alignment go on TooltipContent. Wrap the app in TooltipProvider once."
        >
          <Option name='side="top"' use="Shows above the element. The default.">
            <Stage>
              <CopyHint side="top" />
            </Stage>
          </Option>
          <Option name='side="bottom"' use="Shows below the element.">
            <Stage>
              <CopyHint side="bottom" />
            </Stage>
          </Option>
          <Option name='side="left" / "right"' use="Shows beside the element.">
            <Stage>
              <div className="flex gap-40">
                <CopyHint side="left" label="Left" />
                <CopyHint side="right" label="Right" />
              </div>
            </Stage>
          </Option>
          <Option
            name='align="start" / "end"'
            use="Lines the label up with one edge of the element."
          >
            <Stage>
              <div className="flex gap-40">
                <CopyHint side="bottom" align="start" label="Start of the button" />
                <CopyHint side="top" align="end" label="End of the button" />
              </div>
            </Stage>
          </Option>
        </OptionTable>
      </div>
    </TooltipProvider>
  ),
};
