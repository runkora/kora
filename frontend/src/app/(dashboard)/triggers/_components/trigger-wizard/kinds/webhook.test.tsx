import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';

import { chatKind, webhookKind } from './webhook';
import { initialState } from '../wizard-state';
import { NEW_SOURCE } from '../types';
import type { TriggerWizardSource, WizardContext } from '../types';

const CONTEXT: WizardContext = {
  agents: [{ id: 'at-1', slug: 'coder', name: 'Coder' }],
  sources: [{ id: 'src-1', name: 'acme/widgets', provider: 'github', category: 'webhook' }],
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

  it('names no provider in its own wording', () => {
    renderDone({ endpoint: 'https://orbit.local/api/v1/webhooks/src-1', secret: 'whsec_abc' });
    expect(screen.queryByText(/Slack/)).toBeNull();
  });

  it('renders nothing without an endpoint', () => {
    const { container } = renderDone({});
    expect(container).toBeEmptyDOMElement();
  });
});

const SLACK_SOURCE: TriggerWizardSource = {
  id: 'src-9',
  name: 'acme-slack',
  provider: 'slack',
  category: 'chat',
};

describe('chat DoneContent', () => {
  const DoneContent = chatKind.doneContent;
  const endpoint = 'https://orbit.local/api/v1/webhooks/src-9';

  it('shows a new Slack source the request URL to set under Event Subscriptions', () => {
    const state = {
      ...initialState(chatKind, CONTEXT.sources),
      sourceChoice: NEW_SOURCE,
      newSourceProvider: 'slack',
    };
    render(<DoneContent state={state} context={CONTEXT} triggerId="trg-1" endpoint={endpoint} />);
    expect(screen.getByText(endpoint)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Copy request URL' })).toBeInTheDocument();
    expect(screen.getByText(/Event Subscriptions in your Slack app/)).toBeInTheDocument();
    expect(screen.queryByText('application/json')).toBeNull();
    expect(screen.queryByText(/secret/)).toBeNull();
  });

  it('shows the request URL for an existing Slack source as already set', () => {
    const sources = [SLACK_SOURCE];
    render(
      <DoneContent
        state={{ ...initialState(chatKind, sources), sourceChoice: 'src-9' }}
        context={{ ...CONTEXT, sources }}
        triggerId="trg-1"
        endpoint={endpoint}
      />,
    );
    expect(screen.getByRole('button', { name: 'Copy request URL' })).toBeInTheDocument();
    expect(screen.getByText(/already set under Event Subscriptions/)).toBeInTheDocument();
  });

  it('renders nothing without an endpoint', () => {
    const { container } = render(
      <DoneContent state={initialState(chatKind, [])} context={CONTEXT} triggerId="trg-1" />,
    );
    expect(container).toBeEmptyDOMElement();
  });
});

describe('chat validation, review and draft', () => {
  const validate = chatKind.steps[0]!.validate;
  const ready = {
    ...initialState(chatKind, [SLACK_SOURCE]),
    name: 'ask',
    promptTemplate: 'Answer {{ text }}',
  };

  it('asks for the message to listen for', () => {
    expect(validate(ready, CONTEXT)).toEqual({
      valid: false,
      hint: 'Choose the message this trigger listens for.',
    });
    expect(validate({ ...ready, eventType: 'mention' }, CONTEXT).valid).toBe(true);
  });

  it('asks for a platform for a new source', () => {
    expect(
      validate({ ...ready, sourceChoice: NEW_SOURCE, newSourceName: 'acme-slack' }, CONTEXT),
    ).toEqual({ valid: false, hint: 'Choose a platform.' });
  });

  it('requires the signing secret for a new Slack source and puts it on the draft', () => {
    const slack = {
      ...ready,
      eventType: 'mention',
      sourceChoice: NEW_SOURCE,
      newSourceName: 'acme',
      newSourceProvider: 'slack',
    };
    expect(validate(slack, CONTEXT)).toEqual({
      valid: false,
      hint: 'Paste the signing secret from your Slack app.',
    });
    const withSecret = { ...slack, newSourceSecret: ' slack-secret ' };
    expect(validate(withSecret, CONTEXT).valid).toBe(true);
    expect(chatKind.draftPatch(withSecret).source).toEqual({
      newName: 'acme',
      provider: 'slack',
      signingSecret: 'slack-secret',
    });
  });

  it('shows the event by its label and the platform by name on review', () => {
    const rows = chatKind.summarize(
      {
        ...ready,
        eventType: 'direct_message',
        sourceChoice: NEW_SOURCE,
        newSourceName: 'acme',
        newSourceProvider: 'slack',
      },
      CONTEXT,
    );
    expect(rows).toEqual([
      { key: 'Source', value: 'acme (new, Slack)' },
      { key: 'Event', value: 'Direct message' },
    ]);
  });

  it('puts sender ids on the draft', () => {
    expect(
      chatKind.draftPatch({
        ...ready,
        eventType: 'mention',
        senders: { ids: 'U012AB3CD U045EF6GH', roles: [] },
      }).senderRestriction,
    ).toEqual({ ids: ['U012AB3CD', 'U045EF6GH'], roles: [] });
  });

  it('gives a conversation key example per event', () => {
    expect(chatKind.coalesceKeyExample?.('mention')).toBe('{{ conversation.id }}:{{ thread.id }}');
    expect(chatKind.coalesceKeyExample?.('direct_message')).toBe('{{ conversation.id }}');
    expect(webhookKind.coalesceKeyExample).toBeUndefined();
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

  it('puts the sender restriction on the draft only when something is set', () => {
    expect(webhookKind.draftPatch({ ...ready, eventType: 'push' })).not.toHaveProperty(
      'senderRestriction',
    );
    expect(
      webhookKind.draftPatch({
        ...ready,
        eventType: 'push',
        senders: { ids: 'octocat, hubot octocat', roles: ['MEMBER'] },
      }).senderRestriction,
    ).toEqual({ ids: ['octocat', 'hubot'], roles: ['MEMBER'] });
  });

  it('splits is and contains rows between the two filter maps', () => {
    const patch = webhookKind.draftPatch({
      ...ready,
      eventType: 'issue_comment',
      filters: [
        { field: 'action', values: 'created' },
        { field: 'comment.body', operator: 'contains', values: '@kora-agent' },
      ],
    });
    expect(patch.matchCondition).toEqual({ action: ['created'] });
    expect(patch.matchContains).toEqual({ 'comment.body': ['@kora-agent'] });
  });

  it('never sends a signing secret for a provider that generates one', () => {
    const github = {
      ...ready,
      eventType: 'push',
      sourceChoice: NEW_SOURCE,
      newSourceName: 'acme/widgets',
      newSourceProvider: 'github',
      newSourceSecret: 'left over',
    };
    expect(webhookKind.draftPatch(github).source).toEqual({
      newName: 'acme/widgets',
      provider: 'github',
    });
  });

  it('summarises the senders for review', () => {
    const rows = webhookKind.summarize(
      { ...ready, eventType: 'push', senders: { ids: '', roles: ['OWNER'] } },
      CONTEXT,
    );
    expect(rows).toEqual(expect.arrayContaining([{ key: 'Senders', value: 'OWNER', mono: true }]));
  });
});
