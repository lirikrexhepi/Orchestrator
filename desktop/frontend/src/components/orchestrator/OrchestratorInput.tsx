import React, { useRef, useEffect, useState, useLayoutEffect } from 'react';
import { motion, AnimatePresence } from 'motion/react';
import { useOrchestratorStore } from './useOrchestratorStore';
import { ModelPicker } from './ModelPicker';
import { EffortPicker } from './EffortPicker';
import { ProjectsState } from './useProjects';
import { LiquidGlass } from '../../liquid-glass';
import { useTheme } from '../../themes';
import { RefractiveGlass } from '../common/RefractiveGlass';
import { useTransitionMount } from '../common/useTransitionMount';
import { SmoothTextarea } from '../common/SmoothTextarea';
import { AttachmentStrip } from '../common/AttachmentStrip';
import { AttachmentsState, filesFromTransfer } from '../common/useAttachments';
import { readChatDrag } from '../common/chatDrag';
import { leaveChatContextTarget, markChatContextTarget } from '../common/ChatDragLayer';
import { providerIcon } from './providerIcons';
import { SkillTestPicker } from './SkillTestPicker';
import type { QueuedMessage } from '../scene/QueuedMessages';
import { SlashMenu } from './SlashMenu';
import { ListSlashCommands } from '../../../wailsjs/go/main/App';
import { slashcmd } from '../../../wailsjs/go/models';
import { EventsOn } from '../../../wailsjs/runtime/runtime';
import { ArrowUp, ChevronDown, Loader2, Paperclip, Plus, MaterialIcon } from '../common/icons';

export interface OrchestratorInputProps {
  onSubmit?: (message: string, modelId: string) => void;
  onSkillTest?: (message: string, modelId: string, skills: string[]) => void;
  attachments?: AttachmentsState;
  queue?: {
    items: QueuedMessage[];
    onSendNow: (id: string) => void;
    onEdit: (id: string) => void;
    onDelete: (id: string) => void;
  };
  onInterrupt?: () => void;
  isBusy?: boolean;
  projects?: ProjectsState;
  targetTitle?: string;
  commandDriver?: string;
  hasActiveAgent?: boolean;
  onNewAgent?: () => void;
  isDraft?: boolean;
  viewMode?: 'deck' | 'grid' | 'orchestrator';
  onToggleViewMode?: () => void;
  className?: string;
}

// One rendered line of the message field. The measuring effect, the collapsed
// height and the capsule maths all derive from this, so the line-height in
// textClassName is the only place it may change.
const LINE_HEIGHT = 20;
const MAX_FIELD_HEIGHT = 160;
// The bottom toolbar row, matching h-[40px] in the markup. Named so the
// height maths and the markup cannot drift apart.
const TOOLBAR_HEIGHT = 40;
// Chrome around the rows: the capsule's own padding. Named for the same reason.
const CAPSULE_CHROME = 18;
const QUEUE_ROW_HEIGHT = 32;
const QUEUE_MAX_ROWS = 3;
// Chips get their own row between the field and the toolbar rather than
// growing the capsule without bound.

export const OrchestratorInput: React.FC<OrchestratorInputProps> = ({
  onSubmit,
  onSkillTest,
  onInterrupt,
  isBusy = false,
  projects,
  attachments,
  queue,
  targetTitle,
  commandDriver,
  hasActiveAgent = false,
  onNewAgent,
  isDraft = false,
  viewMode = 'deck',
  onToggleViewMode,
  className = '',
}) => {
  const {
    selectedModelId,
    messageText,
    isModelPickerOpen,
    isEffortPickerOpen,
    isLoadingProviders,
    setMessageText,
    toggleModelPicker,
    closeAllPickers,
    getSelectedModel,
    getSelectedProvider,
    loadProviders,
  } = useOrchestratorStore();

  const { currentTheme } = useTheme();
  const isLight = currentTheme.id === 'light' || currentTheme.id === 'white';
  const useOpticalRefraction = currentTheme.id === 'glass' || currentTheme.id === 'refractive-glass' || currentTheme.glass.mode === 'optical-refraction';

  useEffect(() => {
    void loadProviders();
    return EventsOn('providers:changed', () => void loadProviders());
  }, [loadProviders]);

  const containerRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [textareaHeight, setTextareaHeight] = useState(LINE_HEIGHT);
  const railRef = useRef(attachments?.items ?? []);

  // Transition mount coordination for guaranteed entrance AND exit
  const modelPickerMount = useTransitionMount(isModelPickerOpen, 200);
  const effortPickerMount = useTransitionMount(isEffortPickerOpen, 200);

  const currentModel = getSelectedModel();
  const accountName = useOrchestratorStore((state) => {
    const provider = state.providers.find((p) => p.id === currentModel?.providerId);
    if (!provider?.accounts || provider.accounts.length < 2) return '';
    const shown = state.shownAccount(provider.id);
    return provider.accounts.find((account) => account.id === shown)?.name ?? '';
  });
  const currentProvider = getSelectedProvider();
  const iconSrc =
    providerIcon(currentModel?.providerId || currentProvider?.id || '', isLight) ||
    currentModel?.icon ||
    currentProvider?.icon;

  // Accurately measure textarea height without causing layout jumps
  useLayoutEffect(() => {
    const el = textareaRef.current;
    if (!el) return;

    if (!messageText || messageText.length === 0) {
      el.style.height = `${LINE_HEIGHT}px`;
      setTextareaHeight(LINE_HEIGHT);
      return;
    }

    // Set to 0px synchronously to read exact natural scrollHeight
    el.style.height = '0px';
    const scrollH = el.scrollHeight;
    const targetHeight = Math.min(Math.max(scrollH, LINE_HEIGHT), MAX_FIELD_HEIGHT);
    el.style.height = `${targetHeight}px`;
    setTextareaHeight(targetHeight);
  }, [messageText]);

  // Handle clicking outside to close pickers
  useEffect(() => {
    const handleOutsideClick = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        closeAllPickers();
      }
    };

    if (isModelPickerOpen || isEffortPickerOpen) {
      document.addEventListener('mousedown', handleOutsideClick);
    }
    return () => {
      document.removeEventListener('mousedown', handleOutsideClick);
    };
  }, [isModelPickerOpen, isEffortPickerOpen, closeAllPickers]);

  // An attachment on its own is a complete message, so a turn is sendable when
  // there is either text or a staged file.
  const hasAttachments = (attachments?.items.length ?? 0) > 0;
  const canSubmit = messageText.trim().length > 0 || hasAttachments;

  const [testSkills, setTestSkills] = useState<string[]>([]);
  const skillTest = testSkills.length > 0 && Boolean(onSkillTest);

  const submitMessage = () => {
    if (!canSubmit) return;
    if (skillTest) onSkillTest?.(messageText, selectedModelId, testSkills);
    else onSubmit?.(expandSlash(messageText), selectedModelId);
    setMessageText('');
    if (textareaRef.current) {
      textareaRef.current.style.height = `${LINE_HEIGHT}px`;
    }
    setTextareaHeight(LINE_HEIGHT);
  };

  // Pasting a screenshot stages it instead of inserting its text form. Pastes
  // carrying no file fall through untouched so ordinary text still pastes.
  const handlePaste = (e: React.ClipboardEvent) => {
    if (!attachments) return;
    const files = filesFromTransfer(e.clipboardData);
    if (files.length === 0) return;
    e.preventDefault();
    void attachments.accept(files);
  };

  const handleDrop = (e: React.DragEvent) => {
    if (!attachments) return;
    const chat = readChatDrag(e.dataTransfer);
    if (chat) {
      e.preventDefault();
      attachments.addChat(chat);
      return;
    }
    const files = filesFromTransfer(e.dataTransfer);
    if (files.length === 0) return;
    e.preventDefault();
    void attachments.accept(files);
  };

  const storeProviderId = useOrchestratorStore((state) => state.selectedProviderId);
  const slashDriver = ['claude', 'codex', 'antigravity', 'opencode'].includes(commandDriver ?? '')
    ? (commandDriver as string)
    : ['claude', 'codex', 'antigravity', 'opencode'].includes(storeProviderId)
      ? storeProviderId
      : 'claude';
  const slashCwd = projects?.active?.path ?? '';
  const [slashCommands, setSlashCommands] = useState<slashcmd.Command[]>([]);
  const [slashIndex, setSlashIndex] = useState(0);
  const [slashDismissed, setSlashDismissed] = useState(false);

  useEffect(() => {
    let live = true;
    ListSlashCommands(slashDriver, slashCwd)
      .then((list) => {
        if (live) setSlashCommands(list ?? []);
      })
      .catch(() => {
        if (live) setSlashCommands([]);
      });
    return () => {
      live = false;
    };
  }, [slashDriver, slashCwd]);

  const slashQuery = /^\/([^\s]*)$/.exec(messageText)?.[1];
  const slashMatches = React.useMemo(() => {
    if (slashQuery === undefined) return [];
    const needle = slashQuery.toLowerCase();
    const starts = slashCommands.filter((c) => c.name.toLowerCase().startsWith(needle));
    const rest = slashCommands.filter((c) => !c.name.toLowerCase().startsWith(needle) && c.name.toLowerCase().includes(needle));
    return [...starts, ...rest];
  }, [slashQuery, slashCommands]);
  const slashOpen = !slashDismissed && !skillTest && slashMatches.length > 0;

  useEffect(() => {
    setSlashIndex(0);
    setSlashDismissed(false);
  }, [slashQuery]);

  const pickSlash = (command: slashcmd.Command) => {
    setMessageText('/' + command.name + ' ');
    setSlashDismissed(true);
    textareaRef.current?.focus();
  };

  const expandSlash = (text: string): string => {
    if (slashDriver === 'claude' || !text.startsWith('/')) return text;
    const [head, ...tail] = text.slice(1).split(/\s+/);
    const command = slashCommands.find((c) => c.kind === 'custom' && c.name === head && c.prompt);
    if (!command || !command.prompt) return text;
    const args = tail.join(' ').trim();
    if (/\$ARGUMENTS|\{\{args\}\}/.test(command.prompt)) {
      return command.prompt.replace(/\$ARGUMENTS|\{\{args\}\}/g, args);
    }
    return args ? command.prompt + '\n\n' + args : command.prompt;
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (slashOpen) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        const step = e.key === 'ArrowDown' ? 1 : -1;
        setSlashIndex((index) => (index + step + slashMatches.length) % slashMatches.length);
        return;
      }
      if (e.key === 'Tab' || (e.key === 'Enter' && !e.shiftKey)) {
        e.preventDefault();
        pickSlash(slashMatches[Math.min(slashIndex, slashMatches.length - 1)]);
        return;
      }
      if (e.key === 'Escape') {
        e.preventDefault();
        setSlashDismissed(true);
        return;
      }
    }
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      submitMessage();
    }
    if (e.key === 'Escape') {
      closeAllPickers();
      // Esc stops the running turn, as in Claude Code.
      if (isBusy) {
        e.preventDefault();
        onInterrupt?.();
      }
    }
  };

  // Expanded means the field has wrapped past its first line.
  const isExpanded = textareaHeight > LINE_HEIGHT + 4;
  const isScrollable = textareaHeight >= MAX_FIELD_HEIGHT;
  // The field keeps its full width on its own row; the toolbar lives beneath
  // it, so the capsule stacks text, chips and controls instead of squeezing
  // them side by side.
  const textZoneHeight = isExpanded ? Math.min(textareaHeight + 24, 188) : 40;
  const queueItems = queue?.items ?? [];
  const queueHeight = queueItems.length > 0 ? Math.min(queueItems.length, QUEUE_MAX_ROWS) * QUEUE_ROW_HEIGHT + 10 : 0;
  const capsuleHeight = Math.max(textZoneHeight + TOOLBAR_HEIGHT + CAPSULE_CHROME, 98) + queueHeight;
  if (hasAttachments && attachments) railRef.current = attachments.items;
  const railItems = hasAttachments && attachments ? attachments.items : railRef.current;

  const capsuleContent = (
    <div className="flex flex-col w-full h-full">
      {queue && queueItems.length > 0 && (
        <div
          className={`shrink-0 flex flex-col px-[6px] pt-[4px] pb-[6px] mb-[2px] border-b ${isLight ? 'border-black/[0.07]' : 'border-white/[0.07]'}`}
          style={{ height: queueHeight }}
        >
          <div className="flex flex-col overflow-y-auto custom-scrollbar" style={{ maxHeight: QUEUE_MAX_ROWS * QUEUE_ROW_HEIGHT }}>
            {queueItems.map((item, index) => (
              <div
                key={item.id}
                className={`group/queued flex items-center gap-2 px-2 rounded-[9px] transition-colors duration-150 ${isLight ? 'hover:bg-black/[0.04]' : 'hover:bg-white/[0.05]'}`}
                style={{ height: QUEUE_ROW_HEIGHT }}
              >
                <span className={`text-[10px] font-mono tabular-nums shrink-0 ${isLight ? 'text-black/35' : 'text-white/35'}`}>{index + 1}</span>
                <span className={`flex-1 min-w-0 truncate text-[12.5px] tracking-tight font-(family-name:--app-font) ${isLight ? 'text-black/70' : 'text-white/70'}`}>
                  {item.text || (item.files?.length ? `${item.files.length} attached file(s)` : 'Empty prompt')}
                </span>
                <span className={`text-[10.5px] tracking-tight shrink-0 group-hover/queued:hidden ${isLight ? 'text-black/35' : 'text-white/35'}`}>
                  Queued
                </span>
                <div className="hidden group-hover/queued:flex items-center gap-0.5 shrink-0">
                  {[
                    { label: 'Send now', icon: 'arrow_upward', run: () => queue.onSendNow(item.id), danger: false },
                    { label: 'Edit', icon: 'edit', run: () => queue.onEdit(item.id), danger: false },
                    { label: 'Remove', icon: 'close', run: () => queue.onDelete(item.id), danger: true },
                  ].map((action) => (
                    <button
                      key={action.label}
                      type="button"
                      title={action.label}
                      aria-label={action.label}
                      onClick={(e) => {
                        e.stopPropagation();
                        action.run();
                      }}
                      className={`w-[24px] h-[24px] rounded-[7px] flex items-center justify-center active:scale-90 transition-all cursor-pointer ${
                        action.danger
                          ? 'text-rose-400/70 hover:text-rose-300 hover:bg-rose-500/15'
                          : isLight
                            ? 'text-black/50 hover:text-black hover:bg-black/[0.07]'
                            : 'text-white/55 hover:text-white hover:bg-white/[0.1]'
                      }`}
                    >
                      <MaterialIcon name={action.icon} className="text-[14px]" />
                    </button>
                  ))}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
      {/* Message field: the full top row. */}
      <div className="flex-1 min-w-0 flex items-start gap-2 px-[14px] pt-[8px] overflow-hidden">
        <SmoothTextarea
          ref={textareaRef}
          rows={1}
          value={messageText}
          onValueChange={setMessageText}
          onKeyDown={handleKeyDown}
          placeholder={
            viewMode === 'grid' || viewMode === 'orchestrator'
              ? 'Ask orchestrator…'
              : isDraft
                ? 'Type to start this chat…'
                : !hasActiveAgent
                  ? 'Ask me anything…'
                  : 'Ask a follow up…'
          }
          caretColor={isLight ? '#030303' : 'rgba(255, 255, 255, 0.95)'}
          textClassName={`w-full text-[13.5px] font-normal font-(family-name:--app-font) tracking-tight leading-[20px] block ${
            isLight ? 'text-[#030303]' : 'text-white/90'
          }`}
          className={`flex-1 min-w-0 ${isScrollable ? 'overflow-y-auto' : 'overflow-hidden'}`}
          placeholderClassName={isLight ? 'text-black/40 truncate' : 'text-white/40 truncate'}
          style={{
            minHeight: `${LINE_HEIGHT}px`,
            maxHeight: `${MAX_FIELD_HEIGHT}px`,
          }}
        />
      </div>

      {/* Toolbar: pickers on the left, actions on the right. Borderless, like
          text — the squircle itself is the shape, nothing inside needs a box. */}
      <div className="flex items-center justify-between gap-1.5 px-[8px] pb-[2px] shrink-0" style={{ height: `${TOOLBAR_HEIGHT}px` }}>
        <div className="flex items-center gap-1 min-w-0">
          {/* Model & Provider Selector Trigger */}
          <button
            type="button"
            title={currentModel ? [currentModel.name, accountName].filter(Boolean).join(' · ') : 'Choose model'}
            onClick={(e) => {
              e.stopPropagation();
              toggleModelPicker();
            }}
            className={`h-[32px] flex items-center gap-1.5 px-2 rounded-[10px] active:scale-95 transition-all duration-150 cursor-pointer shrink-0 group ${
              isLight
                ? 'hover:bg-black/[0.05] text-[#030303]'
                : 'hover:bg-white/[0.08] text-white/90'
            }`}
          >
            {iconSrc && (
              <img
                src={iconSrc}
                alt=""
                className="w-[18px] h-[18px] object-contain shrink-0 transition-transform duration-200 group-hover:scale-105"
                draggable={false}
              />
            )}
            <span className={`text-[13px] font-medium font-(family-name:--app-font) tracking-tight select-none leading-none max-w-[130px] truncate`}>
              {currentModel?.name || (isLoadingProviders ? 'Detecting CLIs…' : 'No CLI found')}
            </span>
            {accountName && (
              <span className={`text-[11px] font-medium font-(family-name:--app-font) tracking-tight select-none leading-none max-w-[80px] truncate ${
                isLight ? 'text-black/45' : 'text-white/45'
              }`}>
                {accountName}
              </span>
            )}
            <ChevronDown size={14} strokeWidth={1.75} className={`transition-transform duration-200 ease-[cubic-bezier(0.16,1,0.3,1)] flex items-center opacity-60 ${ isModelPickerOpen ? 'rotate-180' : '' }`} />
          </button>

          {/* New chat tab - deck mode, same as the top + */}
          {viewMode === 'deck' && (
            <button
              type="button"
              title="New chat (Ctrl + N)"
              aria-label="New chat"
              onClick={(e) => {
                e.stopPropagation();
                onNewAgent?.();
              }}
              className={`h-[32px] w-[32px] flex items-center justify-center rounded-[11px] transition-all duration-150 active:scale-90 cursor-pointer shrink-0 group select-none ${
                isLight
                  ? 'text-black/45 hover:text-[#030303] hover:bg-black/[0.05]'
                  : 'text-white/45 hover:text-white hover:bg-white/[0.08]'
              }`}
            >
              <Plus size={15} strokeWidth={1.75} className="group-hover:rotate-90 transition-transform duration-200" />
            </button>
          )}
        </div>

        <div className="flex items-center gap-0.5 shrink-0">
          {onSkillTest && (
            <SkillTestPicker cwd={projects?.active?.path ?? ''} selected={testSkills} onChange={setTestSkills} isLight={isLight} />
          )}
          {/* Attach */}
          {attachments && (
            <button
              type="button"
              title="Attach files"
              aria-label="Attach files"
              disabled={attachments.isBusy}
              onClick={(e) => {
                e.stopPropagation();
                void attachments.browse();
              }}
              className={`w-[32px] h-[32px] rounded-[11px] flex items-center justify-center transition-all duration-150 shrink-0 group ${
                attachments.isBusy
                  ? (isLight ? 'text-black/25 cursor-default' : 'text-white/25 cursor-default')
                  : (isLight
                      ? 'text-black/45 hover:text-[#030303] hover:bg-black/[0.05] active:scale-95 cursor-pointer'
                      : 'text-white/45 hover:text-white hover:bg-white/[0.08] active:scale-95 cursor-pointer')
              }`}
            >
              {attachments.isBusy ? <Loader2 size={16} strokeWidth={1.75} className="animate-spin" /> : <Paperclip size={16} strokeWidth={1.75} />}
            </button>
          )}

          <AnimatePresence initial={false} mode="popLayout">
            {isBusy && (
              <motion.button
                key="stop"
                type="button"
                title="Stop agent (Esc)"
                aria-label="Stop agent"
                initial={{ opacity: 0, scale: 0.7 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.7 }}
                transition={{ type: 'spring', stiffness: 520, damping: 30, mass: 0.7 }}
                whileTap={{ scale: 0.92 }}
                onClick={(e) => {
                  e.stopPropagation();
                  onInterrupt?.();
                }}
                className="glass-orb glass-orb-stop relative w-[34px] h-[34px] rounded-full flex items-center justify-center shrink-0 cursor-pointer group"
              >
                <span className="w-[10px] h-[10px] rounded-[3px] bg-white shadow-[0_1px_2px_rgba(150,40,60,0.45)] transition-transform duration-150 group-hover:scale-90" />
              </motion.button>
            )}

            {(!isBusy || canSubmit) && (
              <motion.button
                key="send"
                type="button"
                title={skillTest ? 'Run skill test' : isBusy ? 'Queue message' : 'Send (Enter)'}
                aria-label={skillTest ? 'Run skill test' : isBusy ? 'Queue message' : 'Send'}
                disabled={!canSubmit}
                initial={{ opacity: 0, scale: 0.7 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.7 }}
                transition={{ type: 'spring', stiffness: 520, damping: 30, mass: 0.7 }}
                whileTap={canSubmit ? { scale: 0.92 } : undefined}
                onClick={(e) => {
                  e.stopPropagation();
                  submitMessage();
                }}
                className={`relative w-[34px] h-[34px] rounded-full flex items-center justify-center shrink-0 group transition-all duration-200 ${
                  canSubmit
                    ? 'glass-orb text-white cursor-pointer'
                    : isLight
                      ? 'bg-black/[0.05] text-black/25 cursor-default shadow-[inset_0_0_0_1px_rgba(0,0,0,0.06)]'
                      : 'bg-white/[0.05] text-white/25 cursor-default shadow-[inset_0_0_0_1px_rgba(255,255,255,0.07)]'
                }`}
              >
                <ArrowUp size={16} strokeWidth={2.5} className="drop-shadow-[0_1px_1px_rgba(40,120,180,0.45)] transition-transform duration-200 group-enabled:group-hover:-translate-y-px" />
              </motion.button>
            )}
          </AnimatePresence>
        </div>
      </div>
    </div>
  );

  return (
    <div
      ref={containerRef}
      onPaste={handlePaste}
      onDrop={handleDrop}
      onDragOver={(e) => {
        if (!attachments) return;
        e.preventDefault();
        markChatContextTarget(e);
      }}
      onDragLeave={leaveChatContextTarget}
      className={`relative flex flex-col items-center select-none ${className}`}
    >
      <AnimatePresence>
        {attachments && hasAttachments && (
          <motion.div
            key="attachment-rail"
            initial={{ opacity: 0, x: 16, scale: 0.96 }}
            animate={{ opacity: 1, x: 0, scale: 1 }}
            exit={{ opacity: 0, x: 12, scale: 0.97, transition: { duration: 0.14, ease: 'easeIn' } }}
            transition={{ type: 'spring', stiffness: 420, damping: 32, mass: 0.8 }}
            style={{ transformOrigin: 'bottom right', right: 'calc(50% + 370px)', maxHeight: capsuleHeight }}
            className="absolute bottom-0 overflow-y-auto overflow-x-hidden custom-scrollbar pointer-events-auto z-10"
          >
            <AttachmentStrip items={railItems} onRemove={attachments.remove} vertical />
          </motion.div>
        )}
      </AnimatePresence>

      {/* Main Orchestrator Squircle Bar - Expands strictly downward with stationary top elements */}
      {useOpticalRefraction ? (
        <RefractiveGlass
          borderRadius={22}
          borderWidth={0.07}
          distortionScale={-180}
          redOffset={0}
          greenOffset={10}
          blueOffset={20}
          brightness={50}
          opacity={0.93}
          blur={7}
          displace={0.5}
          backgroundOpacity={0.18}
          saturation={1.8}
          mixBlendMode="screen"
          className="w-[720px] max-w-[calc(100vw-32px)] p-[9px] flex justify-between"
          style={{
            height: `${capsuleHeight}px`,
            transition:
              'height 220ms cubic-bezier(0.16, 1, 0.3, 1), border-radius 220ms cubic-bezier(0.16, 1, 0.3, 1), box-shadow 220ms ease',
            boxShadow: isExpanded
              ? '0 28px 60px rgba(0, 0, 0, 0.65), 0 8px 24px rgba(0, 0, 0, 0.4), inset 0 1px 1.5px rgba(255, 255, 255, 0.45), inset 0 0 0 0.5px rgba(255, 255, 255, 0.25)'
              : '0 20px 48px -8px rgba(0, 0, 0, 0.55), 0 6px 18px rgba(0, 0, 0, 0.3), inset 0 1px 1.5px rgba(255, 255, 255, 0.4), inset 0 0 0 0.5px rgba(255, 255, 255, 0.2)',
          }}
        >
          {capsuleContent}
        </RefractiveGlass>
      ) : (
        <LiquidGlass
          variant="panel"
          surface="squircle"
          radius={22}
          bezelWidth={16}
          glassThickness={28}
          refractionScale={0.5}
          blur={0.5}
          frost={24}
          frostSaturation={isLight ? 110 : 130}
          specularOpacity={isLight ? 0.3 : 0.06}
          tint="var(--theme-input-bg, rgba(10, 10, 10, 0.96))"
          shadow={isLight ? 'subtle' : 'apple'}
          border="1px solid var(--theme-input-border, rgba(255, 255, 255, 0.08))"
          className="w-[720px] max-w-[calc(100vw-32px)] p-[9px] flex justify-between"
          style={{
            height: `${capsuleHeight}px`,
            transition:
              'height 220ms cubic-bezier(0.16, 1, 0.3, 1), border-radius 220ms cubic-bezier(0.16, 1, 0.3, 1), box-shadow 220ms ease',
            boxShadow: isExpanded
              ? (isLight
                  ? '0 24px 50px rgba(0, 0, 0, 0.12), 0 6px 18px rgba(0, 0, 0, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.9)'
                  : '0 28px 60px rgba(0, 0, 0, 0.8), 0 8px 24px rgba(0, 0, 0, 0.55), inset 0 1px 0 rgba(255, 255, 255, 0.12)')
              : (isLight
                  ? '0 16px 36px -6px rgba(0, 0, 0, 0.10), 0 4px 12px rgba(0, 0, 0, 0.05), inset 0 1px 0 rgba(255, 255, 255, 0.9)'
                  : '0 20px 48px -8px rgba(0, 0, 0, 0.75), 0 6px 18px rgba(0, 0, 0, 0.45), inset 0 1px 0 rgba(255, 255, 255, 0.08)'),
          }}
        >
          {capsuleContent}
        </LiquidGlass>
      )}

      {slashOpen && (
        <div className="absolute bottom-[calc(100%+12px)] left-[9px] z-50 pointer-events-auto">
          <SlashMenu
            commands={slashMatches}
            selected={Math.min(slashIndex, slashMatches.length - 1)}
            isLight={isLight}
            onPick={pickSlash}
            onHover={setSlashIndex}
          />
        </div>
      )}

      {/* Popups Row (Model Picker & Effort Picker) - anchored ABOVE the input bar, flush with model pill */}
      <div
        className="absolute bottom-[calc(100%+12px)] left-[9px] flex items-end gap-2.5 z-50 pointer-events-none"
        onClick={(e) => e.stopPropagation()}
      >
        <AnimatePresence>
          {isModelPickerOpen && (
            <motion.div
              key="orchestrator-model-picker"
              initial={{ opacity: 0, scale: 0.95, y: 8 }}
              animate={{ opacity: 1, scale: 1, y: 0 }}
              exit={{ opacity: 0, scale: 0.96, y: 4, transition: { duration: 0.12, ease: 'easeIn' } }}
              transition={{
                type: 'spring',
                stiffness: 440,
                damping: 30,
                mass: 0.8,
              }}
              style={{ transformOrigin: 'bottom left' }}
              className="origin-bottom-left pointer-events-auto"
            >
              <ModelPicker />
            </motion.div>
          )}
        </AnimatePresence>

        <AnimatePresence>
          {isEffortPickerOpen && (
            <motion.div
              key="orchestrator-effort-picker"
              initial={{ opacity: 0, scale: 0.95, y: 8, x: -6 }}
              animate={{ opacity: 1, scale: 1, y: 0, x: 0 }}
              exit={{ opacity: 0, scale: 0.96, y: 4, x: -4, transition: { duration: 0.12, ease: 'easeIn' } }}
              transition={{
                type: 'spring',
                stiffness: 440,
                damping: 30,
                mass: 0.8,
              }}
              style={{ transformOrigin: 'bottom left' }}
              className="origin-bottom-left pointer-events-auto"
            >
              <EffortPicker />
            </motion.div>
          )}
        </AnimatePresence>
      </div>
    </div>
  );
};
