import React from 'react';
import { Heading, Text } from '../components/typography/typography';

/**
 * Helpers shared by the Foundation pages. Values come from the browser's
 * computed styles, so they match the active brand and theme.
 */

/** Reads a style now, and again whenever the brand or theme attributes on <html> change. */
export function useLive(read: (el: HTMLElement) => string) {
  const ref = React.useRef<HTMLDivElement>(null);
  const [value, setValue] = React.useState('');
  React.useEffect(() => {
    const run = () => {
      if (ref.current) setValue(read(ref.current));
    };
    run();
    const observer = new MutationObserver(run);
    observer.observe(document.documentElement, { attributes: true });
    return () => observer.disconnect();
  }, [read]);
  return [ref, value] as const;
}

export const readColour = (el: HTMLElement) => getComputedStyle(el).backgroundColor;
export const readRadius = (el: HTMLElement) => getComputedStyle(el).borderTopLeftRadius;
export const readShadow = (el: HTMLElement) => getComputedStyle(el).boxShadow;
export const readWidth = (el: HTMLElement) => `${Math.round(el.getBoundingClientRect().width)}px`;
export const readFontFamily = (el: HTMLElement) => getComputedStyle(el).fontFamily;

interface TileProps {
  label: string;
  /** Renders the preview. The value shown is read from the element that gets the ref. */
  preview: (ref: React.RefObject<HTMLDivElement | null>) => React.ReactNode;
  read: (el: HTMLElement) => string;
  cssVar?: string;
  /** The Tailwind class that uses this token. */
  utility?: string;
  detail?: string;
}

/** One token: the preview on top, then its name, purpose, value and class. */
export function Tile({ label, preview, read, cssVar, utility, detail }: TileProps) {
  const [ref, value] = useLive(read);
  return (
    <div className="flex min-w-0 flex-col overflow-hidden rounded-lg border border-border-subtle">
      {preview(ref)}
      <div className="space-y-2 border-t border-border-subtle p-3">
        <div className="space-y-0.5">
          <Text size="supporting" weight="emphasis">
            {label}
          </Text>
          {detail && (
            <Text size="caption" color="muted">
              {detail}
            </Text>
          )}
        </div>
        <div className="space-y-0.5 font-mono text-[11px] leading-snug text-subtle">
          <div className="break-words text-muted">{value}</div>
          {cssVar && <div className="break-words">{cssVar}</div>}
          {utility && <div className="break-words">{utility}</div>}
        </div>
      </div>
    </div>
  );
}

interface ColourTileProps extends Omit<TileProps, 'preview' | 'read'> {
  cssVar: string;
}

/** A colour token shown as a swatch over the page background, so transparency is visible. */
export function ColourTile({ cssVar, ...rest }: ColourTileProps) {
  return (
    <Tile
      {...rest}
      cssVar={cssVar}
      read={readColour}
      preview={(ref) => (
        <div className="bg-background">
          <div ref={ref} className="h-20 w-full" style={{ background: `var(${cssVar})` }} />
        </div>
      )}
    />
  );
}

interface GroupProps {
  title: string;
  description?: string;
  /** Minimum tile width in px. */
  min?: number;
  children: React.ReactNode;
}

export function Group({ title, description, min = 210, children }: GroupProps) {
  return (
    <section className="space-y-3">
      <div>
        <Heading variant="label" asChild>
          <h2>{title}</h2>
        </Heading>
        {description && (
          <Text size="supporting" color="muted">
            {description}
          </Text>
        )}
      </div>
      <div
        className="grid gap-4"
        style={{ gridTemplateColumns: `repeat(auto-fill, minmax(${min}px, 1fr))` }}
      >
        {children}
      </div>
    </section>
  );
}

export function Page({ children }: { children: React.ReactNode }) {
  return <div className="space-y-10 p-2">{children}</div>;
}
