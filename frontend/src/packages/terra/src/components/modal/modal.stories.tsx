import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Button } from '../button/button';
import { Input } from '../input/input';
import { NativeSelect } from '../native-select/native-select';
import { Textarea } from '../textarea/textarea';
import { Text } from '../typography/typography';
import { FormField } from '../form-field/form-field';
import { Modal, ModalHeader, ModalBody, ModalFooter, ModalAccessibleTitle } from './modal';

const meta: Meta<typeof Modal> = {
  title: 'Terra/Molecules/Modal',
  component: Modal,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof Modal>;

// ── Controlled wrapper ────────────────────────────────────────────────────────

const ModalTrigger = ({
  label,
  size,
  children,
}: {
  label: string;
  size?: 'sm' | 'md' | 'lg' | 'builder';
  children: (onClose: () => void) => React.ReactNode;
}) => {
  const [open, setOpen] = React.useState(false);
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        {label}
      </Button>
      <Modal open={open} onClose={() => setOpen(false)} size={size}>
        {children(() => setOpen(false))}
      </Modal>
    </>
  );
};

// ── Stories ───────────────────────────────────────────────────────────────────

/**
 * The default modal composition: header with title + description, a body, and
 * a two-button footer. Press Escape, click the × button, or click the overlay
 * to dismiss.
 */
export const Default: Story = {
  render: () => (
    <ModalTrigger label="Open modal" size="md">
      {(onClose) => (
        <>
          <ModalHeader
            title="Revoke access token"
            description="The token will be immediately invalidated. Any services using it will lose access."
          />
          <ModalBody>
            <Text size="supporting" color="muted">
              Token{' '}
              <Text as="span" size="supporting" weight="heavy">
                ci-prod-2024
              </Text>{' '}
              was last used 3 minutes ago. Revoking it cannot be undone.
            </Text>
          </ModalBody>
          <ModalFooter>
            <Button variant="outline" size="sm" onClick={onClose}>
              Cancel
            </Button>
            <Button variant="destructive" size="sm" onClick={onClose}>
              Revoke token
            </Button>
          </ModalFooter>
        </>
      )}
    </ModalTrigger>
  ),
};

/**
 * All three width tiers — `sm` (384 px), `md` (448 px), and `lg` (512 px).
 * Choose the narrowest size that comfortably fits the content.
 */
export const Sizes: Story = {
  render: () => (
    <div className="flex items-center gap-3">
      {(['sm', 'md', 'lg'] as const).map((size) => (
        <ModalTrigger key={size} label={`Open ${size}`} size={size}>
          {(onClose) => (
            <>
              <ModalHeader title={`Size: ${size}`} description={`max-w-${size} — modal panel`} />
              <ModalBody>
                <Text size="supporting" color="muted">
                  This modal uses the{' '}
                  <Text as="span" size="supporting" weight="heavy">
                    {size}
                  </Text>{' '}
                  size preset. Use the narrowest size that comfortably fits the content without
                  horizontal scrolling.
                </Text>
              </ModalBody>
              <ModalFooter>
                <Button variant="outline" size="sm" onClick={onClose}>
                  Close
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalTrigger>
      ))}
    </div>
  ),
};

/**
 * Destructive confirmation pattern. A single `ModalBody` paragraph explains
 * the irreversible consequence; the footer leads with a ghost Cancel on the
 * left and a destructive primary CTA on the right.
 */
export const Destructive: Story = {
  render: () => (
    <ModalTrigger label="Delete agent" size="sm">
      {(onClose) => (
        <>
          <ModalHeader title="Delete agent?" showClose={false} />
          <ModalBody>
            <Text size="supporting" color="muted">
              <Text as="span" size="supporting" weight="heavy">
                apollo-3
              </Text>{' '}
              will be permanently removed along with all its policies, credentials, and event
              history. This action cannot be undone.
            </Text>
          </ModalBody>
          <ModalFooter justify="between">
            <Button variant="ghost" size="sm" onClick={onClose}>
              Keep agent
            </Button>
            <Button variant="destructive" size="sm" onClick={onClose}>
              Yes, delete
            </Button>
          </ModalFooter>
        </>
      )}
    </ModalTrigger>
  ),
};

/**
 * Form body pattern: `ModalBody` contains `FormField` elements, footer has
 * Cancel + primary submit. Fields use `id ↔ htmlFor` wiring via `FormField`.
 */
export const WithFormBody: Story = {
  render: () => (
    <ModalTrigger label="Create token" size="md">
      {(onClose) => (
        <>
          <ModalHeader
            title="Create access token"
            description="Tokens grant programmatic access to the Orbit API."
          />
          <ModalBody>
            <FormField
              id="token-name"
              label="Token name"
              hint="Human-readable label for this credential."
            >
              <Input id="token-name" placeholder="e.g. ci-prod" />
            </FormField>
            <FormField
              id="token-scope"
              label="Scope"
              hint="The set of resources this token may access."
            >
              <NativeSelect id="token-scope">
                <option value="inference:read">inference:read</option>
                <option value="inference:write">inference:write</option>
                <option value="policy:read">policy:read</option>
                <option value="admin">admin (all scopes)</option>
              </NativeSelect>
            </FormField>
          </ModalBody>
          <ModalFooter>
            <Button variant="outline" size="sm" onClick={onClose}>
              Cancel
            </Button>
            <Button variant="default" size="sm" onClick={onClose}>
              Create token
            </Button>
          </ModalFooter>
        </>
      )}
    </ModalTrigger>
  ),
};

/**
 * When `showClose={false}` is passed to `ModalHeader`, the × button is hidden.
 * Useful for flows where the modal should only be dismissed via explicit footer
 * actions (e.g. a required acknowledgement step).
 */
export const NoCloseButton: Story = {
  render: () => (
    <ModalTrigger label="Required acknowledgement" size="sm">
      {(onClose) => (
        <>
          <ModalHeader
            title="Review terms of service"
            description="You must accept before continuing."
            showClose={false}
          />
          <ModalBody>
            <Text size="supporting" color="muted">
              By clicking Accept you agree that all agent actions are subject to the
              organisation&apos;s policy framework and that credential access is logged and audited.
            </Text>
          </ModalBody>
          <ModalFooter>
            <Button variant="default" size="sm" onClick={onClose}>
              Accept and continue
            </Button>
          </ModalFooter>
        </>
      )}
    </ModalTrigger>
  ),
};

/**
 * Custom header layout without `ModalHeader`. `ModalAccessibleTitle` provides
 * the visually-hidden title required by Radix for screen-reader compatibility.
 */
export const CustomHeader: Story = {
  render: () => (
    <ModalTrigger label="Custom header" size="md">
      {(onClose) => (
        <>
          <ModalAccessibleTitle>Export audit log</ModalAccessibleTitle>
          {/* Custom header — not using ModalHeader */}
          <div className="flex items-center gap-3 border-b border-border p-5">
            <div className="flex flex-col gap-0.5 flex-1">
              <Text weight="heavy">Export audit log</Text>
              <Text size="supporting" color="muted">
                Download events as CSV or JSON.
              </Text>
            </div>
          </div>
          <ModalBody>
            <FormField
              id="export-format"
              label="Format"
              hint="Choose the output format for your export."
            >
              <NativeSelect id="export-format">
                <option value="csv">CSV</option>
                <option value="json">JSON (newline-delimited)</option>
              </NativeSelect>
            </FormField>
            <FormField
              id="export-notes"
              label="Notes"
              hint="Optional memo attached to the export record."
            >
              <Textarea id="export-notes" placeholder="e.g. Q2 compliance review" rows={3} />
            </FormField>
          </ModalBody>
          <ModalFooter>
            <Button variant="outline" size="sm" onClick={onClose}>
              Cancel
            </Button>
            <Button variant="default" size="sm" onClick={onClose}>
              Export
            </Button>
          </ModalFooter>
        </>
      )}
    </ModalTrigger>
  ),
};

/**
 * The `builder` size — a full workspace modal (1200 × 840 px, clamped to the
 * viewport minus a 2 rem margin). Unlike the width-only sizes it has an
 * explicit height and `overflow-hidden`: children `flex-1` to fill the panel
 * and manage their own internal scrolling (rail and content columns scroll
 * independently). Used by the agent builder, which supplies a custom header
 * with `ModalAccessibleTitle` for the accessible name.
 */
export const BuilderSize: Story = {
  name: 'Size: builder',
  render: () => (
    <ModalTrigger label="Open builder" size="builder">
      {(onClose) => (
        <>
          <ModalAccessibleTitle>Agent builder</ModalAccessibleTitle>
          <div className="flex min-h-0 flex-1">
            <div className="w-56 shrink-0 border-r border-border p-5">
              <Text size="supporting" color="muted">
                Step rail (fixed)
              </Text>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto p-5">
              <Text size="supporting" color="muted">
                Step content — scrolls independently inside the fixed-height panel.
              </Text>
            </div>
          </div>
          <ModalFooter borderless={false}>
            <Button variant="outline" size="sm" onClick={onClose}>
              Close
            </Button>
          </ModalFooter>
        </>
      )}
    </ModalTrigger>
  ),
};
