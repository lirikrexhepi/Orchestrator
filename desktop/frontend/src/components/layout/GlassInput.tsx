import React, { useState, useRef, useEffect } from 'react';
import { GlassSquircle } from '../../ui/glass/GlassSquircle';
import { Paperclip, Square, ChevronDown, Plus, MaterialIcon } from '../common/icons';
import { useOrchestratorStore } from '../orchestrator/useOrchestratorStore';
import { providerIcon } from '../orchestrator/providerIcons';
import { ModelPicker } from '../orchestrator/ModelPicker';
import { AttachmentsState, filesFromTransfer } from '../common/useAttachments';
import { AttachmentStrip } from '../common/AttachmentStrip';
import { ProjectsState } from '../orchestrator/useProjects';
import { readChatDrag, isChatDrag } from '../common/chatDrag';
import { markChatContextTarget, leaveChatContextTarget } from '../common/ChatDragLayer';

export interface GlassInputProps {
  onSubmit: (prompt: string, modelId?: string) => void;
  onInterrupt?: () => void;
  onNewChat?: () => void;
  isBusy?: boolean;
  attachments?: AttachmentsState;
  isLight?: boolean;
  projects?: ProjectsState;
}

export const GlassInput: React.FC<GlassInputProps> = ({
  onSubmit,
  onInterrupt,
  onNewChat,
  isBusy = false,
  attachments,
  isLight = false,
  projects,
}) => {
  const [text, setText] = useState('');
  const [isModelPickerOpen, setIsModelPickerOpen] = useState(false);
  const [isProjectPickerOpen, setIsProjectPickerOpen] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const modelPickerRef = useRef<HTMLDivElement>(null);
  const projectPickerRef = useRef<HTMLDivElement>(null);

  // Close popups on outside click or Escape
  useEffect(() => {
    if (!isModelPickerOpen && !isProjectPickerOpen) return;
    const handleDown = (e: MouseEvent) => {
      const target = e.target as Node;
      if (modelPickerRef.current && !modelPickerRef.current.contains(target)) {
        setIsModelPickerOpen(false);
      }
      if (projectPickerRef.current && !projectPickerRef.current.contains(target)) {
        setIsProjectPickerOpen(false);
      }
    };
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setIsModelPickerOpen(false);
        setIsProjectPickerOpen(false);
      }
    };
    document.addEventListener('mousedown', handleDown);
    document.addEventListener('keydown', handleKey);
    return () => {
      document.removeEventListener('mousedown', handleDown);
      document.removeEventListener('keydown', handleKey);
    };
  }, [isModelPickerOpen, isProjectPickerOpen]);

  const store = useOrchestratorStore();
  const selectedModel = store.getSelectedModel();
  const selectedModelId = selectedModel?.id;
  const provider = selectedModel?.providerId ?? 'antigravity';
  const icon = providerIcon(provider, isLight);

  // Auto-resize textarea
  useEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 200)}px`;
  }, [text]);

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if ((e.ctrlKey || e.metaKey) && (e.key === ';' || e.key === 'p')) {
      if (projects) {
        e.preventDefault();
        setIsProjectPickerOpen((prev) => !prev);
        return;
      }
    }
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  const handlePaste = (e: React.ClipboardEvent) => {
    if (!attachments) return;
    const files = filesFromTransfer(e.clipboardData);
    if (files.length === 0) return;
    e.preventDefault();
    void attachments.accept(files);
  };

  const handleDragOver = (e: React.DragEvent) => {
    if (isChatDrag(e.dataTransfer)) {
      markChatContextTarget(e);
      return;
    }
    if (e.dataTransfer.types.includes('Files')) {
      e.preventDefault();
      e.dataTransfer.dropEffect = 'copy';
    }
  };

  const handleDragLeave = (e: React.DragEvent) => {
    leaveChatContextTarget(e);
  };

  const handleDrop = (e: React.DragEvent) => {
    leaveChatContextTarget(e);
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

  const handleSend = () => {
    const trimmed = text.trim();
    if (!trimmed && (!attachments || attachments.items.length === 0)) return;
    onSubmit(trimmed, selectedModelId);
    setText('');
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto';
    }
  };

  const canSubmit = text.trim().length > 0 || (attachments && attachments.items.length > 0);

  return (
    <div
      className="relative w-full max-w-[640px] mx-auto select-none font-(family-name:--app-font)"
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      {/* Model Picker Popup */}
      {isModelPickerOpen && (
        <div ref={modelPickerRef} className="absolute bottom-[calc(100%+10px)] left-0 z-50 animate-scale-in">
          <ModelPicker
            selectedModelId={selectedModelId}
            onSelectModel={(id) => {
              store.selectModel(id);
              setIsModelPickerOpen(false);
            }}
            isFloatingPopup
          />
        </div>
      )}

      {/* Attached files side rail (2-column compact wrap layout so images don't stack way too high) */}
      {attachments && attachments.items.length > 0 && (
        <div className="absolute right-[calc(100%+14px)] bottom-0 max-h-[190px] max-w-[128px] overflow-y-auto overflow-x-hidden flex flex-wrap-reverse justify-end content-end gap-1.5 z-40 pointer-events-auto select-none custom-scrollbar">
          <AttachmentStrip items={attachments.items} onRemove={attachments.remove} compact />
        </div>
      )}

      {/* Main Glass Input Squircle */}
      <GlassSquircle
        radius={22}
        fill={isLight ? 'rgba(255, 255, 255, 0.90)' : 'linear-gradient(135deg, #0B0A09 0%, #191616 100%)'}
        frost={16}
        border={{
          strokeWidth: 1,
          align: 'inside',
          gradient: isLight
            ? 'linear-gradient(135deg, rgba(0, 0, 0, 0.25) 0%, rgba(0, 0, 0, 0.06) 25%, rgba(0, 0, 0, 0) 45%, rgba(0, 0, 0, 0) 55%, rgba(0, 0, 0, 0.04) 75%, rgba(0, 0, 0, 0.18) 100%)'
            : 'linear-gradient(135deg, rgba(255, 255, 255, 0.45) 0%, rgba(255, 255, 255, 0.10) 25%, rgba(255, 255, 255, 0) 45%, rgba(255, 255, 255, 0) 55%, rgba(255, 255, 255, 0.08) 75%, rgba(255, 255, 255, 0.32) 100%)',
        }}
        style={{
          borderRadius: 22,
          padding: '14px 18px 12px',
          display: 'flex',
          flexDirection: 'column',
          gap: 10,
          boxShadow: isLight
            ? '0 12px 32px rgba(0, 0, 0, 0.06)'
            : '0 14px 40px rgba(0, 0, 0, 0.55)',
        }}
      >
        {/* Top: Project Selector (Visible only when projects prop is provided, e.g. Home view before sending first chat) */}
        {projects && (
          <div className="relative flex items-center -mt-1 -mb-0.5">
            <button
              type="button"
              onClick={() => setIsProjectPickerOpen((prev) => !prev)}
              title="Select Project (Ctrl+;)"
              className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-[8px] text-[12px] font-medium transition-colors cursor-pointer ${
                isLight
                  ? 'text-black/60 hover:text-black hover:bg-black/[0.05]'
                  : 'text-white/60 hover:text-white hover:bg-white/[0.06]'
              }`}
            >
              <MaterialIcon name="folder" size={14} className="opacity-70" />
              <span className="truncate max-w-[200px]">
                {projects.active?.name || 'No Project'}
              </span>
              <ChevronDown size={12} className="opacity-60" />
            </button>

            {/* Floating Glass Project Picker Menu */}
            {isProjectPickerOpen && (
              <div
                ref={projectPickerRef}
                className="absolute top-[calc(100%+6px)] left-0 z-50 min-w-[220px] max-w-[280px]"
              >
                <GlassSquircle
                  radius={14}
                  fill={isLight ? 'rgba(255, 255, 255, 0.95)' : 'linear-gradient(135deg, #11100F 0%, #1D1A1A 100%)'}
                  frost={20}
                  border={{
                    strokeWidth: 1,
                    align: 'inside',
                    gradient: isLight
                      ? 'linear-gradient(135deg, rgba(0, 0, 0, 0.2) 0%, rgba(0, 0, 0, 0.05) 50%, rgba(0, 0, 0, 0.15) 100%)'
                      : 'linear-gradient(135deg, rgba(255, 255, 255, 0.35) 0%, rgba(255, 255, 255, 0.05) 50%, rgba(255, 255, 255, 0.2) 100%)',
                  }}
                  style={{
                    borderRadius: 14,
                    boxShadow: isLight
                      ? '0 10px 30px rgba(0,0,0,0.12)'
                      : '0 12px 36px rgba(0,0,0,0.6)',
                    overflow: 'hidden',
                  }}
                  className="flex flex-col p-1.5 text-[12px]"
                >
                  <div className="px-2 py-1 text-[10.5px] uppercase tracking-wider font-semibold opacity-40">
                    Projects
                  </div>
                  <div className="max-h-[220px] overflow-y-auto custom-scrollbar flex flex-col gap-0.5">
                    {projects.projects.length === 0 ? (
                      <div className="px-2 py-1.5 text-[11.5px] opacity-50">
                        No projects yet
                      </div>
                    ) : (
                      projects.projects.map((p) => {
                        const isCurrent = projects.active?.id === p.id;
                        return (
                          <button
                            key={p.id}
                            type="button"
                            onClick={() => {
                              void projects.select(p.id);
                              setIsProjectPickerOpen(false);
                            }}
                            className={`flex items-center justify-between gap-2 px-2 py-1.5 rounded-[8px] text-left transition-colors cursor-pointer ${
                              isCurrent
                                ? isLight
                                  ? 'bg-black/[0.08] font-semibold text-black'
                                  : 'bg-white/15 font-semibold text-white'
                                : isLight
                                  ? 'hover:bg-black/[0.05] text-black/80'
                                  : 'hover:bg-white/[0.07] text-white/80'
                            }`}
                          >
                            <span className="flex items-center gap-2 truncate">
                              <MaterialIcon name="folder" size={14} className="opacity-70 shrink-0" />
                              <span className="truncate">{p.name}</span>
                            </span>
                            {isCurrent && (
                              <MaterialIcon name="check" size={14} className="text-blue-400 shrink-0" />
                            )}
                          </button>
                        );
                      })
                    )}
                  </div>
                  <div className="my-1 border-t border-white/10" />
                  <button
                    type="button"
                    onClick={async () => {
                      setIsProjectPickerOpen(false);
                      await projects.choose();
                    }}
                    className={`flex items-center gap-2 px-2 py-1.5 rounded-[8px] text-left transition-colors cursor-pointer ${
                      isLight
                        ? 'hover:bg-black/[0.05] text-black/80'
                        : 'hover:bg-white/[0.07] text-white/80'
                    }`}
                  >
                    <Plus size={14} className="opacity-70 shrink-0" />
                    <span>Open Folder…</span>
                  </button>
                </GlassSquircle>
              </div>
            )}
          </div>
        )}
        {/* Text Area */}
        <textarea
          ref={textareaRef}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={handleKeyDown}
          onPaste={handlePaste}
          placeholder="Write a task, or have an agent handle something for you"
          rows={1}
          className={`w-full bg-transparent border-0 outline-none resize-none text-[14px] leading-relaxed tracking-tight ${
            isLight
              ? 'text-black placeholder:text-black/35'
              : 'text-white/90 placeholder:text-white/35'
          }`}
          style={{ minHeight: '34px', maxHeight: '180px' }}
        />

        {/* Bottom Action Bar */}
        <div className="flex items-center justify-between pt-0.5">
          {/* Left: Model Pill & + Button */}
          <div className="flex items-center gap-1.5">
            <button
              type="button"
              onClick={() => setIsModelPickerOpen(!isModelPickerOpen)}
              className={`flex items-center gap-1.5 h-[26px] px-2.5 rounded-full transition-all duration-150 cursor-pointer active:scale-95 ${
                isLight
                  ? 'hover:bg-black/[0.05] text-black/80'
                  : 'hover:bg-white/[0.06] text-white/85'
              }`}
              title="Select AI Model"
            >
              {icon ? (
                <img
                  src={icon}
                  alt={provider}
                  className="w-3.5 h-3.5 object-contain rounded-sm"
                />
              ) : (
                <div className="w-3 h-3 rounded-full bg-blue-500/80" />
              )}
              <span className="text-[12px] font-medium tracking-tight">
                {selectedModel?.name ?? 'Gemini Carbon'}
              </span>
              <ChevronDown size={11} className="opacity-60 shrink-0 ml-0.5" />
            </button>

            {/* + Button to open new chat */}
            {onNewChat && (
              <button
                type="button"
                onClick={onNewChat}
                className={`w-[24px] h-[24px] rounded-full flex items-center justify-center transition-all duration-150 cursor-pointer active:scale-90 ${
                  isLight
                    ? 'text-black/50 hover:text-black hover:bg-black/[0.06]'
                    : 'text-white/50 hover:text-white hover:bg-white/[0.08]'
                }`}
                title="New Chat (Ctrl+N)"
                aria-label="New Chat"
              >
                <Plus size={13} strokeWidth={2.2} />
              </button>
            )}
          </div>

          {/* Right: Attachments & Send Button */}
          <div className="flex items-center gap-2">
            {/* Paperclip */}
            {attachments && (
              <button
                type="button"
                onClick={() => attachments.browse()}
                className={`w-[28px] h-[28px] rounded-full flex items-center justify-center transition-all duration-150 cursor-pointer active:scale-90 ${
                  isLight
                    ? 'text-black/40 hover:text-black hover:bg-black/[0.05]'
                    : 'text-white/35 hover:text-white hover:bg-white/[0.06]'
                }`}
                title="Attach files"
                aria-label="Attach files"
              >
                <Paperclip size={16} />
              </button>
            )}

            {/* Circular Send / Stop Button matching Screenshot (Glass #132B40 with border #A5BFD5) */}
            {isBusy ? (
              <button
                type="button"
                onClick={onInterrupt}
                className="w-[32px] h-[32px] rounded-full flex items-center justify-center transition-all duration-150 cursor-pointer active:scale-90 bg-red-500/20 hover:bg-red-500/30 text-red-400 border border-red-500/40 shadow-[0_2px_8px_rgba(239,68,68,0.3)]"
                title="Stop agent"
                aria-label="Stop"
              >
                <Square size={11} className="fill-current" />
              </button>
            ) : (
              <button
                type="button"
                disabled={!canSubmit}
                onClick={handleSend}
                style={{
                  backgroundColor: '#132B40',
                  borderColor: canSubmit ? 'rgba(165, 191, 213, 0.45)' : 'rgba(165, 191, 213, 0.2)',
                  boxShadow: '0 2px 4px rgba(0, 0, 0, 0.35), inset 0 1px 0 rgba(255, 255, 255, 0.12)',
                }}
                className={`w-[32px] h-[32px] rounded-full border flex items-center justify-center transition-all duration-150 cursor-pointer active:scale-90 ${
                  canSubmit
                    ? 'opacity-100 hover:brightness-110 active:scale-95'
                    : 'opacity-70 cursor-pointer hover:opacity-85'
                }`}
                title="Send task (Enter)"
                aria-label="Send"
              >
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke={canSubmit ? '#A5BFD5' : 'rgba(165, 191, 213, 0.5)'}
                  strokeWidth="3.2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <path d="M7 17v-2a3 3 0 0 1 3-3h7" />
                  <polyline points="13 7 18 12 13 17" />
                </svg>
              </button>
            )}
          </div>
        </div>
      </GlassSquircle>
    </div>
  );
};
