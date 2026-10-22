import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { PrincipalBadge, principalLabel } from './principal-badge';

describe('PrincipalBadge', () => {
  it.each([
    ['AGENT', 'Agent'],
    ['HUMAN', 'Human'],
    ['PAT', 'PAT'],
  ])('shows the %s type as %s', (type, label) => {
    render(<PrincipalBadge type={type} />);
    expect(screen.getByText(label)).toBeInTheDocument();
  });

  it('labels the SERVICE type "Trigger"', () => {
    render(<PrincipalBadge type="SERVICE" />);
    expect(screen.getByText('Trigger')).toBeInTheDocument();
    expect(screen.queryByText('SERVICE')).toBeNull();
  });

  it('maps only SERVICE to a different label', () => {
    expect(principalLabel('SERVICE')).toBe('Trigger');
    expect(principalLabel('AGENT')).toBe('Agent');
    expect(principalLabel('SOMETHING_ELSE')).toBe('SOMETHING_ELSE');
    expect(principalLabel('constructor')).toBe('constructor');
    expect(principalLabel('toString')).toBe('toString');
  });

  it('gives each of the four types a distinct colour except service and unknown', () => {
    const { rerender } = render(<PrincipalBadge type="AGENT" />);
    const agent = screen.getByText('Agent').className;
    rerender(<PrincipalBadge type="HUMAN" />);
    const human = screen.getByText('Human').className;
    rerender(<PrincipalBadge type="SERVICE" />);
    const service = screen.getByText('Trigger').className;
    rerender(<PrincipalBadge type="SOMETHING_ELSE" />);
    const unknown = screen.getByText('SOMETHING_ELSE').className;
    expect(new Set([agent, human, service]).size).toBe(3);
    expect(unknown).toBe(service);
  });
});
