import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';

import { catalogProvider, webhookKind } from './webhook';
import { initialState } from '../wizard-state';
import { NEW_SOURCE } from '../types';
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

describe('webhook validation and draft', () => {
  const validate = webhookKind.steps[0]!.validate;
  const ready = {
    ...initialState(webhookKind, CONTEXT.sources),
    name: 'gh',
    promptTemplate: 'go',
  };

  it('requires an event type', () => {
    expect(validate(ready, CONTEXT)).toEqual({
      valid: false,
      hint: 'Choose the event this trigger listens for.',
    });
    expect(validate({ ...ready, eventType: 'push' }, CONTEXT).valid).toBe(true);
  });

  it('refuses a pattern filter whose name is not filled in', () => {
    const state = {
      ...ready,
      eventType: 'alert.firing',
      filters: [{ field: '', pattern: 'commonLabels.<name>', values: 'critical' }],
    };
    expect(validate(state, CONTEXT).valid).toBe(false);
  });

  it('puts the sender restriction on the draft only when something is set', () => {
    expect(webhookKind.draftPatch({ ...ready, eventType: 'push' })).not.toHaveProperty(
      'senderRestriction',
    );
    expect(
      webhookKind.draftPatch({
        ...ready,
        eventType: 'push',
        senderLogins: 'octocat, hubot octocat',
        senderAssociations: ['MEMBER'],
      }).senderRestriction,
    ).toEqual({ logins: ['octocat', 'hubot'], associations: ['MEMBER'] });
  });

  it('summarises the senders for review', () => {
    const rows = webhookKind.summarize(
      { ...ready, eventType: 'push', senderAssociations: ['OWNER'] },
      CONTEXT,
    );
    expect(rows).toEqual(expect.arrayContaining([{ key: 'Senders', value: 'OWNER', mono: true }]));
  });
});

describe('catalogProvider', () => {
  const sources = [
    { id: 'src-1', provider: 'github' },
    { id: 'src-2', provider: 'alertmanager' },
  ];
  const base = initialState(webhookKind, sources);

  it("reads the chosen source's provider", () => {
    expect(catalogProvider({ ...base, sourceChoice: 'src-2' }, sources)).toBe('alertmanager');
  });

  it('reads the provider being created for a new source', () => {
    expect(
      catalogProvider(
        { ...base, sourceChoice: NEW_SOURCE, newSourceProvider: 'alertmanager' },
        sources,
      ),
    ).toBe('alertmanager');
  });

  it('has no provider for an unknown source', () => {
    expect(catalogProvider({ ...base, sourceChoice: 'gone' }, sources)).toBe('');
  });
});
