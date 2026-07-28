import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';

import { TriggerWizard } from './trigger-wizard';
import { StepDelivery } from './step-delivery';
import { StepReview } from './step-review';
import { StepTarget } from './step-target';
import { KIND_ORDER, PLANNED_KINDS, TRIGGER_KINDS } from './kinds';
import { initialState } from './wizard-state';
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

describe('TriggerWizard picker phase', () => {
  it('opens on the picker when no kind is given', () => {
    renderWizard();
    expect(screen.getByText('Choose what starts the run.', { exact: false })).toBeTruthy();
  });

  it('lists every registry entry, and every planned kind', () => {
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

  it('enters the chosen kind and can come back to the picker', () => {
    renderWizard();
    fireEvent.click(screen.getByText(TRIGGER_KINDS.scheduled.description));
    expect(screen.getByText('How often')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: /Choose kind/ }));
    expect(screen.getByText('Choose what starts the run.', { exact: false })).toBeTruthy();
  });
});

describe('TriggerWizard step phase', () => {
  const FIRST_STEP: Record<TriggerKind, string> = {
    webhook: 'Source',
    scheduled: 'Schedule',
    manual: 'Inputs',
  };

  it.each(KIND_ORDER)('opens %s on the step that defines the kind', (kind) => {
    const view = renderWizard(kind);
    expect(screen.getAllByText(FIRST_STEP[kind]).length).toBeGreaterThan(0);
    expect(screen.getByText(`Step 1 of 4`)).toBeTruthy();
    view.unmount();
  });

  it('holds Continue closed until the step validates', () => {
    renderWizard('webhook');
    expect(screen.getByRole('button', { name: /Continue/ }).hasAttribute('disabled')).toBe(true);
    expect(screen.getByText('Give the trigger a name.')).toBeTruthy();
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
  };

  it('renders the target step', () => {
    render(<StepTarget {...shared} />);
    expect(screen.getByText('Max parallel runs')).toBeTruthy();
    expect(screen.getByText('Up to 4 at once')).toBeTruthy();
  });

  it('renders the delivery step', () => {
    render(<StepDelivery {...shared} />);
    expect(screen.getByText('Suppress duplicates')).toBeTruthy();
  });

  it('renders the review step, including a create error', () => {
    render(<StepReview {...shared} error="source already exists" />);
    expect(screen.getByText('source already exists')).toBeTruthy();
    expect(screen.getByText('4 parallel · wait')).toBeTruthy();
  });
});
