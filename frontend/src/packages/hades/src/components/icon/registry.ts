/**
 * Hades Icon Registry
 *
 * This is the ONLY file in the entire frontend codebase that may import from
 * 'lucide-react'. All product code must use  instead.
 *
 * To add a new icon:
 *   1. Import the Lucide component here.
 *   2. Add the kebab-case name to the IconName union.
 *   3. Add the mapping to ICON_REGISTRY.
 *
 * All three lists are kept in alphabetical order (the imports by Lucide name,
 * the union and the map by Hades name) so an addition has exactly one correct
 * home and a duplicate is visible on sight. Insert, do not append.
 */
import {
  Activity,
  AlertCircle,
  ArrowDown,
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  ArrowUpDown,
  BarChart3,
  Bell,
  Bot,
  Box,
  Braces,
  Building2,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronUp,
  CircleAlert,
  CircleCheck,
  CircleDot,
  CircleX,
  Clipboard,
  Clock,
  Code2,
  Copy,
  Cpu,
  Database,
  DollarSign,
  Ellipsis,
  ExternalLink,
  Eye,
  EyeOff,
  File,
  FileCheck2,
  FileCode,
  FileText,
  Filter,
  FlaskConical,
  Folder,
  FolderOpen,
  Gauge,
  Globe,
  GripVertical,
  HardDrive,
  Heart,
  Info,
  KeyRound,
  Layers,
  LayoutDashboard,
  LayoutGrid,
  LayoutList,
  Library,
  LineChart,
  List,
  Loader2,
  Lock,
  LogOut,
  Menu,
  Minus,
  Monitor,
  Moon,
  MoreHorizontal,
  MoreVertical,
  Network,
  PanelLeft,
  Pencil,
  Play,
  PlayCircle,
  Plus,
  RefreshCw,
  Rocket,
  RotateCcw,
  Route,
  ScrollText,
  Search,
  SearchX,
  Send,
  Server,
  Settings,
  Settings2,
  Shield,
  ShieldCheck,
  ShieldOff,
  Sparkles,
  SquareTerminal,
  Sun,
  Table,
  Terminal,
  ThumbsDown,
  ThumbsUp,
  Trash2,
  TrendingUp,
  TriangleAlert,
  User,
  UserCheck,
  Users,
  Webhook,
  Wrench,
  X,
  XCircle,
  Zap,
  type LucideIcon,
} from 'lucide-react';

// ── IconName union ────────────────────────────────────────────────────────────

export type IconName =
  | 'activity'
  | 'alert-circle'
  | 'arrow-down'
  | 'arrow-left'
  | 'arrow-right'
  | 'arrow-up'
  | 'arrow-up-down'
  | 'bar-chart'
  | 'bell'
  | 'bot'
  | 'box'
  | 'braces'
  | 'building'
  | 'check'
  | 'check-circle'
  | 'chevron-down'
  | 'chevron-left'
  | 'chevron-right'
  | 'chevron-up'
  | 'circle-alert'
  | 'circle-check'
  | 'circle-dot'
  | 'circle-x'
  | 'clipboard'
  | 'clock'
  | 'close'
  | 'code-2'
  | 'copy'
  | 'cpu'
  | 'database'
  | 'dollar-sign'
  | 'edit'
  | 'ellipsis'
  | 'external-link'
  | 'eye'
  | 'eye-off'
  | 'file'
  | 'file-check'
  | 'file-code'
  | 'file-text'
  | 'filter'
  | 'flask'
  | 'folder'
  | 'folder-open'
  | 'gauge'
  | 'globe'
  | 'grip-vertical'
  | 'hard-drive'
  | 'heart'
  | 'info'
  | 'key'
  | 'layers'
  | 'layout-dashboard'
  | 'layout-grid'
  | 'layout-list'
  | 'library'
  | 'line-chart'
  | 'list'
  | 'loader'
  | 'lock'
  | 'log-out'
  | 'menu'
  | 'minus'
  | 'monitor'
  | 'moon'
  | 'more-horizontal'
  | 'more-vertical'
  | 'network'
  | 'panel-left'
  | 'play'
  | 'play-circle'
  | 'plus'
  | 'refresh'
  | 'refresh-cw'
  | 'rocket'
  | 'route'
  | 'scroll-text'
  | 'search'
  | 'search-x'
  | 'send'
  | 'server'
  | 'settings'
  | 'settings-2'
  | 'shield'
  | 'shield-check'
  | 'shield-off'
  | 'sparkles'
  | 'square-terminal'
  | 'sun'
  | 'table'
  | 'terminal'
  | 'thumbs-down'
  | 'thumbs-up'
  | 'trash'
  | 'trending-up'
  | 'triangle-alert'
  | 'user'
  | 'user-check'
  | 'users'
  | 'webhook'
  | 'wrench'
  | 'x-circle'
  | 'zap';

// ── Registry map ──────────────────────────────────────────────────────────────

export const ICON_REGISTRY: Record<IconName, LucideIcon> = {
  activity: Activity,
  'alert-circle': AlertCircle,
  'arrow-down': ArrowDown,
  'arrow-left': ArrowLeft,
  'arrow-right': ArrowRight,
  'arrow-up': ArrowUp,
  'arrow-up-down': ArrowUpDown,
  'bar-chart': BarChart3,
  bell: Bell,
  bot: Bot,
  box: Box,
  braces: Braces,
  building: Building2,
  check: Check,
  'check-circle': CheckCircle2,
  'chevron-down': ChevronDown,
  'chevron-left': ChevronLeft,
  'chevron-right': ChevronRight,
  'chevron-up': ChevronUp,
  'circle-alert': CircleAlert,
  'circle-check': CircleCheck,
  'circle-dot': CircleDot,
  'circle-x': CircleX,
  clipboard: Clipboard,
  clock: Clock,
  close: X,
  'code-2': Code2,
  copy: Copy,
  cpu: Cpu,
  database: Database,
  'dollar-sign': DollarSign,
  edit: Pencil,
  ellipsis: Ellipsis,
  'external-link': ExternalLink,
  eye: Eye,
  'eye-off': EyeOff,
  file: File,
  'file-check': FileCheck2,
  'file-code': FileCode,
  'file-text': FileText,
  filter: Filter,
  flask: FlaskConical,
  folder: Folder,
  'folder-open': FolderOpen,
  gauge: Gauge,
  globe: Globe,
  'grip-vertical': GripVertical,
  'hard-drive': HardDrive,
  heart: Heart,
  info: Info,
  key: KeyRound,
  layers: Layers,
  'layout-dashboard': LayoutDashboard,
  'layout-grid': LayoutGrid,
  'layout-list': LayoutList,
  library: Library,
  'line-chart': LineChart,
  list: List,
  loader: Loader2,
  lock: Lock,
  'log-out': LogOut,
  menu: Menu,
  minus: Minus,
  monitor: Monitor,
  moon: Moon,
  'more-horizontal': MoreHorizontal,
  'more-vertical': MoreVertical,
  network: Network,
  'panel-left': PanelLeft,
  play: Play,
  'play-circle': PlayCircle,
  plus: Plus,
  refresh: RotateCcw,
  'refresh-cw': RefreshCw,
  rocket: Rocket,
  route: Route,
  'scroll-text': ScrollText,
  search: Search,
  'search-x': SearchX,
  send: Send,
  server: Server,
  settings: Settings,
  'settings-2': Settings2,
  shield: Shield,
  'shield-check': ShieldCheck,
  'shield-off': ShieldOff,
  sparkles: Sparkles,
  'square-terminal': SquareTerminal,
  sun: Sun,
  table: Table,
  terminal: Terminal,
  'thumbs-down': ThumbsDown,
  'thumbs-up': ThumbsUp,
  trash: Trash2,
  'trending-up': TrendingUp,
  'triangle-alert': TriangleAlert,
  user: User,
  'user-check': UserCheck,
  users: Users,
  webhook: Webhook,
  wrench: Wrench,
  'x-circle': XCircle,
  zap: Zap,
};
