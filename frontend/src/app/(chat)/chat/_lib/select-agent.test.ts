import { describe, expect, it } from 'vitest';
import type { RunWithAttribution } from '@runkora/shared/api';
import type { AgentTypeRow } from '@runkora/app/(dashboard)/agents/_types';
import { selectAgent } from './select-agent';

const agent = (slug: string, canRun = true) =>
  ({ id: `at-${slug}`, slug, name: slug, canRun }) as AgentTypeRow;
const run = (slug: string) => ({ agent_type_id: `at-${slug}` }) as RunWithAttribution;

const CODER = agent('coder');
const REVIEWER = agent('reviewer');
const LOCKED = agent('locked', false);

describe('selectAgent', () => {
  it('selects the agent of the newest run', () => {
    expect(selectAgent([CODER, REVIEWER], [run('reviewer'), run('coder')])).toBe(REVIEWER);
  });

  it('falls back to the first runnable agent when the newest run is of one the viewer cannot run', () => {
    expect(selectAgent([LOCKED, CODER, REVIEWER], [run('locked'), run('reviewer')])).toBe(CODER);
  });

  it('selects the first runnable agent with no runs', () => {
    expect(selectAgent([LOCKED, REVIEWER], [])).toBe(REVIEWER);
  });

  it('selects the first agent when the viewer may run none', () => {
    expect(selectAgent([LOCKED, agent('other', false)], [])).toBe(LOCKED);
  });

  it('selects the agent in the address over the newest run', () => {
    expect(selectAgent([CODER, REVIEWER], [run('reviewer')], 'coder')).toBe(CODER);
  });

  it('selects an agent in the address the viewer cannot run', () => {
    expect(selectAgent([CODER, LOCKED], [], 'locked')).toBe(LOCKED);
  });

  it('ignores an agent in the address that is not in the list', () => {
    expect(selectAgent([CODER, REVIEWER], [run('reviewer')], 'gone')).toBe(REVIEWER);
  });

  it('selects nothing with no agents', () => {
    expect(selectAgent([], [run('coder')], 'coder')).toBeNull();
  });
});
