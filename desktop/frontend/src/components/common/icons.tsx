import React from 'react';
import { HugeiconsIcon, type HugeiconsProps, type IconSvgElement } from '@hugeicons/react';
import {
  Activity01Icon,
  Add01Icon,
  AiBrain01Icon,
  AiMagicIcon,
  Alert02Icon,
  AlertCircleIcon,
  Archive01Icon,
  ArchiveRestoreIcon,
  ArrowDown01Icon,
  ArrowDown02Icon,
  ArrowHorizontalIcon,
  ArrowLeft01Icon,
  ArrowLeft02Icon,
  ArrowRight01Icon,
  ArrowRight02Icon,
  ArrowUp01Icon,
  ArrowUp02Icon,
  ArrowUpRight02Icon,
  Attachment01Icon,
  BlockedIcon,
  Cancel01Icon,
  CheckListIcon,
  CheckmarkCircle02Icon,
  Chat01Icon,
  Clock01Icon,
  CircleArrowRight02Icon,
  CircleIcon,
  Coffee01Icon,
  CommandLineIcon,
  ComputerIcon,
  ComputerRemoveIcon,
  ContrastIcon,
  Copy01Icon,
  CpuIcon,
  CubeIcon,
  DashboardSpeed01Icon,
  DashboardSquare01Icon,
  Delete02Icon,
  DragDropVerticalIcon,
  Edit02Icon,
  FileEditIcon,
  File01Icon,
  File02Icon,
  FlashIcon,
  Folder01Icon,
  MinimizeScreenIcon,
  FullScreenIcon,
  FolderAddIcon,
  FolderGitIcon,
  FolderOpenIcon,
  FolderRemoveIcon,
  GitBranchIcon,
  GitCommitIcon,
  GitCompareIcon,
  GitForkIcon,
  Globe02Icon,
  HardDriveIcon,
  HelpCircleIcon,
  Home01Icon,
  HourglassIcon,
  Image01Icon,
  Layers01Icon,
  Leaf01Icon,
  Link01Icon,
  LinkSquare02Icon,
  Loading03Icon,
  LockIcon,
  Menu01Icon,
  MessageAdd01Icon,
  MessageAdd02Icon,
  MessageMultiple01Icon,
  MessageQuestionIcon,
  Mic01Icon,
  MinusSignIcon,
  Moon02Icon,
  Note01Icon,
  Notification01Icon,
  Notification03Icon,
  PaintBoardIcon,
  Pdf01Icon,
  PlayIcon,
  PlugSocketIcon,
  QrCodeIcon,
  RadioButtonIcon,
  RefreshIcon,
  RotateLeft01Icon,
  RotateRight01Icon,
  RoboticIcon,
  Search01Icon,
  SecurityCheckIcon,
  SentIcon,
  Settings01Icon,
  Shield01Icon,
  ShutDownIcon,
  SlidersHorizontalIcon,
  SmartPhone01Icon,
  SourceCodeSquareIcon,
  SquareIcon,
  Structure01Icon,
  Sun03Icon,
  SunriseIcon,
  Task01Icon,
  TestTube01Icon,
  Tick02Icon,
  TickDouble02Icon,
  Time04Icon,
  UnfoldLessIcon,
  UnfoldMoreIcon,
  Upload01Icon,
  ViewIcon,
  ViewOffIcon,
  Wifi01Icon,
  WifiDisconnected01Icon,
  Wrench01Icon,
  ZoomInAreaIcon,
} from '@hugeicons/core-free-icons';

export type IconProps = Omit<HugeiconsProps, 'icon' | 'ref'>;
export type IconComponent = React.ComponentType<IconProps>;

const make = (icon: IconSvgElement): IconComponent => {
  const Component: IconComponent = (props) => <HugeiconsIcon icon={icon} {...props} />;
  return React.memo(Component) as unknown as IconComponent;
};

export const Activity = make(Activity01Icon);
export const Archive = make(Archive01Icon);
export const ArchiveRestore = make(ArchiveRestoreIcon);
export const ArrowDown = make(ArrowDown02Icon);
export const ArrowUp = make(ArrowUp02Icon);
export const ArrowUpRight = make(ArrowUpRight02Icon);
export const Bell = make(Notification01Icon);
export const BellRing = make(Notification03Icon);
export const Bot = make(RoboticIcon);
export const Check = make(Tick02Icon);
export const ChevronDown = make(ArrowDown01Icon);
export const ChevronLeft = make(ArrowLeft01Icon);
export const ChevronRight = make(ArrowRight01Icon);
export const CircleAlert = make(AlertCircleIcon);
export const CircleCheck = make(CheckmarkCircle02Icon);
export const ClipboardCopy = make(Copy01Icon);
export const Clock = make(Clock01Icon);
export const Clock3 = make(Time04Icon);
export const Coffee = make(Coffee01Icon);
export const Copy = make(Copy01Icon);
export const Cpu = make(CpuIcon);
export const Eye = make(ViewIcon);
export const EyeOff = make(ViewOffIcon);
export const FileText = make(File02Icon);
export const FlaskConical = make(TestTube01Icon);
export const Folder = make(Folder01Icon);
export const FolderGit2 = make(FolderGitIcon);
export const FolderOpen = make(FolderOpenIcon);
export const GitBranch = make(GitBranchIcon);
export const Globe = make(Globe02Icon);
export const GripVertical = make(DragDropVerticalIcon);
export const HardDrive = make(HardDriveIcon);
export const HelpCircle = make(HelpCircleIcon);
export const House = make(Home01Icon);
export const Layers = make(Layers01Icon);
export const LayoutGrid = make(DashboardSquare01Icon);
export const Loader2 = make(Loading03Icon);
export const Menu = make(Menu01Icon);
export const MessageCirclePlus = make(MessageAdd01Icon);
export const MessageCircleQuestion = make(MessageQuestionIcon);
export const MessageSquarePlus = make(MessageAdd02Icon);
export const Mic = make(Mic01Icon);
export const Minimize = make(MinimizeScreenIcon);
export const Maximize = make(FullScreenIcon);
export const Minus = make(MinusSignIcon);
export const Monitor = make(ComputerIcon);
export const MonitorOff = make(ComputerRemoveIcon);
export const Moon = make(Moon02Icon);
export const Palette = make(PaintBoardIcon);
export const Paperclip = make(Attachment01Icon);
export const Play = make(PlayIcon);
export const Plus = make(Add01Icon);
export const Power = make(ShutDownIcon);
export const QrCode = make(QrCodeIcon);
export const RefreshCw = make(RefreshIcon);
export const RotateCcw = make(RotateLeft01Icon);
export const RotateCw = make(RotateRight01Icon);
export const ScrollText = make(Note01Icon);
export const Search = make(Search01Icon);
export const Send = make(SentIcon);
export const Settings = make(Settings01Icon);
export const ShieldCheck = make(SecurityCheckIcon);
export const SlidersHorizontal = make(SlidersHorizontalIcon);
export const Smartphone = make(SmartPhone01Icon);
export const Sparkles = make(AiMagicIcon);
export const Square = make(SquareIcon);
export const SquareArrowOutUpRight = make(LinkSquare02Icon);
export const SquareTerminal = make(SourceCodeSquareIcon);
export const SunMedium = make(Sun03Icon);
export const SunMoon = make(ContrastIcon);
export const Sunrise = make(SunriseIcon);
export const Terminal = make(CommandLineIcon);
export const Trash2 = make(Delete02Icon);
export const Unplug = make(PlugSocketIcon);
export const Wifi = make(Wifi01Icon);
export const WifiOff = make(WifiDisconnected01Icon);
export const X = make(Cancel01Icon);
export const Zap = make(FlashIcon);

const MATERIAL: Record<string, IconSvgElement> = {
  account_tree: Structure01Icon,
  add: Add01Icon,
  arrow_back: ArrowLeft02Icon,
  arrow_circle_right: CircleArrowRight02Icon,
  arrow_forward: ArrowRight02Icon,
  arrow_upward: ArrowUp02Icon,
  attach_file: Attachment01Icon,
  block: BlockedIcon,
  bolt: FlashIcon,
  build: Wrench01Icon,
  chat: Chat01Icon,
  check: Tick02Icon,
  check_circle: CheckmarkCircle02Icon,
  checklist: CheckListIcon,
  chevron_left: ArrowLeft01Icon,
  chevron_right: ArrowRight01Icon,
  close: Cancel01Icon,
  commit: GitCommitIcon,
  computer: ComputerIcon,
  content_copy: Copy01Icon,
  create_new_folder: FolderAddIcon,
  delete: Delete02Icon,
  description: File02Icon,
  difference: GitCompareIcon,
  done_all: TickDouble02Icon,
  draft: File01Icon,
  edit: Edit02Icon,
  edit_document: FileEditIcon,
  energy_savings_leaf: Leaf01Icon,
  error: AlertCircleIcon,
  expand_less: ArrowUp01Icon,
  expand_more: ArrowDown01Icon,
  fact_check: Task01Icon,
  folder: Folder01Icon,
  folder_off: FolderRemoveIcon,
  folder_open: FolderOpenIcon,
  fork_right: GitForkIcon,
  forum: MessageMultiple01Icon,
  globe: Globe02Icon,
  history: Clock01Icon,
  home_storage: HardDriveIcon,
  hourglass_top: HourglassIcon,
  image: Image01Icon,
  language: Globe02Icon,
  linked_services: Link01Icon,
  lock: LockIcon,
  neurology: AiBrain01Icon,
  open_in_new: LinkSquare02Icon,
  picture_as_pdf: Pdf01Icon,
  play_arrow: PlayIcon,
  public_off: Globe02Icon,
  radio_button_checked: RadioButtonIcon,
  radio_button_unchecked: CircleIcon,
  refresh: RefreshIcon,
  search: Search01Icon,
  settings: Settings01Icon,
  shield: Shield01Icon,
  smart_toy: RoboticIcon,
  speed: DashboardSpeed01Icon,
  stacks: Layers01Icon,
  swap_horiz: ArrowHorizontalIcon,
  terminal: CommandLineIcon,
  unfold_less: UnfoldLessIcon,
  unfold_more: UnfoldMoreIcon,
  upload: Upload01Icon,
  view_in_ar: CubeIcon,
  warning: Alert02Icon,
  zoom_in: ZoomInAreaIcon,
};

export function materialIcon(name: string): IconSvgElement {
  return MATERIAL[name] ?? Wrench01Icon;
}

export const MaterialIcon: React.FC<IconProps & { name: string }> = ({ name, size = '1em', ...rest }) => (
  <HugeiconsIcon icon={materialIcon(name)} size={size} {...rest} />
);

const kebab = (key: string) => key.replace(/[A-Z]/g, (c) => `-${c.toLowerCase()}`);

export function iconSvgString(icon: IconSvgElement, size = 13, strokeWidth = 1.75): string {
  const body = icon
    .map(([tag, attrs]) => {
      const props = Object.entries(attrs as Record<string, string | number>)
        .filter(([key]) => key !== 'key')
        .map(([key, value]) => `${kebab(key)}="${key === 'strokeWidth' ? strokeWidth : value}"`)
        .join(' ');
      return `<${tag} ${props}/>`;
    })
    .join('');
  return `<svg width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" aria-hidden="true">${body}</svg>`;
}

export function materialIconSvg(name: string, size = 13, strokeWidth = 1.75): string {
  return iconSvgString(materialIcon(name), size, strokeWidth);
}
