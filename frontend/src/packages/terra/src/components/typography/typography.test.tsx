import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Heading, Text, Code } from './typography';

// ── Heading ───────────────────────────────────────────────────────────────────

// Each variant has a documented default HTML tag (HEADING_TAG map) and a
// characteristic Tailwind class from headingVariants. Verifying both in one
// it.each gives us a real contract on CVA output AND the tag-dispatch logic.
const HEADING_VARIANT = [
  ['display', 'h1', 'text-5xl'],
  ['title', 'h1', 'text-3xl'],
  ['section', 'h2', 'text-2xl'],
  ['subsection', 'h3', 'text-xl'],
  ['label', 'h4', 'text-base'],
  ['metric', 'div', 'tabular-nums'],
] as const;

// One characteristic class per color value.
const HEADING_COLOR_CLASS = {
  foreground: 'text-foreground',
  muted: 'text-muted',
  accent: 'text-accent',
} as const;

describe('Heading', () => {
  describe('rendering', () => {
    it('renders children', () => {
      render(<Heading>Hello world</Heading>);
      expect(screen.getByText('Hello world')).toBeInTheDocument();
    });

    it('forwards ref to the underlying element', () => {
      let ref: HTMLElement | null = null;
      render(
        <Heading
          ref={(el) => {
            ref = el;
          }}
        >
          Ref test
        </Heading>,
      );
      // Default variant is "section" which renders as <h2> → HTMLHeadingElement
      expect(ref).toBeInstanceOf(HTMLHeadingElement);
    });

    it('renders as a child element when asChild is set', () => {
      render(
        <Heading asChild>
          <h1>Custom heading</h1>
        </Heading>,
      );
      expect(screen.getByRole('heading', { level: 1, name: 'Custom heading' })).toBeInTheDocument();
    });
  });

  describe('variants', () => {
    it.each(HEADING_VARIANT)(
      'renders variant=%s as <%s> with characteristic class',
      (variant, tag, cls) => {
        const { unmount } = render(
          <Heading data-testid="h" variant={variant}>
            Heading
          </Heading>,
        );
        const el = screen.getByTestId('h');
        // Verify HEADING_TAG dispatch — the rendered element must match the
        // documented default tag for this variant.
        expect(el.tagName.toLowerCase()).toBe(tag);
        // Verify CVA wired the right classes.
        expect(el).toHaveClass(cls);
        unmount();
      },
    );
  });

  describe('colors', () => {
    it.each(['foreground', 'muted', 'accent'] as const)(
      'renders color=%s and applies characteristic class',
      (color) => {
        const { unmount } = render(<Heading color={color}>Heading</Heading>);
        expect(screen.getByText('Heading')).toHaveClass(HEADING_COLOR_CLASS[color]);
        unmount();
      },
    );
  });

  describe('native attributes', () => {
    it('passes through data-* attributes', () => {
      render(<Heading data-testid="my-heading">Heading</Heading>);
      expect(screen.getByTestId('my-heading')).toBeInTheDocument();
    });
  });
});

// ── Text ──────────────────────────────────────────────────────────────────────

const TEXT_SIZE_CLASS = {
  micro: 'text-[11px]',
  caption: 'text-xs',
  supporting: 'text-sm',
  body: 'text-base',
  lead: 'text-lg',
} as const;

const TEXT_COLOR_CLASS = {
  foreground: 'text-foreground',
  muted: 'text-muted',
  subtle: 'text-subtle',
  accent: 'text-accent',
  'status-healthy': 'text-status-healthy',
  'status-degraded': 'text-status-degraded',
  'status-error': 'text-status-error',
} as const;

const TEXT_WEIGHT_CLASS = {
  regular: 'font-normal',
  emphasis: 'font-medium',
  strong: 'font-semibold',
  heavy: 'font-bold',
} as const;

const TEXT_ALIGN_CLASS = {
  start: 'text-left',
  center: 'text-center',
  end: 'text-right',
} as const;

describe('Text', () => {
  describe('rendering', () => {
    it('renders children', () => {
      render(<Text>Paragraph text</Text>);
      expect(screen.getByText('Paragraph text')).toBeInTheDocument();
    });

    it('forwards ref to the underlying element', () => {
      let ref: HTMLParagraphElement | null = null;
      render(
        <Text
          ref={(el) => {
            ref = el;
          }}
        >
          Ref test
        </Text>,
      );
      // Default renders a <p> → HTMLParagraphElement
      expect(ref).toBeInstanceOf(HTMLParagraphElement);
    });

    it('renders as a child element when asChild is set', () => {
      const { container } = render(
        <Text asChild>
          <span>Span text</span>
        </Text>,
      );
      expect(screen.getByText('Span text')).toBeInTheDocument();
      // Slot must not leave a wrapping <p> in the DOM
      expect(container.querySelector('p')).not.toBeInTheDocument();
    });
  });

  describe('sizes', () => {
    it.each(['micro', 'caption', 'supporting', 'body', 'lead'] as const)(
      'renders size=%s and applies characteristic class',
      (size) => {
        const { unmount } = render(<Text size={size}>Text</Text>);
        expect(screen.getByText('Text')).toHaveClass(TEXT_SIZE_CLASS[size]);
        unmount();
      },
    );
  });

  describe('colors', () => {
    it.each([
      'foreground',
      'muted',
      'subtle',
      'accent',
      'status-healthy',
      'status-degraded',
      'status-error',
    ] as const)('renders color=%s and applies characteristic class', (color) => {
      const { unmount } = render(<Text color={color}>Text</Text>);
      expect(screen.getByText('Text')).toHaveClass(TEXT_COLOR_CLASS[color]);
      unmount();
    });
  });

  describe('weight', () => {
    it.each(['regular', 'emphasis', 'strong', 'heavy'] as const)(
      'renders weight=%s and applies characteristic class',
      (weight) => {
        const { unmount } = render(<Text weight={weight}>Text</Text>);
        expect(screen.getByText('Text')).toHaveClass(TEXT_WEIGHT_CLASS[weight]);
        unmount();
      },
    );
  });

  describe('transform', () => {
    it('renders transform=uppercase and applies uppercase + tracking-widest', () => {
      render(<Text transform="uppercase">Text</Text>);
      const el = screen.getByText('Text');
      expect(el).toHaveClass('uppercase');
      expect(el).toHaveClass('tracking-widest');
    });
  });

  describe('align', () => {
    it.each(['start', 'center', 'end'] as const)(
      'renders align=%s and applies characteristic class',
      (align) => {
        const { unmount } = render(<Text align={align}>Text</Text>);
        expect(screen.getByText('Text')).toHaveClass(TEXT_ALIGN_CLASS[align]);
        unmount();
      },
    );
  });

  describe('boolean style props', () => {
    it('applies truncate class when truncate=true', () => {
      render(<Text truncate>Truncated text</Text>);
      expect(screen.getByText('Truncated text')).toHaveClass('truncate');
    });

    it('applies font-mono class when mono=true', () => {
      render(<Text mono>Monospace text</Text>);
      expect(screen.getByText('Monospace text')).toHaveClass('font-mono');
    });

    it('applies tabular-nums class when tabular=true', () => {
      render(<Text tabular>Tabular text</Text>);
      expect(screen.getByText('Tabular text')).toHaveClass('tabular-nums');
    });
  });

  describe('native attributes', () => {
    it('passes through data-* attributes', () => {
      render(<Text data-testid="my-text">Text</Text>);
      expect(screen.getByTestId('my-text')).toBeInTheDocument();
    });
  });
});

// ── Code ──────────────────────────────────────────────────────────────────────

const CODE_VARIANT_CLASS = {
  inline: 'bg-surface-elevated',
  bare: 'text-muted',
  highlight: 'text-accent',
} as const;

describe('Code', () => {
  describe('rendering', () => {
    it('renders children', () => {
      render(<Code>const x = 1;</Code>);
      expect(screen.getByText('const x = 1;')).toBeInTheDocument();
    });

    it('forwards ref to the underlying element', () => {
      let ref: HTMLElement | null = null;
      render(
        <Code
          ref={(el) => {
            ref = el;
          }}
        >
          Ref test
        </Code>,
      );
      expect(ref).toBeInstanceOf(HTMLElement);
    });

    it('always renders a semantic <code> element', () => {
      render(<Code data-testid="c">snippet</Code>);
      expect(screen.getByTestId('c').tagName).toBe('CODE');
    });
  });

  describe('variants', () => {
    it.each(['inline', 'bare', 'highlight'] as const)(
      'renders variant=%s and applies characteristic class',
      (variant) => {
        const { unmount } = render(<Code variant={variant}>code</Code>);
        expect(screen.getByText('code')).toHaveClass(CODE_VARIANT_CLASS[variant]);
        unmount();
      },
    );
  });

  describe('native attributes', () => {
    it('passes through data-* attributes', () => {
      render(<Code data-testid="my-code">code</Code>);
      expect(screen.getByTestId('my-code')).toBeInTheDocument();
    });
  });
});
