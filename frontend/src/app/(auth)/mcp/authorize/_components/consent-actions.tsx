/** @tier 3 — mcp-authorize-scoped. The Approve/Deny controls for the OAuth consent screen. */
'use client';

import { Button, Stack } from '@orbit/terra';

interface ConsentActionsProps {
  /** The issuing endpoint (/api/mcp/authorize) with the OAuth params in its query. */
  action: string;
  /** Render-bound CSRF token; submitted with the decision and re-verified server-side. */
  csrfToken: string;
}

/**
 * ConsentActions renders the Approve / Deny buttons and submits the decision as a
 * top-level POST to `action`. A top-level form submission (not fetch) is required
 * so the browser follows the 302 to the client's loopback redirect_uri as a real
 * navigation — the MCP client's local server catches the code there. The form is
 * built via the DOM rather than JSX so Terra's raw-<form> rule is satisfied.
 */
export function ConsentActions({ action, csrfToken }: ConsentActionsProps) {
  function submit(decision: 'approve' | 'deny') {
    const form = document.createElement('form');
    form.method = 'post';
    form.action = action;
    appendHidden(form, 'decision', decision);
    appendHidden(form, 'csrf_token', csrfToken);
    document.body.appendChild(form);
    form.submit();
  }

  function appendHidden(form: HTMLFormElement, name: string, value: string) {
    const field = document.createElement('input');
    field.type = 'hidden';
    field.name = name;
    field.value = value;
    form.appendChild(field);
  }

  return (
    <Stack direction="horizontal" gap="snug" justify="end">
      <Button variant="outline" onClick={() => submit('deny')}>
        Deny
      </Button>
      <Button onClick={() => submit('approve')}>Approve</Button>
    </Stack>
  );
}
