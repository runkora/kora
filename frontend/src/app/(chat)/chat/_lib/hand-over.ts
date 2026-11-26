import type { RunPageEnter } from '@runkora/app/(chat)/agents/[slug]/runs/[runId]/_components/run-page';
import type { OwnRuns } from '../_hooks/use-own-runs';

/** What the chat page passes to the run page it opens: the composer's box and the runs it loaded. */
interface HandOver {
  own: OwnRuns;
  enter?: RunPageEnter;
}

// Module state outlives the client navigation from /chat to the run's route.
const pending = new Map<string, HandOver>();

export const handOver = (runId: string, h: HandOver) => pending.set(runId, h);
export const handedOver = (runId: string) => pending.get(runId);
export const forgetHandOver = (runId: string) => pending.delete(runId);
