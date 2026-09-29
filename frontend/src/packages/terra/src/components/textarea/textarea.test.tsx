import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { Textarea } from './textarea';

// The DOM environment does no layout, so scrollHeight is always 0. Stub it to
// report a content height proportional to the number of lines in the value.
const LINE_PX = 21;
let original: PropertyDescriptor | undefined;

beforeEach(() => {
  original = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'scrollHeight');
  Object.defineProperty(HTMLTextAreaElement.prototype, 'scrollHeight', {
    configurable: true,
    get(this: HTMLTextAreaElement) {
      return this.value.split('\n').length * LINE_PX + 16;
    },
  });
});

afterEach(() => {
  if (original) Object.defineProperty(HTMLTextAreaElement.prototype, 'scrollHeight', original);
});

const lines = (n: number) => Array.from({ length: n }, (_, i) => `line ${i}`).join('\n');

describe('Textarea autoResize', () => {
  it('sizes a pre-filled value on mount', () => {
    render(<Textarea aria-label="t" autoResize maxRows={8} value={lines(4)} onChange={() => {}} />);
    expect(screen.getByLabelText('t').style.height).toBe(`${4 * LINE_PX + 16}px`);
  });

  it('caps at maxRows and scrolls beyond it', () => {
    render(
      <Textarea aria-label="t" autoResize maxRows={3} value={lines(10)} onChange={() => {}} />,
    );
    const el = screen.getByLabelText('t');
    expect(el.style.height).toBe(`${3 * LINE_PX + 16}px`);
    expect(el.style.overflowY).toBe('auto');
  });

  it('re-sizes when the controlled value changes from outside', () => {
    const { rerender } = render(
      <Textarea aria-label="t" autoResize value={lines(1)} onChange={() => {}} />,
    );
    rerender(<Textarea aria-label="t" autoResize value={lines(5)} onChange={() => {}} />);
    expect(screen.getByLabelText('t').style.height).toBe(`${5 * LINE_PX + 16}px`);
  });

  it('re-sizes on user input', () => {
    render(<Textarea aria-label="t" autoResize defaultValue="" />);
    const el = screen.getByLabelText('t') as HTMLTextAreaElement;
    fireEvent.change(el, { target: { value: lines(3) } });
    expect(el.style.height).toBe(`${3 * LINE_PX + 16}px`);
  });

  it('leaves height alone without autoResize', () => {
    render(<Textarea aria-label="t" value={lines(4)} onChange={() => {}} />);
    expect(screen.getByLabelText('t').style.height).toBe('');
  });
});

describe('Textarea action', () => {
  it('renders the bare textarea when no action is given', () => {
    const { container } = render(<Textarea aria-label="t" />);
    expect(container.firstElementChild?.tagName).toBe('TEXTAREA');
  });

  it('pins the action beside the textarea and reserves room for it', () => {
    render(<Textarea aria-label="t" action={<button type="button">Expand</button>} />);
    const el = screen.getByLabelText('t');
    expect(el.className).toContain('pr-12');
    expect(el.parentElement?.getAttribute('data-slot')).toBe('textarea-wrapper');
    expect(screen.getByRole('button', { name: 'Expand' })).toBeTruthy();
  });

  it('forwards the ref to the textarea element when an action is set', () => {
    const ref = { current: null as HTMLTextAreaElement | null };
    render(<Textarea ref={ref} aria-label="t" action={<span>x</span>} />);
    expect(ref.current?.tagName).toBe('TEXTAREA');
  });
});
