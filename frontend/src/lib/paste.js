// Turning clipboard contents into things to send.

// Pasted file contents travel through the JavaScript bridge as base64, which
// needs several times their size in memory, so very big ones are refused.
// Files with a local path (copied in a file manager on Linux) are read from
// disk instead and have no limit.
export const MAX_PASTE_BYTES = 200 * 1024 * 1024

// pastedItems returns what a paste event carries that should be sent as
// files, or null for an ordinary text paste.
//   { paths: [...] }  local files copied in a file manager
//   { files: [...] }  file contents (images, files copied on Windows)
export function pastedItems(e) {
  const cd = e.clipboardData
  if (!cd) return null

  const files = [...(cd.files ?? [])]
  if (!files.length) {
    for (const item of cd.items ?? []) {
      if (item.kind === 'file') {
        const f = item.getAsFile()
        if (f) files.push(f)
      }
    }
  }
  if (files.length) return { files }

  const paths = fileURIs(cd.getData('text/uri-list'))
  if (paths.length) return { paths }
  return null
}

function fileURIs(list) {
  if (!list) return []
  return list
    .split(/\r?\n/)
    .map((l) => l.trim())
    .filter((l) => l.startsWith('file://'))
    .map((uri) => {
      let p = decodeURIComponent(uri.slice('file://'.length).replace(/^localhost/, ''))
      if (/^\/[A-Za-z]:\//.test(p)) p = p.slice(1).replaceAll('/', '\\') // file:///C:/x -> C:\x
      return p
    })
}

// pasteName gives screenshots and other nameless pastes a useful name.
export function pasteName(file, index) {
  const generic = !file.name || /^image\.\w+$/i.test(file.name) || file.name === 'blob'
  if (!generic) return file.name
  const ext = (file.type.split('/')[1] || 'png').replace('jpeg', 'jpg').replace('svg+xml', 'svg')
  const d = new Date()
  const pad = (n) => String(n).padStart(2, '0')
  const stamp = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}-${pad(d.getMinutes())}-${pad(d.getSeconds())}`
  return `Pasted image ${stamp}${index ? ` (${index + 1})` : ''}.${ext}`
}

export function toBase64(file) {
  return new Promise((resolve, reject) => {
    const r = new FileReader()
    r.onload = () => resolve(String(r.result).split(',', 2)[1] ?? '')
    r.onerror = () => reject(r.error)
    r.readAsDataURL(file)
  })
}

export function baseName(path) {
  return path.split(/[\\/]/).pop()
}
