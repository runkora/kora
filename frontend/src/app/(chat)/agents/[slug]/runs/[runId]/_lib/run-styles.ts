/** Styles the run page's panes share, where no Hades prop covers them. */
import type { CSSProperties } from 'react';

/** A pane divider. */
export const LINE = '1px solid var(--color-border-subtle)';
/** A hairline inside a tool block, fainter than the pane dividers. */
export const HAIR = '1px solid color-mix(in oklch, var(--color-foreground) 6%, transparent)';
/** A short label that keeps to one line beside text that truncates. */
export const NOWRAP: CSSProperties = { whiteSpace: 'nowrap', flexShrink: 0 };
/** The thread's reading column. */
export const READING_WIDTH = 720;

/** A branch source's name without the "branch " its label leads with. */
export const branchName = (label: string) => label.replace(/^branch /, '');
