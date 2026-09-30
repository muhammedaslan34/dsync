// Sizes and dates follow the window's language (see i18n.svelte.js).
import { t, i18n, intlLocale, fmtNumber } from './i18n.svelte.js'

export const osLabel = { windows: 'Windows', linux: 'Linux', darwin: 'macOS' }

// ltr keeps a size such as "2.4 MB" in that order inside right-to-left
// text (between left-to-right isolate and pop directional isolate).
function ltr(s) {
  return i18n.rtl ? String.fromCharCode(0x2066) + s + String.fromCharCode(0x2069) : s
}

function sizeText(n) {
  const units = t('units.size')
  let i = 0
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++ }
  return `${fmtNumber(n, i === 0 || n >= 10 ? 0 : 1)} ${units[i]}`
}

export function fmtSize(n) {
  return ltr(sizeText(n))
}

// fmtRate is a transfer speed in bytes per second.
export function fmtRate(n) {
  return ltr(t('units.rate').replace('{size}', sizeText(n)))
}

export function fmtTime(ms) {
  return new Date(ms).toLocaleTimeString(intlLocale(), { hour: '2-digit', minute: '2-digit' })
}

// Short time for the device list: "14:05" today, "Mon" this week, else "12 Sep".
export function fmtShort(ms) {
  const d = new Date(ms)
  const days = daysAgo(d)
  if (days === 0) return fmtTime(ms)
  if (days < 7) return d.toLocaleDateString(intlLocale(), { weekday: 'short' })
  return d.toLocaleDateString(intlLocale(), { day: 'numeric', month: 'short' })
}

export function dayLabel(ms) {
  const d = new Date(ms)
  const days = daysAgo(d)
  if (days === 0) return t('time.today')
  if (days === 1) return t('time.yesterday')
  return d.toLocaleDateString(intlLocale(), { weekday: 'long', day: 'numeric', month: 'long' })
}

// fmtDuration is a remaining time such as "5 sec" or "3 min".
export function fmtDuration(s) {
  let value = s, unit = 'second', digits = 0
  if (s >= 3600) { value = s / 3600; unit = 'hour'; digits = 1 } else if (s >= 60) { value = Math.round(s / 60); unit = 'minute' }
  try {
    return new Intl.NumberFormat(intlLocale(), {
      style: 'unit', unit, unitDisplay: intlLocale().startsWith('ar') ? 'long' : 'short', maximumFractionDigits: digits,
    }).format(value)
  } catch {
    return `${value} ${unit}`
  }
}

function daysAgo(d) {
  const start = (x) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime()
  return Math.round((start(new Date()) - start(d)) / 86400000)
}

export function extOf(name) {
  const i = name.lastIndexOf('.')
  return i > 0 ? name.slice(i + 1).toLowerCase() : ''
}

const kinds = {
  image: ['png', 'jpg', 'jpeg', 'gif', 'webp', 'bmp', 'svg', 'heic', 'ico', 'tiff'],
  video: ['mp4', 'mkv', 'mov', 'avi', 'webm', 'wmv', 'flv', 'm4v'],
  audio: ['mp3', 'wav', 'flac', 'ogg', 'm4a', 'aac', 'opus'],
  archive: ['zip', 'rar', '7z', 'tar', 'gz', 'xz', 'bz2', 'zst', 'iso', 'dmg'],
  code: ['js', 'ts', 'go', 'py', 'rs', 'c', 'cpp', 'h', 'java', 'json', 'yaml', 'yml', 'toml', 'sh', 'ps1', 'html', 'css', 'sql', 'svelte'],
  doc: ['pdf', 'doc', 'docx', 'txt', 'md', 'rtf', 'odt', 'xls', 'xlsx', 'csv', 'ppt', 'pptx'],
}

// fileKind picks an icon and color family for a file name.
export function fileKind(name) {
  const ext = extOf(name)
  for (const [kind, exts] of Object.entries(kinds)) {
    if (exts.includes(ext)) return kind
  }
  return 'file'
}

export function initials(name) {
  const parts = name.split(/[\s\-_.]+/).filter(Boolean)
  return (parts.slice(0, 2).map((w) => w[0]).join('') || '?').toUpperCase()
}

const hues = [250, 205, 160, 25, 335, 280, 45, 185]

export function hueFor(id) {
  let h = 7
  for (const c of id) h = (h * 31 + c.charCodeAt(0)) >>> 0
  return hues[h % hues.length]
}
