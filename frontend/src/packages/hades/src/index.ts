'use client';

/**
 * @runkora/hades
 *
 * kora is another name for Persephone, who lives half the year in Olympus and
 * half in the underworld. Hades is the underworld half of kora: the tokens and
 * primitives beneath every screen.
 *
 * Import the token stylesheet separately in your app's global CSS:
 *   @import "@runkora/hades/src/tokens/tokens.css";
 *
 * The banners below are load-bearing: an atom is a single element with no
 * internal composition, a molecule composes atoms into one reusable unit, an
 * organism is a whole surface. Add a new export under the banner it belongs to
 * rather than at the end of the file, or the grouping stops meaning anything.
 */

// ─── Utilities ───────────────────────────────────────────────────────────────
export { cn } from './utils';

// ─── Atoms ───────────────────────────────────────────────────────────────────

export { Alert } from './components/alert';
export type { AlertProps } from './components/alert';

export { Callout } from './components/callout';
export type { CalloutProps } from './components/callout';

export { Icon } from './components/icon';
export { BrandLogo } from './components/brand-logo';
export type { BrandLogoProps } from './components/brand-logo';
export type {
  IconProps,
  IconName,
  IconSize,
  IconContainerVariant,
  IconColor,
} from './components/icon';

export { IconTile, iconTileVariants } from './components/icon-tile';
export type { IconTileProps } from './components/icon-tile';

export { Button, buttonVariants } from './components/button';
export type { ButtonProps } from './components/button';

export { ButtonGroup } from './components/button-group';
export type { ButtonGroupProps } from './components/button-group';

export { Checkbox } from './components/checkbox';
export type { CheckboxProps } from './components/checkbox';

export { Toggle } from './components/toggle';
export type { ToggleProps } from './components/toggle';

export { Badge, badgeVariants } from './components/badge';
export type { BadgeProps } from './components/badge';

export { StatusDot, statusDotVariants } from './components/status-dot';
export type { StatusDotProps } from './components/status-dot';

export { StepDot, StepConnector } from './components/step-dot';
export type { StepDotProps, StepConnectorProps } from './components/step-dot';

export { Indicator } from './components/indicator';
export type { IndicatorProps } from './components/indicator';

export { Eyebrow } from './components/eyebrow';
export type { EyebrowProps } from './components/eyebrow';

export { SectionLabel, sectionLabelVariants } from './components/section-label';
export type { SectionLabelProps } from './components/section-label';

export { SectionHeading } from './components/section-heading';
export type { SectionHeadingProps } from './components/section-heading';

export { RadioGroup, RadioGroupItem } from './components/radio-group';
export type { RadioGroupProps, RadioGroupItemProps } from './components/radio-group';

export { Input } from './components/input';
export type { InputProps } from './components/input';

export { Textarea } from './components/textarea';
export type { TextareaProps } from './components/textarea';

export { InlineToken, inlineTokenVariants } from './components/inline-token';
export type { InlineTokenProps } from './components/inline-token';

export { InlineSelect } from './components/inline-select';
export type { InlineSelectProps, InlineSelectOption } from './components/inline-select';

export { NativeSelect } from './components/native-select';
export type { NativeSelectProps } from './components/native-select';

export { Anchor, anchorVariants } from './components/anchor';
export type { AnchorProps } from './components/anchor';

export { Label } from './components/label';

export { Separator } from './components/separator';

export { Skeleton } from './components/skeleton';

export { OrbitalLoader } from './components/orbital-loader';
export type { OrbitalLoaderProps } from './components/orbital-loader';

export { Donut, donutSegmentColor } from './components/donut';
export type { DonutProps, DonutSegment } from './components/donut';
export { Sparkline, sparklineBarVariants } from './components/sparkline';
export type { SparklineProps } from './components/sparkline';

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

export { TruncatingBadge } from './components/truncating-badge';
export type { TruncatingBadgeProps } from './components/truncating-badge';

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

export { RadioCard } from './components/radio-card';
export type { RadioCardProps } from './components/radio-card';

export { DragCard } from './components/drag-card';
export type { DragCardProps } from './components/drag-card';

export { Field, FieldHint, FieldError } from './components/field';
export type { FieldProps, FieldHintProps, FieldErrorProps } from './components/field';

export { Form } from './components/form';
export type { FormProps } from './components/form';

export { FormField, useFormField } from './components/form-field';
export type { FormFieldProps, FormFieldContextValue } from './components/form-field';

export { FieldRow } from './components/field-row';
export type { FieldRowProps } from './components/field-row';

export { SettingRow, settingRowVariants } from './components/setting-row';
export type { SettingRowProps } from './components/setting-row';

export { ToggleRow, toggleRowVariants } from './components/toggle-row';
export type {
  ToggleRowProps,
  ToggleRowSwitchProps,
  ToggleRowControlProps,
} from './components/toggle-row';

export { DangerZone, dangerZoneVariants } from './components/danger-zone';
export type { DangerZoneProps } from './components/danger-zone';

export { StatCard } from './components/stat-card';
export type { StatCardProps, StatCardDelta, StatCardBreakdownItem } from './components/stat-card';

export { CopyButton } from './components/copy-button';
export type { CopyButtonProps } from './components/copy-button';
export { CopyField } from './components/copy-field';
export type { CopyFieldProps } from './components/copy-field';
export { SegmentedControl } from './components/segmented-control';
export type { SegmentedControlProps, SegmentedControlOption } from './components/segmented-control';

export { Collapsible, CollapsibleTrigger, CollapsibleContent } from './components/collapsible';
export type {
  CollapsibleProps,
  CollapsibleTriggerProps,
  CollapsibleContentProps,
} from './components/collapsible';

export { Breadcrumbs } from './components/breadcrumbs';
export type { BreadcrumbsProps, BreadcrumbsItem } from './components/breadcrumbs';

export { Tooltip, TooltipProvider, TooltipTrigger, TooltipContent } from './components/tooltip';

export { Popover, PopoverTrigger, PopoverAnchor, PopoverContent } from './components/popover';
export type { PopoverContentProps } from './components/popover';

export { Tabs, TabsList, TabsTrigger, TabsContent } from './components/tabs';
export type { TabsTriggerProps } from './components/tabs';

export { NavItem, navItemVariants } from './components/nav-item';
export type { NavItemProps } from './components/nav-item';

export { SectionNav } from './components/section-nav';
export type { SectionNavProps, SectionNavSection } from './components/section-nav';

export { SaveBar } from './components/save-bar';
export type { SaveBarProps } from './components/save-bar';

export { Progress, progressVariants } from './components/progress';
export type { ProgressProps } from './components/progress';

export { Output } from './components/output';
export type { OutputProps } from './components/output';

export { TemplateField, TemplateCatalogList } from './components/template-field';
export type {
  TemplateFieldProps,
  TemplateCatalogEntry,
  TemplateCatalogListProps,
} from './components/template-field';
/**
 * The minimal template-model surface: parse a template, write one back, agree
 * with the token grammar, and render a preview (`exampleMap` builds the map
 * `renderPreview` consumes, so the pair travels together). Everything else the
 * editor uses — caret snapping, DOM bridging, catalog grouping — stays
 * folder-local. Those are implementation, and withdrawing a public export later
 * is far harder than adding one now.
 */
export {
  parseTemplate,
  serialize,
  renderPreview,
  exampleMap,
  TOKEN_PATTERN_SOURCE,
} from './components/template-field';
export type {
  TemplatePart,
  TemplateTextPart,
  TemplateTokenPart,
  TemplateCatalogGroup,
} from './components/template-field';

// ─── Molecules: Chat ─────────────────────────────────────────────────────────

export { InputSurface, TypingIndicator, StreamingCursor } from './components/chat';

export { ChatBubble, ChatBubbleActions } from './components/chat';
export type {
  ChatBubbleProps,
  ChatBubbleActionsProps,
  ChatBubbleDirection,
  ChatBubbleVariant,
} from './components/chat';

// ─── Layout ──────────────────────────────────────────────────────────────────

export {
  Stack,
  Grid,
  SidebarLayout,
  SidebarLayoutMain,
  SidebarLayoutSidebar,
} from './components/layout';
export type {
  StackProps,
  StackDirection,
  StackAlign,
  StackJustify,
  GridProps,
  ColCount,
  SidebarLayoutProps,
  SidebarLayoutMainProps,
  SidebarLayoutSidebarProps,
  SidebarWidth,
  SpacingSize,
  Responsive,
  ContainerWidth,
  ContainerHeight,
  TextAlign,
  SidebarWidthToken,
} from './components/layout';

export { List, ListItem } from './components/list';
export type { ListProps, ListItemProps } from './components/list';

export { Section } from './components/section';
export type { SectionProps } from './components/section';

export { PageSectionHeader } from './components/page-section-header';
export type { PageSectionHeaderProps } from './components/page-section-header';

export { Navigation } from './components/navigation';
export type { NavigationProps } from './components/navigation';

/** Responsive visibility. */
export { Show } from './components/show';
export type { ShowProps, Breakpoint } from './components/show';

// ─── Charts ──────────────────────────────────────────────────────────────────

export { ChartContainer, ChartTooltipContent, ChartLegendContent } from './components/chart';
export type {
  ChartConfig,
  ChartContainerProps,
  ChartTooltipContentProps,
  ChartLegendContentProps,
} from './components/chart';

// ─── Organisms ───────────────────────────────────────────────────────────────

export { Wizard } from './components/wizard';
export type {
  WizardProps,
  WizardRailProps,
  WizardRailStep,
  WizardStepProps,
  WizardFooterProps,
  WizardReviewCardProps,
} from './components/wizard';

export { PageLayout } from './components/page-layout';
export type { PageLayoutProps } from './components/page-layout';

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

export { BottomSheet } from './components/bottom-sheet';
export type { BottomSheetProps } from './components/bottom-sheet';

export { EmptyState } from './components/empty-state';
export type { EmptyStateProps } from './components/empty-state';

export { ConfirmDialog } from './components/confirm-dialog';
export type { ConfirmDialogProps } from './components/confirm-dialog';

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
} from './components/table';

// DataTable — CSS-grid list surface with sort, filter, column visibility, and pagination.
export { DataTable } from './components/data-table';
export type {
  DataTableProps,
  DataTableColumn,
  DataTableColumnFilter,
  DataTableClientFilter,
  DataTableServerFilter,
  DataTableFilterOption,
} from './components/data-table';

// ─── Hooks ───────────────────────────────────────────────────────────────────

export { useDialog } from './hooks/use-dialog';
export { useCopy } from './hooks/use-copy';
export type { CopyStatus } from './hooks/use-copy';
