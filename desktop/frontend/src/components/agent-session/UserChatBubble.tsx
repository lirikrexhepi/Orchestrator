import { MaterialIcon } from '../common/icons';
import React, { useState, useCallback, useEffect, useMemo } from 'react';
import { createPortal } from 'react-dom';
import { formatMessageTime } from './MessageTimestamp';
import { UserMessageFile } from './types';
import { PreviewAttachment } from '../../../wailsjs/go/main/App';

export interface UserChatBubbleProps {
  message: string;
  timestamp?: number;
  files?: UserMessageFile[];
  className?: string;
}

interface ThumbnailProps {
  file: UserMessageFile;
  onOpen: (name: string, url: string) => void;
}

const isImagePath = (path: string, mime?: string): boolean => {
  if (mime && mime.startsWith('image/')) return true;
  const ext = path.split('.').pop()?.toLowerCase() || '';
  return ['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'ico'].includes(ext);
};

const UserImageThumbnail: React.FC<ThumbnailProps> = ({ file, onOpen }) => {
  const [dataUrl, setDataUrl] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const isImg = isImagePath(file.path, file.mime);

  useEffect(() => {
    if (!isImg) {
      setLoading(false);
      setError(true);
      return;
    }

    let active = true;
    setLoading(true);
    setError(false);

    if (file.path.startsWith('data:') || file.path.startsWith('http://') || file.path.startsWith('https://')) {
      setDataUrl(file.path);
      setLoading(false);
      return;
    }

    PreviewAttachment(file.path)
      .then((data) => {
        if (!active) return;
        if (data) {
          setDataUrl(data);
        } else {
          setError(true);
        }
      })
      .catch(() => {
        if (active) setError(true);
      })
      .finally(() => {
        if (active) setLoading(false);
      });

    return () => {
      active = false;
    };
  }, [file.path, file.mime, isImg]);

  const name = file.name || file.path.split(/[\\/]/).pop() || 'Image';

  if (loading) {
    return (
      <div className="w-20 h-20 rounded-[10px] bg-white/15 animate-pulse flex items-center justify-center border border-white/20">
        <MaterialIcon name="image" className="text-[18px] text-white/50"/>
      </div>
    );
  }

  const isChat = file.mime === 'application/x-orchestrator-chat' || file.path?.startsWith('chat:');

  if (isChat) {
    return (
      <div
        title={file.path}
        className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-[10px] bg-white/20 border border-white/30 text-white text-[11.5px] max-w-[280px] shadow-sm backdrop-blur-sm"
      >
        <MaterialIcon name="forum" className="text-[15px] text-sky-200 shrink-0" />
        <span className="truncate font-medium">{name}</span>
      </div>
    );
  }

  if (error || !dataUrl) {
    return (
      <div
        title={file.path}
        className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-[8px] bg-white/20 border border-white/25 text-white text-[11px] max-w-[220px]"
      >
        <MaterialIcon name="draft" className="text-[14px] text-white/80 shrink-0"/>
        <span className="truncate">{name}</span>
      </div>
    );
  }

  return (
    <button
      type="button"
      title={`Click to view ${name}`}
      onClick={(e) => {
        e.stopPropagation();
        onOpen(name, dataUrl);
      }}
      className="group relative w-20 h-20 rounded-[10px] overflow-hidden border border-white/25 bg-black/25 hover:border-white/60 hover:scale-[1.02] active:scale-[0.98] transition-all cursor-pointer shadow-sm shrink-0"
    >
      <img
        src={dataUrl}
        alt={name}
        className="w-full h-full object-cover transition-transform duration-200 group-hover:scale-105"
        draggable={false}
      />
      <div className="absolute inset-0 bg-black/0 group-hover:bg-black/25 transition-colors flex items-center justify-center opacity-0 group-hover:opacity-100">
        <MaterialIcon name="zoom_in" className="text-white text-[18px] drop-shadow"/>
      </div>
    </button>
  );
};

/**
 * UserChatBubble Component
 * Luminous frosted glass chat bubble for user messages in the session feed.
 * Displays image thumbnails at the top with click-to-enlarge lightbox preview.
 */
const UserChatBubbleImpl: React.FC<UserChatBubbleProps> = ({
  message,
  timestamp,
  files: explicitFiles,
  className = '',
}) => {
  const [copied, setCopied] = useState(false);
  const [openedImage, setOpenedImage] = useState<{ name: string; url: string } | null>(null);

  // Close lightbox on Escape key
  useEffect(() => {
    if (!openedImage) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpenedImage(null);
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [openedImage]);

  // Extract legacy 📎 files and clean text
  const { cleanText, files } = useMemo(() => {
    const legacyFiles: UserMessageFile[] = [];
    const lines = (message || '').split(/\r?\n/);
    const textLines: string[] = [];

    for (const line of lines) {
      const match = line.match(/^\s*📎\s*(.+)$/);
      if (match) {
        const filePath = match[1].trim();
        const fileName = filePath.split(/[\\/]/).pop() || filePath;
        legacyFiles.push({ path: filePath, name: fileName });
      } else {
        textLines.push(line);
      }
    }

    const combinedFiles: UserMessageFile[] = [...(explicitFiles || [])];
    for (const leg of legacyFiles) {
      const exists = combinedFiles.some(
        (f) => f.path === leg.path || f.name === leg.name,
      );
      if (!exists) {
        combinedFiles.push(leg);
      }
    }

    while (textLines.length > 0 && textLines[textLines.length - 1].trim() === '') {
      textLines.pop();
    }

    return {
      cleanText: textLines.join('\n').trim(),
      files: combinedFiles,
    };
  }, [message, explicitFiles]);

  const handleCopy = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      const textToCopy = cleanText || files.map((f) => f.name || f.path).join('\n');
      navigator.clipboard.writeText(textToCopy);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    },
    [cleanText, files],
  );

  const formattedTime = timestamp ? formatMessageTime(timestamp) : '';

  return (
    <>
      <div className={`self-end ml-auto flex flex-col items-end gap-1 max-w-[80%] w-fit ${className}`}>
        <div className="w-fit max-w-full rounded-[18px] bg-[#007AFF] px-4 py-2.5 text-[12.5px] font-(family-name:--app-font) text-white shadow-[0_4px_14px_rgba(0,122,255,0.35)] leading-relaxed select-text font-medium break-words text-left">
          {/* Attached images/files rendered at the TOP */}
          {files.length > 0 && (
            <div className={`flex flex-wrap gap-2 ${cleanText ? 'mb-2.5' : ''}`}>
              {files.map((file, idx) => (
                <UserImageThumbnail
                  key={`${file.path || file.name}-${idx}`}
                  file={file}
                  onOpen={(name, url) => setOpenedImage({ name, url })}
                />
              ))}
            </div>
          )}

          {/* User message text rendered below images */}
          {cleanText && (
            <div className="whitespace-pre-wrap">{cleanText}</div>
          )}
        </div>

        {/* Timestamp & copy action */}
        {(formattedTime || cleanText || files.length > 0) && (
          <div className="flex items-center gap-1.5 px-1 text-[10.5px] text-white/40 font-(family-name:--app-font) select-none">
            {formattedTime && (
              <span
                title={timestamp ? new Date(timestamp).toLocaleString() : undefined}
                className="tracking-tight hover:text-white/60 transition-colors"
              >
                {formattedTime}
              </span>
            )}
            <button
              type="button"
              title={copied ? 'Copied!' : 'Copy message'}
              onClick={handleCopy}
              className="w-[16px] h-[16px] rounded flex items-center justify-center text-white/35 hover:text-white/80 hover:bg-white/10 active:scale-90 transition-all cursor-pointer"
            >
              <MaterialIcon name={copied ? 'check' : 'content_copy'} className="text-[12px]"/>
            </button>
          </div>
        )}
      </div>

      {/* Lightbox full-size image preview via portal to body */}
      {openedImage &&
        createPortal(
          <div
            className="fixed inset-0 z-[100] flex items-center justify-center bg-black/80 backdrop-blur-sm p-6 sm:p-10 cursor-zoom-out select-none animate-in fade-in duration-150"
            onClick={() => setOpenedImage(null)}
          >
            <div className="relative max-w-full max-h-full flex flex-col items-center">
              <img
                src={openedImage.url}
                alt={openedImage.name}
                className="max-w-[90vw] max-h-[85vh] object-contain rounded-xl shadow-2xl border border-white/15 cursor-default select-none"
                onClick={(e) => e.stopPropagation()}
                draggable={false}
              />
              <div className="mt-3 text-xs text-white/80 font-(family-name:--app-font) bg-black/60 backdrop-blur px-3.5 py-1.5 rounded-full border border-white/15 max-w-[80vw] truncate">
                {openedImage.name}
              </div>
              <button
                type="button"
                title="Close (Esc)"
                onClick={() => setOpenedImage(null)}
                className="absolute -top-3.5 -right-3.5 w-8 h-8 rounded-full bg-black/80 hover:bg-black border border-white/20 hover:border-white/40 text-white/80 hover:text-white flex items-center justify-center cursor-pointer shadow-lg hover:scale-105 active:scale-95 transition-all"
              >
                <MaterialIcon name="close" className="text-[18px]"/>
              </button>
            </div>
          </div>,
          document.body,
        )}
    </>
  );
};

export const UserChatBubble = React.memo(UserChatBubbleImpl);
UserChatBubble.displayName = 'UserChatBubble';

export default UserChatBubble;
