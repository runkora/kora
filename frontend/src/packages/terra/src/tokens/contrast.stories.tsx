import type { Meta, StoryObj } from '@storybook/react';
import React from 'react';
import { Heading, Text } from '../components/typography/typography';

/**
 * Contrast
 *
 * Contrast ratio of each colour against each surface, calculated from the
 * current values for the active brand and theme.
 */
const meta: Meta = {
  title: 'Terra/Foundation/Contrast',
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj;

type Rgb = [number, number, number];

const SURFACES = [
  { label: 'Background', cssVar: '--color-background' },
  { label: 'Surface', cssVar: '--color-surface' },
  { label: 'Elevated', cssVar: '--color-surface-elevated' },
  { label: 'Overlay', cssVar: '--color-surface-overlay' },
];

interface Pair {
  label: string;
  cssVar: string;
  /** Minimum ratio: 4.5 for text, 3 for borders and other non-text. */
  need: number;
  /** Set when the colour is checked against something other than the surfaces. */
  against?: { label: string; cssVar: string };
}

const TEXT = 4.5;
const NON_TEXT = 3;

const GROUPS: { title: string; note: string; pairs: Pair[] }[] = [
  {
    title: 'Text',
    note: 'Minimum 4.5:1 (WCAG 1.4.3).',
    pairs: [
      { label: 'Foreground', cssVar: '--color-foreground', need: TEXT },
      { label: 'Muted', cssVar: '--color-muted', need: TEXT },
      { label: 'Subtle', cssVar: '--color-subtle', need: TEXT },
      { label: 'Accent', cssVar: '--color-accent', need: TEXT },
      { label: 'Accent hover', cssVar: '--color-accent-hover', need: TEXT },
      { label: 'Status healthy', cssVar: '--color-status-healthy', need: TEXT },
      { label: 'Status degraded', cssVar: '--color-status-degraded', need: TEXT },
      { label: 'Status error', cssVar: '--color-status-error', need: TEXT },
      { label: 'Status pending', cssVar: '--color-status-pending', need: TEXT },
    ],
  },
  {
    title: 'Borders and chart colours',
    note: 'Minimum 3:1 (WCAG 1.4.11).',
    pairs: [
      { label: 'Border', cssVar: '--color-border', need: NON_TEXT },
      { label: 'Border accent', cssVar: '--color-border-accent', need: NON_TEXT },
      ...[1, 2, 3, 4, 5, 6, 7, 8].map((n) => ({
        label: `Chart ${n}`,
        cssVar: `--color-chart-${n}`,
        need: NON_TEXT,
      })),
    ],
  },
  {
    title: 'Text on filled controls',
    note: 'Text colour against the fill it sits on. Minimum 4.5:1 (WCAG 1.4.3).',
    pairs: [
      {
        label: 'Accent foreground on accent',
        cssVar: '--color-accent-foreground',
        need: TEXT,
        against: { label: 'Accent', cssVar: '--color-accent' },
      },
      {
        label: 'Accent foreground on accent hover',
        cssVar: '--color-accent-foreground',
        need: TEXT,
        against: { label: 'Accent hover', cssVar: '--color-accent-hover' },
      },
    ],
  },
];

// Draws the colour on a canvas to convert any CSS colour format to RGB.
function toRgb(ctx: CanvasRenderingContext2D, value: string): Rgb {
  ctx.clearRect(0, 0, 1, 1);
  ctx.fillStyle = '#000';
  ctx.fillStyle = value;
  ctx.fillRect(0, 0, 1, 1);
  const data = ctx.getImageData(0, 0, 1, 1).data;
  return [data[0] ?? 0, data[1] ?? 0, data[2] ?? 0];
}

function luminance([r, g, b]: Rgb): number {
  const lin = (c: number) => {
    const v = c / 255;
    return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

function ratio(a: Rgb, b: Rgb): number {
  const la = luminance(a);
  const lb = luminance(b);
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

type Resolved = Record<string, Rgb>;

const rgbOf = (tokens: Resolved, cssVar: string): Rgb => tokens[cssVar] ?? [0, 0, 0];

/** Resolves the colours used on this page, and again when the brand or theme changes. */
function useResolvedTokens(cssVars: string[]): Resolved {
  const [resolved, setResolved] = React.useState<Resolved>({});
  const key = cssVars.join('|');
  React.useEffect(() => {
    const ctx = document.createElement('canvas').getContext('2d', { willReadFrequently: true });
    const probe = document.createElement('div');
    document.body.appendChild(probe);
    const read = () => {
      if (!ctx) return;
      const next: Resolved = {};
      for (const cssVar of key.split('|')) {
        probe.style.color = `var(${cssVar})`;
        next[cssVar] = toRgb(ctx, getComputedStyle(probe).color);
      }
      setResolved(next);
    };
    read();
    const observer = new MutationObserver(read);
    observer.observe(document.documentElement, { attributes: true });
    return () => {
      observer.disconnect();
      probe.remove();
    };
  }, [key]);
  return resolved;
}

const hex = ([r, g, b]: Rgb) =>
  `#${[r, g, b].map((c) => c.toString(16).padStart(2, '0')).join('')}`;

interface CellProps {
  ratioValue: number;
  need: number;
  fg: Rgb;
  bg: Rgb;
  /** True when the colour under test is a label on a fill, not a colour on a surface. */
  onFill: boolean;
}

function Cell({ ratioValue, need, fg, bg, onFill }: CellProps) {
  const pass = ratioValue >= need;
  return (
    <div
      className="flex items-center gap-3 px-2 py-1.5"
      style={{ background: onFill ? 'var(--color-background)' : hex(bg) }}
    >
      {/* The colours under test are shown as a chip, so the numbers stay readable. */}
      <span
        className="flex h-4 w-8 shrink-0 items-center justify-center rounded-sm ring-1 ring-inset ring-black/20"
        style={{ background: onFill ? hex(bg) : hex(fg) }}
      >
        {onFill && <span className="h-2 w-4 rounded-sm" style={{ background: hex(fg) }} />}
      </span>
      <span className="font-mono text-xs tabular-nums text-foreground">
        {ratioValue.toFixed(2)}
      </span>
      <span
        className="rounded-sm px-1 text-[11px] font-semibold"
        style={
          pass
            ? { border: '1px solid currentColor', color: 'var(--color-foreground)' }
            : { background: '#ffffff', color: '#b00020' }
        }
      >
        {pass ? 'pass' : 'FAIL'}
      </span>
    </div>
  );
}

function ContrastTable() {
  const cssVars = [
    ...SURFACES.map((s) => s.cssVar),
    ...GROUPS.flatMap((g) =>
      g.pairs.flatMap((p) => [p.cssVar, ...(p.against ? [p.against.cssVar] : [])]),
    ),
  ];
  const tokens = useResolvedTokens([...new Set(cssVars)]);
  if (!SURFACES.every((s) => tokens[s.cssVar])) return <div />;

  const rowsFor = (p: Pair) => {
    const targets = p.against ? [p.against] : SURFACES;
    return targets.map((t) => ({
      target: t,
      value: ratio(rgbOf(tokens, p.cssVar), rgbOf(tokens, t.cssVar)),
    }));
  };

  return (
    <div className="space-y-8">
      <div className="space-y-1">
        <Heading variant="title">Contrast</Heading>
        <Text color="muted">Contrast ratio of each colour against each surface.</Text>
      </div>
      {GROUPS.map((group) => (
        <section key={group.title} className="space-y-2">
          <div>
            <Heading variant="label" asChild>
              <h2>{group.title}</h2>
            </Heading>
            <Text size="supporting" color="muted">
              {group.note}
            </Text>
          </div>
          <div className="grid gap-px" style={{ gridTemplateColumns: '220px repeat(4, 1fr)' }}>
            <span />
            {group.pairs[0]?.against ? (
              <Text size="caption" color="subtle" className="col-span-4 px-2">
                Fill
              </Text>
            ) : (
              SURFACES.map((s) => (
                <Text key={s.label} size="caption" color="subtle" className="px-2">
                  on {s.label}
                </Text>
              ))
            )}
            {group.pairs.map((p) => {
              const rows = rowsFor(p);
              return (
                <React.Fragment key={p.label}>
                  <div className="flex items-center gap-2 py-1.5">
                    <span
                      className="h-4 w-4 shrink-0 rounded-sm ring-1 ring-inset ring-border"
                      style={{ background: hex(rgbOf(tokens, p.cssVar)) }}
                    />
                    <Text size="supporting">{p.label}</Text>
                  </div>
                  {rows.map((r) => (
                    <Cell
                      key={r.target.cssVar}
                      ratioValue={r.value}
                      need={p.need}
                      onFill={Boolean(p.against)}
                      fg={rgbOf(tokens, p.cssVar)}
                      bg={rgbOf(tokens, r.target.cssVar)}
                    />
                  ))}
                  {p.against &&
                    Array.from({ length: 3 }, (_, i) => <span key={`pad-${p.label}-${i}`} />)}
                </React.Fragment>
              );
            })}
          </div>
        </section>
      ))}
    </div>
  );
}

export const Ratios: Story = { render: () => <ContrastTable /> };
