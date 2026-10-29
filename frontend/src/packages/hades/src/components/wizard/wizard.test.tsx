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

describe('Wizard.Success', () => {
  const rail = (
    <Wizard.Rail
      label="Create"
      steps={[{ id: 'a', title: 'A', sub: 'First' }]}
      current={0}
      maxVisited={0}
      onGoTo={() => {}}
    />
  );
  const footer = <Wizard.Footer step={0} totalSteps={1} onNext={() => {}} />;

  it('renders the title, description, children and actions', () => {
    render(
      <Wizard open onClose={() => {}} icon="rocket" title="Demo">
        <Wizard.Success
          title="Thing created"
          description="It is ready."
          actions={<button type="button">Done</button>}
        >
          <p>Extra body</p>
        </Wizard.Success>
      </Wizard>,
    );
    expect(screen.getByRole('heading', { name: 'Thing created' })).toBeInTheDocument();
    expect(screen.getByText('It is ready.')).toBeInTheDocument();
    expect(screen.getByText('Extra body')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Done' })).toBeInTheDocument();
    expect(document.querySelector('[data-slot="wizard-success"]')).not.toBeNull();
  });

  it('hides the rail and footer as a direct child', () => {
    render(
      <Wizard open onClose={() => {}} icon="rocket" title="Demo" rail={rail} footer={footer}>
        <Wizard.Success title="Done" actions={<button type="button">Close</button>} />
      </Wizard>,
    );
    expect(screen.getByRole('heading', { name: 'Done' })).toBeInTheDocument();
    expect(document.querySelector('[data-slot="wizard-rail"]')).toBeNull();
    expect(document.querySelector('[data-slot="wizard-footer"]')).toBeNull();
  });

  it('freezes the progress bar at its last non-full-page value', () => {
    const ui = (done: boolean) => (
      <Wizard
        open
        onClose={() => {}}
        icon="rocket"
        title="Demo"
        rail={rail}
        footer={<Wizard.Footer step={done ? 3 : 1} totalSteps={4} onNext={() => {}} />}
      >
        {done ? (
          <Wizard.Success title="Done" actions={null} />
        ) : (
          <Wizard.Step>
            <p>Body</p>
          </Wizard.Step>
        )}
      </Wizard>
    );
    const view = render(ui(false));
    const bar = () => screen.getByRole('progressbar', { name: 'Wizard progress' });
    expect(bar()).toHaveAttribute('aria-valuenow', '50');
    view.rerender(ui(true));
    // Without the freeze this would follow the footer to step 3 of 4 and read 100.
    expect(bar()).toHaveAttribute('aria-valuenow', '50');
  });

  it('renders the default icon when icon is omitted', () => {
    const iconHtml = (explicit: boolean) => {
      const { unmount } = render(
        <Wizard open onClose={() => {}} icon="rocket" title="Demo">
          {explicit ? (
            <Wizard.Success title="Done" icon="check-circle" actions={null} />
          ) : (
            <Wizard.Success title="Done" actions={null} />
          )}
        </Wizard>,
      );
      const html = document.querySelector(
        '[data-slot="wizard-success"] [data-slot="icon"]',
      )?.outerHTML;
      unmount();
      return html;
    };
    const defaulted = iconHtml(false);
    expect(defaulted).toBeDefined();
    expect(defaulted).toBe(iconHtml(true));
  });

  it('renders no description element when description is omitted', () => {
    render(
      <Wizard open onClose={() => {}} icon="rocket" title="Demo">
        <Wizard.Success title="Done" actions={null} />
      </Wizard>,
    );
    const success = document.querySelector('[data-slot="wizard-success"]');
    expect(screen.getByRole('heading', { name: 'Done' })).toBeInTheDocument();
    expect(success?.querySelectorAll('p')).toHaveLength(0);
  });

  it('keeps hiding the rail and footer for a plain fullPage Wizard.Step', () => {
    render(
      <Wizard open onClose={() => {}} icon="rocket" title="Demo" rail={rail} footer={footer}>
        <Wizard.Step fullPage>
          <p>Pick a kind</p>
        </Wizard.Step>
      </Wizard>,
    );
    expect(screen.getByText('Pick a kind')).toBeInTheDocument();
    expect(document.querySelector('[data-slot="wizard-rail"]')).toBeNull();
    expect(document.querySelector('[data-slot="wizard-footer"]')).toBeNull();
  });
});
