import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { TruncatingBadge } from './truncating-badge';

describe('TruncatingBadge', () => {
  const label = 'Automated Deployment Safety Reviewer';

  it('shows the label', () => {
    render(<TruncatingBadge label={label} variant="outline" textColor="foreground" />);
    expect(screen.getByText(label)).toBeInTheDocument();
  });

  it('exposes the full label as the tooltip', () => {
    const { container } = render(
      <TruncatingBadge label={label} variant="outline" textColor="foreground" />,
    );
    expect(container.querySelector(`[title="${label}"]`)).not.toBeNull();
  });

  it('renders the label in a single truncating span', () => {
    const { container } = render(
      <TruncatingBadge label={label} variant="secondary" textColor="muted" />,
    );
    expect(container.querySelectorAll('.truncate')).toHaveLength(1);
  });
});
