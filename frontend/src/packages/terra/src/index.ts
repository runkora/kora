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
export { Alert } from './components/alert';
export type { AlertProps } from './components/alert';

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

export { Input } from './components/input';
export type { InputProps } from './components/input';

export { Field, FieldHint, FieldError } from './components/field';
export type { FieldProps, FieldHintProps, FieldErrorProps } from './components/field';

export { Anchor, anchorVariants } from './components/anchor';
export type { AnchorProps } from './components/anchor';

export { Label } from './components/label';

export { Separator } from './components/separator';

export { Skeleton } from './components/skeleton';

export { Tooltip, TooltipProvider, TooltipTrigger, TooltipContent } from './components/tooltip';

export { Popover, PopoverTrigger, PopoverAnchor, PopoverContent } from './components/popover';

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

// ─── Molecules ───────────────────────────────────────────────────────────────

export { Textarea } from './components/textarea';
export type { TextareaProps } from './components/textarea';

export { NavItem, navItemVariants } from './components/nav-item';
export type { NavItemProps } from './components/nav-item';

export { Progress, progressVariants } from './components/progress';
export type { ProgressProps } from './components/progress';

export {
  TableRoot,
  Table,
  TableCaption,
  TableHeader,
  TableBody,
  TableFooter,
  TableRow,
  TableHead,
  TableCell,
} from './components/table';

// ─── Layout ──────────────────────────────────────────────────────────────────
export { Stack, Grid } from './components/layout';
export type {
  StackProps,
  StackDirection,
  StackAlign,
  StackJustify,
  GridProps,
  ColCount,
  SpacingSize,
  Responsive,
  ContainerWidth,
  ContainerHeight,
  TextAlign,
} from './components/layout';

// ─── Layout: Section ─────────────────────────────────────────────────────────
export { Section } from './components/section';
export type { SectionProps } from './components/section';

// ─── Molecules ───────────────────────────────────────────────────────────────

export { Tabs, TabsList, TabsTrigger, TabsContent } from './components/tabs';

// ─── Charts ──────────────────────────────────────────────────────────────────

export { ChartContainer, ChartTooltipContent, ChartLegendContent } from './components/chart';
export type {
  ChartConfig,
  ChartContainerProps,
  ChartTooltipContentProps,
  ChartLegendContentProps,
} from './components/chart';

// ─── Organisms ────────────────────────────────────────────────────────────────
export { PageLayout } from './components/page-layout';
export type { PageLayoutProps } from './components/page-layout';
