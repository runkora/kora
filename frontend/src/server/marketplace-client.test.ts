import { describe, expect, it } from 'vitest';
import { fetchSkills } from './marketplace-client';

describe('Marketplace name search', () => {
  it('matches names without matching description-only text', async () => {
    const names = await fetchSkills('token', 'Platform Engineering', {
      offset: 0,
      limit: 24,
      search: ' REACT ',
      sort: 'name-asc',
    });
    expect(names.packages.map((skill) => skill.title_of_package)).toEqual([
      'React Expert',
      'React Reviewer',
    ]);
    const descriptions = await fetchSkills('token', 'Platform Engineering', {
      offset: 0,
      limit: 24,
      search: 'Senior',
    });
    expect(descriptions.total).toBe(0);
  });

  it('treats wildcard characters as literal search text', async () => {
    const result = await fetchSkills('token', 'Platform Engineering', {
      offset: 0,
      limit: 24,
      search: 'react*',
    });
    expect(result.total).toBe(0);
  });
});
