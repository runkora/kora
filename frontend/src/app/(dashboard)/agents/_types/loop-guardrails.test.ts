import { describe, expect, it } from 'vitest';

import { loopGuardrailErrors, loopGuardrailsPayload, loopGuardrailValues } from '.';

describe('loop guardrail helpers', () => {
  it('switches a pair on when either threshold is stored, and fills the rest with defaults', () => {
    expect(loopGuardrailValues({ wait_rearm_pause: 20 })).toEqual({
      repeatedCallOn: false,
      repeatedCallWarn: '4',
      repeatedCallPause: '8',
      waitRearmOn: true,
      waitRearmWarn: '6',
      waitRearmPause: '20',
    });
  });

  it('sends the pairs that are on, and nothing for the platform defaults', () => {
    expect(loopGuardrailsPayload(loopGuardrailValues(undefined))).toEqual({});
    expect(
      loopGuardrailsPayload({
        ...loopGuardrailValues(undefined),
        repeatedCallOn: true,
        repeatedCallWarn: '2',
        repeatedCallPause: '3',
      }),
    ).toEqual({ repeated_call_warn: 2, repeated_call_pause: 3 });
  });

  it('applies the API rules to pairs that are on: warn at least 1, pause above warn', () => {
    const on = { ...loopGuardrailValues(undefined), repeatedCallOn: true, waitRearmOn: true };
    expect(loopGuardrailErrors(on)).toEqual({});
    expect(loopGuardrailErrors({ ...on, repeatedCallWarn: '0' }).repeatedCall).toMatch(
      /at least 1/,
    );
    expect(loopGuardrailErrors({ ...on, waitRearmPause: '0' }).waitRearm).toMatch(/greater than/);
    expect(loopGuardrailErrors({ ...on, waitRearmWarn: '2.5' }).waitRearm).toMatch(/whole number/);
    expect(loopGuardrailErrors({ ...on, waitRearmOn: false, waitRearmPause: '0' })).toEqual({});
  });
});
