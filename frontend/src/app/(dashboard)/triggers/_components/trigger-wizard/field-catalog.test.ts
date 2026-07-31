import { describe, expect, it } from 'vitest';

import {
  ALERTMANAGER_PATTERNS,
  catalogPatterns,
  insertAtCursor,
  manualCatalog,
  scheduledCatalog,
  webhookCatalog,
} from './field-catalog';
import type { WizardField } from './types';

function tokens(entries: { token: string }[]): string[] {
  return entries.map((e) => e.token);
}

describe('webhookCatalog', () => {
  it('offers the always-present GitHub fields on any event', () => {
    const t = tokens(webhookCatalog('github', ''));
    expect(t).toEqual(
      expect.arrayContaining(['event', 'action', 'repo.full_name', 'sender.login']),
    );
  });

  it('narrows to the chosen event, dropping other events fields', () => {
    const pr = tokens(webhookCatalog('github', 'pull_request'));
    expect(pr).toEqual(
      expect.arrayContaining(['pr.number', 'pr.title', 'pr.base_ref', 'pr.author']),
    );
    expect(pr).not.toContain('push.branch');
    expect(pr).not.toContain('issue.title');

    const push = tokens(webhookCatalog('github', 'push'));
    expect(push).toEqual(
      expect.arrayContaining(['push.ref', 'push.branch', 'push.commit_message']),
    );
    expect(push).not.toContain('pr.title');

    const issues = tokens(webhookCatalog('github', 'issues'));
    expect(issues).toEqual(expect.arrayContaining(['issue.number', 'issue.body', 'label.name']));
    expect(issues).not.toContain('pr.head_sha');
  });

  it('keeps the common fields on every event', () => {
    for (const event of ['pull_request', 'push', 'issues', 'release', 'workflow_run']) {
      expect(tokens(webhookCatalog('github', event))).toEqual(
        expect.arrayContaining(['event', 'action', 'repo.full_name']),
      );
    }
  });

  it('offers nothing per-event for an event with no extra fields', () => {
    // release and workflow_run carry only the common map in the adapter.
    expect(tokens(webhookCatalog('github', 'release'))).toEqual([
      'event',
      'action',
      'repo.full_name',
      'repo.default_branch',
      'sender.login',
    ]);
  });

  it('offers the Alertmanager fields regardless of event', () => {
    const firing = tokens(webhookCatalog('alertmanager', 'firing'));
    expect(firing).toEqual(
      expect.arrayContaining(['status', 'receiver', 'alerts.count', 'first_alert.fingerprint']),
    );
    expect(tokens(webhookCatalog('alertmanager', ''))).toEqual(firing);
  });

  it('offers nothing for a provider with no adapter', () => {
    expect(webhookCatalog('gitlab', '')).toEqual([]);
  });

  it('never invents a token: every one is dotted or a bare known key', () => {
    for (const provider of ['github', 'alertmanager']) {
      for (const entry of webhookCatalog(provider, '')) {
        expect(entry.token).toMatch(/^[a-z][a-z0-9_.]*$/);
        expect(entry.description.length).toBeGreaterThan(0);
      }
    }
  });
});

describe('catalogPatterns', () => {
  it('describes Alertmanager label families it cannot enumerate', () => {
    expect(catalogPatterns('alertmanager')).toEqual(ALERTMANAGER_PATTERNS);
    expect(catalogPatterns('alertmanager')).toEqual(
      expect.arrayContaining(['group.labels.<name>', 'common.annotations.<name>']),
    );
  });

  it('has none for GitHub, whose fields are fully enumerable', () => {
    expect(catalogPatterns('github')).toEqual([]);
  });
});

describe('scheduledCatalog', () => {
  it('offers the four the scheduler injects', () => {
    expect(tokens(scheduledCatalog())).toEqual([
      'var.date',
      'var.datetime',
      'var.trigger_id',
      'var.tenant_id',
    ]);
  });
});

describe('manualCatalog', () => {
  function field(overrides: Partial<WizardField> = {}): WizardField {
    return { id: 'f1', label: 'Question', type: 'text', required: true, ...overrides };
  }

  it('derives one var per declared field, keyed like the API', () => {
    const entries = manualCatalog([
      field({ id: 'a', label: 'Question' }),
      field({ id: 'b', label: 'Row limit' }),
    ]);
    expect(tokens(entries)).toEqual(['var.question', 'var.row_limit']);
  });

  it('skips unlabelled rows, which have no key yet', () => {
    expect(manualCatalog([field({ id: 'a', label: '  ' })])).toEqual([]);
  });

  it('names the field it came from', () => {
    expect(manualCatalog([field({ label: 'Row limit' })])[0]?.description).toContain('Row limit');
  });
});

describe('insertAtCursor', () => {
  it('inserts at the cursor', () => {
    expect(insertAtCursor('Review  now', 7, 'pr.title')).toBe('Review {{ pr.title }} now');
  });

  it('inserts at the start and the end', () => {
    expect(insertAtCursor('tail', 0, 'event')).toBe('{{ event }}tail');
    expect(insertAtCursor('head ', 5, 'event')).toBe('head {{ event }}');
  });

  it('appends with a space when the textarea was never focused', () => {
    expect(insertAtCursor('Review the PR', null, 'pr.title')).toBe('Review the PR {{ pr.title }}');
  });

  it('does not double the space when the text already ends in one', () => {
    expect(insertAtCursor('Review ', null, 'pr.title')).toBe('Review {{ pr.title }}');
  });

  it('is the whole value when the prompt is empty', () => {
    expect(insertAtCursor('', null, 'pr.title')).toBe('{{ pr.title }}');
  });

  it('appends rather than corrupting when the cursor is out of range', () => {
    expect(insertAtCursor('abc', 99, 'event')).toBe('abc {{ event }}');
    expect(insertAtCursor('abc', -1, 'event')).toBe('abc {{ event }}');
  });
});
