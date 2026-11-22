import { useCallback, useEffect, useState } from 'react';
import {
  ChooseAttachments,
  DiscardAttachment,
  SaveAttachment,
} from '../../../wailsjs/go/main/App';
import { attachments as models, domain } from '../../../wailsjs/go/models';
import { CHAT_REF_MIME, ChatDragPayload, chatRefPath } from './chatDrag';

export type Attachment = models.Attachment;

export interface AttachmentsState {
  items: Attachment[];
  error: string | null;
  isBusy: boolean;
  /** Opens the OS file picker. */
  browse: () => Promise<void>;
  /** Stages files from a paste or drop. Non-file clipboard data is ignored. */
  accept: (files: File[]) => Promise<void>;
  remove: (id: string) => void;
  /** Empties the composer AND deletes staged files. For abandoning a draft. */
  clear: () => void;
  /**
   * Empties the composer but leaves the files on disk. Used after sending: the
   * turn in flight owns those paths, and the CLI may not have read them yet.
   */
  release: () => void;
  /** The refs a turn is sent with. */
  toRefs: () => domain.FileRef[];
  addChat: (chat: ChatDragPayload) => void;
}

// Reads a File as base64. The bridge takes strings, so raw bytes would cross as
// a JSON number array several times the size.
function encode(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error ?? new Error('could not read file'));
    reader.onload = () => resolve(String(reader.result ?? ''));
    reader.readAsDataURL(file);
  });
}

const attachmentsByScope = new Map<string, Attachment[]>();

/**
 * Staged attachments for one composer.
 *
 * Every CLI adapter takes a filesystem path, so pasted bytes are written to
 * disk by the backend before they can be sent. When scopeKey is provided,
 * staged attachments are persisted per chat tab so switching tabs keeps each
 * tab's pasted images and files isolated.
 */
export function useAttachments(scopeKey?: string): AttachmentsState {
  const scope = scopeKey || '';
  const [items, setItemsState] = useState<Attachment[]>(() =>
    scope ? (attachmentsByScope.get(scope) ?? []) : [],
  );
  const [error, setError] = useState<string | null>(null);
  const [isBusy, setBusy] = useState(false);

  const setItems = useCallback(
    (next: Attachment[] | ((previous: Attachment[]) => Attachment[])) => {
      setItemsState((previous) => {
        const resolved = typeof next === 'function' ? next(previous) : next;
        if (scope) {
          if (resolved.length > 0) {
            attachmentsByScope.set(scope, resolved);
          } else {
            attachmentsByScope.delete(scope);
          }
        }
        return resolved;
      });
    },
    [scope],
  );

  useEffect(() => {
    if (!scope) return;
    setItemsState(attachmentsByScope.get(scope) ?? []);
    setError(null);
  }, [scope]);

  const accept = useCallback(async (files: File[]) => {
    if (files.length === 0) return;
    setBusy(true);
    try {
      const saved: Attachment[] = [];
      const failed: string[] = [];
      for (const file of files) {
        try {
          saved.push(await SaveAttachment(file.name, file.type, await encode(file)));
        } catch (cause) {
          // One rejected file (too large, unreadable) must not discard the rest
          // of a multi-file paste.
          failed.push(`${file.name || 'file'}: ${cause instanceof Error ? cause.message : cause}`);
        }
      }
      if (saved.length > 0) setItems((previous) => [...previous, ...saved]);
      setError(failed.length > 0 ? failed.join('\n') : null);
    } finally {
      setBusy(false);
    }
  }, []);

  const browse = useCallback(async () => {
    setBusy(true);
    try {
      // A cancelled dialog yields an empty list, which is a normal outcome.
      const chosen = await ChooseAttachments();
      if (chosen?.length) setItems((previous) => [...previous, ...chosen]);
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  }, []);

  const remove = useCallback((id: string) => {
    setItems((previous) => previous.filter((item) => item.id !== id));
    // Only files Composer staged are deleted; the backend leaves the user's own
    // files alone.
    if (!id.startsWith('chat:')) void DiscardAttachment(id);
  }, []);

  const addChat = useCallback((chat: ChatDragPayload) => {
    const id = `chat:${chat.workspaceId}/${chat.threadId}`;
    setItems((previous) =>
      previous.some((item) => item.id === id)
        ? previous
        : [...previous, models.Attachment.createFrom({ id, name: chat.title, path: chatRefPath(chat), mime: CHAT_REF_MIME, size: 0 })],
    );
  }, []);

  const clear = useCallback(() => {
    setItems((previous) => {
      for (const item of previous) if (!item.id.startsWith('chat:')) void DiscardAttachment(item.id);
      return [];
    });
    setError(null);
  }, []);

  const release = useCallback(() => {
    setItems([]);
    setError(null);
  }, []);

  const toRefs = useCallback(
    () => items.map((item) => domain.FileRef.createFrom({ path: item.path, mime: item.mime })),
    [items],
  );

  return { items, error, isBusy, browse, accept, remove, clear, release, toRefs, addChat };
}

/**
 * Pulls real files out of a paste or drop.
 *
 * A clipboard payload usually carries several representations of the same
 * thing; taking only file entries avoids staging the plain-text copy of text
 * the user meant to paste as text.
 */
export function filesFromTransfer(data: DataTransfer | null): File[] {
  if (!data) return [];

  const files: File[] = [];
  for (const item of Array.from(data.items ?? [])) {
    if (item.kind !== 'file') continue;
    const file = item.getAsFile();
    if (file) files.push(file);
  }
  if (files.length > 0) return files;

  return Array.from(data.files ?? []);
}
