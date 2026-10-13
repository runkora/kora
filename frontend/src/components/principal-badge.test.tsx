import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { PrincipalBadge } from './principal-badge';

describe('PrincipalBadge', () => {
  it.each(['AGENT', 'HUMAN', 'PAT', 'SERVICE'])('shows the %s type', (type) => {
    render(<PrincipalBadge type={type} />);
    expect(screen.getByText(type)).toBeInTheDocument();
  });

  it('gives each of the four types a distinct colour except service and unknown', () => {
    const { rerender } = render(<PrincipalBadge type="AGENT" />);
    const agent = screen.getByText('AGENT').className;
    rerender(<PrincipalBadge type="HUMAN" />);
    const human = screen.getByText('HUMAN').className;
    rerender(<PrincipalBadge type="SERVICE" />);
    const service = screen.getByText('SERVICE').className;
    rerender(<PrincipalBadge type="SOMETHING_ELSE" />);
    const unknown = screen.getByText('SOMETHING_ELSE').className;
    expect(new Set([agent, human, service]).size).toBe(3);
    expect(unknown).toBe(service);
  });
});
