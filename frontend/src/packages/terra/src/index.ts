'use client';

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
export { Callout } from './components/callout';
export type { CalloutProps } from './components/callout';

export { Icon } from './components/icon';
export type {
  IconProps,
  IconName,
  IconSize,
  IconContainerVariant,
  IconColor,
} from './components/icon';

export { Button, buttonVariants } from './components/button';
export type { ButtonProps } from './components/button';

export { ButtonGroup } from './components/button-group';
export type { ButtonGroupProps } from './components/button-group';

export { Checkbox } from './components/checkbox';
export type { CheckboxProps } from './components/checkbox';

export { Badge, badgeVariants } from './components/badge';
export type { BadgeProps } from './components/badge';

export { StatusDot, statusDotVariants } from './components/status-dot';
export type { StatusDotProps } from './components/status-dot';

export { StepDot, StepConnector } from './components/step-dot';
export type { StepDotProps, StepConnectorProps } from './components/step-dot';

export { Eyebrow } from './components/eyebrow';
export type { EyebrowProps } from './components/eyebrow';

export { SectionHeading } from './components/section-heading';
export type { SectionHeadingProps } from './components/section-heading';

export { RadioGroup, RadioGroupItem } from './components/radio-group';
export type { RadioGroupProps, RadioGroupItemProps } from './components/radio-group';

export { RadioCard } from './components/radio-card';
export type { RadioCardProps } from './components/radio-card';

export { Collapsible, CollapsibleTrigger, CollapsibleContent } from './components/collapsible';
export type {
  CollapsibleProps,
  CollapsibleTriggerProps,
  CollapsibleContentProps,
} from './components/collapsible';

export { Indicator } from './components/indicator';
export type { IndicatorProps } from './components/indicator';

export { OrbitalLoader } from './components/orbital-loader';
export type { OrbitalLoaderProps } from './components/orbital-loader';

export { InputSurface, TypingIndicator, StreamingCursor } from './components/chat';

export { DragCard } from './components/drag-card';
export type { DragCardProps } from './components/drag-card';

export {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from './components/card';
export type {
  CardProps,
  CardHeaderProps,
  CardTitleProps,
  CardDescriptionProps,
  CardContentProps,
  CardFooterProps,
} from './components/card';

export { Input } from './components/input';
export type { InputProps } from './components/input';

export { NativeSelect } from './components/native-select';
export type { NativeSelectProps } from './components/native-select';

export { Toggle } from './components/toggle';
export type { ToggleProps } from './components/toggle';

export { ToggleRow } from './components/toggle-row';
export type { ToggleRowProps } from './components/toggle-row';

export { Field, FieldHint, FieldError } from './components/field';
export type { FieldProps, FieldHintProps, FieldErrorProps } from './components/field';

export { Form } from './components/form';
export type { FormProps } from './components/form';

export { Anchor, anchorVariants } from './components/anchor';
export type { AnchorProps } from './components/anchor';

export { Breadcrumbs } from './components/breadcrumbs';
export type { BreadcrumbsProps, BreadcrumbsItem } from './components/breadcrumbs';

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

export { CodeBlock } from './components/code-block';
export type { CodeBlockProps } from './components/code-block';

// ─── Molecules ───────────────────────────────────────────────────────────────

export { Textarea } from './components/textarea';
export type { TextareaProps } from './components/textarea';

export { NavItem, navItemVariants } from './components/nav-item';
export type { NavItemProps } from './components/nav-item';

export { SectionNav } from './components/section-nav';
export type { SectionNavProps, SectionNavSection } from './components/section-nav';

export { SaveBar } from './components/save-bar';
export type { SaveBarProps } from './components/save-bar';

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
  // Table system additions
  TableContainer,
  TableEmpty,
  nextSortDir,
  // Table toolbar
  TableToolbar,
  TableToolbarShell,
  // Table column header
  TableColumnHeader,
  // Data table (TanStack-backed, fully opinionated)
  DataTable,
} from './components/table';
export type {
  SortDir,
  TableColumnVisibility,
  TableHeadProps,
  TableToolbarProps,
  TableToolbarShellProps,
  TableColumnHeaderProps,
  TableColumnFilter,
  TableColumnFilterOption,
  DataTableProps,
  DataTableColumn,
  DataTableColumnFilter,
  DataTableClientFilter,
  DataTableServerFilter,
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

export { List, ListItem } from './components/list';
export type { ListProps, ListItemProps } from './components/list';

// ─── Layout: Section ─────────────────────────────────────────────────────────
export { Section } from './components/section';
export type { SectionProps } from './components/section';

// ─── Layout: PageSectionHeader ────────────────────────────────────────────────
export { PageSectionHeader } from './components/page-section-header';
export type { PageSectionHeaderProps } from './components/page-section-header';

// ─── Layout: Navigation ──────────────────────────────────────────────────────
export { Navigation } from './components/navigation';
export type { NavigationProps } from './components/navigation';

// ─── Layout: Responsive visibility ───────────────────────────────────────────
export { Show } from './components/show';
export type { ShowProps, Breakpoint } from './components/show';

// ─── Molecules ───────────────────────────────────────────────────────────────

export { Tabs, TabsList, TabsTrigger, TabsContent } from './components/tabs';
export type { TabsTriggerProps } from './components/tabs';

// ─── Charts ──────────────────────────────────────────────────────────────────

export { ChartContainer, ChartTooltipContent, ChartLegendContent } from './components/chart';
export type {
  ChartConfig,
  ChartContainerProps,
  ChartTooltipContentProps,
  ChartLegendContentProps,
} from './components/chart';

// ─── Chat primitives ─────────────────────────────────────────────────────────
export { ChatBubble, ChatBubbleActions } from './components/chat';
export type {
  ChatBubbleProps,
  ChatBubbleActionsProps,
  ChatBubbleDirection,
  ChatBubbleVariant,
} from './components/chat';

// ─── Molecules: SidePanel ────────────────────────────────────────────────────
export {
  SidePanel,
  SidePanelHeader,
  SidePanelBody,
  SidePanelFooter,
  SidePanelAccessibleTitle,
} from './components/side-panel';
export type {
  SidePanelProps,
  SidePanelHeaderProps,
  SidePanelBodyProps,
  SidePanelFooterProps,
} from './components/side-panel';

// ─── Molecules: Modal ────────────────────────────────────────────────────────
export {
  Modal,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalAccessibleTitle,
} from './components/modal';
export type {
  ModalProps,
  ModalHeaderProps,
  ModalBodyProps,
  ModalFooterProps,
} from './components/modal';

// ─── Molecules: FormField ─────────────────────────────────────────────────────
export { FormField, useFormField } from './components/form-field';
export type { FormFieldProps, FormFieldContextValue } from './components/form-field';

// ─── Molecules: Output ───────────────────────────────────────────────────────
export { Output } from './components/output';
export type { OutputProps } from './components/output';

// ─── Hooks ────────────────────────────────────────────────────────────────────
export { useDialog } from './hooks/use-dialog';

// ─── Organisms ────────────────────────────────────────────────────────────────
export { PageLayout } from './components/page-layout';
export type { PageLayoutProps } from './components/page-layout';

export { Sparkline, sparklineBarVariants } from './components/sparkline';
export type { SparklineProps } from './components/sparkline';

export { StatCard } from './components/stat-card';
export type { StatCardProps } from './components/stat-card';

export { SegmentedControl } from './components/segmented-control';
export type { SegmentedControlProps, SegmentedControlOption } from './components/segmented-control';

export { IconTile, iconTileVariants } from './components/icon-tile';
export type { IconTileProps } from './components/icon-tile';
