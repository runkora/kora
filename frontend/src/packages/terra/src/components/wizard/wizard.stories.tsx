import type { Meta, StoryObj } from '@storybook/react';
import { Card, CardContent, Icon, Stack, Text } from '../../index';
import { Wizard } from './wizard';

const meta: Meta<typeof Wizard> = {
  title: 'Terra/Organisms/Wizard',
  component: Wizard,
  tags: ['autodocs'],
  parameters: {
    layout: 'fullscreen',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#070a0f' }],
    },
  },
};
export default meta;

type Story = StoryObj<typeof Wizard>;

const STEPS = [
  { id: 'identity', title: 'Identity', sub: 'Name and slug' },
  { id: 'behaviour', title: 'Behaviour', sub: 'Model and instructions' },
  { id: 'tools', title: 'Tools & access', sub: 'MCP servers, scopes' },
  { id: 'guardrails', title: 'Guardrails', sub: 'Budgets and capacity' },
  { id: 'review', title: 'Review', sub: 'Confirm and create' },
];

const PolicyCheckedInfo = () => (
  <Card subtle>
    <CardContent padded compact>
      <Stack gap="tight">
        <Stack direction="horizontal" align="center" gap="snug">
          <Icon name="info" size="xs" color="accent" label="" />
          <Text as="span" size="caption" weight="heavy" color="muted">
            Policy-checked
          </Text>
        </Stack>
        <Text size="micro" color="subtle">
          A trigger starts work as the agent it targets. Every run is checked against that
          agent&apos;s grants before anything executes.
        </Text>
      </Stack>
    </CardContent>
  </Card>
);

const IdentityFirstInfo = () => (
  <Card subtle>
    <CardContent padded compact>
      <Stack gap="tight">
        <Stack direction="horizontal" align="center" gap="snug">
          <Icon name="info" size="xs" color="accent" label="" />
          <Text as="span" size="caption" weight="heavy" color="muted">
            Identity-first
          </Text>
        </Stack>
        <Text size="micro" color="subtle">
          Every agent gets a verified identity before it can touch a single tool. Access is checked
          before execution, never after.
        </Text>
      </Stack>
    </CardContent>
  </Card>
);

// ── Default ───────────────────────────────────────────────────────────────────

export const Default: Story = {
  render: () => (
    <Wizard
      open
      onClose={() => undefined}
      icon="zap"
      title="New agent"
      subtitle="draft · abc123"
      rail={
        <Wizard.Rail
          label="Create agent"
          steps={STEPS}
          current={1}
          maxVisited={2}
          onGoTo={() => undefined}
          footer={<IdentityFirstInfo />}
        />
      }
      footer={
        <Wizard.Footer
          step={1}
          totalSteps={STEPS.length}
          onBack={() => undefined}
          onNext={() => undefined}
        />
      }
    >
      <Wizard.Step>
        <Text size="supporting" color="muted">
          Step content goes here.
        </Text>
      </Wizard.Step>
    </Wizard>
  ),
};

// ── Header variants (via Wizard props) ────────────────────────────────────────

export const WithSubtitle: Story = {
  render: () => (
    <Wizard
      open
      onClose={() => undefined}
      icon="zap"
      title="New agent"
      subtitle="draft · abc123"
      rail={
        <Wizard.Rail
          label="Create agent"
          steps={STEPS}
          current={0}
          maxVisited={0}
          onGoTo={() => undefined}
        />
      }
      footer={<Wizard.Footer step={0} totalSteps={STEPS.length} onNext={() => undefined} />}
    >
      <Wizard.Step>
        <Text size="supporting" color="muted">Step content.</Text>
      </Wizard.Step>
    </Wizard>
  ),
};

export const WithActions: Story = {
  render: () => (
    <Wizard
      open
      onClose={() => undefined}
      icon="zap"
      title="New agent"
      subtitle="draft · abc123"
      actions={
        <Stack direction="horizontal" align="center" gap="snug">
          <Text as="span" size="caption" color="subtle">
            Draft saved
          </Text>
        </Stack>
      }
      rail={
        <Wizard.Rail
          label="Create agent"
          steps={STEPS}
          current={0}
          maxVisited={0}
          onGoTo={() => undefined}
        />
      }
      footer={<Wizard.Footer step={0} totalSteps={STEPS.length} onNext={() => undefined} />}
    >
      <Wizard.Step>
        <Text size="supporting" color="muted">Step content.</Text>
      </Wizard.Step>
    </Wizard>
  ),
};

// ── fullPage step ─────────────────────────────────────────────────────────────

export const FullPageStep: Story = {
  render: () => (
    <Wizard open onClose={() => undefined} icon="zap" title="New agent">
      <Wizard.Step fullPage>
        <Stack grow align="center" justify="center" gap="comfortable" padding="loose">
          <Icon
            name="check-circle"
            size="xl"
            color="accent"
            container
            containerVariant="accent"
            circle
            label=""
          />
          <Text size="supporting" color="muted" align="center">
            Full-page content — no rail or footer. Progress bar frozen.
          </Text>
        </Stack>
      </Wizard.Step>
    </Wizard>
  ),
};

// ── Footer variants ───────────────────────────────────────────────────────────

export const FooterFirstStep: Story = {
  render: () => (
    <Wizard.Footer step={0} totalSteps={5} onNext={() => undefined} nextDisabled={false} />
  ),
};

export const FooterMidFlow: Story = {
  render: () => (
    <Wizard.Footer step={2} totalSteps={5} onBack={() => undefined} onNext={() => undefined} />
  ),
};

export const FooterSubmit: Story = {
  render: () => (
    <Wizard.Footer
      step={4}
      totalSteps={5}
      onBack={() => undefined}
      onNext={() => undefined}
      onSubmit={() => undefined}
      submitLabel="Create trigger"
    />
  ),
};

export const FooterPending: Story = {
  render: () => (
    <Wizard.Footer
      step={4}
      totalSteps={5}
      onBack={() => undefined}
      onNext={() => undefined}
      onSubmit={() => undefined}
      pending
    />
  ),
};

export const FooterWithHint: Story = {
  render: () => (
    <Wizard.Footer
      step={1}
      totalSteps={5}
      onBack={() => undefined}
      onNext={() => undefined}
      nextDisabled
      hint="Name is required before continuing"
    />
  ),
};

export const FooterChooseKindBack: Story = {
  render: () => (
    <Wizard.Footer
      step={0}
      totalSteps={4}
      onBack={() => undefined}
      backLabel="Choose kind"
      onNext={() => undefined}
    />
  ),
};

// ── Review screen ─────────────────────────────────────────────────────────────

export const ReviewScreen: Story = {
  render: () => (
    <Wizard.ReviewStep>
      <Wizard.ReviewCard
        icon="user"
        title="Identity"
        onEdit={() => undefined}
        rows={[
          { label: 'Name', value: 'my-agent' },
          { label: 'Slug', value: 'my-agent', mono: true },
          { label: 'Identity model', value: 'SPIFFE / SVID' },
        ]}
      />
      <Wizard.ReviewCard
        icon="bot"
        title="Behaviour"
        onEdit={() => undefined}
        rows={[
          { label: 'Model', value: 'claude-sonnet-4-5 · Anthropic' },
          { label: 'Framework', value: 'Claude Code' },
          { label: 'Instructions', value: 'You are a helpful assistant…' },
        ]}
      />
      <Wizard.ReviewCard
        icon="zap"
        title="Scheduled trigger"
        onEdit={() => undefined}
        rows={[
          { label: 'Name', value: 'nightly-scan', mono: true },
          { label: 'Kind', value: 'Scheduled' },
          { label: 'Schedule', value: '0 2 * * *', mono: true },
        ]}
      >
        <Stack gap="tight" paddingBlock="snug">
          <Text as="span" size="caption" color="subtle">
            Prompt
          </Text>
          <Text size="caption" mono color="muted" preserveWhitespace>
            {'Run the nightly security scan.\nReport findings to the webhook.'}
          </Text>
        </Stack>
      </Wizard.ReviewCard>
    </Wizard.ReviewStep>
  ),
};

// ── Rail states ───────────────────────────────────────────────────────────────

export const RailStep0: Story = {
  render: () => (
    <Wizard.Rail
      label="Create agent"
      steps={STEPS}
      current={0}
      maxVisited={0}
      onGoTo={() => undefined}
      footer={<IdentityFirstInfo />}
    />
  ),
};

export const RailStep2: Story = {
  render: () => (
    <Wizard.Rail
      label="Create agent"
      steps={STEPS}
      current={2}
      maxVisited={2}
      onGoTo={() => undefined}
      footer={<IdentityFirstInfo />}
    />
  ),
};

export const RailAllComplete: Story = {
  render: () => (
    <Wizard.Rail
      label="Create agent"
      steps={STEPS}
      current={STEPS.length}
      maxVisited={STEPS.length - 1}
      onGoTo={() => undefined}
      footer={<IdentityFirstInfo />}
    />
  ),
};

// ── Rail footer variants ──────────────────────────────────────────────────────

export const RailFooterPolicyChecked: Story = {
  render: () => (
    <Wizard.Rail
      label="New trigger"
      steps={STEPS.slice(0, 3)}
      current={0}
      maxVisited={0}
      onGoTo={() => undefined}
      footer={<PolicyCheckedInfo />}
    />
  ),
};

export const RailFooterIdentityFirst: Story = {
  render: () => (
    <Wizard.Rail
      label="Create agent"
      steps={STEPS.slice(0, 3)}
      current={0}
      maxVisited={0}
      onGoTo={() => undefined}
      footer={<IdentityFirstInfo />}
    />
  ),
};
