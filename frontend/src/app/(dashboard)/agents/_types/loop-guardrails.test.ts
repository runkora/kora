import { describe, expect, it } from 'vitest';

import { loopGuardrailErrors, loopGuardrailsPayload, loopGuardrailValues } from '.';

describe('loop guardrail helpers', () => {
  it('fills unset thresholds with the platform defaults', () => {
    expect(loopGuardrailValues({ wait_rearm_pause: 20 })).toEqual({
      repeatedCallWarn: '4',
      repeatedCallPause: '8',
      waitRearmWarn: '6',
      waitRearmPause: '20',
    });
  });

  it('sends only thresholds that differ from the defaults', () => {
    expect(loopGuardrailsPayload(loopGuardrailValues(undefined))).toEqual({});
    expect(
      loopGuardrailsPayload({
        ...loopGuardrailValues(undefined),
        repeatedCallWarn: '2',
        repeatedCallPause: '3',
      }),
    ).toEqual({ repeated_call_warn: 2, repeated_call_pause: 3 });
  });

  it('applies the API rules: warn at least 1, pause above warn, no off switch', () => {
    const d = loopGuardrailValues(undefined);
    expect(loopGuardrailErrors(d)).toEqual({});
    expect(loopGuardrailErrors({ ...d, repeatedCallWarn: '0' }).repeatedCall).toMatch(/at least 1/);
    expect(loopGuardrailErrors({ ...d, waitRearmPause: '0' }).waitRearm).toMatch(/greater than/);
    expect(loopGuardrailErrors({ ...d, waitRearmWarn: '2.5' }).waitRearm).toMatch(/whole number/);
  });
});
