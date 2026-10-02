/** A file name safe to use on the phone's file system. */
export function safeFileName(name: string): string {
  const n = name
    .replace(/[\/\\:*?"<>|\u0000-\u001f]/g, '_')
    .replace(/\p{Cf}/gu, '')
    .replace(/^\.+/, '')
    .trim();
  return n.slice(0, 180) || 'file';
}
