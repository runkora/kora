import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Button } from './button';

// One characteristic Tailwind class per variant — lets the it.each below verify
// that CVA actually wired the right classes, not just that the component renders.
const VARIANT_CLASS = {
  default: 'bg-accent',
  secondary: 'bg-surface-elevated',
  outline: 'border',
  ghost: 'text-foreground',
  muted: 'text-subtle',
  destructive: 'bg-status-error',
} as const;

// One characteristic Tailwind class per size — height for text sizes, width for
// icon sizes (icon and md share h-9, so w-9 distinguishes icon; ditto icon-sm/xs).
const SIZE_CLASS = {
  xs: 'h-7',
  sm: 'h-8',
  md: 'h-9',
  lg: 'h-10',
  icon: 'w-9',
  'icon-sm': 'w-7',
} as const;

describe('Button', () => {
  describe('rendering', () => {
    it('renders children', () => {
      render(<Button>Click me</Button>);
      expect(screen.getByRole('button', { name: 'Click me' })).toBeInTheDocument();
    });

    it('forwards ref to the underlying button element', () => {
      let ref: HTMLButtonElement | null = null;
      render(
        <Button
          ref={(el) => {
            ref = el;
          }}
        >
          Ref test
        </Button>,
      );
      expect(ref).toBeInstanceOf(HTMLButtonElement);
    });

    it('renders as a child element when asChild is set', () => {
      render(
        <Button asChild>
          <a href="/test">Link button</a>
        </Button>,
      );
      const link = screen.getByRole('link', { name: 'Link button' });
      expect(link).toBeInTheDocument();
      expect(link).toHaveAttribute('href', '/test');
      // The <button> element itself must not be in the DOM
      expect(screen.queryByRole('button')).not.toBeInTheDocument();
    });
  });

  describe('disabled state', () => {
    it('is disabled when the disabled prop is set', () => {
      render(<Button disabled>Disabled</Button>);
      expect(screen.getByRole('button', { name: 'Disabled' })).toBeDisabled();
    });
  });

  describe('variants', () => {
    it.each(['default', 'secondary', 'outline', 'ghost', 'muted', 'destructive'] as const)(
      'renders variant=%s and applies characteristic class',
      (variant) => {
        const { unmount } = render(<Button variant={variant}>Button</Button>);
        expect(screen.getByRole('button')).toHaveClass(VARIANT_CLASS[variant]);
        unmount();
      },
    );
  });

  describe('sizes', () => {
    it.each(['xs', 'sm', 'md', 'lg', 'icon', 'icon-sm'] as const)(
      'renders size=%s and applies characteristic class',
      (size) => {
        const { unmount } = render(<Button size={size}>Button</Button>);
        expect(screen.getByRole('button')).toHaveClass(SIZE_CLASS[size]);
        unmount();
      },
    );
  });

  describe('shapes', () => {
    it('renders shape=pill and applies rounded-full', () => {
      render(<Button shape="pill">Pill</Button>);
      expect(screen.getByRole('button')).toHaveClass('rounded-full');
    });
  });

  describe('fullWidth', () => {
    it('renders fullWidth and applies w-full', () => {
      render(<Button fullWidth>Full</Button>);
      expect(screen.getByRole('button')).toHaveClass('w-full');
    });
  });

  describe('native button attributes', () => {
    it('passes through type attribute', () => {
      render(<Button type="submit">Submit</Button>);
      expect(screen.getByRole('button')).toHaveAttribute('type', 'submit');
    });

    it('passes through data-* attributes', () => {
      render(<Button data-testid="my-btn">Button</Button>);
      expect(screen.getByTestId('my-btn')).toBeInTheDocument();
    });

    it('passes through aria-label', () => {
      render(<Button aria-label="Close dialog">×</Button>);
      expect(screen.getByRole('button', { name: 'Close dialog' })).toBeInTheDocument();
    });
  });
});
