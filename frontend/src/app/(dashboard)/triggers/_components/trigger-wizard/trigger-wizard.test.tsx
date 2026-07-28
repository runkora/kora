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

describe('filters and prompt fields', () => {
  it('offers the source provider fields behind the Insert field picker', () => {
    renderWizard('webhook');
    // src-1 is a github source, so github fields, unnarrowed on "any event".
    fireEvent.click(screen.getByRole('button', { name: /Insert field/ }));
    expect(screen.getByText('repo.full_name')).toBeTruthy();
    expect(screen.getByText('pr.base_ref')).toBeTruthy();
  });

  it('inserts a placeholder into the prompt when a field is picked', () => {
    renderWizard('webhook');
    fireEvent.click(screen.getByRole('button', { name: /Insert field/ }));
    fireEvent.click(screen.getByText('repo.full_name'));
    // The editor serialises tokens back to braces; the pill shows the bare token.
    const prompt = screen.getByRole('textbox', { name: 'Prompt' });
    expect(prompt.textContent).toContain('repo.full_name');
    expect(prompt.querySelector('[data-slot=template-token]')).toBeTruthy();
  });

  it('adds and removes a filter row', () => {
    renderWizard('webhook');
    expect(screen.queryByLabelText('Filter 1 field')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: /Add filter/ }));
    expect(screen.getByLabelText('Filter 1 field')).toBeTruthy();
    expect(screen.getByLabelText('Filter 1 values')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Remove filter 1' }));
    expect(screen.queryByLabelText('Filter 1 field')).toBeNull();
  });

  it('blocks Continue on a half-written filter', () => {
    renderWizard('webhook');
    fireEvent.change(screen.getByLabelText(/Trigger name/), { target: { value: 'gh' } });
    const prompt = screen.getByRole('textbox', { name: 'Prompt' });
    prompt.textContent = 'go';
    fireEvent.input(prompt);
    expect(screen.getByRole('button', { name: /Continue/ }).hasAttribute('disabled')).toBe(false);

    fireEvent.click(screen.getByRole('button', { name: /Add filter/ }));
    fireEvent.change(screen.getByLabelText('Filter 1 field'), { target: { value: 'action' } });

    expect(screen.getByRole('button', { name: /Continue/ }).hasAttribute('disabled')).toBe(true);
    expect(screen.getByText('Finish or remove the empty filter')).toBeTruthy();

    fireEvent.change(screen.getByLabelText('Filter 1 values'), { target: { value: 'opened' } });
    expect(screen.getByRole('button', { name: /Continue/ }).hasAttribute('disabled')).toBe(false);
  });

  it('lists the Alertmanager label patterns as hints, not chips', () => {
    render(
      <TriggerWizard
        initialKind="webhook"
        agents={CONTEXT.agents}
        sources={[{ id: 'src-2', name: 'prod alerts', provider: 'alertmanager' }]}
        ingestHost={CONTEXT.ingestHost}
        onCreate={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: /Insert field/ }));
    expect(screen.getByText('group.labels.<name>')).toBeTruthy();
    // A pattern is not insertable: its literal token is not a real field.
    expect(screen.queryByRole('option', { name: /group\.labels/ })).toBeNull();
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
    expect(screen.getByText('Up to 4 at once, then wait')).toBeTruthy();
  });
});
