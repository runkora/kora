import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { TemplateField } from './template-field';
import { TemplateCatalogList } from './template-catalog-list';
import type { TemplateCatalogEntry } from './template-model';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { CodeBlock } from '../code-block';
import { Label } from '../label';
import { SectionLabel } from '../section-label';

const GITHUB_CATALOG: TemplateCatalogEntry[] = [
  {
    token: 'pr.title',
    description: 'Title of the pull request',
    example: 'Fix login retry backoff',
    group: 'Pull request',
  },
  {
    token: 'pr.number',
    description: 'Pull request number',
    example: '412',
    group: 'Pull request',
  },
  {
    token: 'pr.author',
    description: 'Login of the author',
    example: 'some-user',
    group: 'Pull request',
  },
  {
    token: 'pr.body',
    description: 'Description body, as written',
    example: 'Retries were firing twice on 401.',
    group: 'Pull request',
  },
  {
    token: 'repo.full_name',
    description: 'Owner and repository name',
    example: 'orbital/orbit',
    group: 'Repository',
  },
  {
    token: 'repo.default_branch',
    description: 'Default branch of the repository',
    example: 'main',
    group: 'Repository',
  },
];

/** A field that holds its own text, the way a form would. */
function Editor({
  id,
  initial,
  showValue = false,
  ...props
}: {
  id: string;
  initial: string;
  showValue?: boolean;
} & Omit<React.ComponentProps<typeof TemplateField>, 'id' | 'value' | 'onChange' | 'catalog'>) {
  const [value, setValue] = React.useState(initial);
  return (
    <div className="flex w-full max-w-[34rem] flex-col gap-3">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={id}>Prompt</Label>
        <TemplateField
          id={id}
          aria-label="Prompt"
          value={value}
          onChange={setValue}
          catalog={GITHUB_CATALOG}
          {...props}
        />
      </div>
      {showValue && (
        <div className="flex flex-col gap-1.5">
          <SectionLabel>Saved value</SectionLabel>
          <CodeBlock text={value || '(empty)'} density="compact" wrap />
        </div>
      )}
    </div>
  );
}

/**
 * TemplateField
 *
 * A text box for prompts that mix your own words with fields from an event, such as the pull request title. Fields show as pills and are picked from a list.
 */
const meta: Meta<typeof TemplateField> = {
  title: 'Terra/Molecules/TemplateField',
  component: TemplateField,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof TemplateField>;

export const Overview: Story = {
  render: () => (
    <div className="space-y-8">
      <Example title="A prompt with fields">
        <Editor
          id="overview-filled"
          showValue
          initial="Review {{ pr.title }} on {{ repo.full_name }} and comment on the riskiest change."
        />
      </Example>
      <Example title="Empty">
        <Editor
          id="overview-empty"
          initial=""
          placeholder="Write a prompt, then insert fields from the event"
        />
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Options"
        note='Use "Insert field" to pick from the list and "Preview" to see sample values.'
      >
        <Option name="default" use="Text with known fields shown as pills.">
          <Editor id="opt-default" initial="Review {{ pr.title }} by {{ pr.author }}." />
        </Option>
        <Option
          name="unknown field"
          use="A field that is not in the list turns red and is left blank when used."
        >
          <Editor id="opt-unknown" initial="Review {{ pr.titel }} on {{ repo.full_name }}." />
        </Option>
        <Option name="placeholder" use="Text shown while the box is empty.">
          <Editor id="opt-placeholder" initial="" placeholder="Write a prompt" />
        </Option>
        <Option name="catalogLabel" use="A line in the picker that says which fields are listed.">
          <Editor
            id="opt-label"
            initial="Comment on {{ pr.title }}."
            catalogLabel="Fields for pull request events"
          />
        </Option>
        <Option name="minRows" use="Sets the minimum height, for longer prompts.">
          <Editor
            id="opt-rows"
            minRows={8}
            initial={
              'You are reviewing {{ pr.title }} on {{ repo.full_name }}.\n\n' +
              'Read the diff, then comment on the change most likely to break. If nothing stands out, say so and stop.'
            }
          />
        </Option>
        <Option name="disabled" use="Read-only and dimmed, including the footer buttons.">
          <Editor
            id="opt-disabled"
            disabled
            initial="Review {{ pr.title }} on {{ repo.full_name }}."
          />
        </Option>
      </OptionTable>
    </div>
  ),
};

/** The list plus two pattern families, which need a name to complete them. */
const CATALOG_WITH_PATTERNS: TemplateCatalogEntry[] = [
  ...GITHUB_CATALOG,
  {
    token: 'group.labels.<name>',
    description: 'Any label on the event, by name',
    pattern: true,
    group: 'Labels',
  },
  {
    token: 'group.annotations.<name>',
    description: 'Any annotation on the event, by name',
    pattern: true,
    group: 'Labels',
  },
];

/** Shows the token `onSelect` receives. */
function CatalogListDemo() {
  const [chosen, setChosen] = React.useState('');
  return (
    <div className="flex flex-col gap-2">
      <TemplateCatalogList
        catalog={CATALOG_WITH_PATTERNS}
        onSelect={(token) => setChosen(token)}
        searchPlaceholder="Search events…"
        selectablePatterns
        autoFocusSearch={false}
      />
      <CodeBlock text={chosen || 'Nothing chosen yet'} />
    </div>
  );
}

/**
 * The picker on its own, as a standalone field or event chooser. With
 * `selectablePatterns` the pattern families can be chosen; the caller asks for
 * the `<name>` that completes them.
 */
export const CatalogListWithSelectablePatterns: Story = {
  render: () => <CatalogListDemo />,
};
