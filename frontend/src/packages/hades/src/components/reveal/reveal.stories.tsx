import { useState } from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Button } from '../button/button';
import { Heading } from '../typography/typography';
import { Reveal } from './reveal';

/**
 * Plays its children in once on mount, one after another: each rises from a little below its
 * place and fades in. Under reduced motion the children show in place. Use it for a page's
 * greeting, once. Do not use it for content that updates.
 */
const meta: Meta<typeof Reveal> = {
  title: 'Hades/Atoms/Reveal',
  component: Reveal,
  tags: ['autodocs'],
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj<typeof Reveal>;

const words = (text: string) => text.split(' ').flatMap((w, i) => (i ? [' ', w] : [w]));

/** Remounts the reveal to play it again. */
function Replay({ children }: { children: (key: number) => React.ReactNode }) {
  const [key, setKey] = useState(0);
  return (
    <div className="flex flex-col items-center gap-6">
      {children(key)}
      <Button variant="outline" size="sm" onClick={() => setKey((k) => k + 1)}>
        Play again
      </Button>
    </div>
  );
}

/** Words of a heading, 60 ms apart. */
export const Default: Story = {
  render: () => (
    <Replay>
      {(key) => (
        <Heading variant="section">
          <Reveal key={key}>{words('Good morning, the build is green')}</Reveal>
        </Heading>
      )}
    </Replay>
  ),
};

/** A slower reveal that starts after a pause: 120 ms apart, 400 ms each, 300 ms delay. */
export const Slow: Story = {
  render: () => (
    <Replay>
      {(key) => (
        <Heading variant="section">
          <Reveal key={key} stagger={120} duration={400} delay={300}>
            {words('Three runs finished overnight')}
          </Reveal>
        </Heading>
      )}
    </Replay>
  ),
};
