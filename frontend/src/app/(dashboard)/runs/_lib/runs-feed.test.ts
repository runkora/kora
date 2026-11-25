import { describe, expect, it } from 'vitest';
import type { RunStreamMessage, RunWithAttribution } from '@runkora/shared/api';
import {
  RUN_CREATED,
  emptyFeed,
  feedReducer,
  type FeedAction,
  type FeedPage,
  type FeedState,
  type UnkeyedFeedAction,
} from './runs-feed';

const KEY = 'state=idle';

function run(id: string, patch: Partial<RunWithAttribution> = {}): RunWithAttribution {
  return {
    id,
    tenant_id: 'ten_1',
    agent_type_id: 'agt_coder',
    state: 'active',
    pod_name: '',
    created_by: 'usr_dana',
    started_at: '2026-11-25T10:00:00Z',
    completed_at: null,
    created_at: '2026-11-25T10:00:00Z',
    updated_at: '2026-11-25T10:00:00Z',
    ...patch,
  };
}

const page = (ids: string[], nextCursor: string | null = null): FeedPage => ({
  runs: ids.map((id) => run(id)),
  nextCursor,
});

const message = (
  r: RunWithAttribution,
  type = 'agents.run.idle.v1',
  matches = true,
): RunStreamMessage => ({ type, matches, run: r });

/** Applies actions under KEY, starting from an empty feed. */
function apply(...actions: UnkeyedFeedAction[]): FeedState {
  return actions.reduce<FeedState>(
    (s, a) => feedReducer(s, { ...a, key: KEY } as FeedAction),
    emptyFeed(KEY),
  );
}

const ids = (s: FeedState, p = s.index) => s.pages[p]?.runs.map((r) => r.id);

describe('feedReducer', () => {
  it('inserts a new matching run at the top of the first page', () => {
    const s = apply(
      { type: 'first', page: page(['b', 'a']) },
      { type: 'message', message: message(run('c'), RUN_CREATED) },
    );
    expect(ids(s)).toEqual(['c', 'b', 'a']);
  });

  it('ignores a new run that does not match the filters', () => {
    const s = apply(
      { type: 'first', page: page(['a']) },
      { type: 'message', message: message(run('c'), RUN_CREATED, false) },
    );
    expect(ids(s)).toEqual(['a']);
    expect(s.fresh).toEqual([]);
  });

  it('never inserts a run for a message other than created', () => {
    const s = apply(
      { type: 'first', page: page(['a']) },
      { type: 'message', message: message(run('c', { state: 'idle' })) },
    );
    expect(ids(s)).toEqual(['a']);
  });

  it('replaces a shown row in place, even when it no longer matches', () => {
    const s = apply(
      { type: 'first', page: page(['c', 'b', 'a']) },
      {
        type: 'message',
        message: message(
          run('b', { state: 'failed', summary: 'Fixing CI', updated_at: '2026-11-25T10:05:00Z' }),
          'agents.run.failed.v1',
          false,
        ),
      },
    );
    expect(ids(s)).toEqual(['c', 'b', 'a']);
    expect(s.pages[0]!.runs[1]).toMatchObject({ state: 'failed', summary: 'Fixing CI' });
  });

  it('keeps a row newer than the message that would replace it', () => {
    const s = apply(
      {
        type: 'first',
        page: { runs: [run('a', { updated_at: '2026-11-25T10:09:00Z' })], nextCursor: null },
      },
      { type: 'message', message: message(run('a', { state: 'pending' })) },
    );
    expect(s.pages[0]!.runs[0]!.state).toBe('active');
  });

  it('replaces a row on a later page that is not shown', () => {
    const s = apply(
      { type: 'first', page: page(['b'], 'cur1') },
      { type: 'page', page: page(['a']) },
      { type: 'goTo', index: 0 },
      { type: 'message', message: message(run('a', { state: 'completed' })) },
    );
    expect(s.pages[1]!.runs[0]!.state).toBe('completed');
  });

  it('counts new runs for the bar on a later page and leaves the page alone', () => {
    const s = apply(
      { type: 'first', page: page(['b'], 'cur1') },
      { type: 'page', page: page(['a']) },
      { type: 'message', message: message(run('c'), RUN_CREATED) },
      { type: 'message', message: message(run('d'), RUN_CREATED) },
      { type: 'message', message: message(run('c', { state: 'idle' }), RUN_CREATED) },
    );
    expect(s.index).toBe(1);
    expect(ids(s, 0)).toEqual(['b']);
    expect(ids(s, 1)).toEqual(['a']);
    expect(s.fresh).toEqual(['c', 'd']);
  });

  it('clears the bar when the first page loads again', () => {
    const s = apply(
      { type: 'first', page: page(['b'], 'cur1') },
      { type: 'page', page: page(['a']) },
      { type: 'message', message: message(run('c'), RUN_CREATED) },
      { type: 'reload' },
      { type: 'first', page: page(['c', 'b'], 'cur1') },
    );
    expect(s.index).toBe(0);
    expect(s.pages).toHaveLength(1);
    expect(ids(s)).toEqual(['c', 'b']);
    expect(s.fresh).toEqual([]);
  });

  it('merges messages that arrived before the first page once it lands', () => {
    const s = apply(
      { type: 'message', message: message(run('c'), RUN_CREATED) },
      {
        type: 'message',
        message: message(run('a', { state: 'idle', updated_at: '2026-11-25T10:01:00Z' })),
      },
      { type: 'message', message: message(run('b', { state: 'idle' }), RUN_CREATED) },
      { type: 'first', page: page(['b', 'a']) },
    );
    // b was on the page already: its created message replaces the row instead of adding one.
    expect(ids(s)).toEqual(['c', 'b', 'a']);
    expect(s.pages[0]!.runs[2]!.state).toBe('idle');
    expect(s.early).toEqual([]);
  });

  it('keeps the rows shown while a reset reloads the first page', () => {
    const s = apply({ type: 'first', page: page(['b', 'a']) }, { type: 'reload' });
    expect(s.loading).toBe(true);
    expect(ids(s)).toEqual(['b', 'a']);
    const reloaded = feedReducer(s, { type: 'first', page: page(['x']), key: KEY });
    expect(ids(reloaded)).toEqual(['x']);
  });

  it('starts over for an action under other filters', () => {
    const s = apply({ type: 'first', page: page(['a']) });
    const other = feedReducer(s, {
      type: 'message',
      message: message(run('z'), RUN_CREATED),
      key: 'state=failed',
    });
    expect(other.key).toBe('state=failed');
    expect(other.pages).toEqual([]);
    expect(other.loading).toBe(true);
    expect(other.early).toHaveLength(1);
  });

  it('pages forward and back within the loaded pages', () => {
    let s = apply(
      { type: 'first', page: page(['c'], 'cur1') },
      { type: 'pageLoading' },
      { type: 'page', page: page(['b'], 'cur2') },
    );
    expect(s.index).toBe(1);
    expect(s.pageLoading).toBe(false);
    s = feedReducer(s, { type: 'goTo', index: 0, key: KEY });
    expect(ids(s)).toEqual(['c']);
    s = feedReducer(s, { type: 'goTo', index: 5, key: KEY });
    expect(s.index).toBe(0);
  });

  it('keeps the rows and the changes that arrived when a reload fails', () => {
    const s = apply(
      { type: 'first', page: page(['b', 'a']) },
      { type: 'reload' },
      { type: 'message', message: message(run('a', { state: 'failed' })) },
      { type: 'firstFailed', error: 'Could not load runs. Try again.' },
    );
    expect(s.loading).toBe(false);
    expect(s.error).toBe('Could not load runs. Try again.');
    expect(s.pages[0]!.runs[1]!.state).toBe('failed');
  });

  it('keeps counting new runs for the bar after going back to the first page', () => {
    const s = apply(
      { type: 'first', page: page(['b'], 'cur1') },
      { type: 'page', page: page(['a']) },
      { type: 'message', message: message(run('c'), RUN_CREATED) },
      { type: 'goTo', index: 0 },
      { type: 'message', message: message(run('d'), RUN_CREATED) },
    );
    expect(ids(s)).toEqual(['b']);
    expect(s.fresh).toEqual(['c', 'd']);
  });

  it('drops a later page still loading when the first page reloads', () => {
    const s = apply(
      { type: 'first', page: page(['b'], 'cur1') },
      { type: 'pageLoading' },
      { type: 'reload' },
      { type: 'firstFailed', error: 'Could not load runs. Try again.' },
    );
    expect(s.pageLoading).toBe(false);
  });

  it('records a failed page without leaving the page shown', () => {
    const s = apply(
      { type: 'first', page: page(['c'], 'cur1') },
      { type: 'pageLoading' },
      { type: 'pageFailed', error: 'Could not load runs. Try again.' },
    );
    expect(s.index).toBe(0);
    expect(s.pageError).toBe('Could not load runs. Try again.');
  });
});
