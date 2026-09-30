// Formatting helpers, matching the desktop app's (frontend/src/lib/format.js).
import { getI18n } from './i18n';

export const osLabel: Record<string, string> = { windows: 'Windows', linux: 'Linux', darwin: 'macOS' };

// ---------- numbers, sizes, dates in the chosen language ----------
//
// Intl does the work where the engine supports the language (Hermes has
// Intl.DateTimeFormat and NumberFormat on Android and iOS); otherwise the
// names and patterns in the locale file are used. Arabic keeps Latin digits,
// like sizes, addresses and codes elsewhere in the app.

type Fmt = { format(x: number | Date): string };
const cache = new Map<string, Fmt | null>();

/** A cached Intl formatter for the current locale, or null if Intl can't do this language. */
function intl(kind: 'num' | 'date', opts: Record<string, unknown>): Fmt | null {
  const i = getI18n();
  const id = `${kind}|${i.locale}|${JSON.stringify(opts)}`;
  const hit = cache.get(id);
  if (hit !== undefined) return hit;
  let f: Fmt | null = null;
  try {
    if (typeof Intl !== 'undefined') {
      const make = kind === 'num' ? Intl.NumberFormat : Intl.DateTimeFormat;
      if (typeof make === 'function') {
        const x =
          kind === 'num'
            ? new Intl.NumberFormat(i.locale, opts as Intl.NumberFormatOptions)
            : new Intl.DateTimeFormat(i.locale, opts as Intl.DateTimeFormatOptions);
        // Some engines quietly fall back to English: then use our own names.
        const got = String(x.resolvedOptions().locale || '').toLowerCase();
        if (i.lang === 'en' || got.startsWith(i.lang)) f = x;
      }
    }
  } catch {
    f = null;
  }
  cache.set(id, f);
  return f;
}

/** Latin digits (and "." as decimal point) for Arabic. */
function latinDigits(s: string): string {
  if (getI18n().lang !== 'ar') return s;
  return s
    .replace(/[\u0660-\u0669]/g, (d) => String(d.charCodeAt(0) - 0x0660))
    .replace(/[\u06f0-\u06f9]/g, (d) => String(d.charCodeAt(0) - 0x06f0))
    .replace(/\u066b/g, '.')
    .replace(/\u066c/g, ',');
}

function fmtNumber(n: number, digits: number): string {
  const f = intl('num', { minimumFractionDigits: digits, maximumFractionDigits: digits, useGrouping: false });
  if (f) {
    try {
      return latinDigits(f.format(n));
    } catch {}
  }
  return n.toFixed(digits).replace('.', getI18n().dict.dates.decimal);
}

function fmtDate(ms: number, opts: Record<string, unknown>): string | null {
  const f = intl('date', opts);
  if (!f) return null;
  try {
    return latinDigits(f.format(new Date(ms))).trim();
  } catch {
    return null;
  }
}

const UNITS = ['KB', 'MB', 'GB', 'TB'] as const;

export function fmtSize(n: number): string {
  if (!Number.isFinite(n) || n < 0) return '';
  const u = getI18n().dict.units;
  if (n < 1024) return `${n}\u00a0${u.B}`;
  let i = -1;
  do {
    n /= 1024;
    i++;
  } while (n >= 1024 && i < UNITS.length - 1);
  // A no-break space keeps "2.4 MB" on one line.
  return `${fmtNumber(n, n < 10 ? 1 : 0)}\u00a0${u[UNITS[i]]}`;
}

function pad(n: number): string {
  return n < 10 ? '0' + n : String(n);
}

/** "14:05" or "2:05 PM", as the phone is set up. */
export function fmtTime(ms: number): string {
  const { hour12 } = getI18n();
  const opts: Record<string, unknown> = { hour: 'numeric', minute: '2-digit' };
  if (hour12 !== undefined) opts.hour12 = hour12;
  const s = fmtDate(ms, opts);
  if (s) return s;
  const d = new Date(ms);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function daysAgo(d: Date): number {
  const start = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  return Math.round((start(new Date()) - start(d)) / 86400000);
}

function fill(pattern: string, vars: Record<string, string | number>): string {
  return pattern.replace(/\{(\w+)\}/g, (m, k: string) => (k in vars ? String(vars[k]) : m));
}

function capitalize(s: string): string {
  return getI18n().lang === 'ar' ? s : s.charAt(0).toLocaleUpperCase() + s.slice(1);
}

/** "14:05" today, "Mon" this week, else "12 Sep". */
export function fmtShort(ms: number): string {
  const d = new Date(ms);
  const days = daysAgo(d);
  if (days === 0) return fmtTime(ms);
  const names = getI18n().dict.dates;
  if (days < 7) return fmtDate(ms, { weekday: 'short' }) ?? names.weekdaysShort[d.getDay()];
  return fmtDate(ms, { day: 'numeric', month: 'short' }) ?? fill(names.dayMonth, { day: d.getDate(), month: names.monthsShort[d.getMonth()] });
}

/** "Today", "Yesterday", or "Monday, 12 September" (with the year if it isn't this year). */
export function dayLabel(ms: number): string {
  const d = new Date(ms);
  const days = daysAgo(d);
  const names = getI18n().dict.dates;
  if (days === 0) return names.today;
  if (days === 1) return names.yesterday;
  const otherYear = d.getFullYear() !== new Date().getFullYear();
  const opts: Record<string, unknown> = { weekday: 'long', day: 'numeric', month: 'long' };
  if (otherYear) opts.year = 'numeric';
  const s = fmtDate(ms, opts);
  if (s) return capitalize(s);
  let out = fill(names.full, { weekday: names.weekdays[d.getDay()], day: d.getDate(), month: names.months[d.getMonth()] });
  if (otherYear) out += ` ${d.getFullYear()}`;
  return capitalize(out);
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
