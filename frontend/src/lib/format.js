export const osLabel = { windows: 'Windows', linux: 'Linux', darwin: 'macOS' }

export function fmtSize(n) {
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let i = -1
  do { n /= 1024; i++ } while (n >= 1024 && i < units.length - 1)
  return `${n.toFixed(n < 10 ? 1 : 0)} ${units[i]}`
}

export function fmtTime(ms) {
  return new Date(ms).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

// Short time for the device list: "14:05" today, "Mon" this week, else "12 Sep".
export function fmtShort(ms) {
  const d = new Date(ms)
  const days = daysAgo(d)
  if (days === 0) return fmtTime(ms)
  if (days < 7) return d.toLocaleDateString([], { weekday: 'short' })
  return d.toLocaleDateString([], { day: 'numeric', month: 'short' })
}

export function dayLabel(ms) {
  const d = new Date(ms)
  const days = daysAgo(d)
  if (days === 0) return 'Today'
  if (days === 1) return 'Yesterday'
  return d.toLocaleDateString([], { weekday: 'long', day: 'numeric', month: 'long' })
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
