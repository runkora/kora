import { describe, expect, it } from 'vitest';

import { KIND_ORDER, PLANNED_KINDS, TRIGGER_KINDS } from './index';
import { SHARED_STEPS } from '../shared-steps';
import { initialState, stepsFor } from '../wizard-state';
import type { TriggerKind, WizardContext } from '../types';

const CONTEXT: WizardContext = {
  agents: [{ id: 'at-1', slug: 'coder', name: 'Coder' }],
  sources: [{ id: 'src-1', name: 'acme/widgets', provider: 'github' }],
  ingestHost: 'orbit.local.clearroute.io:8443',
};

const KINDS = Object.values(TRIGGER_KINDS);

describe('TRIGGER_KINDS', () => {
  it('keys every definition by its own id, so lookups cannot drift', () => {
    for (const [key, def] of Object.entries(TRIGGER_KINDS)) {
      expect(def.id).toBe(key as TriggerKind);
    }
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

describe('KIND_ORDER', () => {
  it('lists every registry entry exactly once', () => {
    expect([...KIND_ORDER].sort()).toEqual(Object.keys(TRIGGER_KINDS).sort());
    expect(new Set(KIND_ORDER).size).toBe(KIND_ORDER.length);
  });
});

describe('PLANNED_KINDS', () => {
  it('names kinds that are not in the registry yet', () => {
    const built = new Set(KINDS.map((d) => d.title));
    for (const planned of PLANNED_KINDS) {
      expect(built.has(planned.title)).toBe(false);
      expect(planned.description.length).toBeGreaterThan(0);
    }
  });
});
