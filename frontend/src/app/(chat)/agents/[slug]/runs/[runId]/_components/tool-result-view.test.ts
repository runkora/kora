import { describe, expect, it } from 'vitest';
import { editDiff } from './tool-result-view';

describe('editDiff', () => {
  it.each([
    ['Claude Code', { old_string: 'a', new_string: 'b' }],
    ['OpenCode', { oldString: 'a', newString: 'b' }],
  ])('reads %s edits', (_, input) => {
    expect(editDiff(input)).toEqual([
      { kind: 'del', text: '-a' },
      { kind: 'add', text: '+b' },
    ]);
  });

  it('is null for inputs without both strings', () => {
    expect(editDiff({ file_path: 'x' })).toBeNull();
  });
});
