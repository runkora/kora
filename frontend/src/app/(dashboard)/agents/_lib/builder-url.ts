'use client';

/** @tier 3 — agents-scoped. Open/close the agent builder by mutating the
 * `agent` search param through the History API instead of the Next router.
 *
 * Next patches history.pushState/replaceState so useSearchParams observes them,
 * which lets the builder mount and unmount as instant client state with no RSC
 * round-trip on the interaction hot path. router.push/replace would instead
 * fetch an RSC payload from the Next server on every open and close, which is
 * imperceptible locally but a visible stall on a remote deployment. The URL
 * stays deep-linkable (?agent=new | {draftId}); see
 * docs/adr/2026-07-17-agent-builder.md §1. */

function withAgentParam(value: string | null): string {
  const params = new URLSearchParams(window.location.search);
  if (value === null) params.delete('agent');
  else params.set('agent', value);
  const query = params.toString();
  return query ? `${window.location.pathname}?${query}` : window.location.pathname;
}

/** Open the builder, fresh or resuming a draft. Pushes a history entry so Back
 * closes the modal, matching the previous Link-based behaviour. */
export function openBuilder(agentId: string = 'new'): void {
  window.history.pushState(null, '', withAgentParam(agentId));
}

/** Close the builder, preserving any other query params. Replaces rather than
 * pushes, matching the previous router.replace close. */
export function closeBuilder(): void {
  window.history.replaceState(null, '', withAgentParam(null));
}

/** Adopt the created draft's id into the URL without a new history entry, so a
 * refresh resumes the draft. */
export function adoptBuilderDraft(draftId: string): void {
  window.history.replaceState(null, '', withAgentParam(draftId));
}
