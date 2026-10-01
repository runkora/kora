import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ScopeChip } from './scope-chip';

describe('ScopeChip', () => {
  const scope = 'inference:anthropic:orbit-claude-sonnet:use';

  it('renders every segment', () => {
    render(<ScopeChip scope={scope} />);
    expect(screen.getByText('orbit-claude-sonnet')).toBeInTheDocument();
  });

  it('exposes the full scope as the tooltip', () => {
    const { container } = render(<ScopeChip scope={scope} />);
    expect(container.querySelector(`[title="${scope}"]`)).not.toBeNull();
  });

  it('renders the text in a single truncating span', () => {
    const { container } = render(<ScopeChip scope={scope} />);
    expect(container.querySelectorAll('.truncate')).toHaveLength(1);
  });
});
