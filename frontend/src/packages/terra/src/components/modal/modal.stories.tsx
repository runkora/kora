import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Button } from '../button/button';
import { FormField } from '../form-field/form-field';
import { Input } from '../input/input';
import { Text } from '../typography/typography';
import { Modal, ModalBody, ModalFooter, ModalHeader } from './modal';

/**
 * A box that opens over the page and asks for the user's attention. Use it for
 * confirmations and short forms.
 */
const meta: Meta<typeof Modal> = {
  title: 'Terra/Molecules/Modal',
  component: Modal,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Modal>;

type ModalSize = 'sm' | 'md' | 'lg' | 'builder';

function ConfirmContent({ onClose, destructive }: { onClose: () => void; destructive?: boolean }) {
  return (
    <>
      <ModalHeader
        title={destructive ? 'Delete agent?' : 'Unsaved changes'}
        description={
          destructive
            ? 'Billing agent and its run history will be removed.'
            : 'Save your edits before you leave this page.'
        }
      />
      <ModalBody>
        <Text size="supporting" color="muted">
          {destructive
            ? 'You cannot undo this. Anyone using the agent will lose access.'
            : 'If you leave now, your edits to the agent settings will be lost.'}
        </Text>
      </ModalBody>
      <ModalFooter>
        <Button variant="outline" size="sm" onClick={onClose}>
          Cancel
        </Button>
        <Button variant={destructive ? 'destructive' : 'default'} size="sm" onClick={onClose}>
          {destructive ? 'Delete agent' : 'Save changes'}
        </Button>
      </ModalFooter>
    </>
  );
}

function FormContent({ onClose }: { onClose: () => void }) {
  return (
    <>
      <ModalHeader title="New agent" description="Give the agent a name and a short purpose." />
      <ModalBody>
        <FormField label="Name" id="modal-agent-name">
          <Input id="modal-agent-name" placeholder="Billing agent" />
        </FormField>
        <FormField label="Purpose" id="modal-agent-purpose">
          <Input id="modal-agent-purpose" placeholder="Reads invoices" />
        </FormField>
      </ModalBody>
      <ModalFooter>
        <Button variant="outline" size="sm" onClick={onClose}>
          Cancel
        </Button>
        <Button size="sm" onClick={onClose}>
          Create agent
        </Button>
      </ModalFooter>
    </>
  );
}

function ScrollContent({ onClose }: { onClose: () => void }) {
  const scopes = ['agents:read', 'agents:write', 'runs:read', 'runs:write', 'models:read'];
  return (
    <>
      <ModalHeader title="Token scopes" description="What this token is allowed to do." />
      <ModalBody constrained>
        {Array.from({ length: 4 }, (_, round) =>
          scopes.map((scope) => (
            <Text key={`${round}-${scope}`} size="supporting" mono>
              {scope}
            </Text>
          )),
        )}
      </ModalBody>
      <ModalFooter>
        <Button size="sm" onClick={onClose}>
          Done
        </Button>
      </ModalFooter>
    </>
  );
}

function BuilderContent({ onClose }: { onClose: () => void }) {
  return (
    <>
      <ModalHeader title="Agent builder" description="Set up an agent step by step." />
      <ModalBody>
        <Text size="supporting" color="muted">
          The panel fills most of the screen. Content inside manages its own scrolling.
        </Text>
      </ModalBody>
      <ModalFooter>
        <Button size="sm" onClick={onClose}>
          Close
        </Button>
      </ModalFooter>
    </>
  );
}

function RequiredContent({ onClose }: { onClose: () => void }) {
  return (
    <>
      <ModalHeader title="Copy your token" description="It is shown only once." showClose={false} />
      <ModalBody>
        <Text size="supporting" mono>
          orb_3f9a1c07
        </Text>
      </ModalBody>
      <ModalFooter>
        <Button size="sm" onClick={onClose}>
          I have copied it
        </Button>
      </ModalFooter>
    </>
  );
}

function OpenButton({
  label,
  size,
  dismissOnOutsideClick,
  children,
}: {
  label: string;
  size?: ModalSize;
  dismissOnOutsideClick?: boolean;
  children: (onClose: () => void) => React.ReactNode;
}) {
  const [open, setOpen] = React.useState(false);
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        {label}
      </Button>
      <Modal
        open={open}
        onClose={() => setOpen(false)}
        {...(size ? { size } : {})}
        {...(dismissOnOutsideClick === undefined ? {} : { dismissOnOutsideClick })}
      >
        {children(() => setOpen(false))}
      </Modal>
    </>
  );
}

function OpenModal() {
  const [open, setOpen] = React.useState(false);
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        Open modal
      </Button>
      <Modal open={open} onClose={() => setOpen(false)}>
        <ConfirmContent destructive onClose={() => setOpen(false)} />
      </Modal>
    </>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl">
      <Example title="Confirmation">
        <Text size="supporting" color="muted">
          The modal opens over the page. Select the button to see it.
        </Text>
        <OpenModal />
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Options"
        note="Size and dismissal go on Modal. Each button opens its own sample."
      >
        <Option name='size="sm" (384px wide)' use="For short confirmations.">
          <OpenButton label="Open small modal" size="sm">
            {(onClose) => <ConfirmContent onClose={onClose} />}
          </OpenButton>
        </Option>
        <Option name='size="md" (448px wide)' use="The default. Fits a short form.">
          <OpenButton label="Open medium modal" size="md">
            {(onClose) => <FormContent onClose={onClose} />}
          </OpenButton>
        </Option>
        <Option name='size="lg" (512px wide)' use="For forms with longer labels or more fields.">
          <OpenButton label="Open wide modal" size="lg">
            {(onClose) => <FormContent onClose={onClose} />}
          </OpenButton>
        </Option>
        <Option
          name='size="builder" (up to 1200px wide)'
          use="For a full workspace such as a step-by-step builder."
        >
          <OpenButton label="Open builder modal" size="builder">
            {(onClose) => <BuilderContent onClose={onClose} />}
          </OpenButton>
        </Option>
        <Option
          name="ModalFooter with a destructive button"
          use="For actions that cannot be undone."
        >
          <OpenButton label="Open destructive modal">
            {(onClose) => <ConfirmContent destructive onClose={onClose} />}
          </OpenButton>
        </Option>
        <Option
          name="ModalBody constrained"
          use="Keeps a long body inside the screen and scrolls it."
        >
          <OpenButton label="Open scrolling modal">
            {(onClose) => <ScrollContent onClose={onClose} />}
          </OpenButton>
        </Option>
        <Option
          name="dismissOnOutsideClick={false}"
          use="Clicking outside does not close it. For steps the user must finish."
        >
          <OpenButton label="Open required modal" dismissOnOutsideClick={false}>
            {(onClose) => <RequiredContent onClose={onClose} />}
          </OpenButton>
        </Option>
      </OptionTable>
    </div>
  ),
};
