import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';

import { webhookKind } from './webhook';
import { initialState } from '../wizard-state';
import type { WizardContext } from '../types';

const CONTEXT: WizardContext = {
  agents: [{ id: 'at-1', slug: 'coder', name: 'Coder' }],
  sources: [{ id: 'src-1', name: 'acme/widgets', provider: 'github' }],
  ingestHost: 'orbit.local.clearroute.io:8443',
};

function renderDone(props: { endpoint?: string; secret?: string }) {
  const state = initialState(webhookKind, CONTEXT.sources);
  const DoneContent = webhookKind.doneContent;
  return render(<DoneContent state={state} context={CONTEXT} triggerId="trg-1" {...props} />);
}

describe('webhook DoneContent', () => {
  it('shows the required application/json content type beside a new source secret', () => {
    renderDone({ endpoint: 'https://orbit.local/api/v1/webhooks/src-1', secret: 'whsec_abc' });
    expect(screen.getByText('application/json')).toBeInTheDocument();
    expect(screen.getByText(/content type/, { selector: 'span' })).toBeInTheDocument();
  });

  it('shows the content type when reusing an existing source without a secret', () => {
    renderDone({ endpoint: 'https://orbit.local/api/v1/webhooks/src-1' });
    expect(screen.getByText('application/json')).toBeInTheDocument();
  });

  it('renders nothing without an endpoint', () => {
    const { container } = renderDone({});
    expect(container).toBeEmptyDOMElement();
  });
});
