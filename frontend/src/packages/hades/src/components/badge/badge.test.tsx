import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Badge } from './badge';

describe('Badge', () => {
  describe('rendering', () => {
    it('renders children', () => {
      render(<Badge>Active</Badge>);
      expect(screen.getByText('Active')).toBeInTheDocument();
    });

    it('forwards ref to the underlying span element', () => {
      let ref: HTMLSpanElement | null = null;
      render(
        <Badge
          ref={(el) => {
            ref = el;
          }}
        >
          Ref test
        </Badge>,
      );
      expect(ref).toBeInstanceOf(HTMLSpanElement);
    });
  });

  describe('variants', () => {
    it.each(['default', 'secondary', 'outline', 'dim', 'destructive'] as const)(
      'renders variant=%s without throwing',
      (variant) => {
        const { unmount } = render(<Badge variant={variant}>Badge</Badge>);
        expect(screen.getByText('Badge')).toBeInTheDocument();
        unmount();
      },
    );

    it.each(['healthy', 'degraded', 'error', 'idle', 'pending'] as const)(
      'renders status variant=%s without throwing',
      (variant) => {
        const { unmount } = render(<Badge variant={variant}>Status</Badge>);
        expect(screen.getByText('Status')).toBeInTheDocument();
        unmount();
      },
    );
  });

  describe('sizes', () => {
    it.each(['md', 'sm'] as const)('renders size=%s without throwing', (size) => {
      const { unmount } = render(<Badge size={size}>Badge</Badge>);
      expect(screen.getByText('Badge')).toBeInTheDocument();
      unmount();
    });
  });

  describe('dot indicator', () => {
    it('renders a dot element when dot=true', () => {
      render(
        <Badge data-testid="badge" dot>
          With dot
        </Badge>,
      );
      const badge = screen.getByTestId('badge');
      // Verify the aria-hidden dot span is a child of this badge, not just
      // somewhere on the page.
      expect(badge.querySelector('[aria-hidden="true"]')).toBeInTheDocument();
    });

    it('does not render a dot element when dot is omitted', () => {
      render(<Badge data-testid="badge">No dot</Badge>);
      const badge = screen.getByTestId('badge');
      expect(badge.querySelector('[aria-hidden="true"]')).not.toBeInTheDocument();
    });

    it('renders dot alongside status variant', () => {
      render(
        <Badge data-testid="badge" variant="healthy" dot>
          Healthy
        </Badge>,
      );
      const badge = screen.getByTestId('badge');
      expect(screen.getByText('Healthy')).toBeInTheDocument();
      expect(badge.querySelector('[aria-hidden="true"]')).toBeInTheDocument();
    });
  });

  describe('native span attributes', () => {
    it('passes through data-* attributes', () => {
      render(<Badge data-testid="my-badge">Badge</Badge>);
      expect(screen.getByTestId('my-badge')).toBeInTheDocument();
    });

    it('passes through aria-label', () => {
      render(<Badge aria-label="System status: healthy">Healthy</Badge>);
      expect(screen.getByLabelText('System status: healthy')).toBeInTheDocument();
    });
  });

  describe('asChild', () => {
    it('renders its styles onto the child link', () => {
      render(
        <Badge asChild interactive variant="secondary">
          <a href="https://example.com/pr/1">PR #1</a>
        </Badge>,
      );
      const link = screen.getByRole('link', { name: 'PR #1' });
      expect(link).toHaveAttribute('href', 'https://example.com/pr/1');
      expect(link).toHaveAttribute('data-slot', 'badge');
      expect(link.className).toContain('cursor-pointer');
      expect(link.parentElement?.tagName).not.toBe('SPAN');
    });

    it('renders a span without asChild and no pointer without interactive', () => {
      render(<Badge>Plain</Badge>);
      const badge = screen.getByText('Plain');
      expect(badge.tagName).toBe('SPAN');
      expect(badge.className).not.toContain('cursor-pointer');
    });
  });
});
