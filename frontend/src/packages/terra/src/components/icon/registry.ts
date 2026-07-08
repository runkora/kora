/**
 * Terra Icon Registry
 *
 * This is the ONLY file in the entire frontend codebase that may import from
 * 'lucide-react'. All product code must use  instead.
 *
 * To add a new icon:
 *   1. Import the Lucide component here.
 *   2. Add the kebab-case name to the IconName union.
 *   3. Add the mapping to ICON_REGISTRY.
 */
import {
  Activity,
  Cpu,
  AlertCircle,
  ArrowDown,
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  ArrowUpDown,
  BarChart3,
  Bell,
  Bot,
  Check,
  CheckCircle2,
  ChevronDown,
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
  Database,
  DollarSign,
  Ellipsis,
  ExternalLink,
  Eye,
  EyeOff,
  FileCheck2,
  FileText,
  Filter,
  FlaskConical,
  GripVertical,
  Heart,
  Info,
  KeyRound,
  Play,
  PlayCircle,
  SearchX,
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
  MoreHorizontal,
  MoreVertical,
  Network,
  PanelLeft,
  Pencil,
  Plus,
  RefreshCw,
  Rocket,
  RotateCcw,
  Route,
  ScrollText,
  Search,
  Send,
  Settings,
  Shield,
  ShieldCheck,
  ShieldOff,
  Sparkles,
  SquareTerminal,
  Terminal,
  HardDrive,
  Server,
  ThumbsDown,
  ThumbsUp,
  TrendingUp,
  Trash2,
  TriangleAlert,
  User,
  UserCheck,
  Users,
  Wrench,
  X,
  XCircle,
  Zap,
  Globe,
  Monitor,
  Braces,
  Table,
  ChevronLeft,
  Building2,
  Box,
  type LucideIcon,
} from 'lucide-react';

// ── IconName union ────────────────────────────────────────────────────────────

export type IconName =
  | 'activity'
  | 'globe'
  | 'monitor'
  | 'braces'
  | 'table'
  | 'chevron-left'
  | 'building'
  | 'box'
  | 'alert-circle'
  | 'arrow-down'
  | 'arrow-left'
  | 'arrow-right'
  | 'arrow-up'
  | 'arrow-up-down'
  | 'bar-chart'
  | 'bell'
  | 'bot'
  | 'check'
  | 'check-circle'
  | 'chevron-down'
  | 'chevron-right'
  | 'chevron-up'
  | 'circle-alert'
  | 'circle-check'
  | 'cpu'
  | 'circle-dot'
  | 'circle-x'
  | 'clipboard'
  | 'clock'
  | 'close'
  | 'code-2'
  | 'copy'
  | 'database'
  | 'dollar-sign'
  | 'edit'
  | 'ellipsis'
  | 'external-link'
  | 'eye'
  | 'eye-off'
  | 'file-check'
  | 'file-text'
  | 'filter'
  | 'flask'
  | 'grip-vertical'
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
  | 'settings'
  | 'shield'
  | 'shield-check'
  | 'shield-off'
  | 'sparkles'
  | 'square-terminal'
  | 'terminal'
  | 'hard-drive'
  | 'server'
  | 'thumbs-down'
  | 'thumbs-up'
  | 'trending-up'
  | 'trash'
  | 'triangle-alert'
  | 'user'
  | 'user-check'
  | 'users'
  | 'wrench'
  | 'x-circle'
  | 'zap';

// ── Registry map ──────────────────────────────────────────────────────────────

export const ICON_REGISTRY: Record<IconName, LucideIcon> = {
  activity: Activity,
  globe: Globe,
  monitor: Monitor,
  braces: Braces,
  table: Table,
  'chevron-left': ChevronLeft,
  building: Building2,
  box: Box,
  'alert-circle': AlertCircle,
  'arrow-down': ArrowDown,
  'arrow-left': ArrowLeft,
  'arrow-right': ArrowRight,
  'arrow-up': ArrowUp,
  'arrow-up-down': ArrowUpDown,
  'bar-chart': BarChart3,
  bell: Bell,
  bot: Bot,
  check: Check,
  'check-circle': CheckCircle2,
  'chevron-down': ChevronDown,
  'chevron-right': ChevronRight,
  'chevron-up': ChevronUp,
  'circle-alert': CircleAlert,
  cpu: Cpu,
  'circle-check': CircleCheck,
  'circle-dot': CircleDot,
  'circle-x': CircleX,
  clipboard: Clipboard,
  clock: Clock,
  close: X,
  'code-2': Code2,
  copy: Copy,
  database: Database,
  'dollar-sign': DollarSign,
  edit: Pencil,
  ellipsis: Ellipsis,
  'external-link': ExternalLink,
  eye: Eye,
  'eye-off': EyeOff,
  'file-check': FileCheck2,
  'file-text': FileText,
  filter: Filter,
  flask: FlaskConical,
  'grip-vertical': GripVertical,
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
  settings: Settings,
  shield: Shield,
  'shield-check': ShieldCheck,
  'shield-off': ShieldOff,
  sparkles: Sparkles,
  'square-terminal': SquareTerminal,
  terminal: Terminal,
  'hard-drive': HardDrive,
  server: Server,
  'thumbs-down': ThumbsDown,
  'thumbs-up': ThumbsUp,
  'trending-up': TrendingUp,
  trash: Trash2,
  'triangle-alert': TriangleAlert,
  user: User,
  'user-check': UserCheck,
  users: Users,
  wrench: Wrench,
  'x-circle': XCircle,
  zap: Zap,
};
