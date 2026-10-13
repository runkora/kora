// ─── Base table primitives ───────────────────────────────────────────────────
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
  TableEmpty,
  nextSortDir,
} from './table';
export type { SortDir, TableColumnVisibility, TableHeadProps } from './table';

// ─── Table container ─────────────────────────────────────────────────────────
export { TableContainer } from './table-extras';

// ─── Table toolbar ───────────────────────────────────────────────────────────
export { TableToolbar, TableToolbarShell } from './table-toolbar';
export type { TableToolbarShellProps } from './table-toolbar';
export type { TableToolbarProps } from './table-toolbar';

// ─── Table column header ─────────────────────────────────────────────────────
export { TableColumnHeader } from './table-column-header';
export type {
  TableColumnHeaderProps,
  TableColumnFilter,
  TableColumnFilterOption,
} from './table-column-header';
