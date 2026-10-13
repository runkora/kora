import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { CodeBlock } from './code-block';

/**
 * CodeBlock
 *
 * Shows code or a command in a box with a button to copy it. Use it for anything the reader
 * will paste somewhere else.
 */
const meta: Meta<typeof CodeBlock> = {
  title: 'Hades/Molecules/CodeBlock',
  component: CodeBlock,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof CodeBlock>;

const ENV_FILE = `ORBIT_API_URL="https://api.orbit.example.com"
ORBIT_AGENT_ID="apollo-3"`;

const PAYLOAD = `{
  "event": "run.completed",
  "agent": "apollo-3",
  "attempts": 2,
  "cached": false,
  "error": null
}`;

const PROMPT =
  'You are a support agent for kora customers. Answer in plain language, keep replies under 150 words, and hand the conversation to a person when you are unsure.';

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl space-y-8">
      <Example title="Command">
        <CodeBlock text="orbit agents deploy apollo-3 --env production" />
      </Example>
      <Example title="File with a name">
        <CodeBlock filename="~/.zshrc" text={ENV_FILE} />
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="text" use="The code to show. The copy button copies exactly this text.">
          <CodeBlock text="orbit login" />
        </Option>
        <Option name="filename" use="Adds a bar with a file name above the code.">
          <CodeBlock filename="agent.env" text={ENV_FILE} />
        </Option>
        <Option
          name='language="json"'
          use="Colours keys, strings, numbers and true, false and null. Text that is not valid JSON still shows."
        >
          <CodeBlock language="json" text={PAYLOAD} />
        </Option>
        <Option
          name='density="compact"'
          use="A smaller block for a sample inside a row or panel. The copy button shows on hover or focus."
        >
          <CodeBlock density="compact" language="json" text={PAYLOAD} />
        </Option>
        <Option
          name="wrap"
          use="Wraps long lines instead of scrolling sideways. Use it for sentences, not code."
        >
          <CodeBlock wrap text={PROMPT} />
        </Option>
      </OptionTable>
    </div>
  ),
};
