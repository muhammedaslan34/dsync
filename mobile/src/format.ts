// Formatting helpers, matching the desktop app's (frontend/src/lib/format.js).

export const osLabel: Record<string, string> = { windows: 'Windows', linux: 'Linux', darwin: 'macOS' };

export function fmtSize(n: number): string {
  if (!Number.isFinite(n) || n < 0) return '';
  if (n < 1024) return `${n} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let i = -1;
  do {
    n /= 1024;
    i++;
  } while (n >= 1024 && i < units.length - 1);
  return `${n.toFixed(n < 10 ? 1 : 0)} ${units[i]}`;
}

function pad(n: number): string {
  return n < 10 ? '0' + n : String(n);
}

export function fmtTime(ms: number): string {
  const d = new Date(ms);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
const WEEKDAYS_LONG = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const MONTHS_LONG = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];

function daysAgo(d: Date): number {
  const start = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  return Math.round((start(new Date()) - start(d)) / 86400000);
}

/** "14:05" today, "Mon" this week, else "12 Sep". */
export function fmtShort(ms: number): string {
  const d = new Date(ms);
  const days = daysAgo(d);
  if (days === 0) return fmtTime(ms);
  if (days < 7) return WEEKDAYS[d.getDay()];
  return `${d.getDate()} ${MONTHS[d.getMonth()]}`;
}

export function dayLabel(ms: number): string {
  const d = new Date(ms);
  const days = daysAgo(d);
  if (days === 0) return 'Today';
  if (days === 1) return 'Yesterday';
  return `${WEEKDAYS_LONG[d.getDay()]}, ${d.getDate()} ${MONTHS_LONG[d.getMonth()]}`;
}

export function sameDay(a: number, b: number): boolean {
  const x = new Date(a);
  const y = new Date(b);
  return x.getFullYear() === y.getFullYear() && x.getMonth() === y.getMonth() && x.getDate() === y.getDate();
}

export function extOf(name: string): string {
  const i = name.lastIndexOf('.');
  return i > 0 ? name.slice(i + 1).toLowerCase() : '';
}

const kinds: Record<string, string[]> = {
  image: ['png', 'jpg', 'jpeg', 'gif', 'webp', 'bmp', 'svg', 'heic', 'ico', 'tiff'],
  video: ['mp4', 'mkv', 'mov', 'avi', 'webm', 'wmv', 'flv', 'm4v'],
  audio: ['mp3', 'wav', 'flac', 'ogg', 'm4a', 'aac', 'opus'],
  archive: ['zip', 'rar', '7z', 'tar', 'gz', 'xz', 'bz2', 'zst', 'iso', 'dmg'],
  code: ['js', 'ts', 'go', 'py', 'rs', 'c', 'cpp', 'h', 'java', 'json', 'yaml', 'yml', 'toml', 'sh', 'ps1', 'html', 'css', 'sql', 'svelte'],
  doc: ['pdf', 'doc', 'docx', 'txt', 'md', 'rtf', 'odt', 'xls', 'xlsx', 'csv', 'ppt', 'pptx'],
};

export type FileKind = 'image' | 'video' | 'audio' | 'archive' | 'code' | 'doc' | 'file';

export function fileKind(name: string): FileKind {
  const ext = extOf(name);
  for (const [kind, exts] of Object.entries(kinds)) {
    if (exts.includes(ext)) return kind as FileKind;
  }
  return 'file';
}

export function initials(name: string): string {
  const parts = name.split(/[\s\-_.]+/).filter(Boolean);
  return (parts.slice(0, 2).map((w) => w[0]).join('') || '?').toUpperCase();
}

const hues = [250, 205, 160, 25, 335, 280, 45, 185];

export function hueFor(id: string): number {
  let h = 7;
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0;
  return hues[h % hues.length];
}

/** A file name safe to use on the phone's file system. */
export function safeFileName(name: string): string {
  const n = name.replace(/[\/\\:*?"<>|\u0000-\u001f]/g, '_').replace(/^\.+/, '').trim();
  return n.slice(0, 180) || 'file';
}
