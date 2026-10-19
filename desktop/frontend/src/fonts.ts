export interface FontPreset {
  id: string;
  label: string;
  family: string;
  css?: string;
  note?: string;
}

const FALLBACK = '-apple-system, BlinkMacSystemFont, "Segoe UI Variable Text", "Segoe UI", Roboto, Helvetica, Arial, sans-serif';
const google = (family: string) =>
  `https://fonts.googleapis.com/css2?family=${family.replace(/ /g, '+')}:wght@300;400;500;600;700&display=swap`;
const fontshare = (slug: string) => `https://api.fontshare.com/v2/css?f[]=${slug}@300,400,500,600,700&display=swap`;

export const FONT_PRESETS: FontPreset[] = [
  { id: 'geist', label: 'Geist', family: 'Geist Variable' },
  { id: 'matter', label: 'Matter', family: 'Matter', note: 'Install or upload it' },
  { id: 'inter', label: 'Inter', family: 'Inter', css: google('Inter') },
  { id: 'system', label: 'System', family: '"Segoe UI Variable Text", -apple-system, BlinkMacSystemFont' },
  { id: 'satoshi', label: 'Satoshi', family: 'Satoshi', css: fontshare('satoshi') },
  { id: 'general-sans', label: 'General Sans', family: 'General Sans', css: fontshare('general-sans') },
  { id: 'ibm-plex', label: 'IBM Plex Sans', family: 'IBM Plex Sans', css: google('IBM Plex Sans') },
  { id: 'dm-sans', label: 'DM Sans', family: 'DM Sans', css: google('DM Sans') },
  { id: 'manrope', label: 'Manrope', family: 'Manrope', css: google('Manrope') },
  { id: 'figtree', label: 'Figtree', family: 'Figtree', css: google('Figtree') },
  { id: 'instrument', label: 'Instrument Sans', family: 'Instrument Sans', css: google('Instrument Sans') },
];

const STORAGE_KEY = 'orchestrator_ui_font';
const DB_NAME = 'orchestrator-fonts';
const STORE = 'faces';

export interface StoredFace {
  id: string;
  family: string;
  weight: string;
  style: string;
  fileName: string;
  data: ArrayBuffer;
}

function quote(family: string): string {
  return family.includes(',') || family.startsWith('"') ? family : `"${family}"`;
}

export function fontStack(family: string): string {
  return `${quote(family)}, ${FALLBACK}`;
}

const loadedCss = new Set<string>();

export function loadFontCss(url?: string) {
  if (!url || loadedCss.has(url)) return;
  loadedCss.add(url);
  const link = document.createElement('link');
  link.rel = 'stylesheet';
  link.href = url;
  document.head.appendChild(link);
}

export function presetFor(id: string): FontPreset | undefined {
  return FONT_PRESETS.find((preset) => preset.id === id);
}

export function currentFontChoice(): string {
  try {
    return localStorage.getItem(STORAGE_KEY) || 'geist';
  } catch {
    return 'geist';
  }
}

export function familyForChoice(choice: string): string {
  if (choice.startsWith('custom:')) return choice.slice('custom:'.length);
  return presetFor(choice)?.family ?? 'Geist Variable';
}

export function applyFont(choice: string) {
  const preset = presetFor(choice);
  if (preset) loadFontCss(preset.css);
  document.documentElement.style.setProperty('--app-font', fontStack(familyForChoice(choice)));
  try {
    localStorage.setItem(STORAGE_KEY, choice);
  } catch {
    return;
  }
}

function openDb(): Promise<IDBDatabase | null> {
  return new Promise((resolve) => {
    try {
      const req = indexedDB.open(DB_NAME, 1);
      req.onupgradeneeded = () => {
        if (!req.result.objectStoreNames.contains(STORE)) req.result.createObjectStore(STORE, { keyPath: 'id' });
      };
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => resolve(null);
    } catch {
      resolve(null);
    }
  });
}

export async function storedFaces(): Promise<StoredFace[]> {
  const db = await openDb();
  if (!db) return [];
  return new Promise((resolve) => {
    const req = db.transaction(STORE, 'readonly').objectStore(STORE).getAll();
    req.onsuccess = () => resolve((req.result as StoredFace[]) ?? []);
    req.onerror = () => resolve([]);
  });
}

async function putFace(face: StoredFace) {
  const db = await openDb();
  if (!db) return;
  await new Promise<void>((resolve) => {
    const tx = db.transaction(STORE, 'readwrite');
    tx.objectStore(STORE).put(face);
    tx.oncomplete = () => resolve();
    tx.onerror = () => resolve();
  });
}

export async function removeFamily(family: string) {
  const db = await openDb();
  if (!db) return;
  const faces = await storedFaces();
  await new Promise<void>((resolve) => {
    const tx = db.transaction(STORE, 'readwrite');
    for (const face of faces) if (face.family === family) tx.objectStore(STORE).delete(face.id);
    tx.oncomplete = () => resolve();
    tx.onerror = () => resolve();
  });
  for (const face of Array.from(document.fonts)) {
    if (face.family.replace(/"/g, '') === family) document.fonts.delete(face);
  }
}

const WEIGHTS: [RegExp, string][] = [
  [/thin|hairline/i, '100'],
  [/extra-?light|ultra-?light/i, '200'],
  [/light/i, '300'],
  [/semi-?bold|demi-?bold/i, '600'],
  [/extra-?bold|ultra-?bold|heavy/i, '800'],
  [/black/i, '900'],
  [/bold/i, '700'],
  [/medium/i, '500'],
];

export function describeFontFile(fileName: string): { family: string; weight: string; style: string } {
  const stem = fileName.replace(/\.(woff2?|ttf|otf)$/i, '');
  let sep = stem.search(/[-_]/);
  if (sep <= 0) sep = stem.lastIndexOf(' ');
  const familyPart = sep > 0 ? stem.slice(0, sep) : stem;
  const tail = sep > 0 ? stem.slice(sep + 1) : '';
  let weight = /variable|\bvf\b/i.test(stem) ? '100 900' : '400';
  for (const [pattern, value] of WEIGHTS) {
    if (pattern.test(tail)) {
      weight = value;
      break;
    }
  }
  const style = /italic|oblique/i.test(tail) ? 'italic' : 'normal';
  const family = familyPart.replace(/(Variable|VF)$/i, '').replace(/([a-z])([A-Z])/g, '$1 $2').trim() || stem;
  return { family, weight, style };
}

async function register(face: StoredFace) {
  const font = new FontFace(face.family, face.data, { weight: face.weight, style: face.style });
  await font.load();
  document.fonts.add(font);
}

export async function addFontFiles(files: File[]): Promise<string[]> {
  const families = new Set<string>();
  for (const file of files) {
    if (!/\.(woff2?|ttf|otf)$/i.test(file.name)) continue;
    const { family, weight, style } = describeFontFile(file.name);
    const face: StoredFace = {
      id: `${family}|${weight}|${style}`,
      family,
      weight,
      style,
      fileName: file.name,
      data: await file.arrayBuffer(),
    };
    await register(face);
    await putFace(face);
    families.add(family);
  }
  return [...families];
}

export function isFontAvailable(family: string): boolean {
  const canvas = document.createElement('canvas');
  const ctx = canvas.getContext('2d');
  if (!ctx) return false;
  const sample = 'mmmmmmmmmmlliWW@#0123456789';
  return ['monospace', 'serif'].some((base) => {
    ctx.font = `72px ${base}`;
    const baseline = ctx.measureText(sample).width;
    ctx.font = `72px ${quote(family)}, ${base}`;
    return ctx.measureText(sample).width !== baseline;
  });
}

export async function initFonts() {
  const faces = await storedFaces();
  await Promise.all(faces.map((face) => register(face).catch(() => undefined)));
  applyFont(currentFontChoice());
}
