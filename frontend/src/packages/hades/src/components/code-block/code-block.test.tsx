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

  describe('json highlighting', () => {
    const classesOf = (container: HTMLElement, text: string) =>
      Array.from(container.querySelectorAll('pre span'))
        .filter((el) => el.textContent === text)
        .map((el) => el.className);

    it('colours keys, string values, literals and numbers by kind', () => {
      const { container } = render(
        <CodeBlock
          language="json"
          text={'{"name": "orb\\"it", "n": -1.5, "ok": true, "x": null}'}
        />,
      );
      expect(classesOf(container, '"name":')).toEqual(['text-accent']);
      expect(classesOf(container, '"orb\\"it"')).toEqual(['text-status-healthy']);
      expect(classesOf(container, '-1.5')).toEqual(['text-status-healthy']);
      expect(classesOf(container, 'true')).toEqual(['text-status-degraded']);
      expect(classesOf(container, 'null')).toEqual(['text-status-degraded']);
      expect(container.querySelector('pre')?.textContent).toBe(
        '{"name": "orb\\"it", "n": -1.5, "ok": true, "x": null}',
      );
    });

    it('does not treat letters inside words as literals', () => {
      const { container } = render(<CodeBlock language="json" text={'{"nullable": "truest"}'} />);
      expect(classesOf(container, 'null')).toEqual([]);
      expect(classesOf(container, 'true')).toEqual([]);
    });

    it('renders an unterminated string as plain text without rescanning it', () => {
      const text = '{"a": "' + '\\"'.repeat(5000);
      const started = performance.now();
      const { container } = render(<CodeBlock language="json" text={text} />);
      expect(performance.now() - started).toBeLessThan(2000);
      expect(container.querySelector('pre')?.textContent).toBe(text);
      expect(classesOf(container, '"a":')).toEqual(['text-accent']);
    });
  });
});
