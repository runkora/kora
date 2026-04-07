'use client';

import * as React from 'react';
import { ResponsiveContainer } from 'recharts';
import { cn } from '../../utils';

// ── ChartConfig ───────────────────────────────────────────────────────────────

/**
 * Visual configuration for each data series in a chart.
 * Keys must match the `dataKey` prop on recharts series elements (e.g. `<Line>`).
 *
 * ```ts
 * const config: ChartConfig = {
 *   totalUsd: { label: 'Total', color: 'var(--color-accent)' },
 *   promptUsd: { label: 'Prompt', color: 'var(--color-subtle)' },
 * };
 * ```
 */
export type ChartConfig = Record<string, { label: string; color: string }>;

// ── Context ───────────────────────────────────────────────────────────────────

interface ChartContextValue {
  config: ChartConfig;
}

const ChartContext = React.createContext<ChartContextValue | null>(null);

function useChartConfig() {
  const ctx = React.useContext(ChartContext);
  if (!ctx) throw new Error('Chart sub-components must be used inside <ChartContainer>.');
  return ctx.config;
}

// ── ChartContainer ────────────────────────────────────────────────────────────

export interface ChartContainerProps extends React.HTMLAttributes<HTMLDivElement> {
  /**
   * Series configuration — keys must match recharts `dataKey` props.
   * Each entry's `color` is injected as `--color-{key}` so you can reference
   * it directly in recharts props: `stroke="var(--color-totalUsd)"`.
   */
  config: ChartConfig;
  /**
   * Chart height in pixels. Passed to the inner `ResponsiveContainer`.
   * @default 260
   */
  height?: number;
  /** A single recharts `<XxxChart>` element. */
  children: React.ReactElement;
}

/**
 * ChartContainer
 *
 * Wraps a recharts chart in a `ResponsiveContainer`, injects each series'
 * color as a CSS custom property (`--color-{key}`), and provides a React
 * context consumed by `ChartTooltipContent` and `ChartLegendContent`.
 *
 * ```tsx
 * const config: ChartConfig = {
 *   total: { label: 'Total', color: 'var(--color-accent)' },
 *   prompt: { label: 'Prompt', color: 'var(--color-subtle)' },
 * };
 *
 * <ChartContainer config={config} height={260}>
 *   <LineChart data={rows}>
 *     <Line dataKey="total" stroke="var(--color-total)" dot={false} />
 *     <Line dataKey="prompt" stroke="var(--color-prompt)" dot={false} />
 *     <Tooltip content={<ChartTooltipContent />} />
 *     <Legend content={<ChartLegendContent />} />
 *   </LineChart>
 * </ChartContainer>
 * ```
 */
const ChartContainer = React.forwardRef<HTMLDivElement, ChartContainerProps>(
  ({ config, height = 260, className, children, style, ...props }, ref) => {
    // Inject --color-{key} for every series so recharts props can reference them.
    const cssVars = Object.fromEntries(
      Object.entries(config).map(([key, val]) => [`--color-${key}`, val.color]),
    ) as React.CSSProperties;

    return (
      <ChartContext.Provider value={{ config }}>
        <div
          ref={ref}
          style={{ ...cssVars, height, ...style }}
          className={cn('w-full', className)}
          {...props}
        >
          <ResponsiveContainer width="100%" height="100%">
            {children}
          </ResponsiveContainer>
        </div>
      </ChartContext.Provider>
    );
  },
);
ChartContainer.displayName = 'ChartContainer';

// ── ChartTooltipContent ───────────────────────────────────────────────────────

interface TooltipPayloadEntry {
  dataKey?: string | number;
  value?: number | string | null;
  color?: string;
  name?: string | number;
}

export interface ChartTooltipContentProps {
  active?: boolean;
  payload?: TooltipPayloadEntry[];
  label?: string | number;
  /**
   * Custom value formatter. Receives (value, dataKey) and returns a display string.
   * Used for all series in the tooltip.
   */
  formatter?: (value: number | string, key: string) => string;
  /**
   * Custom label formatter. Receives the raw x-axis label and returns a display string.
   * Useful for formatting date strings in the tooltip header.
   */
  labelFormatter?: (label: string) => string;
}

/**
 * ChartTooltipContent
 *
 * A styled tooltip body for recharts' `<Tooltip content={...}>` prop.
 * Reads series labels and colors from the nearest `<ChartContainer>`'s config.
 *
 * ```tsx
 * <Tooltip
 *   cursor={{ stroke: 'var(--color-border-subtle)', strokeWidth: 1 }}
 *   content={
 *     <ChartTooltipContent formatter={(v) => `$${Number(v).toFixed(2)}`} />
 *   }
 * />
 * ```
 */
function ChartTooltipContent({
  active,
  payload,
  label,
  formatter,
  labelFormatter,
}: ChartTooltipContentProps) {
  const config = useChartConfig();

  if (!active || !payload?.length) return null;

  const rawLabel = label != null ? String(label) : '';
  const displayLabel = labelFormatter ? labelFormatter(rawLabel) : rawLabel;

  return (
    <div className="rounded-lg border border-border-subtle bg-surface-elevated px-3 py-2.5 shadow-lg text-sm min-w-[10rem]">
      {displayLabel && <p className="mb-2 text-xs font-medium text-muted">{displayLabel}</p>}
      <div className="flex flex-col gap-1.5">
        {payload.map((entry, i) => {
          const key = String(entry.dataKey ?? '');
          const series = config[key];
          const value = entry.value;
          const color = entry.color ?? series?.color ?? 'var(--color-subtle)';

          const displayValue =
            formatter != null && value != null
              ? formatter(value as number | string, key)
              : value != null
                ? typeof value === 'number'
                  ? value.toLocaleString()
                  : String(value)
                : '—';

          return (
            <div key={`${key}-${i}`} className="flex items-center gap-2">
              <span className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: color }} />
              <span className="flex-1 text-muted">{series?.label ?? key}</span>
              <span className="font-mono font-medium text-foreground tabular-nums">
                {displayValue}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ── ChartLegendContent ────────────────────────────────────────────────────────

interface LegendPayloadEntry {
  dataKey?: string | number;
  color?: string;
  value?: string;
}

export interface ChartLegendContentProps {
  payload?: LegendPayloadEntry[];
}

/**
 * ChartLegendContent
 *
 * A styled legend body for recharts' `<Legend content={...}>` prop.
 * Reads series labels from the nearest `<ChartContainer>`'s config.
 *
 * ```tsx
 * <Legend content={<ChartLegendContent />} verticalAlign="bottom" />
 * ```
 */
function ChartLegendContent({ payload }: ChartLegendContentProps) {
  const config = useChartConfig();

  if (!payload?.length) return null;

  return (
    <div className="flex flex-wrap items-center justify-center gap-4 pt-2">
      {payload.map((entry, i) => {
        const key = String(entry.dataKey ?? entry.value ?? '');
        const series = config[key];
        const color = entry.color ?? series?.color ?? 'var(--color-subtle)';
        return (
          <div key={`${key}-${i}`} className="flex items-center gap-1.5 text-xs text-muted">
            <span className="h-2 w-2 shrink-0 rounded-sm" style={{ backgroundColor: color }} />
            <span>{series?.label ?? key}</span>
          </div>
        );
      })}
    </div>
  );
}

// ── Exports ───────────────────────────────────────────────────────────────────

export { ChartContainer, ChartTooltipContent, ChartLegendContent };
