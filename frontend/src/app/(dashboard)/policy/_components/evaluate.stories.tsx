import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import type { CheckSimulateRequest } from '../_types';
import { EvaluateTab } from './evaluate-tab';
import { AVAILABLE_ROLES, AVAILABLE_SCOPES, EXPLAIN, EXPLAIN_NO_SCOPES } from './policy-mock-data';
import { policyParameters, withPolicy } from './policy-story-setup';

/** The simulate endpoint allows any scope the principal's effective set holds and denies the rest. */
const simulate = ({ body }: { body: unknown }) => {
  const req = body as CheckSimulateRequest;
  const scope = [req.resource.type, ...(req.resource.path ?? []), req.action].join(':');
  const allow = EXPLAIN.effective_scopes.includes(scope);
  return {
    data: {
      allow,
      reason: allow
        ? `scope ${scope} is in the effective set via role ml-engineer`
        : `scope ${scope} is not granted by any role or direct grant`,
    },
  };
};

const routes = {
  'POST /api/policy/evaluate/simulate': simulate,
  'GET /api/policy/evaluate/explain': { data: EXPLAIN },
};

const meta: Meta<typeof EvaluateTab> = {
  title: 'Pages/Policy/Evaluate',
  component: EvaluateTab,
  parameters: policyParameters('/policy/evaluate'),
  decorators: withPolicy(routes),
  args: {
    initialExplain: EXPLAIN,
    availableScopes: AVAILABLE_SCOPES,
    availableRoles: AVAILABLE_ROLES,
    initialError: null,
  },
};

export default meta;
type Story = StoryObj<typeof EvaluateTab>;

/** The signed-in principal pre-loaded in the scope lookup. */
export const Default: Story = {};

/** A principal with no roles or grants. */
export const NoEffectiveScopes: Story = {
  args: { initialExplain: EXPLAIN_NO_SCOPES },
};

/** No principal loaded yet, because the session lookup failed. */
export const LoadError: Story = {
  args: { initialExplain: null, initialError: 'policy-gateway unavailable (503)' },
};

/** Runs a policy check for a scope the principal holds. */
const runCheck =
  (scope: string): Story['play'] =>
  async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const id = canvasElement.querySelector<HTMLInputElement>('#sim-principal-id');
    if (!id) throw new Error('principal id input not found');
    await userEvent.type(id, 'a3f1c9e2-5b7d-4c1a-9e60-2d8b4f7a1c35');
    await userEvent.selectOptions(await canvas.findByLabelText('Role'), 'ml-engineer');
    await userEvent.selectOptions(await canvas.findByLabelText('Scope to check'), scope);
    await userEvent.click(await canvas.findByRole('button', { name: 'Run check' }));
    // Bring the decision into view so the story shot shows it.
    (await canvas.findByText(/ALLOW|DENY/)).scrollIntoView({ block: 'center' });
  };

export const CheckAllowed: Story = { play: runCheck('agents:run:start') };

export const CheckDenied: Story = { play: runCheck('platform:grants:write') };
