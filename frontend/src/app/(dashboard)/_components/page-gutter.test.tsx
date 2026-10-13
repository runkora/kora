import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Text } from '@orbit/hades';
import { PageGutter } from './page-gutter';

const pathname = vi.fn();
vi.mock('next/navigation', () => ({ usePathname: () => pathname() }));

describe('PageGutter', () => {
  it('pads page content once with the standard gutter', () => {
    pathname.mockReturnValue('/agents');
    render(
      <PageGutter>
        <Text>Page</Text>
      </PageGutter>,
    );
    const wrapper = screen.getByText('Page').parentElement;
    expect(wrapper).toHaveClass('p-4');
    expect(wrapper?.parentElement?.querySelectorAll('.p-4')).toHaveLength(1);
  });

  it('leaves /chat full bleed', () => {
    pathname.mockReturnValue('/chat');
    render(
      <PageGutter>
        <Text>Chat</Text>
      </PageGutter>,
    );
    expect(screen.getByText('Chat').parentElement).not.toHaveClass('p-4');
  });
});
