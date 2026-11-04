import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ManagedInGitBadge } from './managed-in-git';

describe('ManagedInGitBadge', () => {
  const repository = 'github.com/example-org/a-very-long-repository-name-for-agents';
  const label = `Managed by ${repository}`;

  it('truncates the repository and exposes the full text as the tooltip', () => {
    const { container } = render(<ManagedInGitBadge repository={repository} />);
    expect(container.querySelector(`[title="${label}"]`)).not.toBeNull();
    expect(screen.getByText(label)).toHaveClass('truncate');
  });
});
