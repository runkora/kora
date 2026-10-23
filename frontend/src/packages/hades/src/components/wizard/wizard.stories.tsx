import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import {
  Button,
  Card,
  CardContent,
  FormField,
  Grid,
  Heading,
  Icon,
  Input,
  RadioCard,
  RadioGroup,
  SegmentedControl,
  Stack,
  Text,
  Textarea,
} from '../../index';
import type { IconName } from '../icon/icon';
import { Frame, Option, OptionTable } from '../../docs/story-ui';
import { Wizard } from './wizard';

/**
 * A wizard is a full-screen builder that walks someone through creating one thing in ordered
 * steps. It has a step rail, a footer with Back and Continue, and a review step before the final
 * submit. Use it when the input is too big for a dialog and the order of the steps matters.
 */
const meta: Meta<typeof Wizard> = {
  title: 'Hades/Organisms/Wizard',
  component: Wizard,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;

type Story = StoryObj<typeof Wizard>;

const STEPS = [
  { id: 'basics', title: 'Basics', sub: 'Name and describe it' },
  { id: 'delivery', title: 'Delivery', sub: 'How events arrive' },
  { id: 'review', title: 'Review', sub: 'Check and create' },
];

type Kind = { id: string; icon: IconName; title: string; description: string };

const KINDS: Kind[] = [
  {
    id: 'Webhook',
    icon: 'webhook',
    title: 'Webhook',
    description: 'Starts when another system sends a request.',
  },
  {
    id: 'Schedule',
    icon: 'clock',
    title: 'Schedule',
    description: 'Starts at set times, like every weekday at 9am.',
  },
  {
    id: 'Email',
    icon: 'send',
    title: 'Email',
    description: 'Starts when a message arrives at an address.',
  },
];

const RailNote = () => (
  <Card subtle>
    <CardContent padded compact>
      <Stack gap="tight">
        <Stack direction="horizontal" align="center" gap="snug">
          <Icon name="info" size="xs" color="accent" label="" />
          <Text as="span" size="caption" weight="heavy" color="muted">
            Checked before it runs
          </Text>
        </Stack>
        <Text size="micro" color="subtle">
          Every run is checked against the agent&apos;s permissions first.
        </Text>
      </Stack>
    </CardContent>
  </Card>
);

const StepHeading = ({ title, description }: { title: string; description: string }) => (
  <Stack gap="snug">
    <Heading variant="subsection">{title}</Heading>
    <Text size="supporting" color="muted">
      {description}
    </Text>
  </Stack>
);

const SaveStatus = ({ saving }: { saving: boolean }) => (
  <Stack direction="horizontal" align="center" gap="snug" paddingInline="comfortable">
    <Icon name={saving ? 'refresh-cw' : 'check-circle'} size="xs" color="subtle" label="" />
    <Text as="span" size="micro" color="subtle">
      {saving ? 'Saving…' : 'Saved'}
    </Text>
  </Stack>
);

const DELIVERY_OPTIONS = [
  { value: 'each', label: 'One run per event' },
  { value: 'batch', label: 'Group events' },
];

type Phase = 'pick' | 'flow' | 'done';

function TriggerWizardDemo() {
  const [open, setOpen] = React.useState(false);
  const [phase, setPhase] = React.useState<Phase>('pick');
  const [kind, setKind] = React.useState<Kind>(KINDS[0] as Kind);
  const [step, setStep] = React.useState(0);
  const [maxVisited, setMaxVisited] = React.useState(0);
  const [name, setName] = React.useState('');
  const [description, setDescription] = React.useState('');
  const [delivery, setDelivery] = React.useState('batch');
  const [saving, setSaving] = React.useState(false);
  const [pending, setPending] = React.useState(false);
  const timers = React.useRef<number[]>([]);

  React.useEffect(() => {
    const active = timers.current;
    return () => active.forEach((t) => window.clearTimeout(t));
  }, []);

  const later = (fn: () => void, ms: number) => {
    timers.current.push(window.setTimeout(fn, ms));
  };

  const edit = (apply: () => void) => {
    apply();
    setSaving(true);
    later(() => setSaving(false), 700);
  };

  const start = () => {
    setPhase('pick');
    setStep(0);
    setMaxVisited(0);
    setName('');
    setDescription('');
    setDelivery('batch');
    setPending(false);
    setSaving(false);
    setOpen(true);
  };

  const choose = (id: string) => {
    setKind(KINDS.find((k) => k.id === id) ?? (KINDS[0] as Kind));
    setPhase('flow');
  };

  const goTo = (index: number) => {
    setStep(index);
    setMaxVisited((m) => Math.max(m, index));
  };

  const isLast = step === STEPS.length - 1;
  const nameMissing = phase === 'flow' && step === 0 && name.trim() === '';
  const batchWarning = phase === 'flow' && step === 1 && delivery === 'each';

  let hint: string | undefined;
  if (nameMissing) hint = 'Add a name to continue.';
  if (batchWarning)
    hint = 'A busy source can start many runs at once. Group events to slow it down.';

  const submit = () => {
    setPending(true);
    later(() => {
      setPending(false);
      setPhase('done');
    }, 1200);
  };

  const inFlow = phase === 'flow';

  return (
    <>
      <Button onClick={start}>Open the wizard</Button>
      <Wizard
        open={open}
        onClose={() => setOpen(false)}
        icon={phase === 'pick' ? 'zap' : kind.icon}
        title="New trigger"
        subtitle="drf_8f21c0"
        hintVariant={nameMissing ? 'error' : 'warning'}
        {...(hint ? { hint } : {})}
        {...(inFlow
          ? {
              actions: <SaveStatus saving={saving} />,
              rail: (
                <Wizard.Rail
                  label="New trigger"
                  steps={STEPS}
                  current={step}
                  maxVisited={maxVisited}
                  onGoTo={setStep}
                  footer={<RailNote />}
                />
              ),
              footer: (
                <Wizard.Footer
                  step={step}
                  totalSteps={STEPS.length}
                  onNext={() => goTo(step + 1)}
                  nextDisabled={nameMissing}
                  submitLabel="Create trigger"
                  pending={pending}
                  pendingLabel="Creating…"
                  {...(step > 0 ? { onBack: () => setStep(step - 1) } : {})}
                  {...(isLast ? { onSubmit: submit } : {})}
                />
              ),
            }
          : {})}
      >
        {phase === 'pick' && (
          <Wizard.Step fullPage>
            <Stack grow overflowY="auto" align="center">
              <Stack
                minWidth={{ lg: 'document' }}
                maxWidth={{ lg: 'document' }}
                padding="loose"
                gap="relaxed"
              >
                <StepHeading
                  title="New trigger"
                  description="Choose what starts it. You can change the details on the next steps."
                />
                <RadioGroup value="" aria-label="Trigger kind">
                  <Grid cols={3} gap="comfortable">
                    {KINDS.map((k) => (
                      <RadioCard
                        key={k.id}
                        value={k.id}
                        selected={false}
                        onSelect={choose}
                        icon={k.icon}
                        title={k.title}
                        description={k.description}
                      />
                    ))}
                  </Grid>
                </RadioGroup>
              </Stack>
            </Stack>
          </Wizard.Step>
        )}

        {inFlow && step === 0 && (
          <Wizard.Step>
            <StepHeading
              title="Basics"
              description={`Give the ${kind.title.toLowerCase()} trigger a name people will recognise.`}
            />
            <FormField label="Name" required>
              <Input
                value={name}
                placeholder="Support inbox"
                onChange={(e) => edit(() => setName(e.target.value))}
              />
            </FormField>
            <FormField label="Description" hint="Optional. Shown in the trigger list.">
              <Textarea
                value={description}
                placeholder="Starts the triage agent for each new support request."
                onChange={(e) => edit(() => setDescription(e.target.value))}
              />
            </FormField>
          </Wizard.Step>
        )}

        {inFlow && step === 1 && (
          <Wizard.Step>
            <StepHeading title="Delivery" description="Choose how events turn into runs." />
            <Stack gap="snug">
              <Text weight="heavy">Delivery mode</Text>
              <SegmentedControl
                aria-label="Delivery mode"
                options={DELIVERY_OPTIONS}
                value={delivery}
                onChange={(v) => edit(() => setDelivery(v))}
              />
              <Text size="supporting" color="muted">
                {delivery === 'each'
                  ? 'Every event starts its own run right away.'
                  : 'Events are collected and sent to one run every 5 minutes.'}
              </Text>
            </Stack>
          </Wizard.Step>
        )}

        {inFlow && step === 2 && (
          <Wizard.Step>
            <StepHeading
              title="Review"
              description="Check the details. Use Edit to go back and change anything."
            />
            <Wizard.ReviewStep>
              <Wizard.ReviewCard
                icon={kind.icon}
                title="Basics"
                onEdit={() => setStep(0)}
                rows={[
                  { label: 'Kind', value: kind.title },
                  { label: 'Name', value: name },
                ]}
              >
                <Stack paddingBlock="snug">
                  <Text size="caption" color={description ? 'foreground' : 'subtle'}>
                    {description || 'No description'}
                  </Text>
                </Stack>
              </Wizard.ReviewCard>
              <Wizard.ReviewCard
                icon="route"
                title="Delivery"
                onEdit={() => setStep(1)}
                rows={[
                  {
                    label: 'Mode',
                    value: delivery === 'each' ? 'One run per event' : 'Group events',
                  },
                  {
                    label: 'Window',
                    value: delivery === 'each' ? 'None' : '5 minutes',
                  },
                ]}
              />
              <Wizard.ReviewCard
                icon="shield-check"
                title="Access"
                rows={[
                  { label: 'Runs as', value: 'support-triage', mono: true },
                  { label: 'Checked by', value: 'Agent permissions' },
                ]}
              />
            </Wizard.ReviewStep>
          </Wizard.Step>
        )}

        {phase === 'done' && (
          <Wizard.Step fullPage>
            <Stack grow align="center" justify="center" gap="relaxed" padding="loose">
              <Icon
                name="check-circle"
                size="md"
                container
                containerVariant="accent"
                color="accent"
                label=""
              />
              <Stack gap="snug" align="center" textAlign="center">
                <Heading variant="subsection">Trigger created</Heading>
                <Text size="supporting" color="muted">
                  {name} is live and will start runs as soon as events arrive.
                </Text>
              </Stack>
              <Button onClick={() => setOpen(false)}>Done</Button>
            </Stack>
          </Wizard.Step>
        )}
      </Wizard>
    </>
  );
}

/**
 * The whole flow with local state: pick a kind, fill in three steps, review, submit, and finish.
 * Continue stays disabled until the name is filled, the delivery step shows a warning for one
 * option, and the rail lets you jump back to any step you have reached.
 */
export const Overview: Story = {
  render: () => <TriggerWizardDemo />,
};

function SuccessDemo() {
  const [open, setOpen] = React.useState(false);
  return (
    <>
      <Button onClick={() => setOpen(true)}>Open success screen</Button>
      {open && (
        <Wizard open onClose={() => setOpen(false)} icon="shield-check" title="Create grant">
          <Wizard.Success
            title="Grant created"
            description="Ada Lovelace now has 3 scopes."
            actions={
              <>
                <Button variant="secondary" onClick={() => setOpen(false)}>
                  Done
                </Button>
                <Button onClick={() => setOpen(false)}>
                  Create another
                  <Icon name="plus" />
                </Button>
              </>
            }
          />
        </Wizard>
      )}
    </>
  );
}

/**
 * `Wizard.Success` is the post-flow screen. As a direct child of `Wizard` it hides the rail and
 * footer and freezes the progress bar. Pass `children` for extra body, such as a one-time secret.
 */
export const Success: Story = {
  render: () => <SuccessDemo />,
};

type Demo = 'error-hint' | 'warning-hint' | 'toggle-hint' | 'actions' | 'full-page';

const DEMO_TITLES: Record<Demo, string> = {
  'error-hint': 'Error hint',
  'warning-hint': 'Warning hint',
  'toggle-hint': 'Toggling hint',
  actions: 'Header actions',
  'full-page': 'Full page step',
};

function MiniWizard({ demo, onClose }: { demo: Demo; onClose: () => void }) {
  const fullPage = demo === 'full-page';
  const [toggled, setToggled] = React.useState(false);
  const hint =
    demo === 'error-hint'
      ? 'Add a name to continue.'
      : demo === 'warning-hint'
        ? 'A busy source can start many runs at once.'
        : demo === 'toggle-hint' && toggled
          ? 'Add a name to continue.'
          : undefined;

  return (
    <Wizard
      open
      onClose={onClose}
      icon="zap"
      title={DEMO_TITLES[demo]}
      subtitle="drf_8f21c0"
      hintVariant={demo === 'warning-hint' ? 'warning' : 'error'}
      {...(hint ? { hint } : {})}
      {...(demo === 'actions'
        ? {
            actions: (
              <Stack direction="horizontal" align="center" gap="snug" paddingInline="comfortable">
                <Icon name="check-circle" size="xs" color="subtle" label="" />
                <Text as="span" size="micro" color="subtle">
                  Saved
                </Text>
              </Stack>
            ),
          }
        : {})}
      {...(fullPage
        ? {}
        : {
            footer: (
              <Wizard.Footer
                step={0}
                totalSteps={3}
                onNext={onClose}
                nextDisabled={demo === 'error-hint'}
              />
            ),
          })}
    >
      <Wizard.Step fullPage={fullPage}>
        <Stack grow align="center" justify="center" gap="relaxed">
          <Text color="muted">
            {fullPage
              ? 'A full page step hides the rail and footer.'
              : 'The banner sits above the step content.'}
          </Text>
          {demo === 'toggle-hint' && (
            <Button variant="secondary" onClick={() => setToggled((on) => !on)}>
              {toggled ? 'Clear the hint' : 'Show the hint'}
            </Button>
          )}
          {fullPage && <Button onClick={onClose}>Done</Button>}
        </Stack>
      </Wizard.Step>
    </Wizard>
  );
}

function RailDemo({
  initial,
  maxVisited,
  footer,
}: {
  initial: number;
  maxVisited: number;
  footer?: React.ReactNode;
}) {
  const [current, setCurrent] = React.useState(initial);
  return (
    <Frame>
      <Wizard.Rail
        label="New trigger"
        steps={STEPS}
        current={current}
        maxVisited={maxVisited}
        onGoTo={setCurrent}
        footer={footer}
      />
    </Frame>
  );
}

const noop = () => undefined;

function PartsDemo() {
  const [demo, setDemo] = React.useState<Demo | null>(null);
  const opener = (d: Demo) => (
    <Button variant="secondary" onClick={() => setDemo(d)}>
      Open {DEMO_TITLES[d].toLowerCase()} demo
    </Button>
  );

  return (
    <Stack gap="loose">
      <OptionTable
        title="Wizard.Rail"
        note="The step list. It shows where you are, and lets you go back to steps you have reached."
      >
        <Option name="First step" use="Later steps are disabled until they are reached.">
          <RailDemo initial={0} maxVisited={0} />
        </Option>
        <Option
          name="Middle step"
          use="Earlier steps show a check. Steps up to maxVisited can be clicked."
        >
          <RailDemo initial={1} maxVisited={2} />
        </Option>
        <Option name="All complete" use="Set current past the last step.">
          <RailDemo initial={STEPS.length} maxVisited={2} />
        </Option>
        <Option name="footer" use="An info card pinned to the bottom of the rail.">
          <RailDemo initial={0} maxVisited={1} footer={<RailNote />} />
        </Option>
      </OptionTable>

      <OptionTable
        title="Wizard.Footer"
        note="Back, step count, and the main action. Handlers are empty here."
      >
        <Option name="First step" use="Leave out onBack and the Back button is hidden.">
          <Frame>
            <Wizard.Footer step={0} totalSteps={3} onNext={noop} />
          </Frame>
        </Option>
        <Option name="Middle of the flow" use="Back and Continue.">
          <Frame>
            <Wizard.Footer step={1} totalSteps={3} onBack={noop} onNext={noop} />
          </Frame>
        </Option>
        <Option name="Last step" use="Pass onSubmit to swap Continue for the submit action.">
          <Frame>
            <Wizard.Footer
              step={2}
              totalSteps={3}
              onBack={noop}
              onNext={noop}
              onSubmit={noop}
              submitLabel="Create trigger"
            />
          </Frame>
        </Option>
        <Option name="pending" use="Disables both buttons and shows pendingLabel while saving.">
          <Frame>
            <Wizard.Footer
              step={2}
              totalSteps={3}
              onBack={noop}
              onNext={noop}
              onSubmit={noop}
              submitLabel="Create trigger"
              pending
              pendingLabel="Creating…"
            />
          </Frame>
        </Option>
        <Option name="backLabel" use="Rename the Back button.">
          <Frame>
            <Wizard.Footer
              step={1}
              totalSteps={3}
              onBack={noop}
              backLabel="Previous"
              onNext={noop}
            />
          </Frame>
        </Option>
      </OptionTable>

      <OptionTable
        title="Wizard.ReviewCard"
        note="A summary of one step, used on the review step inside Wizard.ReviewStep."
      >
        <Option name="rows" use="Label and value pairs. Set mono for ids and code.">
          <Wizard.ReviewCard
            icon="webhook"
            title="Basics"
            rows={[
              { label: 'Kind', value: 'Webhook' },
              { label: 'Name', value: 'Support inbox' },
              { label: 'Runs as', value: 'support-triage', mono: true },
            ]}
          />
        </Option>
        <Option name="onEdit" use="Adds an Edit button that takes the reader back to that step.">
          <Wizard.ReviewCard
            icon="route"
            title="Delivery"
            onEdit={noop}
            rows={[
              { label: 'Mode', value: 'Group events' },
              { label: 'Window', value: '5 minutes' },
            ]}
          />
        </Option>
        <Option name="children" use="Block content under the rows, like a long description.">
          <Wizard.ReviewCard
            icon="file-text"
            title="Description"
            onEdit={noop}
            rows={[{ label: 'Length', value: '61 characters' }]}
          >
            <Stack paddingBlock="snug">
              <Text size="caption">Starts the triage agent for each new support request.</Text>
            </Stack>
          </Wizard.ReviewCard>
        </Option>
      </OptionTable>

      <OptionTable
        title="Only inside an open wizard"
        note="These need the full-screen modal. Each button opens a small wizard that shows just that part."
      >
        <Option
          name="hint"
          use="Red banner when the step is invalid. Pair it with nextDisabled on the footer."
        >
          {opener('error-hint')}
        </Option>
        <Option
          name='hintVariant="warning"'
          use="Amber banner when the step is valid but worth a second look."
        >
          {opener('warning-hint')}
        </Option>
        <Option
          name="hint animation"
          use="The banner rolls down when a hint is set and up when it clears. Toggle it to watch the content move."
        >
          {opener('toggle-hint')}
        </Option>
        <Option name="actions" use="Controls in the header, like a save status.">
          {opener('actions')}
        </Option>
        <Option
          name="Wizard.Step fullPage"
          use="Fills the modal with no rail or footer. Use it before the flow, such as a kind picker."
        >
          {opener('full-page')}
        </Option>
      </OptionTable>

      {demo && <MiniWizard demo={demo} onClose={() => setDemo(null)} />}
    </Stack>
  );
}

/**
 * The pieces that render on their own, shown inline, plus small demos for the parts that only
 * work inside an open wizard.
 */
export const Parts: Story = {
  render: () => <PartsDemo />,
};
