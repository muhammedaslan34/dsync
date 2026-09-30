// The dsync phone protocol, v1 (docs/phone-protocol.md): pairing URL parsing,
// sealed requests, and the pair / sync / text / upload / download / unpair calls.

import { File, FileMode } from 'expo-file-system';
import { EnvelopeError, KEY_SIZE, fromBase64, openEnvelope, sealEnvelope, toBase64, utf8Encode } from './crypto';

export const DEFAULT_PORT = 47102;
export const MAX_TEXT_BYTES = 1 << 20;
export const DOWNLOAD_CHUNK = 1 << 20;

// ---------- types ----------

export interface FileMeta {
  name: string;
  size: number;
  status: 'active' | 'done' | 'failed' | 'canceled' | string;
  error?: string;
}

export interface Message {
  id: number;
  time: number;
  fromPhone: boolean;
  text?: string;
  file?: FileMeta;
}

/** A paired computer, as stored on the phone. */
export interface Computer {
  id: string;
  name: string;
  os: string;
  /** 32-byte key, standard base64. */
  key: string;
  phoneId: string;
  /** Addresses from the QR code, in the computer's order. */
  addresses: string[];
  /** The address that worked last. */
  address: string;
  port: number;
  pairedAt: number;
}

export interface PairingCode {
  v: number;
  pid: string;
  key: Uint8Array;
  name: string;
  cid: string;
  os: string;
  port: number;
  addrs: string[];
}

// ---------- errors ----------

export type ErrorKind =
  | 'badCode' // the scanned text isn't a dsync pairing URL
  | 'unreachable' // network error or timeout
  | 'expired' // 410, or 401 while pairing: the code is used, expired or wrong
  | 'unauthorized' // 401 after pairing: the computer doesn't know this phone any more
  | 'badRequest' // 400
  | 'notFound' // 404
  | 'tooBig' // 413
  | 'badReply' // the reply didn't open
  | 'clock' // the reply was too old / from the future
  | 'server'; // anything else

export class DsyncError extends Error {
  kind: ErrorKind;
  status?: number;
  constructor(kind: ErrorKind, message: string, status?: number) {
    super(message);
    this.kind = kind;
    this.status = status;
  }
}

export function isDsyncError(e: unknown, kind?: ErrorKind): e is DsyncError {
  return e instanceof DsyncError && (kind === undefined || e.kind === kind);
}

/** A short, human message for an error. */
export function describeError(e: unknown): string {
  if (e instanceof DsyncError) {
    switch (e.kind) {
      case 'unreachable':
        return "Can't reach the computer. Phone and computer must be on the same Wi-Fi, and dsync must be running.";
      case 'expired':
        return 'This code has expired or was already used. Show a new code on the computer.';
      case 'unauthorized':
        return 'This computer no longer knows this phone. Forget it here and connect again.';
      case 'clock':
        return "The phone's and the computer's clocks are more than 2 minutes apart. Check the time settings.";
      case 'badReply':
        return 'The computer sent a reply that could not be decrypted.';
      default:
        return e.message || 'Something went wrong';
    }
  }
  if (e instanceof Error && e.message) return e.message;
  return 'Something went wrong';
}

// ---------- pairing URL ----------

function decodeParam(s: string): string {
  try {
    return decodeURIComponent(s.replace(/\+/g, ' '));
  } catch {
    return s;
  }
}

/** Parses dsync://pair?v=1&pid=..&key=..&name=..&cid=..&os=..&port=47102&addr=a,b */
export function parsePairingUrl(raw: string): PairingCode {
  const text = raw.trim();
  const m = /^dsync:\/\/pair\/?\?(.*)$/i.exec(text);
  if (!m) throw new DsyncError('badCode', "That's not a dsync pairing code. On the computer open Settings → Connect a phone.");
  const params: Record<string, string> = {};
  for (const part of m[1].split('#')[0].split('&')) {
    if (!part) continue;
    const eq = part.indexOf('=');
    const k = decodeParam(eq < 0 ? part : part.slice(0, eq));
    params[k] = eq < 0 ? '' : decodeParam(part.slice(eq + 1));
  }
  const bad = (what: string) => new DsyncError('badCode', `The pairing code is incomplete (${what}). Show a new code on the computer.`);
  const v = Number(params.v ?? '1');
  if (v !== 1) throw new DsyncError('badCode', 'This pairing code is from a newer dsync. Update the phone app.');
  if (!params.pid) throw bad('pid');
  let key: Uint8Array;
  try {
    key = fromBase64(params.key ?? '');
  } catch {
    throw bad('key');
  }
  if (key.length !== KEY_SIZE) throw bad('key');
  if (!params.cid) throw bad('cid');
  const port = params.port ? Number(params.port) : DEFAULT_PORT;
  if (!Number.isInteger(port) || port <= 0 || port > 65535) throw bad('port');
  const addrs = (params.addr ?? '')
    .split(',')
    .map((a) => a.trim())
    .filter(Boolean);
  if (addrs.length === 0) throw bad('addr');
  return { v, pid: params.pid, key, name: params.name || 'Computer', cid: params.cid, os: params.os || '', port, addrs };
}

// ---------- requests ----------

function hostPart(addr: string): string {
  return addr.includes(':') && !addr.startsWith('[') ? `[${addr}]` : addr;
}

interface Target {
  key: Uint8Array;
  address: string;
  port: number;
}

async function post<T>(
  t: Target,
  path: string,
  header: Record<string, string>,
  body: unknown,
  timeoutMs: number,
): Promise<T> {
  const sealed = sealEnvelope(t.key, body);
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), timeoutMs);
  let res: Response;
  let text: string;
  try {
    res = await fetch(`http://${hostPart(t.address)}:${t.port}${path}`, {
      method: 'POST',
      headers: { 'Content-Type': 'text/plain', ...header },
      body: sealed,
      signal: ctrl.signal,
    });
    text = await res.text();
  } catch {
    throw new DsyncError('unreachable', "Can't reach the computer");
  } finally {
    clearTimeout(timer);
  }
  if (res.status !== 200) {
    const msg = text.trim().slice(0, 300) || `HTTP ${res.status}`;
    switch (res.status) {
      case 400:
        throw new DsyncError('badRequest', msg, 400);
      case 401:
        throw new DsyncError('unauthorized', msg, 401);
      case 404:
        throw new DsyncError('notFound', msg, 404);
      case 410:
        throw new DsyncError('expired', msg, 410);
      case 413:
        throw new DsyncError('tooBig', msg, 413);
      default:
        throw new DsyncError('server', msg, res.status);
    }
  }
  try {
    return openEnvelope<T>(t.key, text);
  } catch (e) {
    if (e instanceof EnvelopeError && e.kind === 'stale') throw new DsyncError('clock', e.message);
    throw new DsyncError('badReply', e instanceof Error ? e.message : 'Bad reply');
  }
}

// ---------- pairing ----------

interface PairResponse {
  phoneId: string;
  computerId: string;
  computerName: string;
  os: string;
}

/**
 * Pairs with the computer from a scanned code, trying each address in order
 * (short timeout each) and keeping the first that answers.
 */
export async function pair(
  code: PairingCode,
  phone: { name: string; platform: 'ios' | 'android' },
  onTry?: (address: string) => void,
): Promise<Computer> {
  let lastErr: unknown = new DsyncError('unreachable', "Can't reach the computer");
  for (const address of code.addrs) {
    onTry?.(address);
    try {
      const r = await post<PairResponse>(
        { key: code.key, address, port: code.port },
        '/phone/v1/pair',
        { 'X-Dsync-Pair': code.pid },
        { name: phone.name, platform: phone.platform },
        4000,
      );
      if (!r || typeof r.phoneId !== 'string' || !r.phoneId) throw new DsyncError('badReply', 'The computer sent an unexpected reply');
      return {
        id: r.computerId || code.cid,
        name: r.computerName || code.name,
        os: r.os || code.os,
        key: toBase64(code.key),
        phoneId: r.phoneId,
        addresses: code.addrs,
        address,
        port: code.port,
        pairedAt: Date.now(),
      };
    } catch (e) {
      // While pairing a 401 means the body didn't open: the key/code is wrong.
      if (isDsyncError(e, 'unauthorized')) throw new DsyncError('expired', e.message, 401);
      if (isDsyncError(e, 'unreachable')) {
        lastErr = e;
        continue;
      }
      throw e; // the computer answered: don't try other addresses
    }
  }
  throw lastErr;
}

// ---------- after pairing ----------

export interface SyncResponse {
  computer: { id: string; name: string; os: string };
  messages: Message[];
}

export class PhoneClient {
  readonly key: Uint8Array;
  readonly phoneId: string;
  readonly port: number;
  address: string;

  constructor(c: Computer, address?: string) {
    this.key = fromBase64(c.key);
    this.phoneId = c.phoneId;
    this.port = c.port;
    this.address = address ?? c.address;
  }

  private call<T>(path: string, body: unknown, timeoutMs = 10000): Promise<T> {
    return post<T>(this, `/phone/v1/${path}`, { 'X-Dsync-Phone': this.phoneId }, body, timeoutMs);
  }

  sync(since: number): Promise<SyncResponse> {
    return this.call<SyncResponse>('sync', { since }, 5000);
  }

  async sendText(text: string): Promise<Message> {
    if (utf8Encode(text).length > MAX_TEXT_BYTES) throw new DsyncError('tooBig', 'Text is too long (at most 1 MB)');
    const r = await this.call<{ message: Message }>('text', { text }, 20000);
    return r.message;
  }

  unpair(): Promise<unknown> {
    return this.call('unpair', {}, 5000);
  }

  /**
   * Sends a local file (file:// URI) with upload/start, upload/chunk, upload/finish.
   * onProgress gets bytes sent so far.
   */
  async upload(
    file: { uri: string; name: string },
    onProgress: (sent: number, size: number) => void,
    isCanceled: () => boolean = () => false,
  ): Promise<Message> {
    const f = new File(file.uri);
    const size = f.size;
    if (typeof size !== 'number' || size < 0) throw new Error('Could not read the file');
    const start = await this.call<{ uploadId: string; chunkSize: number }>('upload/start', { name: file.name, size });
    const chunkSize = Math.max(1, Math.floor(start.chunkSize || 512 * 1024));
    onProgress(0, size);
    let offset = 0;
    if (size > 0) {
      const h = f.open(FileMode.ReadOnly);
      try {
        while (offset < size) {
          if (isCanceled()) throw new Error('Canceled');
          h.offset = offset;
          const bytes = h.readBytes(Math.min(chunkSize, size - offset));
          if (bytes.length === 0) throw new Error('The file ended early');
          const r = await this.call<{ received: number }>(
            'upload/chunk',
            { uploadId: start.uploadId, offset, data: toBase64(bytes) },
            60000,
          );
          if (r.received !== offset + bytes.length) throw new Error('The computer lost part of the file');
          offset = r.received;
          onProgress(offset, size);
        }
      } finally {
        h.close();
      }
    }
    const done = await this.call<{ message: Message }>('upload/finish', { uploadId: start.uploadId }, 30000);
    return done.message;
  }

  /**
   * Downloads a file message from the computer into dest (overwritten), in
   * chunks of at most 1 MB. onProgress gets bytes received so far.
   */
  async download(messageId: number, dest: File, onProgress: (got: number, size: number) => void): Promise<void> {
    if (dest.exists) dest.delete();
    dest.create({ intermediates: true });
    const h = dest.open(FileMode.WriteOnly);
    let offset = 0;
    try {
      for (;;) {
        const r = await this.call<{ data: string; size: number; eof: boolean }>(
          'download',
          { messageId, offset, length: DOWNLOAD_CHUNK },
          60000,
        );
        const bytes = fromBase64(r.data || '');
        if (bytes.length > 0) h.writeBytes(bytes);
        offset += bytes.length;
        onProgress(offset, r.size);
        if (r.eof || offset >= r.size) break;
        if (bytes.length === 0) throw new Error('The download stalled');
      }
    } catch (e) {
      h.close();
      try {
        dest.delete();
      } catch {}
      throw e;
    }
    h.close();
  }
}
