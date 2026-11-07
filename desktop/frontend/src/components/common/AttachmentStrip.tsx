import { MaterialIcon } from './icons';
import React, { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { PreviewAttachment } from '../../../wailsjs/go/main/App';
import { Attachment } from './useAttachments';

export interface AttachmentStripProps {
  items: Attachment[];
  onRemove: (id: string) => void;
  /** Smaller tiles for the agent windows, which are narrower. */
  compact?: boolean;
  vertical?: boolean;
  asSquares?: boolean;
  className?: string;
}

function iconFor(mime: string | undefined): string {
  if (!mime) return 'draft';
  if (mime === 'application/x-orchestrator-chat') return 'forum';
  if (mime.startsWith('image/')) return 'image';
  if (mime === 'application/pdf') return 'picture_as_pdf';
  if (mime.startsWith('text/')) return 'description';
  return 'draft';
}

function shortName(name: string, limit: number): string {
  if (name.length <= limit) return name;
  // The extension identifies the kind of file, so the middle is cut.
  const dot = name.lastIndexOf('.');
  const ext = dot > 0 ? name.slice(dot) : '';
  const stem = dot > 0 ? name.slice(0, dot) : name;
  const keep = Math.max(4, limit - ext.length - 1);
  return `${stem.slice(0, keep)}…${ext}`;
}

function readableSize(bytes: number): string {
  if (!bytes) return '';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/**
 * Loads an image attachment's bytes as a data URL.
 *
 * Requested per tile rather than up front so a non-image attachment costs
 * nothing, and returns null on any failure so the caller falls back to an icon
 * instead of rendering a broken image.
 */
function usePreview(item: Attachment): string | null {
  const [source, setSource] = useState<string | null>(null);

  useEffect(() => {
    if (!item.mime?.startsWith('image/')) {
      setSource(null);
      return;
    }

    let active = true;
    PreviewAttachment(item.path)
      .then((data) => {
        if (active) setSource(data || null);
      })
      .catch(() => {
        if (active) setSource(null);
      });

    return () => {
      active = false;
    };
  }, [item.path, item.mime]);

  return source;
}

interface TileProps {
  item: Attachment;
  compact: boolean;
  asSquares?: boolean;
  onRemove: (id: string) => void;
  onOpen: (item: Attachment, preview: string) => void;
}

const Tile: React.FC<TileProps> = ({ item, compact, asSquares = false, onRemove, onOpen }) => {
  const preview = usePreview(item);
  const size = compact ? 52 : 64;
  const isImage = Boolean(item.mime?.startsWith('image/'));

  if (asSquares && !isImage) {
    const isChat = item.mime === 'application/x-orchestrator-chat' || item.id?.startsWith('chat:');
    return (
      <span className="relative group shrink-0 inline-block">
        <div
          title={`${item.name}${item.size ? ` · ${readableSize(item.size)}` : ''}`}
          className={`flex flex-col items-center justify-center p-1 rounded-[10px] overflow-hidden border shadow-sm select-none ${
            isChat
              ? 'bg-sky-500/15 border-sky-400/30 text-sky-200'
              : 'bg-white/[0.08] border-white/20 text-white/80'
          }`}
          style={{ width: size, height: size }}
        >
          <MaterialIcon
            name={isChat ? 'forum' : iconFor(item.mime)}
            className={`text-[20px] ${isChat ? 'text-sky-300' : 'text-white/60'} shrink-0`}
          />
          <span className="text-[9px] font-medium leading-tight truncate w-full text-center mt-1 opacity-90 px-0.5">
            {item.name}
          </span>
        </div>

        <button
          type="button"
          title={`Remove ${item.name}`}
          aria-label={`Remove ${item.name}`}
          onClick={(event) => {
            event.stopPropagation();
            onRemove(item.id);
          }}
          className="absolute top-1 right-1 w-[18px] h-[18px] rounded-full bg-black/80 hover:bg-black border border-white/30 flex items-center justify-center text-white/80 hover:text-white opacity-0 group-hover:opacity-100 focus-visible:opacity-100 transition-all duration-150 cursor-pointer active:scale-90 shadow-md z-10"
        >
          <svg
            className="w-2.5 h-2.5"
            viewBox="0 0 10 10"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.75"
            strokeLinecap="round"
          >
            <line x1="2.5" y1="2.5" x2="7.5" y2="7.5" />
            <line x1="7.5" y1="2.5" x2="2.5" y2="7.5" />
          </svg>
        </button>
      </span>
    );
  }

  // An image shows itself; anything else keeps the icon-and-name chip, since a
  // thumbnail of a PDF or a log would say nothing useful.
  if (isImage) {
    return (
      <span className="relative group shrink-0 inline-block">
        <button
          type="button"
          title={`${item.name}${item.size ? ` · ${readableSize(item.size)}` : ''}`}
          onClick={(event) => {
            event.stopPropagation();
            if (preview) onOpen(item, preview);
          }}
          disabled={!preview}
          className="block rounded-[10px] overflow-hidden border border-white/20 hover:border-white/40 transition-colors duration-150 cursor-pointer bg-black/30 shadow-sm relative"
          style={{ width: size, height: size }}
        >
          {preview ? (
            <img
              src={preview}
              alt={item.name}
              className="w-full h-full object-cover"
              draggable={false}
            />
          ) : (
            <div className="w-full h-full flex items-center justify-center animate-pulse bg-white/10">
              <MaterialIcon name="image" className="text-[18px] text-white/40"/>
            </div>
          )}
        </button>

        <button
          type="button"
          title={`Remove ${item.name}`}
          aria-label={`Remove ${item.name}`}
          onClick={(event) => {
            event.stopPropagation();
            onRemove(item.id);
          }}
          className="absolute top-1 right-1 w-[18px] h-[18px] rounded-full bg-black/80 hover:bg-black border border-white/30 flex items-center justify-center text-white/80 hover:text-white opacity-0 group-hover:opacity-100 focus-visible:opacity-100 transition-all duration-150 cursor-pointer active:scale-90 shadow-md z-10"
        >
          <svg
            className="w-2.5 h-2.5"
            viewBox="0 0 10 10"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.75"
            strokeLinecap="round"
          >
            <line x1="2.5" y1="2.5" x2="7.5" y2="7.5" />
            <line x1="7.5" y1="2.5" x2="2.5" y2="7.5" />
          </svg>
        </button>
      </span>
    );
  }

  return (
    <span
      title={`${item.name}${item.size ? ` · ${readableSize(item.size)}` : ''}`}
      className={`relative group inline-flex items-center gap-1.5 rounded-[8px] bg-white/10 border border-white/15 text-white/90 max-w-full shrink-0 ${
        compact ? 'h-[26px] pl-2 pr-1.5' : 'h-[28px] pl-2.5 pr-2'
      }`}
    >
      <MaterialIcon name={iconFor(item.mime)} className={`shrink-0 text-white/60 ${compact ? 'text-[14px]' : 'text-[15px]' }`}/>
      <span
        className={`font-(family-name:--app-font) font-medium tracking-tight truncate ${
          compact ? 'text-[11px]' : 'text-[12px]'
        }`}
      >
        {shortName(item.name, compact ? 14 : 24)}
      </span>
      <button
        type="button"
        title={`Remove ${item.name}`}
        aria-label={`Remove ${item.name}`}
        onClick={(event) => {
          event.stopPropagation();
          onRemove(item.id);
        }}
        className="w-4 h-4 rounded-full hover:bg-white/20 flex items-center justify-center text-white/50 hover:text-white transition-colors duration-150 cursor-pointer shrink-0 ml-0.5 active:scale-90"
      >
        <svg
          className="w-2 h-2"
          viewBox="0 0 10 10"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.75"
          strokeLinecap="round"
        >
          <line x1="2.5" y1="2.5" x2="7.5" y2="7.5" />
          <line x1="7.5" y1="2.5" x2="2.5" y2="7.5" />
        </svg>
      </button>
    </span>
  );
};

export const AttachmentStrip: React.FC<AttachmentStripProps> = ({
  items,
  onRemove,
  compact = false,
  vertical = false,
  asSquares = false,
  className = '',
}) => {
  const [opened, setOpened] = useState<{ item: Attachment; preview: string } | null>(null);

  // Escape closes the lightbox, matching every other dismissable surface here.
  useEffect(() => {
    if (!opened) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpened(null);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [opened]);

  if (items.length === 0) return null;

  return (
    <>
      <div className={`flex gap-1.5 ${vertical ? 'flex-col items-end' : 'flex-wrap items-center'} ${className}`}>
        {items.map((item) => (
          <Tile
            key={item.id}
            item={item}
            compact={compact}
            asSquares={asSquares}
            onRemove={onRemove}
            onOpen={(opening, preview) => setOpened({ item: opening, preview })}
          />
        ))}
      </div>

      {/* Full view, rendered through a portal to document.body. `fixed` alone is
          not enough: the glass capsule applies a transform and a backdrop
          filter, either of which makes it the containing block for fixed
          descendants, so the overlay would be trapped and scaled inside the
          input it was attached to. */}
      {opened && createPortal(
        <div
          className="fixed inset-0 z-[100] flex items-center justify-center bg-black/70 p-10 cursor-zoom-out"
          onClick={() => setOpened(null)}
        >
          <img
            src={opened.preview}
            alt={opened.item.name}
            className="max-w-full max-h-full object-contain rounded-[10px] shadow-2xl"
            onClick={(event) => event.stopPropagation()}
            draggable={false}
          />
          <button
            type="button"
            title="Close"
            aria-label="Close preview"
            onClick={() => setOpened(null)}
            className="absolute top-5 right-5 w-[32px] h-[32px] rounded-full bg-black/60 border border-white/20 flex items-center justify-center text-white/80 hover:text-white hover:bg-black/80 transition-colors duration-150 cursor-pointer"
          >
            <svg
              className="w-3.5 h-3.5"
              viewBox="0 0 10 10"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.75"
              strokeLinecap="round"
            >
              <line x1="2" y1="2" x2="8" y2="8" />
              <line x1="8" y1="2" x2="2" y2="8" />
            </svg>
          </button>
        </div>,
        document.body,
      )}
    </>
  );
};
