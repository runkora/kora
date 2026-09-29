import React from 'react';
import { Heading, Text } from '../components/typography/typography';

/** Helpers for component stories: labelled placeholder blocks and option tables. */

/** A labelled placeholder used to show layout without real content. */
export function Block({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-center rounded-md border border-border-subtle bg-surface-elevated px-3 py-2">
      <Text as="span" size="caption" color="muted">
        {children}
      </Text>
    </div>
  );
}

/** A dashed outline that shows where a container ends. */
export function Frame({
  children,
  className = '',
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={`rounded-md border border-dashed border-border p-3 ${className}`}>
      {children}
    </div>
  );
}

const COLUMNS = 'grid grid-cols-[210px_1fr_230px] items-start gap-6';

/** One row of an option table: the option, a live sample, and when to use it. */
export function Option({
  name,
  use,
  children,
}: {
  name: string;
  use: string;
  children: React.ReactNode;
}) {
  return (
    <div className={`${COLUMNS} border-b border-border-subtle py-4 last:border-0`}>
      <Text size="supporting" mono color="muted" className="break-words">
        {name}
      </Text>
      <div className="min-w-0">{children}</div>
      <Text size="supporting" color="muted">
        {use}
      </Text>
    </div>
  );
}

export function OptionTable({
  title,
  note,
  children,
}: {
  title: string;
  note?: string;
  children: React.ReactNode;
}) {
  return (
    <section className="space-y-2">
      <div>
        <Heading variant="label" asChild>
          <h2>{title}</h2>
        </Heading>
        {note && (
          <Text size="supporting" color="muted">
            {note}
          </Text>
        )}
      </div>
      <div>
        <div className={`${COLUMNS} border-b border-border py-2`}>
          {['Option', 'Sample', 'Use'].map((heading) => (
            <Text key={heading} size="caption" color="subtle">
              {heading}
            </Text>
          ))}
        </div>
        {children}
      </div>
    </section>
  );
}

/** A titled example on a story page. */
export function Example({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-3">
      <Heading variant="label" asChild>
        <h2>{title}</h2>
      </Heading>
      {children}
    </section>
  );
}
