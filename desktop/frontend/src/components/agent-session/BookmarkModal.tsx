import React, { useState, useRef, useEffect } from 'react';
import { X, Trash2, Check, Globe } from '../common/icons';
import { GlassSquircle } from '../../ui/glass/GlassSquircle';
import { useTheme } from '../../themes';
import { getDomain, getFaviconUrl } from './useWebBookmarks';

export interface BookmarkModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSave: (data: { title: string; url: string; project?: string | null }) => void;
  onDelete?: () => void;
  initialData?: {
    id?: string;
    title?: string;
    url?: string;
    project?: string | null;
  };
  currentProjectName?: string | null;
  availableProjects?: string[];
  isEditing?: boolean;
}

export const BookmarkModal: React.FC<BookmarkModalProps> = ({
  isOpen,
  onClose,
  onSave,
  onDelete,
  initialData,
  currentProjectName,
  availableProjects = [],
  isEditing = false,
}) => {
  const { currentTheme } = useTheme();
  const isLight = currentTheme.id === 'light' || currentTheme.id === 'white';

  const [url, setUrl] = useState(() => initialData?.url || 'https://');
  const [title, setTitle] = useState(() => initialData?.title || '');
  const [scopeType, setScopeType] = useState<'universal' | 'project'>(() =>
    initialData?.project ? 'project' : 'universal',
  );
  const [selectedProject, setSelectedProject] = useState<string>(
    () => initialData?.project || currentProjectName || availableProjects[0] || '',
  );

  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const timer = setTimeout(() => {
      if (inputRef.current) {
        inputRef.current.focus();
        inputRef.current.select();
      }
    }, 50);
    return () => clearTimeout(timer);
  }, []);

  if (!isOpen) return null;

  const handleSubmit = (e?: React.FormEvent) => {
    if (e) {
      e.preventDefault();
      e.stopPropagation();
    }
    const cleanUrl = url.trim();
    if (!cleanUrl || cleanUrl === 'https://' || cleanUrl === 'http://') return;

    const cleanTitle = title.trim() || getDomain(cleanUrl) || cleanUrl;
    const finalProject =
      scopeType === 'project' ? (selectedProject.trim() || currentProjectName || null) : null;

    onSave({
      title: cleanTitle,
      url: cleanUrl,
      project: finalProject,
    });
    onClose();
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    e.stopPropagation();
    if (e.key === 'Escape') {
      e.preventDefault();
      onClose();
    } else if (e.key === 'Enter') {
      e.preventDefault();
      handleSubmit();
    }
  };

  const favicon = url && url !== 'https://' && url !== 'http://' ? getFaviconUrl(url) : null;
  const projectName = currentProjectName || selectedProject || 'Project';

  return (
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-black/60 backdrop-blur-md animate-in fade-in duration-150"
      onClick={onClose}
      onKeyDown={handleKeyDown}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => e.stopPropagation()}
        className="w-full max-w-[340px]"
      >
        <GlassSquircle
          radius={22}
          fill={isLight ? 'rgba(255, 255, 255, 0.94)' : 'linear-gradient(135deg, #0B0A09 0%, #191616 100%)'}
          frost={20}
          border={{
            strokeWidth: 1,
            align: 'inside',
            gradient: isLight
              ? 'linear-gradient(135deg, rgba(0, 0, 0, 0.22) 0%, rgba(0, 0, 0, 0.05) 25%, rgba(0, 0, 0, 0) 45%, rgba(0, 0, 0, 0) 55%, rgba(0, 0, 0, 0.04) 75%, rgba(0, 0, 0, 0.16) 100%)'
              : 'linear-gradient(135deg, rgba(255, 255, 255, 0.40) 0%, rgba(255, 255, 255, 0.08) 25%, rgba(255, 255, 255, 0) 45%, rgba(255, 255, 255, 0) 55%, rgba(255, 255, 255, 0.06) 75%, rgba(255, 255, 255, 0.28) 100%)',
          }}
          style={{
            borderRadius: 22,
            padding: '16px 18px',
            boxShadow: isLight
              ? '0 16px 40px rgba(0, 0, 0, 0.12)'
              : '0 20px 48px rgba(0, 0, 0, 0.7)',
          }}
          className="flex flex-col gap-3.5 select-none font-(family-name:--app-font)"
        >
          {/* Header Bar */}
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <div className="w-5 h-5 rounded-[6px] flex items-center justify-center bg-white/10 overflow-hidden shrink-0">
                {favicon ? (
                  <img
                    src={favicon}
                    alt=""
                    className="w-3.5 h-3.5 object-contain"
                    onError={(e) => {
                      (e.currentTarget as HTMLElement).style.display = 'none';
                    }}
                  />
                ) : (
                  <Globe size={13} strokeWidth={1.8} className="text-white/60" />
                )}
              </div>
              <span className={`text-[13px] font-semibold tracking-tight ${isLight ? 'text-black/90' : 'text-white/90'}`}>
                {isEditing ? 'Edit Bookmark' : 'Add Bookmark'}
              </span>
            </div>

            <div className="flex items-center gap-1">
              {isEditing && onDelete && (
                <button
                  type="button"
                  onClick={() => {
                    onDelete();
                    onClose();
                  }}
                  title="Delete bookmark"
                  className="w-6 h-6 rounded-full flex items-center justify-center text-rose-400/70 hover:text-rose-400 hover:bg-rose-500/10 transition-colors cursor-pointer"
                >
                  <Trash2 size={13} strokeWidth={1.8} />
                </button>
              )}
              <button
                type="button"
                onClick={onClose}
                title="Close"
                className={`w-6 h-6 rounded-full flex items-center justify-center transition-colors cursor-pointer ${
                  isLight ? 'text-black/40 hover:text-black hover:bg-black/10' : 'text-white/40 hover:text-white hover:bg-white/10'
                }`}
              >
                <X size={14} strokeWidth={2} />
              </button>
            </div>
          </div>

          {/* Clean Glass Input Fields */}
          <div className="flex flex-col gap-2">
            {/* URL Input */}
            <div className="relative flex items-center">
              <input
                ref={inputRef}
                type="text"
                value={url}
                placeholder="https://..."
                onChange={(e) => {
                  setUrl(e.target.value);
                  if (!title) {
                    const d = getDomain(e.target.value);
                    if (d && d !== 'https://' && d !== 'http://') {
                      setTitle(d);
                    }
                  }
                }}
                onKeyDown={handleKeyDown}
                className={`w-full h-[34px] px-3 rounded-[11px] text-[12.5px] font-mono tracking-tight transition-all outline-none select-text ${
                  isLight
                    ? 'bg-black/[0.05] border border-black/10 focus:border-black/25 text-black placeholder:text-black/30'
                    : 'bg-white/[0.06] border border-white/[0.08] focus:border-white/20 text-white placeholder:text-white/30'
                }`}
              />
            </div>

            {/* Title / Name Input */}
            <div className="relative flex items-center">
              <input
                type="text"
                value={title}
                placeholder="Name (optional)"
                onChange={(e) => setTitle(e.target.value)}
                onKeyDown={handleKeyDown}
                className={`w-full h-[34px] px-3 rounded-[11px] text-[12.5px] font-medium tracking-tight transition-all outline-none select-text ${
                  isLight
                    ? 'bg-black/[0.05] border border-black/10 focus:border-black/25 text-black placeholder:text-black/30'
                    : 'bg-white/[0.06] border border-white/[0.08] focus:border-white/20 text-white placeholder:text-white/30'
                }`}
              />
            </div>
          </div>

          {/* Minimal 2-Way Scope Pill */}
          <div
            className={`grid grid-cols-2 p-0.5 rounded-[10px] border ${
              isLight ? 'bg-black/[0.04] border-black/[0.06]' : 'bg-white/[0.04] border-white/[0.06]'
            }`}
          >
            <button
              type="button"
              onClick={() => setScopeType('universal')}
              className={`py-1 rounded-[8px] text-[11px] font-medium transition-all text-center cursor-pointer ${
                scopeType === 'universal'
                  ? isLight
                    ? 'bg-white text-black shadow-xs'
                    : 'bg-white/[0.16] text-white shadow-xs'
                  : isLight
                  ? 'text-black/50 hover:text-black'
                  : 'text-white/45 hover:text-white'
              }`}
            >
              Universal
            </button>
            <button
              type="button"
              onClick={() => setScopeType('project')}
              className={`py-1 rounded-[8px] text-[11px] font-medium transition-all text-center truncate px-2 cursor-pointer ${
                scopeType === 'project'
                  ? isLight
                    ? 'bg-white text-black shadow-xs'
                    : 'bg-white/[0.16] text-white shadow-xs'
                  : isLight
                  ? 'text-black/50 hover:text-black'
                  : 'text-white/45 hover:text-white'
              }`}
            >
              {projectName}
            </button>
          </div>

          {/* Submit Action */}
          <button
            type="button"
            onClick={() => handleSubmit()}
            className="w-full h-[34px] rounded-[11px] bg-white text-black hover:bg-white/90 active:scale-[0.98] transition-all flex items-center justify-center gap-1.5 text-[12.5px] font-semibold shadow-sm cursor-pointer"
          >
            <Check size={14} strokeWidth={2.5} />
            <span>{isEditing ? 'Save' : 'Add'}</span>
          </button>
        </GlassSquircle>
      </div>
    </div>
  );
};
