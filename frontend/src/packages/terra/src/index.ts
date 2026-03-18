/**
 * @orbit/terra
 *
 * Terra is the destination of every orbital descent. 'Terra firma' means solid
 * ground. The design system is the solid, stable visual foundation that
 * everything else lands on.
 *
 * Import the token stylesheet separately in your app's global CSS:
 *   @import "@orbit/terra/src/tokens/tokens.css";
 */

// ─── Utilities ───────────────────────────────────────────────────────────────
export { cn } from './utils';

// ─── Atoms ───────────────────────────────────────────────────────────────────
export { Button, buttonVariants } from './components/button';
export type { ButtonProps } from './components/button';

export { Badge, badgeVariants } from './components/badge';
export type { BadgeProps } from './components/badge';

export {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from './components/card';

export { Anchor, anchorVariants } from './components/anchor';
export type { AnchorProps } from './components/anchor';

export { Tooltip, TooltipProvider, TooltipTrigger, TooltipContent } from './components/tooltip';

// ─── Typography ──────────────────────────────────────────────────────────────
export {
  Heading,
  headingVariants,
  Text,
  textVariants,
  Code,
  codeVariants,
} from './components/typography';
export type { HeadingProps, TextProps, CodeProps } from './components/typography';

// ─── Organisms ────────────────────────────────────────────────────────────────
export { PageLayout, Section } from './components/page-layout';
export type { PageLayoutProps, SectionProps } from './components/page-layout';
