import React, { useState, useRef, useEffect } from 'react';
import { GlassSquircle } from '../../ui/glass/GlassSquircle';
import { Paperclip, Square, ChevronDown, Plus } from '../common/icons';
import { useOrchestratorStore } from '../orchestrator/useOrchestratorStore';
import { providerIcon } from '../orchestrator/providerIcons';
import { ModelPicker } from '../orchestrator/ModelPicker';
import { AttachmentsState, filesFromTransfer } from '../common/useAttachments';
import { AttachmentStrip } from '../common/AttachmentStrip';

export interface GlassInputProps {
  onSubmit: (prompt: string, modelId?: string) => void;
  onInterrupt?: () => void;
  onNewChat?: () => void;
  isBusy?: boolean;
  attachments?: AttachmentsState;
  isLight?: boolean;
}

export const GlassInput: React.FC<GlassInputProps> = ({
  onSubmit,
  onInterrupt,
  onNewChat,
  isBusy = false,
  attachments,
  isLight = false,
}) => {
  const [text, setText] = useState('');
  const [isModelPickerOpen, setIsModelPickerOpen] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const modelPickerRef = useRef<HTMLDivElement>(null);

  // Close ModelPicker on outside click or Escape
  useEffect(() => {
    if (!isModelPickerOpen) return;
    const handleDown = (e: MouseEvent) => {
      if (modelPickerRef.current && !modelPickerRef.current.contains(e.target as Node)) {
        setIsModelPickerOpen(false);
      }
    };
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setIsModelPickerOpen(false);
    };
    document.addEventListener('mousedown', handleDown);
    document.addEventListener('keydown', handleKey);
    return () => {
      document.removeEventListener('mousedown', handleDown);
      document.removeEventListener('keydown', handleKey);
    };
  }, [isModelPickerOpen]);

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

  const handleDrop = (e: React.DragEvent) => {
    if (!attachments) return;
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

      {/* Attached files side rail (positioned on the side so the input height never expands upwards or blocks chat) */}
      {attachments && attachments.items.length > 0 && (
        <div className="absolute right-[calc(100%+14px)] bottom-0 max-h-[380px] overflow-y-auto overflow-x-hidden flex flex-col items-end gap-1.5 z-40 pointer-events-auto select-none custom-scrollbar">
          <AttachmentStrip items={attachments.items} onRemove={attachments.remove} compact vertical />
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
          light: {
            angle: 303,
            intensity: isLight ? 0.35 : 0.35,
            backIntensity: 0.22,
          },
        }}
        style={{
          padding: '14px 18px 12px',
          display: 'flex',
          flexDirection: 'column',
          gap: 10,
          boxShadow: isLight
            ? '0 12px 32px rgba(0, 0, 0, 0.06)'
            : '0 14px 40px rgba(0, 0, 0, 0.55)',
        }}
      >
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
                className="w-[32px] h-[32px] rounded-[10px] flex items-center justify-center transition-all duration-150 cursor-pointer active:scale-90 bg-red-500/20 hover:bg-red-500/30 text-red-400 border border-red-500/40 shadow-[0_2px_8px_rgba(239,68,68,0.3)]"
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
                className={`w-[32px] h-[32px] rounded-[10px] border flex items-center justify-center transition-all duration-150 cursor-pointer active:scale-90 ${
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
