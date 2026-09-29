import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { TemplateField } from './template-field';
import { TemplateCatalogList } from './template-catalog-list';
import type { TemplateCatalogEntry } from './template-model';
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

const LONG_CATALOG: TemplateCatalogEntry[] = [
  ...GITHUB_CATALOG,
  {
    token: 'issue.title',
    description: 'Title of the issue',
    example: 'Login loops on expired token',
    group: 'Issue',
  },
  { token: 'issue.number', description: 'Issue number', example: '87', group: 'Issue' },
  {
    token: 'issue.state',
    description: 'Open or closed',
    example: 'open',
    group: 'Issue',
  },
  {
    token: 'actor.login',
    description: 'Who triggered the event',
    example: 'some-user',
    group: 'Actor',
  },
  {
    token: 'actor.email',
    description: 'Email of the triggering user, when known',
    example: 'justin@example.com',
    group: 'Actor',
  },
  {
    token: 'run.id',
    description: 'Identifier of this agent run',
    example: 'run_01HQ9',
    group: 'Run',
  },
  {
    token: 'run.started_at',
    description: 'When the run began, RFC 3339',
    example: '2026-07-27T09:12:04Z',
    group: 'Run',
  },
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

/** A story host that owns the template string, the way a form would. */
function Editor({
  initial,
  catalog,
  ...props
}: {
  initial: string;
  catalog: TemplateCatalogEntry[];
} & Omit<React.ComponentProps<typeof TemplateField>, 'value' | 'onChange' | 'catalog'>) {
  const [value, setValue] = React.useState(initial);
  return (
    <div className="flex w-[34rem] flex-col gap-3">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="prompt-template">Prompt template</Label>
        <TemplateField
          id="prompt-template"
          value={value}
          onChange={setValue}
          catalog={catalog}
          {...props}
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <SectionLabel>Value stored</SectionLabel>
        <CodeBlock text={value || '(empty)'} density="compact" wrap />
      </div>
    </div>
  );
}

const meta: Meta<typeof TemplateField> = {
  title: 'Terra/Molecules/TemplateField',
  component: TemplateField,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof TemplateField>;

/**
 * The everyday case: a prompt written against a GitHub pull request event. The
 * panel underneath shows the raw string the parent holds, which is always the
 * template with its braces intact.
 */
export const Default: Story = {
  render: () => (
    <Editor
      initial="Review {{ pr.title }} on {{ repo.full_name }} and leave one comment on the riskiest change."
      catalog={GITHUB_CATALOG}
      placeholder="Write a prompt…"
    />
  ),
};

/** Empty, so the placeholder and the picker are the way in. */
export const Empty: Story = {
  render: () => (
    <Editor
      initial=""
      catalog={GITHUB_CATALOG}
      placeholder="Write a prompt, and insert fields from the event…"
    />
  ),
};

/**
 * A token the catalog does not know. It is not a blocking error: an unresolved
 * token renders as the empty string. It reads as wrong all the same, because it
 * is nearly always a typo for something real.
 */
export const UnknownToken: Story = {
  render: () => (
    <Editor
      initial="Review {{ pr.titel }} for {{ repo.full_name }}."
      catalog={GITHUB_CATALOG}
      placeholder="Write a prompt…"
    />
  ),
};

/**
 * A larger catalog with several groups. Open "Insert field" and type to filter:
 * the pattern families at the bottom are listed so their existence is
 * discoverable, but cannot be inserted verbatim.
 */
export const LongCatalogWithGroups: Story = {
  render: () => (
    <Editor
      initial="Triage {{ issue.title }} ({{ issue.state }}) raised by {{ actor.login }}."
      catalog={LONG_CATALOG}
      placeholder="Write a prompt…"
    />
  ),
};

/**
 * The preview panel, showing the template with every field replaced by its
 * sample value. `repo.default_branch` and the rest resolve; anything without a
 * sample renders empty.
 */
export const PreviewOn: Story = {
  render: () => (
    <Editor
      initial="Review {{ pr.title }} (#{{ pr.number }}) by {{ pr.author }} on {{ repo.full_name }}."
      catalog={GITHUB_CATALOG}
      placeholder="Write a prompt…"
    />
  ),
  parameters: {
    docs: {
      description: {
        story: 'Click "Preview" in the footer bar to substitute the sample values.',
      },
    },
  },
};

/** Read-only: the whole frame dims, including the footer controls. */
export const Disabled: Story = {
  render: () => (
    <Editor
      initial="Review {{ pr.title }} on {{ repo.full_name }}."
      catalog={GITHUB_CATALOG}
      disabled
    />
  ),
};

/** A taller editor for prompts that run to a paragraph or more. */
export const TallerEditor: Story = {
  render: () => (
    <Editor
      initial={
        'You are reviewing {{ pr.title }} on {{ repo.full_name }}.\n\n' +
        'Read the diff, then leave a single comment on the change most likely to break in production. ' +
        'If nothing stands out, say so and stop.'
      }
      catalog={GITHUB_CATALOG}
      minRows={8}
    />
  ),
};

/** Shows the token `onSelect` receives. */
function CatalogListDemo() {
  const [chosen, setChosen] = React.useState('');
  return (
    <div className="flex flex-col gap-2">
      <TemplateCatalogList
        catalog={LONG_CATALOG}
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
