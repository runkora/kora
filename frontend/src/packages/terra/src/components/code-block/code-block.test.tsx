import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { CodeBlock } from './code-block';

describe('CodeBlock', () => {
  it('renders the code text', () => {
    const { getByText } = render(<CodeBlock text="echo hi" />);
    expect(getByText('echo hi')).toBeInTheDocument();
  });

  it('renders the optional filename header', () => {
    const { getByText } = render(<CodeBlock text="x" filename="~/.zshrc" />);
    expect(getByText('~/.zshrc')).toBeInTheDocument();
  });

  it('scrolls horizontally by default (no wrap)', () => {
    const { container } = render(<CodeBlock text="x" />);
    const pre = container.querySelector('pre');
    expect(pre).toHaveClass('overflow-x-auto');
    expect(pre).not.toHaveClass('whitespace-pre-wrap');
  });

  it('soft-wraps long lines when wrap is set, so it cannot force the page wider', () => {
    const { container } = render(<CodeBlock text="x" wrap />);
    const pre = container.querySelector('pre');
    // overflow-x-auto gives the block an automatic min-width of 0 (it can never
    // be wider than its column); whitespace-pre-wrap + break-words wrap the text.
    expect(pre).toHaveClass('overflow-x-auto', 'whitespace-pre-wrap', 'break-words');
  });
});
