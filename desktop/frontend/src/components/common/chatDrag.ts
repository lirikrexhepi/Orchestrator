export const CHAT_DRAG_MIME = 'application/x-orchestrator-chat';
export const CHAT_REF_MIME = 'application/x-orchestrator-chat';
const PREFIX = 'chat://';

export interface ChatDragPayload {
  workspaceId: string;
  threadId: string;
  title: string;
  subtitle?: string;
  driver?: string;
  model?: string;
  projectPath?: string;
}

export interface ChatDragCard {
  payload: ChatDragPayload;
  x: number;
  y: number;
  origin: DOMRect | null;
}

type Listener = (card: ChatDragCard | null) => void;

let current: ChatDragCard | null = null;
const listeners = new Set<Listener>();

export function subscribeChatDragCard(listener: Listener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function activeChatDrag(): ChatDragCard | null {
  return current;
}

function publish(card: ChatDragCard | null) {
  current = card;
  for (const listener of listeners) listener(card);
}

let blankImage: HTMLImageElement | null = null;
function blank(): HTMLImageElement {
  if (!blankImage) {
    blankImage = new Image();
    blankImage.src = 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7';
  }
  return blankImage;
}
blank();

export function beginChatDrag(event: React.DragEvent, payload: ChatDragPayload) {
  event.dataTransfer.effectAllowed = 'copyMove';
  event.dataTransfer.setData(CHAT_DRAG_MIME, JSON.stringify(payload));
  event.dataTransfer.setData('text/plain', payload.title);
  event.dataTransfer.setDragImage(blank(), 0, 0);
  const source = event.currentTarget instanceof HTMLElement ? event.currentTarget.getBoundingClientRect() : null;
  publish({ payload, x: event.clientX, y: event.clientY, origin: source });
}

export function endChatDragCard() {
  if (current) publish(null);
}

export function isChatDrag(data: DataTransfer | null): boolean {
  return Boolean(data && Array.from(data.types).includes(CHAT_DRAG_MIME));
}

export function readChatDrag(data: DataTransfer | null): ChatDragPayload | null {
  if (!data) return null;
  try {
    const raw = data.getData(CHAT_DRAG_MIME);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Partial<ChatDragPayload>;
    if (!parsed.workspaceId) return null;
    return {
      workspaceId: parsed.workspaceId,
      threadId: parsed.threadId ?? '',
      title: parsed.title || 'Chat',
      projectPath: parsed.projectPath,
    };
  } catch {
    return null;
  }
}

export function chatRefPath(payload: ChatDragPayload): string {
  return `${PREFIX}${payload.workspaceId}/${payload.threadId}?title=${encodeURIComponent(payload.title)}`;
}

export function isChatRefPath(path: string | undefined): boolean {
  return Boolean(path && path.startsWith(PREFIX));
}

export function chatRefTitle(path: string): string {
  const query = path.split('?')[1] ?? '';
  const title = new URLSearchParams(query).get('title');
  return title || 'Chat';
}
