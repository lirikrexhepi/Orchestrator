import React, { useLayoutEffect, useRef, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { MaterialIcon } from '../common/icons';

interface ChatTitleProps {
  title: string;
  className: string;
  onRename?: (title: string) => void;
}

function selectAll(element: HTMLElement) {
  const range = document.createRange();
  range.selectNodeContents(element);
  const selection = window.getSelection();
  selection?.removeAllRanges();
  selection?.addRange(range);
}

export const ChatTitle: React.FC<ChatTitleProps> = ({ title, className, onRename }) => {
  const [editing, setEditing] = useState(false);
  const editRef = useRef<HTMLSpanElement>(null);
  const cancelled = useRef(false);

  useLayoutEffect(() => {
    const element = editRef.current;
    if (!editing || !element) return;
    element.textContent = title;
    element.focus();
    selectAll(element);
  }, [editing]);

  const finish = () => {
    const element = editRef.current;
    const next = (element?.textContent ?? '').replace(/\s+/g, ' ').trim();
    setEditing(false);
    window.getSelection()?.removeAllRanges();
    if (cancelled.current) {
      cancelled.current = false;
      return;
    }
    if (next && next !== title) onRename?.(next);
  };

  if (editing) {
    return (
      <span
        key="edit"
        ref={editRef}
        contentEditable="plaintext-only"
        suppressContentEditableWarning
        spellCheck={false}
        role="textbox"
        aria-label="Chat name"
        className={`${className} block min-w-0 whitespace-nowrap overflow-hidden outline-none select-text caret-current`}
        onBlur={finish}
        onKeyDown={(event) => {
          event.stopPropagation();
          if (event.key === 'Enter') {
            event.preventDefault();
            editRef.current?.blur();
          } else if (event.key === 'Escape') {
            event.preventDefault();
            cancelled.current = true;
            editRef.current?.blur();
          }
        }}
        onPaste={(event) => {
          event.preventDefault();
          const text = event.clipboardData.getData('text/plain').replace(/\s+/g, ' ');
          document.execCommand('insertText', false, text);
        }}
        onPointerDown={(event) => event.stopPropagation()}
      />
    );
  }

  return (
    <span
      key="view"
      className="group/title relative inline-flex items-center gap-1 min-w-0 max-w-full"
      onDoubleClick={
        onRename
          ? (event) => {
              event.stopPropagation();
              setEditing(true);
            }
          : undefined
      }
    >
      <span className="relative grid min-w-0">
        <AnimatePresence initial={false}>
          <motion.span
            key={title}
            initial={{ opacity: 0, filter: 'blur(3px)' }}
            animate={{ opacity: 1, filter: 'blur(0px)' }}
            exit={{ opacity: 0, filter: 'blur(3px)' }}
            transition={{ duration: 0.32, ease: [0.22, 1, 0.36, 1] }}
            className={`${className} [grid-area:1/1] truncate`}
          >
            {title}
          </motion.span>
        </AnimatePresence>
      </span>
      {onRename && (
        <button
          type="button"
          aria-label="Rename chat"
          title="Rename chat"
          onClick={(event) => {
            event.stopPropagation();
            setEditing(true);
          }}
          onDoubleClick={(event) => event.stopPropagation()}
          onPointerDown={(event) => event.stopPropagation()}
          className="shrink-0 opacity-0 group-hover/title:opacity-60 hover:!opacity-100 focus-visible:opacity-100 transition-opacity duration-150 p-0.5 rounded text-current cursor-pointer inline-flex items-center justify-center"
        >
          <MaterialIcon name="edit" size={12} />
        </button>
      )}
    </span>
  );
};
