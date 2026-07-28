import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import { TriggerWizard } from './trigger-wizard';
import { StepDelivery } from './step-delivery';
import { StepReview } from './step-review';
import { StepTarget } from './step-target';
import { KIND_ORDER, PLANNED_KINDS, TRIGGER_KINDS } from './kinds';
import { FIELD_IDS, initialState } from './wizard-state';
import type { TriggerKind, WizardContext } from './types';

const CONTEXT: WizardContext = {
  agents: [{ id: 'at-1', slug: 'coder', name: 'Coder' }],
  sources: [{ id: 'src-1', name: 'acme/widgets', provider: 'github' }],
  ingestHost: 'orbit.local.clearroute.io:8443',
};

function renderWizard(initialKind?: TriggerKind) {
  return render(
    <TriggerWizard
      initialKind={initialKind}
      agents={CONTEXT.agents}
      sources={CONTEXT.sources}
      ingestHost={CONTEXT.ingestHost}
      onCreate={vi.fn()}
      onClose={vi.fn()}
    />,
  );
}

function pickKind(kind: TriggerKind) {
  fireEvent.click(screen.getByText(TRIGGER_KINDS[kind].description));
}

describe('the frame', () => {
  it('keeps the rail mounted on the picker phase, kind first', () => {
    renderWizard();
    // Every phase shows the same rail shape: Kind, then the flow.
    expect(screen.getByText('Kind')).toBeTruthy();
    expect(screen.getByText('Configure')).toBeTruthy();
    expect(screen.getByText('Target')).toBeTruthy();
    expect(screen.getByText('Delivery')).toBeTruthy();
    expect(screen.getByText('Review')).toBeTruthy();
  });

  it('keeps the footer band on the picker phase, with no Back', () => {
    renderWizard();
    expect(screen.getByText('Pick a kind')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Back/ })).toBeNull();
  });

  it('shows one progress display: the rail, not a bar or a counter', () => {
    renderWizard('webhook');
    expect(screen.queryByRole('progressbar')).toBeNull();
    expect(screen.queryByText(/Step \d of \d/)).toBeNull();
  });
});

describe('picker phase', () => {
  it('lists every registry entry with its example, and every planned kind', () => {
    renderWizard();
    for (const id of KIND_ORDER) {
      const def = TRIGGER_KINDS[id];
      expect(screen.getAllByText(def.title).length).toBeGreaterThan(0);
      expect(screen.getByText(def.example)).toBeTruthy();
    }
    for (const planned of PLANNED_KINDS) {
      expect(screen.getByText(planned.title)).toBeTruthy();
    }
  });

  it('enters the chosen kind and names it in the rail', () => {
    renderWizard();
    pickKind('scheduled');
    expect(screen.getByText('How often')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Change' })).toBeTruthy();
  });

  it('keeps what was typed when the same kind is re-picked', () => {
    renderWizard('webhook');
    fireEvent.change(screen.getByLabelText(/Trigger name/), {
      target: { value: 'github-push' },
    });

    fireEvent.click(screen.getByRole('button', { name: 'Change' }));
    pickKind('webhook');

    expect((screen.getByLabelText(/Trigger name/) as HTMLInputElement).value).toBe('github-push');
  });

  it('starts clean when the kind actually changes', () => {
    renderWizard('webhook');
    fireEvent.change(screen.getByLabelText(/Trigger name/), {
      target: { value: 'github-push' },
    });

    fireEvent.click(screen.getByRole('button', { name: 'Change' }));
    pickKind('manual');

    expect((screen.getByLabelText(/Trigger name/) as HTMLInputElement).value).toBe('');
  });
});

describe('step phase', () => {
  const FIRST_STEP: Record<TriggerKind, string> = {
    webhook: 'Source',
    scheduled: 'Schedule',
    manual: 'Inputs',
  };

  it.each(KIND_ORDER)('opens %s on the step that defines the kind', (kind) => {
    const view = renderWizard(kind);
    expect(screen.getAllByText(FIRST_STEP[kind]).length).toBeGreaterThan(0);
    view.unmount();
  });

  it('focuses the step first control on arrival', async () => {
    renderWizard('webhook');
    await waitFor(() => expect(document.activeElement?.id).toBe(FIELD_IDS.name));
  });

  it('leaves Continue enabled and answers with the blocker when pressed', () => {
    renderWizard('webhook');
    const cont = screen.getByRole('button', { name: /Continue/ });
    expect(cont.hasAttribute('disabled')).toBe(false);
    expect(screen.queryByText('Needs a name')).toBeNull();

    fireEvent.click(cont);

    // Once in the footer, and again on the field it points at.
    expect(screen.getAllByText('Needs a name').length).toBeGreaterThan(0);
    expect(document.activeElement?.id).toBe(FIELD_IDS.name);
  });

  it('labels Back plainly, on every step', () => {
    renderWizard('webhook');
    expect(screen.getByRole('button', { name: /Back/ })).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Choose kind/ })).toBeNull();
  });
});

describe('shared steps', () => {
  const state = {
    ...initialState(TRIGGER_KINDS.webhook, CONTEXT.sources),
    name: 'github-push',
    agentTypeId: 'at-1',
  };
  const shared = {
    state,
    patch: vi.fn(),
    context: CONTEXT,
    definition: TRIGGER_KINDS.webhook,
    error: null,
    fieldError: () => undefined,
    firstFieldRef: null,
  };

  it('renders the target step with the policy note under the agent list', () => {
    render(<StepTarget {...shared} />);
    expect(screen.getByText(/Runs start as this agent/)).toBeTruthy();
    expect(screen.getByText('Up to 4 at once')).toBeTruthy();
  });

  it('renders the delivery step', () => {
    render(<StepDelivery {...shared} />);
    expect(screen.getByText('Suppress duplicates')).toBeTruthy();
    expect(screen.getByText(/Orbit gives up after this window/)).toBeTruthy();
  });

  it('renders the review step, including a create error', () => {
    render(<StepReview {...shared} error="source already exists" />);
    expect(screen.getByText('source already exists')).toBeTruthy();
    expect(screen.getByText('Up to 4 at once, then wait')).toBeTruthy();
    expect(screen.getByText('Nothing fires until you create it.')).toBeTruthy();
  });
});
