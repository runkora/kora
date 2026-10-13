import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Badge } from './badge';

/**
 * Badge
 *
 * A small label for a status, a category or a piece of metadata such as a version or a scope.
 */
const meta: Meta<typeof Badge> = {
  title: 'Hades/Atoms/Badge',
  component: Badge,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Badge>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl space-y-8">
      <Example title="Status">
        <div className="flex flex-wrap gap-2">
          <Badge variant="healthy" dot>
            Running
          </Badge>
          <Badge variant="degraded" dot>
            Slow
          </Badge>
          <Badge variant="error" dot>
            Failed
          </Badge>
          <Badge variant="idle" dot>
            Idle
          </Badge>
          <Badge variant="pending" dot>
            Awaiting approval
          </Badge>
        </div>
      </Example>
      <Example title="Metadata">
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant="secondary">v1.4.2</Badge>
          <Badge variant="elevated" size="sm" shape="sharp" mono>
            models:read
          </Badge>
          <Badge variant="dim" size="sm">
            orbit-3-turbo
          </Badge>
          <Badge variant="reserved" shape="pill" size="lg" mono>
            platform-admin
          </Badge>
        </div>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl space-y-10">
      <OptionTable
        title="Variant: status"
        note="Use these to show the state of an agent or service."
      >
        <Option name='variant="healthy"' use="Working normally.">
          <Badge variant="healthy">Running</Badge>
        </Option>
        <Option name='variant="degraded"' use="Working, with warnings.">
          <Badge variant="degraded">Slow</Badge>
        </Option>
        <Option name='variant="error"' use="Something failed.">
          <Badge variant="error">Failed</Badge>
        </Option>
        <Option name='variant="idle"' use="Not running and not scheduled.">
          <Badge variant="idle">Idle</Badge>
        </Option>
        <Option name='variant="pending"' use="Starting up or waiting for a decision.">
          <Badge variant="pending">Awaiting approval</Badge>
        </Option>
        <Option name='variant="muted"' use="Switched off. Says nothing about health.">
          <Badge variant="muted">Disabled</Badge>
        </Option>
        <Option name='variant="destructive"' use="A removed or blocked item.">
          <Badge variant="destructive">Revoked</Badge>
        </Option>
      </OptionTable>

      <OptionTable title="Variant: neutral" note="Use these for labels that carry no status.">
        <Option name='variant="default"' use="A label in the brand colour. The default.">
          <Badge variant="default">New</Badge>
        </Option>
        <Option name='variant="secondary"' use="A plain filled label, such as a version.">
          <Badge variant="secondary">v1.4.2</Badge>
        </Option>
        <Option name='variant="outline"' use="An outlined label with full-strength text.">
          <Badge variant="outline">Beta</Badge>
        </Option>
        <Option name='variant="elevated"' use="A filled chip for a scope, role or service name.">
          <Badge variant="elevated">models:read</Badge>
        </Option>
        <Option name='variant="dim"' use="A quiet label that should not compete with the page.">
          <Badge variant="dim">orbit-3-turbo</Badge>
        </Option>
        <Option name='variant="reserved"' use="A privileged role such as platform-admin.">
          <Badge variant="reserved">platform-admin</Badge>
        </Option>
      </OptionTable>

      <OptionTable
        title="Variant: identity"
        note="Colours that name a provider or a routing strategy."
      >
        <Option name='variant="provider-*"' use="A model provider: bedrock, vertexai or azure.">
          <div className="flex flex-wrap gap-2">
            <Badge variant="provider-bedrock">AWS Bedrock</Badge>
            <Badge variant="provider-vertexai">Vertex AI</Badge>
            <Badge variant="provider-azure">Azure OpenAI</Badge>
          </div>
        </Option>
        <Option name='variant="strategy-weighted"' use="A weighted traffic split between models.">
          <Badge variant="strategy-weighted">Weighted</Badge>
        </Option>
      </OptionTable>

      <OptionTable title="Size, shape and extras">
        <Option name='size="sm"' use="Compact. For toolbars and dense rows.">
          <Badge variant="secondary" size="sm">
            Compact
          </Badge>
        </Option>
        <Option name='size="md"' use="Standard. The default.">
          <Badge variant="secondary" size="md">
            Standard
          </Badge>
        </Option>
        <Option name='size="lg"' use="Roomier. For role chips and prominent labels.">
          <Badge variant="secondary" size="lg">
            Large
          </Badge>
        </Option>
        <Option name='shape="default"' use="Rounded corners. The default.">
          <Badge variant="secondary">Rounded</Badge>
        </Option>
        <Option name='shape="sharp"' use="Tighter corners, for code and scope chips.">
          <Badge variant="secondary" shape="sharp">
            Sharp
          </Badge>
        </Option>
        <Option name='shape="pill"' use="Fully round, for status pills and role chips.">
          <Badge variant="secondary" shape="pill">
            Pill
          </Badge>
        </Option>
        <Option name="dot" use="Add a small dot before the label. Works best with status variants.">
          <Badge variant="healthy" dot>
            Enabled
          </Badge>
        </Option>
        <Option name="mono" use="Use the code typeface for identifiers and scopes.">
          <Badge variant="elevated" mono>
            inference:anthropic:use
          </Badge>
        </Option>
      </OptionTable>
    </div>
  ),
};
