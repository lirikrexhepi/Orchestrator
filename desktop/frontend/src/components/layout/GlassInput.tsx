import React, { useState, useRef, useEffect } from 'react';
import { GlassSquircle } from '../../ui/glass/GlassSquircle';
import { Paperclip, X } from '../common/icons';
import { useOrchestratorStore } from '../orchestrator/useOrchestratorStore';
import { providerIcon } from '../orchestrator/providerIcons';
import { ModelPicker } from '../orchestrator/ModelPicker';
import { AttachmentsState } from '../common/useAttachments';

export interface GlassInputProps {
  onSubmit: (prompt: string, modelId?: string) => void;
  attachments?: AttachmentsState;
  isLight?: boolean;
}

export const GlassInput: React.FC<GlassInputProps> = ({
  onSubmit,
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
    <div className="relative w-full max-w-[640px] mx-auto select-none font-(family-name:--app-font)">
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

      {/* Main Glass Input Squircle */}
      <GlassSquircle
        radius={22}
        fill={isLight ? 'rgba(255, 255, 255, 0.90)' : 'rgba(14, 15, 18, 0.88)'}
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
        {/* Attached files preview chips */}
        {attachments && attachments.items.length > 0 && (
          <div className="flex flex-wrap gap-1.5 pb-0.5">
            {attachments.items.map((item) => (
              <span
                key={item.id}
                className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-[12px] ${
                  isLight
                    ? 'bg-black/[0.04] text-black/80'
                    : 'bg-white/[0.06] text-white/80'
                }`}
              >
                <span className="truncate max-w-[200px]">{item.name}</span>
                <button
                  type="button"
                  onClick={() => attachments.remove(item.id)}
                  className="hover:text-red-400 cursor-pointer"
                >
                  <X size={12} />
                </button>
              </span>
            ))}
          </div>
        )}

        {/* Text Area */}
        <textarea
          ref={textareaRef}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={handleKeyDown}
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
          {/* Left: Model Pill */}
          <button
            type="button"
            onClick={() => setIsModelPickerOpen(!isModelPickerOpen)}
            className={`flex items-center gap-2 h-[26px] px-2.5 rounded-full transition-all duration-150 cursor-pointer active:scale-95 ${
              isLight
                ? 'hover:bg-black/[0.05] text-black/80'
                : 'hover:bg-white/[0.06] text-white/85'
            }`}
            title="Switch Model"
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
          </button>

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

            {/* Circular Send Button with Curved Arrow from Screenshot */}
            <button
              type="button"
              disabled={!canSubmit}
              onClick={handleSend}
              className={`w-[28px] h-[28px] rounded-full flex items-center justify-center transition-all duration-150 cursor-pointer active:scale-90 ${
                canSubmit
                  ? 'bg-[#1a2d40] hover:bg-[#233d57] text-white shadow-[0_2px_8px_rgba(26,45,64,0.6)]'
                  : 'bg-[#1a2d40]/70 text-white/50 cursor-pointer hover:bg-[#1a2d40]'
              }`}
              title="Send task (Enter)"
              aria-label="Send"
            >
              <svg
                width="13"
                height="13"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2.5"
                strokeLinecap="round"
                strokeLinejoin="round"
                className="text-white"
              >
                <path d="M7 17v-5a4 4 0 0 1 4-4h6" />
                <polyline points="14 5 17 8 14 11" />
              </svg>
            </button>
          </div>
        </div>
      </GlassSquircle>
    </div>
  );
};
