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
});
