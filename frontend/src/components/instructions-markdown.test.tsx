import { describe, expect, it } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';
import { InstructionsMarkdown } from './instructions-markdown';

const render = (content: string) => renderWithProviders(<InstructionsMarkdown content={content} />);

describe('InstructionsMarkdown', () => {
  it('numbers an ordered list and gives a nested unordered list bullets', () => {
    const { container } = render('1. first\n   - inner\n2. second');
    const text = container.textContent ?? '';
    expect(text).toContain('1.');
    expect(text).toContain('2.');
    expect(text).toContain('•');
    expect(screen.getAllByRole('list')).toHaveLength(2);
    expect(screen.getAllByRole('listitem')).toHaveLength(3);
  });

  it('keeps the whitespace of fenced code without a language and of indented code', () => {
    const { container } = render('```\na\n  b\n```\n\ntext\n\n    c\n      d');
    const blocks = [
      ...container.querySelectorAll(
        '[style*="white-space"], .whitespace-pre-wrap, .whitespace-pre',
      ),
    ];
    expect(blocks.map((b) => b.textContent)).toEqual(['a\n  b', 'c\n  d']);
  });

  it('puts a fenced block and a quote inside a list item beside its text, not inside a paragraph', () => {
    const { container } = render('- item\n\n  > quoted\n\n  ```\n  code\n  ```');
    expect(container.querySelector('p div')).toBeNull();
    expect(container.textContent).toContain('quoted');
    expect(container.textContent).toContain('code');
  });
});
