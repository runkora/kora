import { describe, expect, it } from 'vitest';

import { KIND_ORDER, PLANNED_KINDS, TRIGGER_KINDS } from './index';
import { SHARED_STEPS } from '../shared-steps';
import { initialState, stepsFor } from '../wizard-state';
import type { ConsoleKind, WizardContext } from '../types';

const CONTEXT: WizardContext = {
  agents: [{ id: 'at-1', slug: 'coder', name: 'Coder' }],
  sources: [
    { id: 'src-1', name: 'acme/widgets', provider: 'github', category: 'webhook' },
    { id: 'src-2', name: 'acme-slack', provider: 'slack', category: 'chat' },
  ],
  ingestHost: 'orbit.local.clearroute.io:8443',
};

const KINDS = Object.values(TRIGGER_KINDS);

describe('TRIGGER_KINDS', () => {
  it('keys every definition by its own id, so lookups cannot drift', () => {
    for (const [key, def] of Object.entries(TRIGGER_KINDS)) {
      expect(def.id).toBe(key as ConsoleKind);
    }
  });

  it('stores the chat command kind as a webhook trigger on chat sources', () => {
    expect(TRIGGER_KINDS.chat.apiKind).toBe('webhook');
    expect(TRIGGER_KINDS.chat.sourceCategory).toBe('chat');
    expect(TRIGGER_KINDS.webhook.apiKind).toBe('webhook');
    expect(TRIGGER_KINDS.webhook.sourceCategory).toBe('webhook');
    expect(TRIGGER_KINDS.scheduled.apiKind).toBe('scheduled');
    expect(TRIGGER_KINDS.manual.apiKind).toBe('manual');
  });

  it('starts each source kind on the first source of its own category', () => {
    expect(initialState(TRIGGER_KINDS.webhook, CONTEXT.sources).sourceChoice).toBe('src-1');
    expect(initialState(TRIGGER_KINDS.chat, CONTEXT.sources).sourceChoice).toBe('src-2');
    expect(initialState(TRIGGER_KINDS.chat, CONTEXT.sources.slice(0, 1)).sourceChoice).toBe('');
  });

  it('gives every kind the copy the picker renders', () => {
    for (const def of KINDS) {
      expect(def.title.length).toBeGreaterThan(0);
      expect(def.description.length).toBeGreaterThan(0);
      expect(def.example.length).toBeGreaterThan(0);
      expect(def.icon.length).toBeGreaterThan(0);
    }
  });

  it('gives every kind at least one step of its own, ending in the shared three', () => {
    for (const def of KINDS) {
      expect(def.steps.length).toBeGreaterThan(0);
      const ids = stepsFor(def, SHARED_STEPS).map((s) => s.id);
      expect(ids.slice(-3)).toEqual(['target', 'delivery', 'review']);
      expect(ids.length).toBe(def.steps.length + 3);
    }
  });

  it('gives every step a rail entry and a description', () => {
    for (const def of KINDS) {
      for (const step of stepsFor(def, SHARED_STEPS)) {
        expect(step.railTitle.length).toBeGreaterThan(0);
        expect(step.railSub.length).toBeGreaterThan(0);
        expect(step.description.length).toBeGreaterThan(0);
      }
    }
  });

  it('summarises itself for the review step', () => {
    for (const def of KINDS) {
      const rows = def.summarize(initialState(def, CONTEXT.sources), CONTEXT);
      expect(rows.length).toBeGreaterThan(0);
      for (const row of rows) {
        expect(row.key.length).toBeGreaterThan(0);
        expect(typeof row.value).toBe('string');
      }
    }
  });

  it('contributes its own fields to the draft', () => {
    for (const def of KINDS) {
      const patchFields = Object.keys(def.draftPatch(initialState(def, CONTEXT.sources)));
      expect(patchFields.length).toBeGreaterThan(0);
    }
  });
});

describe('fieldCatalog', () => {
  function tokens(def: (typeof KINDS)[number], state = initialState(def, CONTEXT.sources)) {
    return (def.fieldCatalog?.(state, CONTEXT) ?? []).map((e) => e.token);
  }

  it('is offered by the scheduled and manual kinds; webhook fields come from the API', () => {
    expect(TRIGGER_KINDS.scheduled.fieldCatalog).toBeTypeOf('function');
    expect(TRIGGER_KINDS.manual.fieldCatalog).toBeTypeOf('function');
    expect(TRIGGER_KINDS.webhook.fieldCatalog).toBeUndefined();
    expect(TRIGGER_KINDS.chat.fieldCatalog).toBeUndefined();
  });

  it('describes every token it offers', () => {
    for (const def of KINDS) {
      for (const entry of def.fieldCatalog?.(initialState(def, CONTEXT.sources), CONTEXT) ?? []) {
        expect(entry.token.length).toBeGreaterThan(0);
        expect(entry.description.length).toBeGreaterThan(0);
      }
    }
  });

  it('offers the scheduler vars for a schedule', () => {
    expect(tokens(TRIGGER_KINDS.scheduled)).toEqual([
      'var.date',
      'var.datetime',
      'var.trigger_id',
      'var.tenant_id',
    ]);
  });

  it('derives the manual catalog from the declared fields', () => {
    const def = TRIGGER_KINDS.manual;
    const state = {
      ...initialState(def, CONTEXT.sources),
      fields: [{ id: 'a', label: 'Row limit', type: 'number' as const, required: false }],
    };
    expect(tokens(def, state)).toEqual(['var.row_limit']);
  });
});

describe('KIND_ORDER', () => {
  it('lists every registry entry exactly once', () => {
    expect([...KIND_ORDER].sort()).toEqual(Object.keys(TRIGGER_KINDS).sort());
    expect(new Set(KIND_ORDER).size).toBe(KIND_ORDER.length);
  });
});

describe('PLANNED_KINDS', () => {
  it('no longer lists the chat command, which is built', () => {
    expect(PLANNED_KINDS.map((k) => k.title)).not.toContain('Chat command');
    expect(KIND_ORDER).toContain('chat');
  });

  it('names kinds that are not in the registry yet', () => {
    const built = new Set(KINDS.map((d) => d.title));
    for (const planned of PLANNED_KINDS) {
      expect(built.has(planned.title)).toBe(false);
      expect(planned.description.length).toBeGreaterThan(0);
    }
  });
});
