// The Runs page's list state: loaded pages, the page shown, and how live stream messages merge
// into them. Kept pure to test the merge rules without a stream.

import type { RunStreamMessage, RunWithAttribution } from '@runkora/shared/api';

/** The one stream message that may add a row. */
export const RUN_CREATED = 'agents.run.created.v1';

export interface FeedPage {
  runs: RunWithAttribution[];
  /** The cursor of the page after this one; null on the last page. */
  nextCursor: string | null;
}

export interface FeedState {
  /** The filters the state answers. An action for other filters starts over. */
  key: string;
  /** Pages loaded since the first page, newest first. */
  pages: FeedPage[];
  /** The page shown. */
  index: number;
  /** The first page is on its way. Rows already loaded stay until it lands. */
  loading: boolean;
  error: string | null;
  /** Messages that arrived while the first page loaded, merged once it lands. */
  early: RunStreamMessage[];
  /** New matching runs that arrived off the first page, for the "N new runs" bar. */
  fresh: string[];
  /** A later page is on its way. */
  pageLoading: boolean;
  pageError: string | null;
}

/** `reload` loads the first page again, after a stream reset or from the new runs bar. */
export type FeedAction = { key: string } & (
  | { type: 'reload' }
  | { type: 'first'; page: FeedPage }
  | { type: 'firstFailed'; error: string }
  | { type: 'message'; message: RunStreamMessage }
  | { type: 'pageLoading' }
  | { type: 'page'; page: FeedPage }
  | { type: 'pageFailed'; error: string }
  | { type: 'goTo'; index: number }
);

/** Omit applied to each member of a union, which keeps each member's own fields. */
type Unkeyed<T> = T extends unknown ? Omit<T, 'key'> : never;

/** An action before the hook adds its filters' key. */
export type UnkeyedFeedAction = Unkeyed<FeedAction>;

/** The state for filters whose first page has not landed. */
export const emptyFeed = (key: string): FeedState => ({
  key,
  pages: [],
  index: 0,
  loading: true,
  error: null,
  early: [],
  fresh: [],
  pageLoading: false,
  pageError: null,
});

/** Whether `incoming` is no older than `current`. A late message must not undo a newer row. */
function isNewer(incoming: RunWithAttribution, current: RunWithAttribution): boolean {
  const a = Date.parse(incoming.updated_at);
  const b = Date.parse(current.updated_at);
  return Number.isNaN(a) || Number.isNaN(b) || a >= b;
}

/** The pages with the run's row replaced in place, or null when no page shows the run. */
function replaceRow(pages: FeedPage[], run: RunWithAttribution): FeedPage[] | null {
  for (let p = 0; p < pages.length; p++) {
    const page = pages[p]!;
    const i = page.runs.findIndex((r) => r.id === run.id);
    if (i === -1) continue;
    if (!isNewer(run, page.runs[i]!)) return pages;
    const runs = page.runs.slice();
    runs[i] = run;
    const next = pages.slice();
    next[p] = { ...page, runs };
    return next;
  }
  return null;
}

/** Merges one message: replace a shown row, insert a new matching run, ignore the rest. */
function merge(state: FeedState, message: RunStreamMessage): FeedState {
  const replaced = replaceRow(state.pages, message.run);
  if (replaced !== null) return replaced === state.pages ? state : { ...state, pages: replaced };
  if (message.type !== RUN_CREATED || !message.matches) return state;
  const [first, ...rest] = state.pages;
  // Once the bar holds runs, later ones join it: inserting them would skip the held ones.
  if (state.index > 0 || first === undefined || state.fresh.length > 0) {
    return state.fresh.includes(message.run.id)
      ? state
      : { ...state, fresh: [...state.fresh, message.run.id] };
  }
  return { ...state, pages: [{ ...first, runs: [message.run, ...first.runs] }, ...rest] };
}

export function feedReducer(prev: FeedState, action: FeedAction): FeedState {
  const state = prev.key === action.key ? prev : emptyFeed(action.key);
  switch (action.type) {
    case 'reload':
      // A later page still on its way is dropped: the reload answers instead.
      return {
        ...state,
        loading: true,
        error: null,
        early: [],
        pageLoading: false,
        pageError: null,
      };
    case 'first':
      // The first page replaces every loaded page and holds any run the bar counted.
      return state.early.reduce(merge, {
        ...emptyFeed(state.key),
        pages: [action.page],
        loading: false,
      });
    case 'firstFailed':
      // The rows already shown stay, with the changes that arrived during the failed load.
      return state.early.reduce(merge, {
        ...state,
        loading: false,
        error: action.error,
        early: [],
      });
    case 'message':
      return state.loading
        ? { ...state, early: [...state.early, action.message] }
        : merge(state, action.message);
    case 'pageLoading':
      return { ...state, pageLoading: true, pageError: null };
    case 'page':
      return {
        ...state,
        pages: [...state.pages.slice(0, state.index + 1), action.page],
        index: state.index + 1,
        pageLoading: false,
      };
    case 'pageFailed':
      return { ...state, pageLoading: false, pageError: action.error };
    case 'goTo':
      return action.index >= 0 && action.index < state.pages.length
        ? { ...state, index: action.index, pageError: null }
        : state;
  }
}
