import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { TableContainer } from './table-extras';

describe('TableContainer', () => {
  it('renders children', () => {
    const { getByText } = render(<TableContainer>body</TableContainer>);
    expect(getByText('body')).toBeInTheDocument();
  });

  it('applies min-w-0 so it can shrink below its table width in a flex layout', () => {
    // Without min-w-0 a flex child defaults to min-width:auto and refuses to
    // shrink, so the inner TableRoot overflow-x-auto never engages and a wide
    // table forces the whole page wider than the viewport on mobile.
    const { container } = render(<TableContainer>x</TableContainer>);
    expect(container.firstElementChild).toHaveClass('min-w-0');
  });
});
