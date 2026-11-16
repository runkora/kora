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
  GitBranch,
  Globe,
  GripVertical,
  HardDrive,
  Hash,
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
  Pause,
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
  Share2,
  Shield,
  ShieldCheck,
  ShieldOff,
  SlidersHorizontal,
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
  createLucideIcon,
  type LucideIcon,
} from 'lucide-react';

// ── Brand icons ───────────────────────────────────────────────────────────────
// Single-colour marks for the services a run can link to. Paths from Simple
// Icons (CC0-1.0), filled with the current colour like the rest of the set.

const brand = (name: string, d: string) =>
  createLucideIcon(name, [['path', { d, fill: 'currentColor', stroke: 'none', key: name }]]);

const GitHubMark = brand(
  'github',
  'M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12',
);
const PrometheusMark = brand(
  'prometheus',
  'M12 0C5.373 0 0 5.372 0 12c0 6.627 5.373 12 12 12s12-5.373 12-12c0-6.628-5.373-12-12-12zm0 22.46c-1.885 0-3.414-1.26-3.414-2.814h6.828c0 1.553-1.528 2.813-3.414 2.813zm5.64-3.745H6.36v-2.046h11.28v2.046zm-.04-3.098H6.391c-.037-.043-.075-.086-.111-.13-1.155-1.401-1.427-2.133-1.69-2.879-.005-.025 1.4.287 2.395.511 0 0 .513.119 1.262.255-.72-.843-1.147-1.915-1.147-3.01 0-2.406 1.845-4.508 1.18-6.207.648.053 1.34 1.367 1.387 3.422.689-.951.977-2.69.977-3.755 0-1.103.727-2.385 1.454-2.429-.648 1.069.168 1.984.894 4.256.272.854.237 2.29.447 3.201.07-1.892.395-4.652 1.595-5.605-.529 1.2.079 2.702.494 3.424.671 1.164 1.078 2.047 1.078 3.716a4.642 4.642 0 01-1.11 2.996c.792-.149 1.34-.283 1.34-.283l2.573-.502s-.374 1.538-1.81 3.019z',
);

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
  | 'git-branch'
  | 'github'
  | 'globe'
  | 'grip-vertical'
  | 'hard-drive'
  | 'hash'
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
  | 'pause'
  | 'play'
  | 'play-circle'
  | 'plus'
  | 'prometheus'
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
  | 'share-2'
  | 'shield'
  | 'shield-check'
  | 'shield-off'
  | 'sliders-horizontal'
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
  'git-branch': GitBranch,
  github: GitHubMark,
  globe: Globe,
  'grip-vertical': GripVertical,
  'hard-drive': HardDrive,
  hash: Hash,
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
  pause: Pause,
  play: Play,
  'play-circle': PlayCircle,
  plus: Plus,
  prometheus: PrometheusMark,
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
  'share-2': Share2,
  shield: Shield,
  'shield-check': ShieldCheck,
  'shield-off': ShieldOff,
  'sliders-horizontal': SlidersHorizontal,
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
