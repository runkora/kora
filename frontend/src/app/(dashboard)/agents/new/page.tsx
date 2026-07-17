import { redirect } from 'next/navigation';

/**
 * The create form that lived here is now the agent builder modal, opened over
 * any dashboard route via `?agent=new` (docs/adr/2026-07-17-agent-builder.md §1).
 * This redirect keeps existing links and bookmarks working.
 */
export default function NewAgentRedirect() {
  redirect('/agents?agent=new');
}
