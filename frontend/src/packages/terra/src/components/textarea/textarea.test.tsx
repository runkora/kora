import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Textarea } from './textarea';

describe('Textarea resize', () => {
  it('is not resizable by default', () => {
    render(<Textarea aria-label="Prompt" />);
    expect(screen.getByLabelText('Prompt')).toHaveClass('resize-none');
  });

  it('is vertically resizable when resize="vertical"', () => {
    render(<Textarea aria-label="Prompt" resize="vertical" />);
    expect(screen.getByLabelText('Prompt')).toHaveClass('resize-y');
  });

  it('is not resizable when autoResize is true and resize="vertical"', () => {
    render(<Textarea aria-label="Prompt" autoResize resize="vertical" />);
    expect(screen.getByLabelText('Prompt')).toHaveClass('resize-none');
  });

  it('floors minHeight at the starting rows when resizable', () => {
    render(<Textarea aria-label="Prompt" resize="vertical" rows={5} />);
    // 5 rows × 21px line-height + 16px padding + 2px border (default variant)
    expect(screen.getByLabelText('Prompt')).toHaveStyle({ minHeight: '123px' });
  });

  it('floors minHeight at the native two rows when resizable without explicit rows', () => {
    render(<Textarea aria-label="Prompt" resize="vertical" />);
    // 2 rows × 21px + 16px padding + 2px border
    expect(screen.getByLabelText('Prompt')).toHaveStyle({ minHeight: '60px' });
  });
});
