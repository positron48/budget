import {
  Home,
  FolderOpen,
  DollarSign,
  BarChart3,
  RefreshCw,
  Building2,
  User,
  LogOut,

  TrendingUp,
  TrendingDown,
  Wallet,
  Target,
  PieChart,
  Menu,
  X,
  Mail,
  Lock,
  Eye,
  EyeOff,
  Search,
  Filter,
  Calendar,
  Edit,
  Trash2,
  Settings,
  ChevronDown,
  ChevronRight,
  Check,
  AlertCircle,
  Info,
  CreditCard,
  Receipt,
  PiggyBank,
  Coins,
  Calculator,
  FileText,
  Download,
  Upload,
  Globe,
  Bell,
  Star,
  Heart,
  Share2,
  Copy,
  ExternalLink,
  ArrowRight,
  ArrowLeft,
  ChevronLeft,
  ChevronUp,
  MoreHorizontal,
  Grid,
  List,
  Clock,
  Tag,
  Hash,
  Percent,
  Minus,
  Divide,
  Plus as PlusIcon,
  Equal,
  LogIn,
  Loader2,
  type LucideIcon,
} from "lucide-react";

export type IconName = 
  | "home"
  | "categories"
  | "transactions"
  | "reports"
  | "fx"
  | "tenants"
  | "profile"
  | "logout"
  | "plus"
  | "trending-up"
  | "trending-down"
  | "wallet"
  | "target"
  | "pie-chart"
  | "menu"
  | "close"
  | "mail"
  | "lock"
  | "eye"
  | "eye-off"
  | "search"
  | "filter"
  | "calendar"
  | "edit"
  | "trash"
  | "settings"
  | "chevron-down"
  | "chevron-right"
  | "check"
  | "alert-circle"
  | "info"
  | "credit-card"
  | "receipt"
  | "piggy-bank"
  | "coins"
  | "calculator"
  | "file-text"
  | "download"
  | "upload"
  | "globe"
  | "bell"
  | "star"
  | "heart"
  | "share"
  | "copy"
  | "external-link"
  | "arrow-right"
  | "arrow-left"
  | "chevron-left"
  | "chevron-up"
  | "more-horizontal"
  | "grid"
  | "list"
  | "clock"
  | "tag"
  | "hash"
  | "percent"
  | "minus"
  | "divide"
  | "equal"
  | "log-in"
  | "loader-2"
  | "folder-open"
  | "user";

const iconMap: Record<IconName, LucideIcon> = {
  home: Home,
  categories: FolderOpen,
  transactions: DollarSign,
  reports: BarChart3,
  fx: RefreshCw,
  tenants: Building2,
  profile: User,
  logout: LogOut,
  plus: PlusIcon,
  "trending-up": TrendingUp,
  "trending-down": TrendingDown,
  wallet: Wallet,
  target: Target,
  "pie-chart": PieChart,
  menu: Menu,
  close: X,
  mail: Mail,
  lock: Lock,
  eye: Eye,
  "eye-off": EyeOff,
  search: Search,
  filter: Filter,
  calendar: Calendar,
  edit: Edit,
  trash: Trash2,
  settings: Settings,
  "chevron-down": ChevronDown,
  "chevron-right": ChevronRight,
  check: Check,
  "alert-circle": AlertCircle,
  info: Info,
  "credit-card": CreditCard,
  receipt: Receipt,
  "piggy-bank": PiggyBank,
  coins: Coins,
  calculator: Calculator,
  "file-text": FileText,
  download: Download,
  upload: Upload,
  globe: Globe,
  bell: Bell,
  star: Star,
  heart: Heart,
  share: Share2,
  copy: Copy,
  "external-link": ExternalLink,
  "arrow-right": ArrowRight,
  "arrow-left": ArrowLeft,
  "chevron-left": ChevronLeft,
  "chevron-up": ChevronUp,
  "more-horizontal": MoreHorizontal,
  grid: Grid,
  list: List,
  clock: Clock,
  tag: Tag,
  hash: Hash,
  percent: Percent,
  minus: Minus,
  divide: Divide,
  equal: Equal,
  "log-in": LogIn,
  "loader-2": Loader2,
  "folder-open": FolderOpen,
  user: User,
};

interface IconProps {
  name: IconName;
  size?: number;
  className?: string;
}

export default function Icon({ name, size = 20, className = "" }: IconProps) {
  const IconComponent = iconMap[name];
  
  if (!IconComponent) {
    console.warn(`Icon "${name}" not found`);
    return null;
  }

  return <IconComponent size={size} className={className} />;
}
