/** Unit tests for the Skills favourites in the preferences document. */
import { describe, expect, it } from 'vitest';
import { mergeWithDefaults, PREFERENCE_DEFAULTS, serializeSparse } from './preferences.schema';

describe('skills.favouriteSkills', () => {
  it.each([
    [{ skills: { favouriteSkills: ['a:skills/pdf', '', 3] } }, ['a:skills/pdf']],
    [{ skills: { favouriteSkills: 'a:skills/pdf' } }, []],
    [{}, []],
  ])('reads %j as %j', (stored, expected) => {
    expect(mergeWithDefaults(stored).skills.favouriteSkills).toEqual(expected);
  });

  it('is left out of the saved document while empty', () => {
    expect(serializeSparse(PREFERENCE_DEFAULTS)).not.toHaveProperty('skills');
    expect(
      serializeSparse({ ...PREFERENCE_DEFAULTS, skills: { favouriteSkills: ['a:skills/pdf'] } }),
    ).toEqual({ skills: { favouriteSkills: ['a:skills/pdf'] } });
  });
});
