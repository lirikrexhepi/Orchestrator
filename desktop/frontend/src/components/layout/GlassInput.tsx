import React, { useState, useRef, useEffect } from 'react';
import { GlassSquircle } from '../../ui/glass/GlassSquircle';
import { Paperclip, ChevronRight, X } from '../common/icons';
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
    el.style.height = `${Math.min(el.scrollHeight, 240)}px`;
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
    <div className="relative w-full max-w-[680px] mx-auto select-none font-(family-name:--app-font)">
      {/* Model Picker Popup */}
      {isModelPickerOpen && (
        <div className="absolute bottom-[calc(100%+12px)] left-0 z-50 animate-scale-in">
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
        fill={isLight ? 'rgba(255, 255, 255, 0.92)' : 'rgba(16, 17, 20, 0.94)'}
        frost={16}
        border={{
          strokeWidth: 1,
          align: 'inside',
          light: {
            angle: 303,
            intensity: isLight ? 0.65 : 0.8,
            backIntensity: 0.65,
          },
        }}
        style={{
          padding: '16px 18px 14px',
          display: 'flex',
          flexDirection: 'column',
          gap: 12,
          boxShadow: isLight
            ? '0 12px 36px rgba(0, 0, 0, 0.08), 0 2px 8px rgba(0, 0, 0, 0.04)'
            : '0 16px 44px rgba(0, 0, 0, 0.55), 0 4px 12px rgba(0, 0, 0, 0.35)',
        }}
      >
        {/* Attached files preview chips */}
        {attachments && attachments.items.length > 0 && (
          <div className="flex flex-wrap gap-1.5 pb-1">
            {attachments.items.map((item) => (
              <span
                key={item.id}
                className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-[12px] border ${
                  isLight
                    ? 'bg-black/[0.04] border-black/10 text-black/80'
                    : 'bg-white/[0.06] border-white/10 text-white/80'
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
          className={`w-full bg-transparent border-0 outline-none resize-none text-[14.5px] leading-relaxed tracking-tight ${
            isLight
              ? 'text-black placeholder:text-black/40'
              : 'text-white/95 placeholder:text-white/35'
          }`}
          style={{ minHeight: '38px', maxHeight: '200px' }}
        />

        {/* Bottom Action Bar */}
        <div className="flex items-center justify-between pt-1">
          {/* Left: Model Pill */}
          <button
            type="button"
            onClick={() => setIsModelPickerOpen(!isModelPickerOpen)}
            className={`flex items-center gap-2 px-2.5 py-1.5 rounded-xl border transition-all duration-150 cursor-pointer active:scale-95 ${
              isLight
                ? 'bg-black/[0.03] border-black/[0.08] text-black/80 hover:bg-black/[0.06]'
                : 'bg-white/[0.04] border-white/[0.08] text-white/80 hover:bg-white/[0.08]'
            }`}
            title="Switch Model"
          >
            {icon ? (
              <img
                src={icon}
                alt={provider}
                className="w-4 h-4 object-contain rounded-sm"
              />
            ) : (
              <div className="w-3.5 h-3.5 rounded-full bg-blue-500/80" />
            )}
            <span className="text-[12.5px] font-medium tracking-tight">
              {selectedModel?.name ?? 'Gemini Carbon'}
            </span>
          </button>

          {/* Right: Attachments & Send Button */}
          <div className="flex items-center gap-2.5">
            {/* Paperclip */}
            {attachments && (
              <button
                type="button"
                onClick={() => attachments.browse()}
                className={`w-[32px] h-[32px] rounded-full flex items-center justify-center transition-all duration-150 cursor-pointer active:scale-90 ${
                  isLight
                    ? 'text-black/50 hover:text-black hover:bg-black/[0.05]'
                    : 'text-white/50 hover:text-white hover:bg-white/[0.07]'
                }`}
                title="Attach files"
                aria-label="Attach files"
              >
                <Paperclip size={18} />
              </button>
            )}

            {/* Circular Send Button */}
            <button
              type="button"
              disabled={!canSubmit}
              onClick={handleSend}
              className={`w-[32px] h-[32px] rounded-full flex items-center justify-center transition-all duration-150 cursor-pointer active:scale-90 ${
                canSubmit
                  ? 'bg-[#1b3a5d] hover:bg-[#234d7d] text-white shadow-[0_2px_10px_rgba(27,58,93,0.5)]'
                  : 'bg-white/[0.06] text-white/20 cursor-not-allowed'
              }`}
              title="Send task (Enter)"
              aria-label="Send"
            >
              <ChevronRight size={18} strokeWidth={2.5} />
            </button>
          </div>
        </div>
      </GlassSquircle>
    </div>
  );
};
