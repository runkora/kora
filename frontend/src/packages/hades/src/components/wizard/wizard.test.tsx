import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Wizard } from './wizard';

type HintProps = { hint?: string; hintVariant?: 'error' | 'warning' };

function renderWizard(props: HintProps) {
  const ui = (p: HintProps) => (
    <Wizard open onClose={() => {}} icon="rocket" title="Demo" {...p}>
      <Wizard.Step>
        <button type="button">Content</button>
      </Wizard.Step>
    </Wizard>
  );
  const view = render(ui(props));
  return { rerender: (p: HintProps) => view.rerender(ui(p)) };
}

const slot = () => document.querySelector('[data-slot="wizard-hint"]');

describe('Wizard hint banner', () => {
  it('is closed with no alert when there is no hint', () => {
    renderWizard({});
    expect(slot()).toHaveAttribute('data-state', 'closed');
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('shows the hint as an alert and opens', () => {
    renderWizard({ hint: 'Choose a user.' });
    expect(slot()).toHaveAttribute('data-state', 'open');
    expect(screen.getByRole('alert')).toHaveTextContent('Choose a user.');
  });

  it('opens when a hint appears and closes when it clears', () => {
    const { rerender } = renderWizard({});
    rerender({ hint: 'Add a name to continue.' });
    expect(slot()).toHaveAttribute('data-state', 'open');
    expect(screen.getByRole('alert')).toHaveTextContent('Add a name to continue.');
    rerender({});
    expect(slot()).toHaveAttribute('data-state', 'closed');
  });

  it('follows a hint change without closing', () => {
    const { rerender } = renderWizard({ hint: 'First.' });
    rerender({ hint: 'Second.', hintVariant: 'warning' });
    expect(slot()).toHaveAttribute('data-state', 'open');
    expect(screen.getByRole('alert')).toHaveTextContent('Second.');
  });
});
