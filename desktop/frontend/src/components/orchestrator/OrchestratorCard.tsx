import React, { useRef, useEffect } from 'react';
import { LiquidGlass } from '../../liquid-glass';
import { useTheme } from '../../themes';
import { ScrollArea } from '../common/ScrollArea';
import { AgentSessionFeed, AgentStreamBlock } from '../agent-session';
import { OrbitLoader } from '../agent-session/OrbitLoader';
import { TextShimmer } from '../agent-session/TextShimmer';

export interface OrchestratorCardProps {
  blocks: AgentStreamBlock[];
  isBusy?: boolean;
  error?: string | null;
  canUseWorktree?: boolean;
  isActive?: boolean;
  launchedKeys?: string[];
  onConfirmPlan?: (
    tasks: { title: string; prompt: string; cwd?: string }[],
    useWorktree: boolean,
    modelIds: string[],
  ) => void;
  onDismissPlan?: () => void;
  className?: string;
  style?: React.CSSProperties;
}

const PANEL_STYLE: React.CSSProperties = {
  boxShadow:
    '0 28px 64px -12px rgba(0, 0, 0, 0.65), 0 4px 16px rgba(0, 0, 0, 0.35), inset 0 0.5px 0.5px rgba(255, 255, 255, 0.22)',
};

export const OrchestratorCard: React.FC<OrchestratorCardProps> = ({
  blocks,
  isBusy = false,
  error,
  canUseWorktree,
  isActive = true,
  launchedKeys,
  onConfirmPlan,
  onDismissPlan,
  className = '',
  style,
}) => {
  const { currentTheme } = useTheme();
  const isLight = currentTheme.id === 'light' || currentTheme.id === 'white';
  const isGlass = currentTheme.id === 'glass';

  const scrollRef = useRef<HTMLDivElement>(null);
  const pinnedToBottom = useRef(true);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el || !pinnedToBottom.current) return;
    el.scrollTop = el.scrollHeight;
  }, [blocks]);

  const handleScroll = () => {
    const el = scrollRef.current;
    if (!el) return;
    pinnedToBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 48;
  };

  const hasLiveIndicator = blocks.some(
    (block) =>
      (block.type === 'thinking' && block.isThinking) ||
      (block.type === 'text' && block.isStreaming) ||
      (block.type === 'tool_bash' && block.status === 'running') ||
      (block.type === 'tool_search' && block.isSearching) ||
      (block.type === 'tool_edit' && (block as any).status === 'running') ||
      (block.type === 'tool_group' && block.items?.some((i) => i.status === 'running')),
  );

  return (
    <LiquidGlass
      variant="panel"
      surface="squircle"
      radius={24}
      bezelWidth={18}
      glassThickness={24}
      refractionScale={0.8}
      blur={0.55}
      specularOpacity={isLight ? 0.3 : isGlass ? 0.8 : 0.65}
      specularSaturation={6}
      lightAngle={-45}
      tint={
        isLight
          ? 'rgba(255, 255, 255, 0.96)'
          : isGlass
          ? 'rgba(10, 14, 26, 0.45)'
          : 'rgba(3, 3, 3, 0.96)'
      }
      shadow={isLight ? 'subtle' : 'apple'}
      border={
        isLight
          ? '1px solid rgba(0, 0, 0, 0.10)'
          : isGlass
          ? '1px solid rgba(255, 255, 255, 0.22)'
          : '1px solid rgba(255, 255, 255, 0.16)'
      }
      className={`relative flex flex-col overflow-hidden select-none p-4 ${
        isLight ? 'text-[#030303]' : 'text-white'
      } ${className}`}
      style={{
        width: 'min(680px, calc(100vw - 120px))',
        height: 'calc(100vh - 210px)',
        maxHeight: '690px',
        minHeight: '440px',
        background: isLight
          ? 'linear-gradient(145deg, #ffffff 0%, #fafafb 40%, #eaedf2 78%, #d8dce5 100%)'
          : isGlass
          ? 'rgba(10, 14, 26, 0.55)'
          : 'linear-gradient(145deg, #030303 0%, #070709 40%, #16171d 78%, #2a2d38 100%)',
        backdropFilter: isGlass ? 'blur(28px) saturate(190%)' : undefined,
        boxShadow: isLight
          ? '0 20px 48px -12px rgba(0, 0, 0, 0.14), 0 4px 16px rgba(0, 0, 0, 0.06), 0 0 0 1px rgba(0, 0, 0, 0.05), inset 0 0.5px 0.5px rgba(255, 255, 255, 0.95), inset -1px -1px 3px rgba(0, 0, 0, 0.05)'
          : isGlass
          ? '0 24px 60px -12px rgba(0, 0, 0, 0.55), 0 4px 16px rgba(0, 0, 0, 0.30), 0 0 0 1px rgba(255, 255, 255, 0.15), inset 0 0.5px 0.5px rgba(255, 255, 255, 0.40)'
          : '0 28px 64px -12px rgba(0, 0, 0, 0.70), 0 4px 16px rgba(0, 0, 0, 0.40), 0 0 0 1px rgba(255, 255, 255, 0.09), inset 0 0.5px 0.5px rgba(255, 255, 255, 0.32), inset -1px -1px 3px rgba(255, 255, 255, 0.12)',
        ...style,
      }}
    >
      <div className="flex flex-col h-full w-full min-h-0 overflow-hidden">
        {/* Top Grab Accent Handle (signature Lovable pill) */}
        <div
          className={`w-10 h-1 ${
            isLight ? 'bg-black/20' : 'bg-white/20'
          } rounded-full mx-auto mb-2 shrink-0`}
        />

        {/* Card Header: Just clean "Orchestrator" */}
        <div className="flex items-center justify-between pb-3 shrink-0">
          <div className="flex items-center gap-2">
            <span
              className={`w-2 h-2 rounded-full shrink-0 ${
                isBusy
                  ? 'bg-amber-400 animate-pulse'
                  : isLight
                    ? 'bg-[#030303]/60'
                    : 'bg-white/70'
              }`}
            />
            <span
              className={`text-[13px] font-medium font-(family-name:--app-font) tracking-tight select-none leading-none ${
                isLight ? 'text-[#030303]' : 'text-white/95'
              }`}
            >
              Orchestrator
            </span>
          </div>
        </div>

        {/* Body Feed / Content */}
        <div className="flex-1 min-h-0 relative flex flex-col">
          {blocks.length > 0 ? (
            <ScrollArea
              ref={scrollRef}
              onScroll={handleScroll}
              className="flex-1 px-1 py-1"
            >
              <AgentSessionFeed
                blocks={blocks}
                isWorking={isBusy}
                canUseWorktree={canUseWorktree}
                isActive={isActive}
                launchedKeys={launchedKeys}
                onConfirmPlan={onConfirmPlan}
                onDismissPlan={onDismissPlan}
              />

              {isBusy && !hasLiveIndicator && (
                <div className="flex items-center gap-2 pt-3 pl-1">
                  <OrbitLoader size={13} />
                  <TextShimmer
                    duration={1.5}
                    className="text-[12px] font-medium font-(family-name:--app-font) tracking-tight"
                  >
                    Thinking
                  </TextShimmer>
                </div>
              )}

              {error && (
                <div className="mt-3 px-3 py-2 rounded-[9px] bg-red-500/10 border border-red-400/25">
                  <span className="text-[12px] font-medium font-(family-name:--app-font) text-red-200/90 leading-relaxed">
                    {error}
                  </span>
                </div>
              )}
            </ScrollArea>
          ) : isBusy ? (
            <div className="flex-1 flex items-center justify-center gap-2">
              <OrbitLoader size={15} />
              <TextShimmer
                duration={1.5}
                className="text-[13px] font-medium font-(family-name:--app-font) tracking-tight"
              >
                Thinking
              </TextShimmer>
            </div>
          ) : null}
        </div>
      </div>
    </LiquidGlass>
  );
};

export default OrchestratorCard;
