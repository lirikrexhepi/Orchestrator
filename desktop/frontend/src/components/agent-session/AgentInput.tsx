import { MaterialIcon } from '../common/icons';
import React, { useRef, useState, useLayoutEffect, useEffect } from 'react';
import { motion, AnimatePresence } from 'motion/react';
import { useOrchestratorStore } from '../orchestrator/useOrchestratorStore';
import { ModelPicker } from '../orchestrator/ModelPicker';
import { EffortPicker } from '../orchestrator/EffortPicker';
import { useTransitionMount } from '../common/useTransitionMount';
import { SmoothTextarea } from '../common/SmoothTextarea';
import { AttachmentStrip } from '../common/AttachmentStrip';
import { AttachmentsState, filesFromTransfer, useAttachments } from '../common/useAttachments';
import { readChatDrag } from '../common/chatDrag';
import { leaveChatContextTarget, markChatContextTarget } from '../common/ChatDragLayer';
import { domain } from '../../../wailsjs/go/models';
import { ThinkingEffort, ThinkingMode } from '../orchestrator/types';

export interface AgentInputProps {
  onSubmit?: (message: string, modelId: string, files?: domain.FileRef[]) => void;
  /** Supply to share staging with a parent; omitted, the input owns its own. */
  attachments?: AttachmentsState;
  onInterrupt?: () => void;
  isStreaming?: boolean;
  placeholder?: string;
  className?: string;
  defaultModelId?: string;
}

/**
 * AgentInput Component
 * Standalone prompt and model selection container.
 * - Normal Send Button: dark purple/blue squircle with white upward arrow
 * - Running Interrupt Button: circular target icon (radio_button_checked) when agent is streaming
 * - Floating ModelPicker: rich backdrop blur for high contrast over text
 */
export const AgentInput: React.FC<AgentInputProps> = ({
  onSubmit,
  onInterrupt,
  isStreaming = false,
  placeholder = 'Send a message',
  className = '',
  defaultModelId,
  attachments: suppliedAttachments,
}) => {
  const { providers, models } = useOrchestratorStore();

  // Hooks cannot be called conditionally, so one is always created and simply
  // ignored when the parent supplies its own.
  const ownFiles = useAttachments();
  const attachments = suppliedAttachments ?? ownFiles;

  const [message, setMessage] = useState('');
  const [textareaHeight, setTextareaHeight] = useState(20);
  // Falls back to the store's selection so a window never displays a model the
  // session is not actually running.
  const [localModelId, setLocalModelId] = useState(
    () => defaultModelId || useOrchestratorStore.getState().selectedModelId,
  );
  const [localProviderId, setLocalProviderId] = useState(() => {
    const m = models.find((item) => item.id === defaultModelId);
    return m?.providerId || 'anthropic';
  });

  // Isolated picker open states for this AgentInput instance. Effort/mode
  // selections write through to the shared store so the picker highlight and
  // the options sent on submit always agree.
  const [isLocalModelPickerOpen, setIsLocalModelPickerOpen] = useState(false);
  const [isLocalEffortPickerOpen, setIsLocalEffortPickerOpen] = useState(false);

  const containerRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const modelPickerMount = useTransitionMount(isLocalModelPickerOpen, 180);
  const effortPickerMount = useTransitionMount(isLocalEffortPickerOpen, 180);

  const currentModel = models.find((m) => m.id === localModelId) || models[0];
  const currentProvider = providers.find((p) => p.id === localProviderId) || providers[0];
  const iconSrc = currentModel?.icon || currentProvider?.icon;

  // Auto-resize textarea height
  useLayoutEffect(() => {
    const el = textareaRef.current;
    if (!el) return;

    if (!message || message.length === 0) {
      el.style.height = '20px';
      setTextareaHeight(20);
      return;
    }

    el.style.height = '0px';
    const scrollH = el.scrollHeight;
    const targetHeight = Math.min(Math.max(scrollH, 20), 120);
    el.style.height = `${targetHeight}px`;
    setTextareaHeight(targetHeight);
  }, [message]);

  // Handle clicking outside to close pickers
  useEffect(() => {
    const handleOutsideClick = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setIsLocalModelPickerOpen(false);
        setIsLocalEffortPickerOpen(false);
      }
    };

    if (isLocalModelPickerOpen || isLocalEffortPickerOpen) {
      document.addEventListener('mousedown', handleOutsideClick);
    }
    return () => {
      document.removeEventListener('mousedown', handleOutsideClick);
    };
  }, [isLocalModelPickerOpen, isLocalEffortPickerOpen]);

  // An attachment on its own is a complete message, so a turn is sendable when
  // there is either text or a staged file.
  const hasAttachments = attachments.items.length > 0;
  const railRef = useRef(attachments.items);
  if (hasAttachments) railRef.current = attachments.items;
  const railItems = hasAttachments ? attachments.items : railRef.current;
  const canSubmit = message.trim().length > 0 || hasAttachments;

  const submitMessage = () => {
    if (!canSubmit) return;
    const files = attachments.toRefs();
    onSubmit?.(message, localModelId, files);
    // Released rather than cleared: the turn in flight owns those paths now.
    if (files.length > 0) attachments.release();
    setMessage('');
    if (textareaRef.current) {
      textareaRef.current.style.height = '22px';
    }
    setTextareaHeight(20);
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      if (isStreaming) {
        onInterrupt?.();
      } else {
        submitMessage();
      }
    }
    if (e.key === 'Escape') {
      setIsLocalModelPickerOpen(false);
      setIsLocalEffortPickerOpen(false);
    }
  };

  const handleButtonClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (isStreaming) {
      onInterrupt?.();
      return;
    }
    submitMessage();
  };

  // Pasting a screenshot stages it instead of inserting its text form. Pastes
  // carrying no file fall through untouched so ordinary text still pastes.
  const handlePaste = (e: React.ClipboardEvent) => {
    const files = filesFromTransfer(e.clipboardData);
    if (files.length === 0) return;
    e.preventDefault();
    void attachments.accept(files);
  };

  const handleDrop = (e: React.DragEvent) => {
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

  const handleSelectModel = (modelId: string) => {
    const m = models.find((item) => item.id === modelId);
    if (!m) return;
    const hasEfforts = Boolean(
      (m.effortLevels && m.effortLevels.length > 0) || m.supportsThinking,
    );
    if (localModelId === modelId && isLocalEffortPickerOpen) {
      setIsLocalEffortPickerOpen(false);
    } else {
      setLocalModelId(modelId);
      setLocalProviderId(m.providerId);
      setIsLocalEffortPickerOpen(hasEfforts);
    }
  };

  const handleSelectProvider = (providerId: string) => {
    setLocalProviderId(providerId);
    const providerModels = models.filter((m) => m.providerId === providerId);
    if (providerModels.length > 0) {
      setLocalModelId(providerModels[0].id);
    }
  };

  const isScrollable = textareaHeight >= 120;

  return (
    <div
      ref={containerRef}
      onPaste={handlePaste}
      onDrop={handleDrop}
      onDragOver={(e) => {
        e.preventDefault();
        markChatContextTarget(e);
      }}
      onDragLeave={leaveChatContextTarget}
      className={`relative flex flex-col select-none ${className}`}
    >
      <AnimatePresence>
        {hasAttachments && (
          <motion.div
            key="attachment-rail"
            initial={{ opacity: 0, x: 14, scale: 0.96 }}
            animate={{ opacity: 1, x: 0, scale: 1 }}
            exit={{ opacity: 0, x: 10, scale: 0.97, transition: { duration: 0.14, ease: 'easeIn' } }}
            transition={{ type: 'spring', stiffness: 420, damping: 32, mass: 0.8 }}
            style={{ transformOrigin: 'bottom right' }}
            className="absolute right-[calc(100%+10px)] bottom-0 max-h-[320px] overflow-y-auto overflow-x-hidden custom-scrollbar"
          >
            <AttachmentStrip items={railItems} onRemove={attachments.remove} compact vertical />
          </motion.div>
        )}
      </AnimatePresence>

      {/* Main Agent Input Box - Obsidian Dark Stealth Bubble */}
      <div
        className="rounded-[14px] glass-card border border-white/10 p-2.5 flex flex-col gap-2 transition-all duration-200"
        style={{
          boxShadow:
            '0 4px 16px rgba(0, 0, 0, 0.35), inset 0 0.5px 0.5px rgba(255, 255, 255, 0.08)',
        }}
      >
        {/* Top Textarea Message Input */}
        <div className="w-full px-1 pt-0.5 overflow-hidden">
          <SmoothTextarea
            ref={textareaRef}
            rows={1}
            value={message}
            onValueChange={setMessage}
            onKeyDown={handleKeyDown}
            placeholder={placeholder}
            caretColor="rgba(255, 255, 255, 0.95)"
            textClassName="w-full text-[11.5px] font-medium font-(family-name:--app-font) text-white tracking-tight leading-[20px] block"
            className={isScrollable ? 'overflow-y-auto custom-scrollbar' : 'overflow-hidden'}
            placeholderClassName="text-white/40"
            style={{
              minHeight: '20px',
              maxHeight: '120px',
            }}
          />
        </div>

        {/* Bottom Toolbar: Model Switcher + Action Button */}
        <div className="flex items-center justify-between pt-0.5">
          {/* Model trigger badge */}
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              setIsLocalModelPickerOpen((prev) => !prev);
              setIsLocalEffortPickerOpen(false);
            }}
            className="flex items-center gap-1.5 px-1.5 py-0.5 rounded-[7px] hover:bg-white/15 active:scale-95 transition-all duration-150 cursor-pointer group"
          >
            {iconSrc && (
              <img
                src={iconSrc}
                alt=""
                className="w-4 h-4 object-contain shrink-0"
                draggable={false}
              />
            )}
            <span className="text-[11.5px] font-medium font-(family-name:--app-font) text-white tracking-tight leading-none">
              {currentModel?.name || 'Select Model'}
            </span>
            <MaterialIcon name="expand_more" className={`text-[15px] text-white/70 group-hover:text-white transition-transform duration-200 ease-[cubic-bezier(0.16,1,0.3,1)] ${ isLocalModelPickerOpen ? 'rotate-180 text-white' : '' }`}/>
          </button>

          <div className="flex items-center gap-1 shrink-0">
          {/* Attach, paired with send: both act on the message being composed. */}
          <button
            type="button"
            title="Attach files"
            aria-label="Attach files"
            disabled={attachments.isBusy}
            onClick={(e) => {
              e.stopPropagation();
              void attachments.browse();
            }}
            className={`w-[26px] h-[26px] rounded-[8px] flex items-center justify-center transition-all duration-150 shrink-0 ${
              attachments.isBusy
                ? 'text-white/25 cursor-default'
                : 'text-white/55 hover:text-white hover:bg-white/15 active:scale-90 cursor-pointer'
            }`}
          >
            <MaterialIcon name={attachments.isBusy ? 'hourglass_top' : 'attach_file'} className="text-[16px]"/>
          </button>

          {/* Action / Send Button (Normal upward arrow or Interrupt target when streaming) */}
          <button
            type="button"
            onClick={handleButtonClick}
            title={isStreaming ? 'Stop agent' : 'Send message'}
            className="w-[28px] h-[28px] rounded-[9px] bg-[#3a3b50]/80 border border-white/20 hover:bg-[#484964]/90 active:scale-90 flex items-center justify-center transition-all duration-150 cursor-pointer group shrink-0"
          >
            {isStreaming ? (
              <MaterialIcon name="radio_button_checked" className="text-[16px] text-blue-300 group-hover:text-rose-300 transition-colors"/>
            ) : (
              <MaterialIcon name="arrow_upward" className="text-[16px] text-white/95 group-hover:text-white transition-colors"/>
            )}
          </button>
          </div>
        </div>
      </div>

      {/* Popups (Independent Model Picker & Effort Picker positioned above input with readable blur) */}
      <div
        className="absolute bottom-[calc(100%+8px)] left-0 flex items-end gap-2.5 z-50 pointer-events-none"
        onClick={(e) => e.stopPropagation()}
      >
        <AnimatePresence>
          {isLocalModelPickerOpen && (
            <motion.div
              key="agent-model-picker"
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
              <ModelPicker
                selectedModelId={localModelId}
                selectedProviderId={localProviderId}
                onSelectModel={handleSelectModel}
                onSelectProvider={handleSelectProvider}
                isFloatingPopup={true}
              />
            </motion.div>
          )}
        </AnimatePresence>

        <AnimatePresence>
          {isLocalEffortPickerOpen && (
            <motion.div
              key="agent-effort-picker"
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
              <EffortPicker
                configuringModelId={localModelId}
                isFloatingPopup={true}
                onSelectEffort={(effort) => {
                  useOrchestratorStore.getState().setModelSettings(localModelId, { effort });
                }}
                onSelectMode={(mode) => {
                  useOrchestratorStore.getState().setModelSettings(localModelId, { mode });
                }}
              />
            </motion.div>
          )}
        </AnimatePresence>
      </div>
    </div>
  );
};

export default AgentInput;
